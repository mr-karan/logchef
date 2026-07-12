import { computed, reactive, ref, watch } from "vue";
import { useAlertsStore } from "@/stores/alerts";
import { useSourcesStore } from "@/stores/sources";
import { useTeamsStore } from "@/stores/teams";
import { alertsApi } from "@/api/alerts";
import { asClickHouseConnection } from "@/api/sources";
import { logchefqlApi } from "@/api/logchefql";
import type { Alert, CreateAlertRequest, UpdateAlertRequest, TestAlertQueryResponse } from "@/api/alerts";
import {
  getNativeQueryLanguageForSource,
  getQueryLanguageLabel,
  resolveAlertMetadata,
  supportsAlertEditorMode,
} from "@/lib/queryMetadata";
import type { AcceptableValue } from "reka-ui";

// Extended types for local usage until API types are updated.
// The form doesn't include source_id — the caller adds it from context.
export type FormCreatePayload = Omit<CreateAlertRequest, "source_id"> & {
  recipient_user_ids: number[];
  webhook_urls: string[];
};

export interface ExtendedUpdateAlertRequest extends UpdateAlertRequest {
  recipient_user_ids?: number[];
  webhook_urls?: string[];
}

export interface AlertFormState {
  name: string;
  description: string;
  editor_mode: "condition" | "native";
  query: string;
  condition_json: string; // LogChefQL condition string
  aggregate_function: "count" | "sum" | "avg" | "min" | "max";
  aggregate_field: string;
  lookback_seconds: number;
  threshold_operator: Alert["threshold_operator"];
  threshold_value: number;
  frequency_seconds: number;
  severity: Alert["severity"];
  is_active: boolean;
  labels: Array<{ id: number; key: string; value: string }>;
  annotations: Array<{ id: number; key: string; value: string }>;
  recipient_user_ids: number[];
  webhook_urls: string[];
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
  const nativeQueryLabel = computed(() => `${nativeEditorLabel.value} Query`);
  const generatedQueryLanguageLabel = computed(() => {
    if (alertMetadata.value.queryLanguage === "logsql") {
      return "Generated LogsQL";
    }
    return "Generated SQL";
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
    lookback_seconds: 300,
    threshold_operator: "gt",
    threshold_value: 1,
    frequency_seconds: 300,
    severity: "warning",
    is_active: true,
    labels: [],
    annotations: [],
    recipient_user_ids: [],
    webhook_urls: [],
  });

  // UI state for adding webhook
  const newWebhookUrl = ref("");

  // LogChefQL validation state
  const conditionError = ref<string | null>(null);

  // Get source details for schema-aware parsing (same pattern as explore page)
  const sourceDetails = currentSource;
  const timestampField = computed(() => sourceDetails.value?._meta_ts_field || "timestamp");
  const alertMetadata = computed(() =>
    resolveAlertMetadata({
      editor_mode: form.editor_mode,
      source_type: sourceType.value,
      query_languages: currentSource.value?.query_languages,
      alert_editor_modes: currentSource.value?.alert_editor_modes,
    })
  );

  // Generate ClickHouse lookback expression based on lookback_seconds
  const lookbackExpression = computed(() => {
    const seconds = form.lookback_seconds || 300;
    return `now() - toIntervalSecond(${seconds})`;
  });

  const nativeQueryPlaceholder = computed(() => {
    if (sourceType.value === "victorialogs") {
      return `level:="error" | stats count() as value`;
    }

    return `SELECT count(*) as value FROM ${currentTableName.value} WHERE severity = 'ERROR' AND \`${timestampField.value}\` >= ${lookbackExpression.value}`;
  });

  const nativeQueryHelpText = computed(() => {
    if (sourceType.value === "victorialogs") {
      return "Use a template above to start with a valid LogsQL stats query. The alert lookback window is applied automatically.";
    }

    return "Use a template above to auto-fill with the correct table, timestamp field, and lookback window.";
  });

  // Generated executable query from LogchefQL condition
  const generatedQuery = ref("");
  const isTranslating = ref(false);

  // Team members for recipient selection
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

