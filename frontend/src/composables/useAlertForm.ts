import { computed, onScopeDispose, reactive, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { useAlertsStore } from "@/stores/alerts";
import { useSourcesStore } from "@/stores/sources";
import { useTeamsStore } from "@/stores/teams";
import { alertsApi } from "@/api/alerts";
import { asClickHouseConnection } from "@/api/sources";
import { logchefqlApi } from "@/api/logchefql";
import { getErrorMessage } from "@/api/types";
import type { Alert, CreateAlertRequest, UpdateAlertRequest, TestAlertQueryResponse } from "@/api/alerts";
import {
  getNativeQueryLanguageForSource,
  getQueryLanguageLabel,
  resolveAlertMetadata,
  supportsAlertEditorMode,
} from "@/lib/queryMetadata";

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
  /** Comma-separated result column names; native mode only. */
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

  // Column suggestions for the aggregate field. ClickHouse schemas narrow to
  // numeric types; VictoriaLogs fields are untyped so all names are offered.
  const aggregateFieldSuggestions = computed(() => {
    const columns = currentSource.value?.columns || [];
    if (sourceType.value === "victorialogs") {
      return columns.map((c) => c.name);
    }
    return columns
      .filter((c) => /Int|Float|Decimal/i.test(c.type || ""))
      .map((c) => c.name);
  });

  function logsqlFieldRef(field: string): string {
    return /^[a-zA-Z_][a-zA-Z0-9_.]*$/.test(field) ? field : JSON.stringify(field);
  }
  const nativeQueryLanguage = computed(() => getNativeQueryLanguageForSource(currentSource.value));
  const nativeEditorLabel = computed(() => getQueryLanguageLabel(nativeQueryLanguage.value));
  const nativeQueryLabel = computed(() => t('alerts.nativeQueryLabel', { language: nativeEditorLabel.value }));
  const generatedQueryLanguageLabel = computed(() => {
    if (alertMetadata.value.queryLanguage === "logsql") {
      return t('alerts.generatedQuery', { language: 'LogsQL' });
    }
    return t('alerts.generatedQuery', { language: 'SQL' });
  });

  // Get current source table name for SQL generation
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

  // UI state for adding webhook

  // LogChefQL validation state
  const conditionError = ref<string | null>(null);

  const alertMetadata = computed(() =>
    resolveAlertMetadata({
      editor_mode: form.editor_mode,
      source_type: sourceType.value,
      query_languages: currentSource.value?.query_languages,
      alert_editor_modes: currentSource.value?.alert_editor_modes,
    })
  );

  // The server expands this to the lookback window that ends at the
  // evaluation instant, so a query can also run for a past instant.
  const timeFilter = "{{__time_filter}}";

  const nativeQueryPlaceholder = computed(() => {
    if (sourceType.value === "victorialogs") {
      return `level:="error" | stats count() as value`;
    }

    return `SELECT count(*) as value FROM ${currentTableName.value} WHERE severity = 'ERROR' AND ${timeFilter}`;
  });

  const nativeQueryHelpText = computed(() => {
    if (sourceType.value === "victorialogs") {
      return t("alerts.logsQLTemplateHint");
    }

    return t("alerts.sqlTemplateHint");
  });

  // Generated executable query from LogchefQL condition
  const generatedQuery = ref("");
  const isTranslating = ref(false);

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

  let translationSequence = 0;

  // Translate LogChefQL condition to SQL using backend API
  async function translateCondition() {
    const requestId = ++translationSequence;
    if (!supportsConditionEditor.value) {
      conditionError.value = null;
      generatedQuery.value = "";
      return;
    }
    if (form.editor_mode !== "condition" || !form.condition_json.trim()) {
      conditionError.value = null;
      generatedQuery.value = "";
      return;
    }

    const teamId = props.teamId;

    if (!teamId || !props.sourceId) {
      conditionError.value = t("alerts.missingContext");
      return;
    }

    isTranslating.value = true;
    try {
      const response = await logchefqlApi.translate(teamId, props.sourceId, { query: form.condition_json });
      if (requestId !== translationSequence) return;

      if (response.data && response.data.valid) {
        conditionError.value = null;
        const translatedQuery = response.data.generated_query || response.data.sql || "";
        const translatedLanguage = response.data.generated_query_language || nativeQueryLanguage.value;

        if (form.aggregate_function !== "count" && !form.aggregate_field.trim()) {
          conditionError.value = `Select the numeric field to ${form.aggregate_function}() over`;
          generatedQuery.value = "";
          return;
        }
        const aggregateField = form.aggregate_field.trim();
        if (translatedLanguage === "logsql") {
          const filterQuery = translatedQuery.trim() || "*";
          const statsExpression = form.aggregate_function === "count"
            ? "count()"
            : `${form.aggregate_function}(${logsqlFieldRef(aggregateField)})`;
          generatedQuery.value = `${filterQuery} | stats ${statsExpression} as value`;
        } else {
          const aggFunc = form.aggregate_function === "count" ? "count(*)" : `${form.aggregate_function}(\`${aggregateField}\`)`;
          const tableName = currentTableName.value;

          let whereClause = response.data.sql ? `(${response.data.sql})` : "1=1";
          whereClause += ` AND ${timeFilter}`;

          generatedQuery.value = `SELECT ${aggFunc} as value\nFROM ${tableName}\nWHERE ${whereClause}`;
        }
      } else if (response.data && !response.data.valid) {
        conditionError.value = response.data.error?.message || t("alerts.invalidCondition");
        generatedQuery.value = "";
      } else {
        conditionError.value = 'status' in response && response.status === 'error' ? response.message : t("alerts.translationFailed");
        generatedQuery.value = "";
      }
    } catch (error: unknown) {
      if (requestId !== translationSequence) return;
      conditionError.value = getErrorMessage(error) || t("alerts.translationFailed");
      generatedQuery.value = "";
    } finally {
      if (requestId === translationSequence) isTranslating.value = false;
    }
  }

  let translateDebounceTimer: ReturnType<typeof setTimeout> | null = null;

  function invalidateTranslation() {
    translationSequence++;
    if (translateDebounceTimer !== null) clearTimeout(translateDebounceTimer);
    translateDebounceTimer = null;
    isTranslating.value = false;
    generatedQuery.value = "";
    conditionError.value = null;
  }

  watch(
    () => [props.open, props.teamId, props.sourceId, form.editor_mode,
      form.condition_json, form.aggregate_function, form.aggregate_field,
      currentTableName.value, nativeQueryLanguage.value, supportsConditionEditor.value],
    () => {
      invalidateTranslation();
      if (form.editor_mode !== "condition") return;
      form.query = "";
      if (!props.open) return;
      translateDebounceTimer = setTimeout(() => {
        translateDebounceTimer = null;
        translateCondition();
      }, 300);
    },
    { flush: "sync" }
  );

  onScopeDispose(invalidateTranslation);

  // Sync generated query to query field when using condition mode
  watch(generatedQuery, (query) => {
    if (form.editor_mode === "condition" && query) {
      form.query = query;
    }
  });

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

  const queryTemplates = computed(() =>
    sourceType.value === "victorialogs" ? getVictoriaLogsQueryTemplates() : getClickHouseQueryTemplates()
  );

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
    conditionError.value = null;
    generatedQuery.value = "";
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

    // For condition mode, check if condition is valid and generates SQL
    if (form.editor_mode === "condition") {
      return hasName && hasThreshold && hasFrequency && hasLookback &&
             !!form.condition_json.trim() && !conditionError.value && !!generatedQuery.value;
    }

    // For SQL mode, just check if query is not empty
    return hasName && !!form.query.trim() && hasThreshold && hasFrequency && hasLookback;
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
    invalidateTranslation();
    testQueryResult.value = null;
    testQueryError.value = null;
    conditionError.value = null;
    generatedQuery.value = "";

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
    form.query = alert.query;
    form.condition_json = alert.condition_json ?? "";
    form.aggregate_function = "count";
    form.aggregate_field = "";
    const aggMatch = (alert.query || "").match(/(count|sum|avg|min|max)\(\s*[`"]?([^`")]*)[`"]?\s*\)/);
    if (aggMatch) {
      form.aggregate_function = aggMatch[1] as typeof form.aggregate_function;
      if (aggMatch[1] !== "count") {
        form.aggregate_field = aggMatch[2].trim();
      }
    }
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

  // A LogchefQL condition is evaluated ungrouped until the server compiles it.
  const groupBy = computed(() => (form.editor_mode === "native" ? parseGroupBy(form.group_by) : []));

  async function handleTestQuery() {
    if (!props.teamId || !props.sourceId || !form.query.trim()) {
      return;
    }

    isTestingQuery.value = true;
    testQueryError.value = null;
    testQueryResult.value = null;

    try {
      const result = await alertsApi.testQuery({
        source_id: props.sourceId,
        query_language: alertMetadata.value.queryLanguage,
        editor_mode: alertMetadata.value.editorMode,
        query: form.query.trim(),
        condition_json: form.editor_mode === "condition" ? form.condition_json.trim() : undefined,
        lookback_seconds: form.lookback_seconds,
        threshold_operator: form.threshold_operator,
        threshold_value: form.threshold_value,
        group_by: groupBy.value,
        no_data_policy: form.no_data_policy,
      });
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
      conditionError.value = null;
      generatedQuery.value = "";
    }
  });

  watch(
    () => [form.query, form.threshold_operator, form.threshold_value, form.group_by, form.no_data_policy],
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
      query_language: alertMetadata.value.queryLanguage,
      editor_mode: alertMetadata.value.editorMode,
      query: form.query.trim(),
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
    supportsConditionEditor,
    aggregateFieldSuggestions,
    nativeEditorLabel,
    nativeQueryLabel,
    nativeQueryPlaceholder,
    nativeQueryHelpText,
    generatedQueryLanguageLabel,
    generatedQuery,
    conditionError,
    conditionTemplates,
    queryTemplates,
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
