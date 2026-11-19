import { computed, reactive, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { useAlertsStore } from "@/stores/alerts";
import { useSourcesStore } from "@/stores/sources";
import { useTeamsStore } from "@/stores/teams";
import { alertsApi } from "@/api/alerts";
import { asClickHouseConnection, getLokiSelector } from "@/api/sources";
import type { Alert, BacktestAlertRequest, CreateAlertRequest, UpdateAlertRequest, TestAlertQueryRequest, TestAlertQueryResponse } from "@/api/alerts";
import {
  getNativeQueryLanguageForSource,
  getQueryLanguageLabel,
  hasSourceCapability,
  type QueryLanguage,
  supportsAlertEditorMode,
} from "@/lib/queryMetadata";
import { aggregationOutline } from "@/utils/logchefql/aggregation";

// Extended types for local usage until API types are updated.
// The form doesn't include source_id — the caller adds it from context.
export type FormCreatePayload = Omit<CreateAlertRequest, "source_id"> & {
  destination_ids: number[];
};

export type ExtendedUpdateAlertRequest = UpdateAlertRequest;

export interface AlertFormState {
  name: string;
  description: string;
  editor_mode: "condition" | "native";
  query: string;
  condition_json: string; // LogChefQL condition string
  aggregate_function: "count" | "sum" | "avg" | "min" | "max";
  aggregate_field: string;
  /** Comma-separated result column names, or the condition's by keys. */
  group_by: string;
  lookback_seconds: number;
  threshold_operator: Alert["threshold_operator"];
  threshold_value: number;
  frequency_seconds: number;
  severity: Alert["severity"];
  no_data_policy: Alert["no_data_policy"];
  notify_on_resolve: boolean;
  is_active: boolean;
  labels: Array<{ id: number; key: string; value: string }>;
  annotations: Array<{ id: number; key: string; value: string }>;
  destination_ids: number[];
}

type AggregateFunction = AlertFormState["aggregate_function"];

/** Writes seconds as a LogQL duration, such as 5m or 1h30m. */
export function logqlDuration(seconds: number): string {
  const parts: string[] = [];
  let rest = seconds;
  for (const [unit, size] of [["h", 3600], ["m", 60], ["s", 1]] as const) {
    if (rest >= size) {
      parts.push(`${Math.floor(rest / size)}${unit}`);
      rest %= size;
    }
  }
  return parts.join("") || "0s";
}

/**
 * The LogchefQL rule of the condition editor: the filter, then one aggregate
 * named value, by the Group by keys. The server compiles it for the source at
 * each evaluation.
 */
export function conditionQuery(condition: string, aggregate: AggregateFunction, field: string, groupBy: string[]): string {
  const value = aggregate === "count" ? "count()" : `${aggregate}(${field})`;
  const by = groupBy.length > 0 ? ` by ${groupBy.join(", ")}` : "";
  return `${condition} | ${value} as value${by}`;
}

/**
 * Whether a LogQL query holds a range equal to the lookback, as [300s] or
 * [5m]. A LogQL query carries its own range, so the lookback does not change
 * what it counts.
 */
export function logqlHasLookbackRange(query: string, lookbackSeconds: number): boolean {
  return query.includes(`[${lookbackSeconds}s]`) || query.includes(`[${logqlDuration(lookbackSeconds)}]`);
}

/** The aggregate a saved condition alert used, read back from its query. */
function conditionAggregate(alert: Alert, sourceType: string): { aggregate: AggregateFunction; field: string } | null {
  const query = alert.query || "";
  if (alert.query_language === "logchefql") {
    const item = aggregationOutline(query)?.items[0];
    if (!item) return null;
    return { aggregate: item.func as AggregateFunction, field: item.field?.name ?? "" };
  }
  // A condition saved before the server compiled conditions holds the native
  // query that the form generated.
  if (sourceType === "loki") {
    const aggregate = query.match(/(count|sum|avg|min|max)_over_time\(/)?.[1] as AggregateFunction | undefined;
    if (!aggregate) return null;
    return { aggregate, field: aggregate === "count" ? "" : query.match(/\| unwrap ([A-Za-z_]\w*)/)?.[1] ?? "" };
  }
  const match = query.match(/(count|sum|avg|min|max)\(\s*[`"]?([^`")]*)[`"]?\s*\)/);
  if (!match) return null;
  const aggregate = match[1] as AggregateFunction;
  return { aggregate, field: aggregate === "count" ? "" : match[2].trim() };
}

/** Splits the Group by field into column names. */
export function parseGroupBy(text: string): string[] {
  return text.split(",").map((name) => name.trim()).filter(Boolean);
}

export type QueryTemplate = {
  name: string;
  description: string;
  editorMode: "native";
  query: string;
};

export interface ConditionTemplate {
  name: string;
  description: string;
  condition: string;
  aggregate: "count";
}

export interface UseAlertFormProps {
  open: boolean;
  mode: "create" | "edit";
  teamId: number | null;
  sourceId: number | null;
  alert: Alert | null;
}

export interface UseAlertFormEmit {
  (e: "create", payload: FormCreatePayload): void;
  (e: "update", payload: ExtendedUpdateAlertRequest): void;
}

/**
 * Owns all state, derived values, and business logic for the alert
 * create/edit form. Kept separate from AlertForm.vue so the component only
 * has to worry about layout (dialog vs. inline) and composition of the
 * section components.
 */
export function useAlertForm(props: UseAlertFormProps, emit: UseAlertFormEmit) {
  const { t } = useI18n();
  const alertsStore = useAlertsStore();
  const sourcesStore = useSourcesStore();
  const teamsStore = useTeamsStore();

  const currentSource = computed(() => {
    if (props.sourceId != null) {
      const teamSource = sourcesStore.teamSources.find((source) => source.id === props.sourceId);
      if (teamSource) {
        return teamSource;
      }
    }
    return sourcesStore.currentSourceDetails;
  });
  const sourceType = computed(() => currentSource.value?.source_type || "clickhouse");
  const supportsConditionEditor = computed(() => supportsAlertEditorMode(currentSource.value, "condition"));
  // Loki compiles a condition with no by keys.
  const conditionGroupBy = computed(() => hasSourceCapability(currentSource.value, "logchefql_aggregation"));

  // Column suggestions for the aggregate field. ClickHouse schemas narrow to
  // numeric types; VictoriaLogs and Loki values are strings that may hold
  // numbers, so all names are offered.
  const aggregateFieldSuggestions = computed(() => {
    const columns = currentSource.value?.columns || [];
    if (sourceType.value === "victorialogs" || sourceType.value === "loki") {
      return columns.map((c) => c.name);
    }
    return columns
      .filter((c) => /Int|Float|Decimal/i.test(c.type || ""))
      .map((c) => c.name);
  });

  const nativeQueryLanguage = computed(() => getNativeQueryLanguageForSource(currentSource.value));
  const nativeEditorLabel = computed(() => getQueryLanguageLabel(nativeQueryLanguage.value));
  const nativeQueryLabel = computed(() => t('alerts.nativeQueryLabel', { language: nativeEditorLabel.value }));

  // The source's table, for the native SQL templates.
  const currentTableName = computed(() => {
    const chConn = asClickHouseConnection(currentSource.value?.connection);
    const database = chConn?.database;
    const tableName = chConn?.table_name;
    if (database && tableName) {
      return `${database}.${tableName}`;
    }
    return "logs";
  });

  const form = reactive<AlertFormState>({
    name: "",
    description: "",
    editor_mode: "condition",
    query: "",
    condition_json: "",
    aggregate_function: "count",
    aggregate_field: "",
    group_by: "",
    lookback_seconds: 300,
    threshold_operator: "gt",
    threshold_value: 1,
    frequency_seconds: 300,
    severity: "warning",
    no_data_policy: "zero",
    notify_on_resolve: true,
    is_active: true,
    labels: [],
    annotations: [],
    destination_ids: [],
  });

  // The server expands this to the lookback window that ends at the
  // evaluation instant, so a query can also run for a past instant.
  const timeFilter = "{{__time_filter}}";

  const lokiSelector = computed(() => getLokiSelector(currentSource.value) ?? `{app="api"}`);

  const nativeQueryPlaceholder = computed(() => {
    if (sourceType.value === "victorialogs") {
      return `level:="error" | stats count() as value`;
    }
    if (sourceType.value === "loki") {
      return `sum(count_over_time(${lokiSelector.value} |= "error" [${logqlDuration(form.lookback_seconds)}]))`;
    }

    return `SELECT count(*) as value FROM ${currentTableName.value} WHERE severity = 'ERROR' AND ${timeFilter}`;
  });

  const nativeQueryHelpText = computed(() => {
    if (sourceType.value === "victorialogs") {
      return t("alerts.logsQLTemplateHint");
    }
    if (sourceType.value === "loki") {
      return t("alerts.logQLTemplateHint");
    }

    return t("alerts.sqlTemplateHint");
  });

  // Team members, offered as recipients of a new email destination
  const teamMembers = computed(() => {
    if (!props.teamId) return [];
    return teamsStore.getTeamMembersByTeamId(props.teamId);
  });

  // Fetch team members on mount or when teamId changes
  watch(() => props.teamId, (id) => {
    if (id) {
      teamsStore.listTeamMembers(id);
    }
  }, { immediate: true });

  const labelCounter = ref(0);
  const annotationCounter = ref(0);

  const testQueryResult = ref<TestAlertQueryResponse | null>(null);
  const isTestingQuery = ref(false);
  const testQueryError = ref<string | null>(null);

  function getClickHouseQueryTemplates(): QueryTemplate[] {
    const tableName = currentTableName.value;

    return [
      {
        name: t("alerts.templates.highErrorCount"),
        description: t("alerts.templates.highErrorCountHelp"),
        editorMode: "native" as const,
        query: `SELECT count(*) as value
FROM ${tableName}
WHERE severity = 'ERROR'
  AND ${timeFilter}`,
      },
      {
        name: t("alerts.templates.criticalLogs"),
        description: t("alerts.templates.criticalLogsHelp"),
        editorMode: "native" as const,
        query: `SELECT count(*) as value
FROM ${tableName}
WHERE severity = 'CRITICAL'
  AND ${timeFilter}`,
      },
      {
        name: t("alerts.templates.highResponseTime"),
        description: t("alerts.templates.highResponseTimeHelp"),
        editorMode: "native" as const,
        query: `SELECT avg(response_time) as value
FROM ${tableName}
WHERE ${timeFilter}`,
      },
      {
        name: t("alerts.templates.failedRequests"),
        description: t("alerts.templates.failedRequestsHelp"),
        editorMode: "native" as const,
        query: `SELECT count(*) as value
FROM ${tableName}
WHERE status_code >= 500
  AND ${timeFilter}`,
      },
      {
        name: t("alerts.templates.lowSuccessRate"),
        description: t("alerts.templates.lowSuccessRateHelp"),
        editorMode: "native" as const,
        query: `SELECT (countIf(status_code < 400) * 100.0 / count(*)) as value
FROM ${tableName}
WHERE ${timeFilter}`,
      },
    ];
  }

  function getVictoriaLogsQueryTemplates(): QueryTemplate[] {
    return [
      {
        name: t("alerts.templates.highErrorCount"),
        description: t("alerts.templates.highErrorCountHelp"),
        editorMode: "native",
        query: `level:="ERROR" | stats count() as value`,
      },
      {
        name: t("alerts.templates.criticalLogs"),
        description: t("alerts.templates.criticalLogsHelp"),
        editorMode: "native",
        query: `level:="CRITICAL" | stats count() as value`,
      },
      {
        name: t("alerts.templates.failedRequests"),
        description: t("alerts.templates.failedRequestsHelp"),
        editorMode: "native",
        query: `status_code:>=500 | stats count() as value`,
      },
      {
        name: t("alerts.templates.highResponseTime"),
        description: t("alerts.templates.highResponseTimeHelp"),
        editorMode: "native",
        query: `response_time:* | stats avg(response_time) as value`,
      },
    ];
  }

  function getLokiQueryTemplates(): QueryTemplate[] {
    const range = `[${logqlDuration(form.lookback_seconds)}]`;
    return [
      {
        name: t("alerts.templates.highErrorCount"),
        description: t("alerts.templates.highErrorCountHelp"),
        editorMode: "native",
        query: `sum(count_over_time(${lokiSelector.value} |~ "(?i)error" ${range}))`,
      },
      {
        name: t("alerts.templates.criticalLogs"),
        description: t("alerts.templates.criticalLogsHelp"),
        editorMode: "native",
        query: `sum(count_over_time(${lokiSelector.value} |~ "(?i)critical" ${range}))`,
      },
    ];
  }

  const queryTemplates = computed(() => {
    if (sourceType.value === "victorialogs") return getVictoriaLogsQueryTemplates();
    if (sourceType.value === "loki") return getLokiQueryTemplates();
    return getClickHouseQueryTemplates();
  });

  // A native LogQL query keeps its own range; say so when it differs from the
  // lookback. The warning never blocks a save.
  const lookbackWarning = computed(() => {
    if (sourceType.value !== "loki" || form.editor_mode !== "native" || !form.query.trim() || form.lookback_seconds <= 0) {
      return null;
    }
    if (logqlHasLookbackRange(form.query, form.lookback_seconds)) return null;
    return t("alerts.logQLLookbackMismatch", { range: `[${logqlDuration(form.lookback_seconds)}]` });
  });

  // LogChefQL condition templates
  const conditionTemplates = computed<ConditionTemplate[]>(() => [
    {
      name: t("alerts.templates.errorLogs"),
      description: t("alerts.templates.errorLogsHelp"),
      condition: `severity = "ERROR"`,
      aggregate: "count",
    },
    {
      name: t("alerts.templates.criticalLogs"),
      description: t("alerts.templates.criticalConditionHelp"),
      condition: `severity = "CRITICAL"`,
      aggregate: "count",
    },
    {
      name: t("alerts.templates.serverErrors"),
      description: t("alerts.templates.serverErrorsHelp"),
      condition: `status_code >= 500`,
      aggregate: "count",
    },
    {
      name: t("alerts.templates.slowRequests"),
      description: t("alerts.templates.slowRequestsHelp"),
      condition: `response_time > 1000`,
      aggregate: "count",
    },
    {
      name: t("alerts.templates.errorMessages"),
      description: t("alerts.templates.errorMessagesHelp"),
      condition: `message ~ "error"`,
      aggregate: "count",
    },
  ]);

  function applyConditionTemplate(template: ConditionTemplate) {
    form.condition_json = template.condition;
    form.aggregate_function = template.aggregate;
    testQueryResult.value = null;
    testQueryError.value = null;
  }

  const isSubmitting = computed(() => {
    if (props.mode === "create") {
      return alertsStore.isLoadingOperation("createAlert");
    }
    return props.alert ? alertsStore.isLoadingOperation(`updateAlert-${props.alert.id}`) : false;
  });

  const isDisabled = computed(() => !props.teamId || !props.sourceId || isSubmitting.value);

  const isValid = computed(() => {
    const hasName = !!form.name.trim();
    const hasThreshold = form.threshold_value !== undefined;
    const hasFrequency = form.frequency_seconds > 0;
    const hasLookback = form.lookback_seconds > 0;

    return hasName && !!ruleQuery.value && hasThreshold && hasFrequency && hasLookback;
  });

  function addLabel() {
    form.labels.push({ id: labelCounter.value++, key: "", value: "" });
  }

  function removeLabel(id: number) {
    const index = form.labels.findIndex((label) => label.id === id);
    if (index >= 0) {
      form.labels.splice(index, 1);
    }
  }

  function addAnnotation() {
    form.annotations.push({ id: annotationCounter.value++, key: "", value: "" });
  }

  function removeAnnotation(id: number) {
    const index = form.annotations.findIndex((annotation) => annotation.id === id);
    if (index >= 0) {
      form.annotations.splice(index, 1);
    }
  }

  function resetForm(alert: Alert | null) {
    testQueryResult.value = null;
    testQueryError.value = null;

    if (!alert) {
      form.name = "";
      form.description = "";
      form.editor_mode = supportsConditionEditor.value ? "condition" : "native";
      form.query = "";
      form.condition_json = "";
      form.aggregate_function = "count";
      form.aggregate_field = "";
      form.group_by = "";
      form.lookback_seconds = 300;
      form.threshold_operator = "gt";
      form.threshold_value = 1;
      form.frequency_seconds = 300;
      form.severity = "warning";
      form.no_data_policy = "zero";
      form.notify_on_resolve = true;
      form.is_active = true;
      form.labels = [];
      form.annotations = [];
      form.destination_ids = [];
      labelCounter.value = 0;
      annotationCounter.value = 0;
      return;
    }

    form.name = alert.name;
    form.description = alert.description ?? "";
    form.editor_mode = supportsConditionEditor.value ? alert.editor_mode : "native";
    form.query = form.editor_mode === "condition" ? "" : alert.query;
    form.condition_json = alert.condition_json ?? "";
    const saved = form.editor_mode === "condition" ? conditionAggregate(alert, sourceType.value) : null;
    form.aggregate_function = saved?.aggregate ?? "count";
    form.aggregate_field = saved?.field ?? "";
    form.group_by = (alert.group_by ?? []).join(", ");
    form.lookback_seconds = alert.lookback_seconds;
    form.threshold_operator = alert.threshold_operator;
    form.threshold_value = alert.threshold_value;
    form.frequency_seconds = alert.frequency_seconds;
    form.severity = alert.severity;
    form.no_data_policy = alert.no_data_policy;
    form.notify_on_resolve = alert.notify_on_resolve;
    form.is_active = alert.is_active;
    labelCounter.value = 0;
    annotationCounter.value = 0;
    form.labels = alert.labels
      ? Object.entries(alert.labels).map(([key, value]) => ({ id: labelCounter.value++, key, value }))
      : [];
    form.annotations = alert.annotations
      ? Object.entries(alert.annotations).map(([key, value]) => ({ id: annotationCounter.value++, key, value }))
      : [];

    form.destination_ids = alert.destination_ids ? [...alert.destination_ids] : [];
  }

  const groupBy = computed(() =>
    form.editor_mode === "native" || conditionGroupBy.value ? parseGroupBy(form.group_by) : []
  );
  const queryLanguage = computed<QueryLanguage>(() => (form.editor_mode === "condition" ? "logchefql" : nativeQueryLanguage.value));

  // The query the rule stores: a condition's LogchefQL, or the native query.
  // Empty until the form has one.
  const ruleQuery = computed(() => {
    if (form.editor_mode === "native") {
      return form.query.trim();
    }
    const condition = form.condition_json.trim();
    const field = form.aggregate_field.trim();
    if (!condition || (form.aggregate_function !== "count" && !field)) {
      return "";
    }
    return conditionQuery(condition, form.aggregate_function, field, groupBy.value);
  });

  // The rule as the test and backtest routes run it; null until it has a query.
  const testRequest = computed<TestAlertQueryRequest | null>(() => {
    if (!props.teamId || !props.sourceId || !ruleQuery.value) {
      return null;
    }
    return {
      source_id: props.sourceId,
      query_language: queryLanguage.value,
      editor_mode: form.editor_mode,
      query: ruleQuery.value,
      condition_json: form.editor_mode === "condition" ? form.condition_json.trim() : undefined,
      lookback_seconds: Number(form.lookback_seconds),
      threshold_operator: form.threshold_operator,
      threshold_value: Number(form.threshold_value),
      group_by: groupBy.value,
      no_data_policy: form.no_data_policy,
    };
  });

  const backtestRequest = computed<Omit<BacktestAlertRequest, "range_seconds"> | null>(() =>
    testRequest.value
      ? { ...testRequest.value, frequency_seconds: Number(form.frequency_seconds), notify_on_resolve: form.notify_on_resolve }
      : null,
  );

  async function handleTestQuery() {
    const request = testRequest.value;
    if (!request) {
      return;
    }

    isTestingQuery.value = true;
    testQueryError.value = null;
    testQueryResult.value = null;

    try {
      const result = await alertsApi.testQuery(request);
      testQueryResult.value = result.data;
    } catch (error: any) {
      testQueryError.value = error?.response?.data?.message || error.message || t("alerts.testFailed");
    } finally {
      isTestingQuery.value = false;
    }
  }

  function applyTemplate(template: QueryTemplate) {
    form.editor_mode = template.editorMode;
    form.query = template.query;
    testQueryResult.value = null;
    testQueryError.value = null;
  }

  watch(
    () => props.alert,
    (alert) => {
      if (props.mode === "edit" || (props.mode === "create" && alert)) {
        resetForm(alert || null);
      }
    },
    { immediate: true }
  );

  watch(
    () => props.open,
    (open) => {
      if (open) {
        if (props.mode === "create" && !props.alert) {
          resetForm(null);
        }
      }
    },
    { immediate: false }
  );

  watch(supportsConditionEditor, () => {
    if (!supportsConditionEditor.value && form.editor_mode === "condition") {
      form.editor_mode = "native";
    }
  });

  watch(
    () => [ruleQuery.value, form.threshold_operator, form.threshold_value, form.group_by, form.no_data_policy],
    () => {
      testQueryResult.value = null;
      testQueryError.value = null;
    }
  );

  function handleSubmit() {
    if (!isValid.value || isSubmitting.value) {
      return;
    }

    const labelsRecord = form.labels.reduce<Record<string, string>>((acc, { key, value }) => {
      const trimmedKey = key.trim();
      if (trimmedKey) acc[trimmedKey] = value;
      return acc;
    }, {});

    const annotationsRecord = form.annotations.reduce<Record<string, string>>((acc, { key, value }) => {
      const trimmedKey = key.trim();
      if (trimmedKey) acc[trimmedKey] = value;
      return acc;
    }, {});

    const basePayload = {
      name: form.name.trim(),
      description: form.description.trim(),
      query_language: queryLanguage.value,
      editor_mode: form.editor_mode,
      query: ruleQuery.value,
      condition_json: form.editor_mode === "condition" ? form.condition_json.trim() : undefined,
      lookback_seconds: Number(form.lookback_seconds),
      threshold_operator: form.threshold_operator,
      threshold_value: Number(form.threshold_value),
      frequency_seconds: Number(form.frequency_seconds),
      severity: form.severity,
      is_active: form.is_active,
      labels: labelsRecord,
      annotations: annotationsRecord,
      destination_ids: form.destination_ids,
      group_by: groupBy.value,
      no_data_policy: form.no_data_policy,
      notify_on_resolve: form.notify_on_resolve,
    };

    if (props.mode === "create") {
      emit("create", basePayload);
    } else {
      emit("update", basePayload);
    }
  }

  return {
    form,
    backtestRequest,
    supportsConditionEditor,
    aggregateFieldSuggestions,
    nativeEditorLabel,
    nativeQueryLabel,
    nativeQueryPlaceholder,
    nativeQueryHelpText,
    conditionGroupBy,
    conditionTemplates,
    queryTemplates,
    lookbackWarning,
    applyConditionTemplate,
    applyTemplate,
    teamMembers,
    testQueryResult,
    isTestingQuery,
    testQueryError,
    handleTestQuery,
    isSubmitting,
    isDisabled,
    isValid,
    addLabel,
    removeLabel,
    addAnnotation,
    removeAnnotation,
    handleSubmit,
  };
}