  // Translate LogChefQL condition to SQL using backend API
  async function translateCondition() {
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
      conditionError.value = "Team or source not available";
      return;
    }

    isTranslating.value = true;
    try {
      const response = await logchefqlApi.translate(teamId, props.sourceId, { query: form.condition_json });

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
          const tsField = timestampField.value;

          let whereClause = response.data.sql ? `(${response.data.sql})` : "1=1";
          whereClause += ` AND \`${tsField}\` >= ${lookbackExpression.value}`;

          generatedQuery.value = `SELECT ${aggFunc} as value\nFROM ${tableName}\nWHERE ${whereClause}`;
        }
      } else if (response.data && !response.data.valid) {
        conditionError.value = response.data.error?.message || "Invalid condition";
        generatedQuery.value = "";
      } else {
        conditionError.value = 'status' in response && response.status === 'error' ? response.message : "Translation failed";
        generatedQuery.value = "";
      }
    } catch (error: any) {
      conditionError.value = error.message || "Translation error";
      generatedQuery.value = "";
    } finally {
      isTranslating.value = false;
    }
  }

  // Watch for changes that should trigger translation with debounce
  let translateDebounceTimer: ReturnType<typeof setTimeout> | null = null;
  watch(
    () => [form.condition_json, form.aggregate_function, form.aggregate_field, form.lookback_seconds],
    () => {
      if (translateDebounceTimer) clearTimeout(translateDebounceTimer);
      translateDebounceTimer = setTimeout(() => {
        if (form.editor_mode === "condition") {
          translateCondition();
        }
      }, 300);
    }
  );

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
    const tsField = timestampField.value;
    const lookback = lookbackExpression.value;

    return [
      {
        name: "High Error Count",
        description: "Alert when error count exceeds threshold in lookback window",
        editorMode: "native" as const,
        query: `SELECT count(*) as value
FROM ${tableName}
WHERE severity = 'ERROR'
  AND \`${tsField}\` >= ${lookback}`,
      },
      {
        name: "Critical Logs",
        description: "Alert on any critical severity logs",
        editorMode: "native" as const,
        query: `SELECT count(*) as value
FROM ${tableName}
WHERE severity = 'CRITICAL'
  AND \`${tsField}\` >= ${lookback}`,
      },
      {
        name: "High Response Time",
        description: "Alert when average response time is high",
        editorMode: "native" as const,
        query: `SELECT avg(response_time) as value
FROM ${tableName}
WHERE \`${tsField}\` >= ${lookback}`,
      },
      {
        name: "Failed Requests",
        description: "Alert on HTTP 5xx status codes",
        editorMode: "native" as const,
        query: `SELECT count(*) as value
FROM ${tableName}
WHERE status_code >= 500
  AND \`${tsField}\` >= ${lookback}`,
      },
      {
        name: "Low Success Rate",
        description: "Alert when success rate drops below threshold",
        editorMode: "native" as const,
        query: `SELECT (countIf(status_code < 400) * 100.0 / count(*)) as value
FROM ${tableName}
WHERE \`${tsField}\` >= ${lookback}`,
      },
    ];
  }

  function getVictoriaLogsQueryTemplates(): QueryTemplate[] {
    return [
      {
        name: "High Error Count",
        description: "Alert when error logs exceed the threshold in the lookback window",
        editorMode: "native",
        query: `level:="ERROR" | stats count() as value`,
      },
      {
        name: "Critical Logs",
        description: "Alert on any critical severity logs",
        editorMode: "native",
        query: `level:="CRITICAL" | stats count() as value`,
      },
      {
        name: "Failed Requests",
        description: "Alert on HTTP 5xx status codes",
        editorMode: "native",
        query: `status_code:>=500 | stats count() as value`,
      },
      {
        name: "High Response Time",
        description: "Alert when the average response time is high",
        editorMode: "native",
        query: `response_time:* | stats avg(response_time) as value`,
      },
    ];
  }

  const queryTemplates = computed(() =>
    sourceType.value === "victorialogs" ? getVictoriaLogsQueryTemplates() : getClickHouseQueryTemplates()
  );

  // LogChefQL condition templates
  const conditionTemplates: ConditionTemplate[] = [
    {
      name: "Error Logs",
      description: "Match logs with ERROR severity",
      condition: `severity = "ERROR"`,
      aggregate: "count",
    },
    {
      name: "Critical Logs",
      description: "Match logs with CRITICAL severity",
      condition: `severity = "CRITICAL"`,
      aggregate: "count",
    },
    {
      name: "Server Errors",
      description: "Match HTTP 5xx status codes",
      condition: `status_code >= 500`,
      aggregate: "count",
    },
    {
      name: "Slow Requests",
      description: "Match requests taking over 1 second",
      condition: `response_time > 1000`,
      aggregate: "count",
    },
    {
      name: "Error Messages",
      description: "Match logs containing 'error' in the message",
      condition: `message ~ "error"`,
      aggregate: "count",
    },
  ];

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

  // Recipient management
  function addRecipient(value: AcceptableValue) {
    const userId = parseInt(String(value ?? ""));
    if (userId && !form.recipient_user_ids.includes(userId)) {
      form.recipient_user_ids.push(userId);
    }
  }

  function removeRecipient(userId: number) {
    form.recipient_user_ids = form.recipient_user_ids.filter(id => id !== userId);
  }

  // Webhook management
  function addWebhook() {
    const url = newWebhookUrl.value.trim();
    if (url && !form.webhook_urls.includes(url)) {
      form.webhook_urls.push(url);
      newWebhookUrl.value = "";
    }
  }

  function removeWebhook(url: string) {
    form.webhook_urls = form.webhook_urls.filter(u => u !== url);
  }

  function resetForm(alert: Alert | null) {
    testQueryResult.value = null;
    testQueryError.value = null;
    conditionError.value = null;
    generatedQuery.value = "";
    newWebhookUrl.value = "";

    if (!alert) {
      form.name = "";
      form.description = "";
      form.editor_mode = supportsConditionEditor.value ? "condition" : "native";
      form.query = "";
      form.condition_json = "";
      form.aggregate_function = "count";
      form.aggregate_field = "";
      form.lookback_seconds = 300;
      form.threshold_operator = "gt";
      form.threshold_value = 1;
      form.frequency_seconds = 300;
      form.severity = "warning";
      form.is_active = true;
      form.labels = [];
      form.annotations = [];
      form.recipient_user_ids = [];
      form.webhook_urls = [];
      labelCounter.value = 0;
      annotationCounter.value = 0;
      return;
    }

    // Type assertion to access extra fields that might exist on the alert object
    const extendedAlert = alert as unknown as { recipient_user_ids?: number[]; webhook_urls?: string[] };

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
    form.lookback_seconds = alert.lookback_seconds;
    form.threshold_operator = alert.threshold_operator;
    form.threshold_value = alert.threshold_value;
    form.frequency_seconds = alert.frequency_seconds;
    form.severity = alert.severity;
    form.is_active = alert.is_active;
    labelCounter.value = 0;
    annotationCounter.value = 0;
    form.labels = alert.labels
      ? Object.entries(alert.labels).map(([key, value]) => ({ id: labelCounter.value++, key, value }))
      : [];
    form.annotations = alert.annotations
      ? Object.entries(alert.annotations).map(([key, value]) => ({ id: annotationCounter.value++, key, value }))
      : [];

    form.recipient_user_ids = extendedAlert.recipient_user_ids ? [...extendedAlert.recipient_user_ids] : [];
    form.webhook_urls = extendedAlert.webhook_urls ? [...extendedAlert.webhook_urls] : [];
  }

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
      });
      testQueryResult.value = result.data;
    } catch (error: any) {
      testQueryError.value = error?.response?.data?.message || error.message || "Failed to test query";
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
    () => [form.query, form.threshold_operator, form.threshold_value],
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
      recipient_user_ids: form.recipient_user_ids,
      webhook_urls: form.webhook_urls,
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
    newWebhookUrl,
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
    addRecipient,
    removeRecipient,
    addWebhook,
    removeWebhook,
    handleSubmit,
  };
}
