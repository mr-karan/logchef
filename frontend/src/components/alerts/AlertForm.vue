<script setup lang="ts">
import { computed, reactive, ref, watch, onMounted } from "vue";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { Label } from "@/components/ui/label";
import { Button } from "@/components/ui/button";
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectLabel,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { useAlertsStore } from "@/stores/alerts";
import { useSourcesStore } from "@/stores/sources";
import { alertsApi } from "@/api/alerts";
import { logchefqlApi } from "@/api/logchefql";
import { useTeamsStore } from "@/stores/teams";
import type { Alert, CreateAlertRequest, UpdateAlertRequest, TestAlertQueryResponse } from "@/api/alerts";
import { X, Plus, User, Bell } from "lucide-vue-next";
import { Badge } from "@/components/ui/badge";

// Extended types for local usage until API types are updated
interface ExtendedCreateAlertRequest extends CreateAlertRequest {
  recipient_user_ids: number[];
  webhook_urls: string[];
}

interface ExtendedUpdateAlertRequest extends UpdateAlertRequest {
  recipient_user_ids?: number[];
  webhook_urls?: string[];
}

const props = withDefaults(defineProps<{
  open: boolean;
  mode: "create" | "edit";
  teamId: number | null;
  sourceId: number | null;
  alert: Alert | null;
  inline?: boolean;
}>(), {
  inline: false,
});

const emit = defineEmits<{
  (e: "cancel"): void;
  (e: "create", payload: ExtendedCreateAlertRequest): void;
  (e: "update", payload: ExtendedUpdateAlertRequest): void;
}>();

const alertsStore = useAlertsStore();
const sourcesStore = useSourcesStore();
const teamsStore = useTeamsStore();

// Get current source table name for SQL generation
const currentTableName = computed(() => sourcesStore.getCurrentSourceTableName || "logs");

const form = reactive({
  name: "",
  description: "",
  query_type: "condition" as Alert["query_type"], // Default to LogChefQL mode
  query: "",
  condition_json: "", // LogChefQL condition string
  aggregate_function: "count" as "count" | "sum" | "avg" | "min" | "max",
  lookback_seconds: 300,
  threshold_operator: "gt" as Alert["threshold_operator"],
  threshold_value: 1,
  frequency_seconds: 300,
  severity: "warning" as Alert["severity"],
  is_active: true,
  labels: [] as Array<{ id: number; key: string; value: string }>,
  annotations: [] as Array<{ id: number; key: string; value: string }>,
  recipient_user_ids: [] as number[],
  webhook_urls: [] as string[],
});

// UI state for adding webhook
const newWebhookUrl = ref("");

// LogChefQL validation state
const conditionError = ref<string | null>(null);

// Get source details for schema-aware parsing (same pattern as explore page)
const sourceDetails = computed(() => sourcesStore.currentSourceDetails);
const timestampField = computed(() => sourceDetails.value?._meta_ts_field || "timestamp");

// Generate ClickHouse lookback expression based on lookback_seconds
const lookbackExpression = computed(() => {
  const seconds = form.lookback_seconds || 300;
  return `now() - toIntervalSecond(${seconds})`;
});

// Generated SQL from LogchefQL condition
const generatedSQL = ref("");
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
  if (form.query_type !== "condition" || !form.condition_json.trim()) {
    conditionError.value = null;
    generatedSQL.value = "";
    return;
  }

  const teamId = teamsStore.currentTeamId;
  
  if (!teamId || !props.sourceId) {
    conditionError.value = "Team or source not available";
    return;
  }

  isTranslating.value = true;
  try {
    const response = await logchefqlApi.translate(teamId, props.sourceId, { query: form.condition_json });
    
    if (response.data && response.data.valid) {
      conditionError.value = null;
      
      // Build the full SQL query with aggregate function and lookback
      const aggFunc = form.aggregate_function === "count" ? "count(*)" : `${form.aggregate_function}(value)`;
      const tableName = currentTableName.value;
      const tsField = timestampField.value;
      
      // Construct WHERE clause: LogChefQL conditions + time filter
      let whereClause = response.data.sql ? `(${response.data.sql})` : "1=1";
      whereClause += ` AND \`${tsField}\` >= ${lookbackExpression.value}`;
      
      generatedSQL.value = `SELECT ${aggFunc} as value\nFROM ${tableName}\nWHERE ${whereClause}`;
    } else if (response.data && !response.data.valid) {
      conditionError.value = response.data.error?.message || "Invalid condition";
      generatedSQL.value = "";
    } else {
      conditionError.value = 'status' in response && response.status === 'error' ? response.message : "Translation failed";
      generatedSQL.value = "";
    }
  } catch (error: any) {
    conditionError.value = error.message || "Translation error";
    generatedSQL.value = "";
  } finally {
    isTranslating.value = false;
  }
}

// Watch for changes that should trigger translation with debounce
let translateDebounceTimer: ReturnType<typeof setTimeout> | null = null;
watch(
  () => [form.condition_json, form.aggregate_function, form.lookback_seconds],
  () => {
    if (translateDebounceTimer) clearTimeout(translateDebounceTimer);
    translateDebounceTimer = setTimeout(() => {
      if (form.query_type === "condition") {
        translateCondition();
      }
    }, 300);
  }
);

// Sync generated SQL to query field when using condition mode
watch(generatedSQL, (sql) => {
  if (form.query_type === "condition" && sql) {
    form.query = sql;
  }
});

const labelCounter = ref(0);
const annotationCounter = ref(0);

const testQueryResult = ref<TestAlertQueryResponse | null>(null);
const isTestingQuery = ref(false);
const testQueryError = ref<string | null>(null);

// SQL query templates
function getQueryTemplates() {
  const tableName = currentTableName.value;
  const tsField = timestampField.value;
  const lookback = lookbackExpression.value;
  
  return [
    {
      name: "High Error Count",
      description: "Alert when error count exceeds threshold in lookback window",
      queryType: "sql" as const,
      query: `SELECT count(*) as value
FROM ${tableName}
WHERE severity = 'ERROR'
  AND \`${tsField}\` >= ${lookback}`,
    },
    {
      name: "Critical Logs",
      description: "Alert on any critical severity logs",
      queryType: "sql" as const,
      query: `SELECT count(*) as value
FROM ${tableName}
WHERE severity = 'CRITICAL'
  AND \`${tsField}\` >= ${lookback}`,
    },
    {
      name: "High Response Time",
      description: "Alert when average response time is high",
      queryType: "sql" as const,
      query: `SELECT avg(response_time) as value
FROM ${tableName}
WHERE \`${tsField}\` >= ${lookback}`,
    },
    {
      name: "Failed Requests",
      description: "Alert on HTTP 5xx status codes",
      queryType: "sql" as const,
      query: `SELECT count(*) as value
FROM ${tableName}
WHERE status_code >= 500
  AND \`${tsField}\` >= ${lookback}`,
    },
    {
      name: "Low Success Rate",
      description: "Alert when success rate drops below threshold",
      queryType: "sql" as const,
      query: `SELECT (countIf(status_code < 400) * 100.0 / count(*)) as value
FROM ${tableName}
WHERE \`${tsField}\` >= ${lookback}`,
    },
  ];
}

const queryTemplates = computed(() => getQueryTemplates());

// LogChefQL condition templates
const conditionTemplates = [
  {
    name: "Error Logs",
    description: "Match logs with ERROR severity",
    condition: `severity = "ERROR"`,
    aggregate: "count" as const,
  },
  {
    name: "Critical Logs",
    description: "Match logs with CRITICAL severity",
    condition: `severity = "CRITICAL"`,
    aggregate: "count" as const,
  },
  {
    name: "Server Errors",
    description: "Match HTTP 5xx status codes",
    condition: `status_code >= 500`,
    aggregate: "count" as const,
  },
  {
    name: "Slow Requests",
    description: "Match requests taking over 1 second",
    condition: `response_time > 1000`,
    aggregate: "count" as const,
  },
  {
    name: "Error Messages",
    description: "Match logs containing 'error' in the message",
    condition: `message ~ "error"`,
    aggregate: "count" as const,
  },
];

function applyConditionTemplate(template: typeof conditionTemplates[0]) {
  form.condition_json = template.condition;
  form.aggregate_function = template.aggregate;
  testQueryResult.value = null;
  testQueryError.value = null;
  conditionError.value = null;
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
  if (form.query_type === "condition") {
    return hasName && hasThreshold && hasFrequency && hasLookback && 
           !!form.condition_json.trim() && !conditionError.value && !!generatedSQL.value;
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
function addRecipient(userIdStr: string) {
  const userId = parseInt(userIdStr);
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
  newWebhookUrl.value = "";
  
  if (!alert) {
    form.name = "";
    form.description = "";
    form.query_type = "condition";
    form.query = "";
    form.condition_json = "";
    form.aggregate_function = "count";
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
  form.query_type = alert.query_type;
  form.query = alert.query;
  form.condition_json = alert.condition_json ?? "";
  form.aggregate_function = "count";
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
    const result = await alertsApi.testAlertQuery(props.teamId, props.sourceId, {
      query_type: form.query_type,
      query: form.query.trim(),
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

function applyTemplate(template: ReturnType<typeof getQueryTemplates>[0]) {
  form.query_type = template.queryType;
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

watch(
  () => [form.query, form.threshold_operator, form.threshold_value],
  () => {
    testQueryResult.value = null;
    testQueryError.value = null;
  }
);

function handleClose() {
  emit("cancel");
}

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
    query_type: form.query_type,
    query: form.query.trim(),
    condition_json: form.query_type === "condition" ? form.condition_json.trim() : undefined,
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
</script>

<template>
  <Dialog v-if="!inline" :open="open" @update:open="(value) => !value && handleClose()">
    <DialogContent class="max-h-[90vh] max-w-4xl overflow-y-auto">
      <DialogHeader>
        <DialogTitle>
          {{ mode === "create" ? "Create alert" : "Edit alert" }}
        </DialogTitle>
        <DialogDescription>
          Configure the evaluation query, thresholds, and delivery targets for this alert rule.
        </DialogDescription>
      </DialogHeader>

      <form class="space-y-6" @submit.prevent="handleSubmit">
        <!-- Basic Information -->
        <section class="space-y-4">
          <div class="grid gap-4 lg:grid-cols-3">
            <div class="space-y-2 lg:col-span-2">
              <Label for="alert-name">Alert name</Label>
              <Input id="alert-name" v-model="form.name" placeholder="High error rate alert" :disabled="isDisabled" />
            </div>
            <div class="space-y-2">
              <Label for="alert-severity">Severity</Label>
              <Select :model-value="form.severity" :disabled="isDisabled" @update:model-value="(value: any) => (form.severity = value)">
                <SelectTrigger id="alert-severity">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectGroup>
                    <SelectLabel>Severity</SelectLabel>
                    <SelectItem value="info">Info</SelectItem>
                    <SelectItem value="warning">Warning</SelectItem>
                    <SelectItem value="critical">Critical</SelectItem>
                  </SelectGroup>
                </SelectContent>
              </Select>
            </div>
          </div>
          <div class="space-y-2">
            <Label for="alert-description">Description <span class="text-xs text-muted-foreground">(optional)</span></Label>
            <Textarea id="alert-description" v-model="form.description" placeholder="Provide context about when this alert should fire and what action to take" :rows="2" :disabled="isDisabled" />
          </div>
        </section>

        <!-- Evaluation Query -->
        <section class="space-y-4 rounded-lg border bg-muted/20 p-5">
          <div class="flex items-start justify-between gap-4">
            <div>
              <h3 class="text-sm font-semibold">Evaluation query</h3>
              <p class="text-xs text-muted-foreground mt-1">
                {{ form.query_type === 'condition' 
                  ? 'Write a simple filter condition. The time filter is auto-applied.' 
                  : 'Write a SQL query that returns a single numeric value.' }}
              </p>
            </div>
            <!-- Query Type Toggle -->
            <Tabs :model-value="form.query_type" @update:model-value="(v: any) => form.query_type = v" class="w-auto">
              <TabsList class="h-8">
                <TabsTrigger value="condition" class="text-xs px-3 h-7">LogChefQL</TabsTrigger>
                <TabsTrigger value="sql" class="text-xs px-3 h-7">SQL</TabsTrigger>
              </TabsList>
            </Tabs>
          </div>

          <!-- LogChefQL Mode -->
          <template v-if="form.query_type === 'condition'">
            <!-- Condition Templates -->
            <div class="space-y-2">
              <Label for="condition-template">Start from a template <span class="text-xs text-muted-foreground">(optional)</span></Label>
              <Select @update:model-value="(value: any) => applyConditionTemplate(conditionTemplates[parseInt(value)])">
                <SelectTrigger id="condition-template">
                  <SelectValue placeholder="Choose a template..." />
                </SelectTrigger>
                <SelectContent>
                  <SelectGroup>
                    <SelectLabel>Condition Templates</SelectLabel>
                    <SelectItem v-for="(template, index) in conditionTemplates" :key="index" :value="String(index)">
                      <div class="flex flex-col gap-0.5">
                        <span class="font-medium">{{ template.name }}</span>
                        <span class="text-xs text-muted-foreground">{{ template.description }}</span>
                      </div>
                    </SelectItem>
                  </SelectGroup>
                </SelectContent>
              </Select>
            </div>

            <!-- Aggregate Function -->
            <div class="grid gap-4 sm:grid-cols-2">
              <div class="space-y-2">
                <Label for="aggregate-function">Aggregate function</Label>
                <Select :model-value="form.aggregate_function" @update:model-value="(v: any) => form.aggregate_function = v">
                  <SelectTrigger id="aggregate-function">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="count">count(*) - Count matching logs</SelectItem>
                    <SelectItem value="sum">sum(value) - Sum of values</SelectItem>
                    <SelectItem value="avg">avg(value) - Average value</SelectItem>
                    <SelectItem value="min">min(value) - Minimum value</SelectItem>
                    <SelectItem value="max">max(value) - Maximum value</SelectItem>
                  </SelectContent>
                </Select>
              </div>
            </div>

            <!-- Condition Input -->
            <div class="space-y-2">
              <div class="flex items-center justify-between">
                <Label for="alert-condition">Filter condition</Label>
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  :disabled="!generatedSQL || isDisabled || isTestingQuery"
                  @click="handleTestQuery"
                >
                  {{ isTestingQuery ? "Testing..." : "Test Query" }}
                </Button>
              </div>
              <Input
                id="alert-condition"
                v-model="form.condition_json"
                placeholder='severity = "ERROR" and status_code >= 500'
                :disabled="isDisabled"
                class="font-mono text-sm"
              />
              <p v-if="conditionError" class="text-xs text-destructive">{{ conditionError }}</p>
              <p class="text-xs text-muted-foreground">
                Examples: <code class="bg-muted px-1 rounded">severity = "ERROR"</code>, 
                <code class="bg-muted px-1 rounded">status_code >= 500</code>, 
                <code class="bg-muted px-1 rounded">message ~ "timeout"</code>
              </p>
            </div>

            <!-- Generated SQL Preview -->
            <div v-if="generatedSQL" class="space-y-2">
              <Label class="text-xs text-muted-foreground">Generated SQL (read-only)</Label>
              <pre class="bg-muted/50 border rounded-md p-3 text-xs font-mono overflow-x-auto whitespace-pre-wrap">{{ generatedSQL }}</pre>
            </div>
          </template>

          <!-- SQL Mode -->
          <template v-else>
            <!-- Query Templates -->
            <div class="space-y-2">
              <Label for="query-template">Start from a template <span class="text-xs text-muted-foreground">(optional)</span></Label>
              <Select @update:model-value="(value: any) => applyTemplate(queryTemplates[parseInt(value)])">
                <SelectTrigger id="query-template">
                  <SelectValue placeholder="Choose a template..." />
                </SelectTrigger>
                <SelectContent>
                  <SelectGroup>
                    <SelectLabel>Query Templates</SelectLabel>
                    <SelectItem v-for="(template, index) in queryTemplates" :key="index" :value="String(index)">
                      <div class="flex flex-col gap-0.5">
                        <span class="font-medium">{{ template.name }}</span>
                        <span class="text-xs text-muted-foreground">{{ template.description }}</span>
                      </div>
                    </SelectItem>
                  </SelectGroup>
                </SelectContent>
              </Select>
            </div>

            <div class="space-y-2">
              <div class="flex items-center justify-between">
                <Label for="alert-query">SQL Query</Label>
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  :disabled="!form.query.trim() || isDisabled || isTestingQuery"
                  @click="handleTestQuery"
                >
                  {{ isTestingQuery ? "Testing..." : "Test Query" }}
                </Button>
              </div>
              <Textarea
                id="alert-query"
                v-model="form.query"
                :placeholder="`SELECT count(*) as value FROM ${currentTableName} WHERE severity = 'ERROR' AND \`${timestampField}\` >= ${lookbackExpression}`"
                :rows="6"
                :disabled="isDisabled"
                class="font-mono text-sm resize-none"
              />
              <p class="text-xs text-muted-foreground">
                Use a template above to auto-fill with the correct table, timestamp field, and lookback window.
              </p>
            </div>
          </template>

          <!-- Test Query Results -->
          <div v-if="testQueryResult" class="rounded-lg border bg-background p-4 space-y-3">
            <div class="flex items-start justify-between gap-4">
              <div class="flex-1 space-y-1">
                <h4 class="text-sm font-medium">Test Result</h4>
                <div class="flex items-baseline gap-3">
                  <span class="text-2xl font-semibold tabular-nums">{{ testQueryResult.value }}</span>
                  <span class="text-sm text-muted-foreground">
                    {{ testQueryResult.threshold_met ? '✓ Threshold met' : '✗ Threshold not met' }}
                  </span>
                </div>
              </div>
              <div class="text-right space-y-1">
                <div class="text-xs text-muted-foreground">Execution time</div>
                <div class="text-sm font-medium tabular-nums">{{ testQueryResult.execution_time_ms }}ms</div>
              </div>
            </div>

            <!-- Warnings -->
            <div v-if="testQueryResult.warnings && testQueryResult.warnings.length > 0" class="space-y-2">
              <div
                v-for="(warning, index) in testQueryResult.warnings"
                :key="index"
                class="flex gap-2 text-sm rounded-md bg-yellow-50 dark:bg-yellow-950/20 border border-yellow-200 dark:border-yellow-800 p-3"
              >
                <span class="text-yellow-600 dark:text-yellow-500 flex-shrink-0">⚠️</span>
                <span class="text-yellow-900 dark:text-yellow-100">{{ warning }}</span>
              </div>
            </div>
          </div>

          <!-- Test Query Error -->
          <div v-if="testQueryError" class="rounded-lg border border-red-200 dark:border-red-800 bg-red-50 dark:bg-red-950/20 p-4">
            <div class="flex gap-2 text-sm">
              <span class="text-red-600 dark:text-red-500 flex-shrink-0">✗</span>
              <span class="text-red-900 dark:text-red-100">{{ testQueryError }}</span>
            </div>
          </div>
        </section>

        <!-- Threshold & Timing -->
        <section class="space-y-4">
          <div>
            <h3 class="text-sm font-semibold mb-3">Threshold & timing</h3>
            <div class="grid gap-4 lg:grid-cols-2">
              <div class="space-y-2">
                <Label for="alert-threshold-operator">Threshold operator</Label>
                <Select :model-value="form.threshold_operator" :disabled="isDisabled" @update:model-value="(value: any) => (form.threshold_operator = value)">
                  <SelectTrigger id="alert-threshold-operator">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="gt">Greater than (&gt;)</SelectItem>
                    <SelectItem value="gte">Greater than or equal (&ge;)</SelectItem>
                    <SelectItem value="lt">Less than (&lt;)</SelectItem>
                    <SelectItem value="lte">Less than or equal (&le;)</SelectItem>
                    <SelectItem value="eq">Equal (=)</SelectItem>
                    <SelectItem value="neq">Not equal (&ne;)</SelectItem>
                  </SelectContent>
                </Select>
              </div>
              <div class="space-y-2">
                <Label for="alert-threshold-value">Threshold value</Label>
                <Input id="alert-threshold-value" v-model.number="form.threshold_value" type="number" min="0" step="0.01" :disabled="isDisabled" placeholder="1" />
              </div>
              <div class="space-y-2">
                <Label for="alert-lookback">
                  Lookback window (seconds)
                  <span class="text-xs font-normal text-muted-foreground ml-1">· Time range to query</span>
                </Label>
                <Input id="alert-lookback" v-model.number="form.lookback_seconds" type="number" min="60" step="60" :disabled="isDisabled" placeholder="300" />
                <p class="text-xs text-muted-foreground">
                  How far back to look in logs (e.g., 300s = last 5 minutes)
                </p>
              </div>
              <div class="space-y-2">
                <Label for="alert-frequency">
                  Evaluation frequency (seconds)
                  <span class="text-xs font-normal text-muted-foreground ml-1">· How often to check</span>
                </Label>
                <Input id="alert-frequency" v-model.number="form.frequency_seconds" type="number" min="30" step="30" :disabled="isDisabled" placeholder="300" />
                <p class="text-xs text-muted-foreground">
                  How often this alert runs (e.g., 300s = every 5 minutes)
                </p>
              </div>
            </div>
          </div>
        </section>

        <!-- Notifications & Routing -->
        <section class="space-y-6 border-t pt-4">
          <div>
            <h3 class="text-sm font-semibold flex items-center gap-2">
              <Bell class="h-4 w-4" />
              Notifications & Routing
            </h3>
            <p class="text-xs text-muted-foreground mt-1">Configure where alerts should be sent when triggered.</p>
          </div>

          <!-- Recipients -->
          <div class="space-y-3">
             <Label class="text-xs font-medium">Team Members <span class="font-normal text-muted-foreground ml-1">· Notify via email</span></Label>
             <div class="flex gap-2">
                <Select @update:model-value="addRecipient">
                  <SelectTrigger class="w-full">
                    <SelectValue placeholder="Select team member to notify..." />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem v-for="member in teamMembers" :key="member.user_id" :value="String(member.user_id)">
                      <div class="flex items-center gap-2">
                        <User class="h-3 w-3" />
                        <span>{{ member.user.name || member.user.email }}</span>
                        <span class="text-xs text-muted-foreground ml-1">({{ member.role }})</span>
                      </div>
                    </SelectItem>
                  </SelectContent>
                </Select>
             </div>
             
             <!-- Selected Recipients List -->
             <div v-if="form.recipient_user_ids.length > 0" class="flex flex-wrap gap-2">
                <Badge v-for="userId in form.recipient_user_ids" :key="userId" variant="secondary" class="flex items-center gap-1 font-normal">
                  <User class="h-3 w-3 opacity-50" />
                  <span>
                    {{ teamMembers.find(m => m.user_id === userId)?.user.name || teamMembers.find(m => m.user_id === userId)?.user.email || `User ${userId}` }}
                  </span>
                  <button type="button" @click="removeRecipient(userId)" class="ml-1 hover:text-destructive">
                    <X class="h-3 w-3" />
                  </button>
                </Badge>
             </div>
          </div>

          <!-- Webhooks -->
          <div class="space-y-3">
            <Label class="text-xs font-medium">Webhook URLs <span class="font-normal text-muted-foreground ml-1">· Send JSON payload</span></Label>
            <div class="flex gap-2">
              <Input v-model="newWebhookUrl" placeholder="https://api.example.com/hooks/..." @keydown.enter.prevent="addWebhook" />
              <Button type="button" variant="secondary" @click="addWebhook">
                <Plus class="h-4 w-4" />
              </Button>
            </div>
            
            <!-- Added Webhooks List -->
            <div v-if="form.webhook_urls.length > 0" class="space-y-2">
              <div v-for="url in form.webhook_urls" :key="url" class="flex items-center justify-between gap-2 border rounded-md px-3 py-2 text-sm">
                <span class="truncate font-mono text-xs">{{ url }}</span>
                <button type="button" @click="removeWebhook(url)" class="text-muted-foreground hover:text-destructive">
                  <X class="h-4 w-4" />
                </button>
              </div>
            </div>
          </div>

          <!-- Metadata -->
          <div class="grid gap-4 md:grid-cols-2">
            <!-- Labels -->
            <div class="space-y-3">
              <div class="flex items-center justify-between">
                <Label class="text-xs font-medium">Labels <span class="font-normal text-muted-foreground ml-1">· Grouping</span></Label>
                <Button type="button" variant="outline" size="sm" @click="addLabel" :disabled="isDisabled">
                  + Add Label
                </Button>
              </div>
              <div class="space-y-2">
                <div v-for="label in form.labels" :key="label.id" class="flex gap-2">
                  <Input v-model="label.key" placeholder="Key" class="flex-1" :disabled="isDisabled" />
                  <Input v-model="label.value" placeholder="Value" class="flex-1" :disabled="isDisabled" />
                  <Button type="button" variant="ghost" size="icon" @click="removeLabel(label.id)" :disabled="isDisabled">
                    <X class="h-4 w-4" />
                  </Button>
                </div>
                <p v-if="form.labels.length === 0" class="text-xs text-muted-foreground">No custom labels.</p>
              </div>
            </div>

            <!-- Annotations -->
            <div class="space-y-3">
              <div class="flex items-center justify-between">
                <Label class="text-xs font-medium">Annotations <span class="font-normal text-muted-foreground ml-1">· Context</span></Label>
                <Button type="button" variant="outline" size="sm" @click="addAnnotation" :disabled="isDisabled">
                  + Add Annotation
                </Button>
              </div>
              <div class="space-y-2">
                <div v-for="annotation in form.annotations" :key="annotation.id" class="flex gap-2">
                  <Input v-model="annotation.key" placeholder="Key" class="flex-1" :disabled="isDisabled" />
                  <Input v-model="annotation.value" placeholder="Value" class="flex-1" :disabled="isDisabled" />
                  <Button type="button" variant="ghost" size="icon" @click="removeAnnotation(annotation.id)" :disabled="isDisabled">
                    <X class="h-4 w-4" />
                  </Button>
                </div>
                <p v-if="form.annotations.length === 0" class="text-xs text-muted-foreground">No custom annotations.</p>
              </div>
            </div>
          </div>
        </section>

        <!-- Alert Status -->
        <section class="space-y-4">
          <div class="flex items-center justify-between rounded-lg border bg-muted/20 p-4">
            <div>
              <h3 class="text-sm font-medium">Alert status</h3>
              <p class="text-xs text-muted-foreground mt-0.5">
                {{ form.is_active ? "This alert will evaluate on schedule" : "Disabled alerts are skipped until re-enabled" }}
              </p>
            </div>
            <Switch :checked="form.is_active" :disabled="isDisabled" @update:checked="(checked) => (form.is_active = Boolean(checked))" />
          </div>
        </section>

        <DialogFooter v-if="!inline" class="pt-4">
          <Button type="button" variant="ghost" @click="handleClose" :disabled="isSubmitting">
            Cancel
          </Button>
          <Button type="submit" :disabled="!isValid || isDisabled">
            {{ isSubmitting ? "Saving..." : mode === "create" ? "Create alert" : "Save changes" }}
          </Button>
        </DialogFooter>
        <div v-else class="flex items-center justify-end gap-2 pt-4">
          <Button type="submit" :disabled="!isValid || isDisabled">
            {{ isSubmitting ? "Saving..." : "Save changes" }}
          </Button>
        </div>
      </form>
    </DialogContent>
  </Dialog>

  <!-- Inline mode (no dialog wrapper) -->
  <form v-else class="space-y-6" @submit.prevent="handleSubmit">
    <!-- Reuse same form sections - copy content from above -->
    <!-- Ideally this should be refactored into components, but for now duplicating as per original pattern -->
    
    <!-- Basic Information -->
        <section class="space-y-4">
          <div class="grid gap-4 lg:grid-cols-3">
            <div class="space-y-2 lg:col-span-2">
              <Label for="alert-name-inline">Alert name</Label>
              <Input id="alert-name-inline" v-model="form.name" placeholder="High error rate alert" :disabled="isDisabled" />
            </div>
            <div class="space-y-2">
              <Label for="alert-severity-inline">Severity</Label>
              <Select :model-value="form.severity" :disabled="isDisabled" @update:model-value="(value: any) => (form.severity = value)">
                <SelectTrigger id="alert-severity-inline">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectGroup>
                    <SelectLabel>Severity</SelectLabel>
                    <SelectItem value="info">Info</SelectItem>
                    <SelectItem value="warning">Warning</SelectItem>
                    <SelectItem value="critical">Critical</SelectItem>
                  </SelectGroup>
                </SelectContent>
              </Select>
            </div>
          </div>
          <div class="space-y-2">
            <Label for="alert-description-inline">Description <span class="text-xs text-muted-foreground">(optional)</span></Label>
            <Textarea id="alert-description-inline" v-model="form.description" placeholder="Provide context about when this alert should fire and what action to take" :rows="2" :disabled="isDisabled" />
          </div>
        </section>

        <!-- Evaluation Query -->
        <section class="space-y-4 rounded-lg border bg-muted/20 p-5">
          <div class="flex items-start justify-between gap-4">
            <div>
              <h3 class="text-sm font-semibold">Evaluation query</h3>
              <p class="text-xs text-muted-foreground mt-1">
                {{ form.query_type === 'condition' 
                  ? 'Write a simple filter condition. The time filter is auto-applied.' 
                  : 'Write a SQL query that returns a single numeric value.' }}
              </p>
            </div>
            <!-- Query Type Toggle -->
            <Tabs :model-value="form.query_type" @update:model-value="(v: any) => form.query_type = v" class="w-auto">
              <TabsList class="h-8">
                <TabsTrigger value="condition" class="text-xs px-3 h-7">LogChefQL</TabsTrigger>
                <TabsTrigger value="sql" class="text-xs px-3 h-7">SQL</TabsTrigger>
              </TabsList>
            </Tabs>
          </div>

          <!-- LogChefQL Mode -->
          <template v-if="form.query_type === 'condition'">
            <!-- Condition Templates -->
            <div class="space-y-2">
              <Label for="condition-template-inline">Start from a template <span class="text-xs text-muted-foreground">(optional)</span></Label>
              <Select @update:model-value="(value: any) => applyConditionTemplate(conditionTemplates[parseInt(value)])">
                <SelectTrigger id="condition-template-inline">
                  <SelectValue placeholder="Choose a template..." />
                </SelectTrigger>
                <SelectContent>
                  <SelectGroup>
                    <SelectLabel>Condition Templates</SelectLabel>
                    <SelectItem v-for="(template, index) in conditionTemplates" :key="index" :value="String(index)">
                      <div class="flex flex-col gap-0.5">
                        <span class="font-medium">{{ template.name }}</span>
                        <span class="text-xs text-muted-foreground">{{ template.description }}</span>
                      </div>
                    </SelectItem>
                  </SelectGroup>
                </SelectContent>
              </Select>
            </div>

            <!-- Aggregate Function -->
            <div class="grid gap-4 sm:grid-cols-2">
              <div class="space-y-2">
                <Label for="aggregate-function-inline">Aggregate function</Label>
                <Select :model-value="form.aggregate_function" @update:model-value="(v: any) => form.aggregate_function = v">
                  <SelectTrigger id="aggregate-function-inline">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="count">count(*) - Count matching logs</SelectItem>
                    <SelectItem value="sum">sum(value) - Sum of values</SelectItem>
                    <SelectItem value="avg">avg(value) - Average value</SelectItem>
                    <SelectItem value="min">min(value) - Minimum value</SelectItem>
                    <SelectItem value="max">max(value) - Maximum value</SelectItem>
                  </SelectContent>
                </Select>
              </div>
            </div>

            <!-- Condition Input -->
            <div class="space-y-2">
              <div class="flex items-center justify-between">
                <Label for="alert-condition-inline">Filter condition</Label>
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  :disabled="!generatedSQL || isDisabled || isTestingQuery"
                  @click="handleTestQuery"
                >
                  {{ isTestingQuery ? "Testing..." : "Test Query" }}
                </Button>
              </div>
              <Input
                id="alert-condition-inline"
                v-model="form.condition_json"
                placeholder='severity = "ERROR" and status_code >= 500'
                :disabled="isDisabled"
                class="font-mono text-sm"
              />
              <p v-if="conditionError" class="text-xs text-destructive">{{ conditionError }}</p>
              <p class="text-xs text-muted-foreground">
                Examples: <code class="bg-muted px-1 rounded">severity = "ERROR"</code>, 
                <code class="bg-muted px-1 rounded">status_code >= 500</code>, 
                <code class="bg-muted px-1 rounded">message ~ "timeout"</code>
              </p>
            </div>

            <!-- Generated SQL Preview -->
            <div v-if="generatedSQL" class="space-y-2">
              <Label class="text-xs text-muted-foreground">Generated SQL (read-only)</Label>
              <pre class="bg-muted/50 border rounded-md p-3 text-xs font-mono overflow-x-auto whitespace-pre-wrap">{{ generatedSQL }}</pre>
            </div>
          </template>

          <!-- SQL Mode -->
          <template v-else>
            <!-- Query Templates -->
            <div class="space-y-2">
              <Label for="query-template-inline">Start from a template <span class="text-xs text-muted-foreground">(optional)</span></Label>
              <Select @update:model-value="(value: any) => applyTemplate(queryTemplates[parseInt(value)])">
                <SelectTrigger id="query-template-inline">
                  <SelectValue placeholder="Choose a template..." />
                </SelectTrigger>
                <SelectContent>
                  <SelectGroup>
                    <SelectLabel>Query Templates</SelectLabel>
                    <SelectItem v-for="(template, index) in queryTemplates" :key="index" :value="String(index)">
                      <div class="flex flex-col gap-0.5">
                        <span class="font-medium">{{ template.name }}</span>
                        <span class="text-xs text-muted-foreground">{{ template.description }}</span>
                      </div>
                    </SelectItem>
                  </SelectGroup>
                </SelectContent>
              </Select>
            </div>

            <div class="space-y-2">
              <div class="flex items-center justify-between">
                <Label for="alert-query-inline">SQL Query</Label>
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  :disabled="!form.query.trim() || isDisabled || isTestingQuery"
                  @click="handleTestQuery"
                >
                  {{ isTestingQuery ? "Testing..." : "Test Query" }}
                </Button>
              </div>
              <Textarea
                id="alert-query-inline"
                v-model="form.query"
                :placeholder="`SELECT count(*) as value FROM ${currentTableName} WHERE severity = 'ERROR' AND \`${timestampField}\` >= ${lookbackExpression}`"
                :rows="6"
                :disabled="isDisabled"
                class="font-mono text-sm resize-none"
              />
              <p class="text-xs text-muted-foreground">
                Use a template above to auto-fill with the correct table, timestamp field, and lookback window.
              </p>
            </div>
          </template>

          <!-- Test Query Results -->
          <div v-if="testQueryResult" class="rounded-lg border bg-background p-4 space-y-3">
            <div class="flex items-start justify-between gap-4">
              <div class="flex-1 space-y-1">
                <h4 class="text-sm font-medium">Test Result</h4>
                <div class="flex items-baseline gap-3">
                  <span class="text-2xl font-semibold tabular-nums">{{ testQueryResult.value }}</span>
                  <span class="text-sm text-muted-foreground">
                    {{ testQueryResult.threshold_met ? '✓ Threshold met' : '✗ Threshold not met' }}
                  </span>
                </div>
              </div>
              <div class="text-right space-y-1">
                <div class="text-xs text-muted-foreground">Execution time</div>
                <div class="text-sm font-medium tabular-nums">{{ testQueryResult.execution_time_ms }}ms</div>
              </div>
            </div>

            <!-- Warnings -->
            <div v-if="testQueryResult.warnings && testQueryResult.warnings.length > 0" class="space-y-2">
              <div
                v-for="(warning, index) in testQueryResult.warnings"
                :key="index"
                class="flex gap-2 text-sm rounded-md bg-yellow-50 dark:bg-yellow-950/20 border border-yellow-200 dark:border-yellow-800 p-3"
              >
                <span class="text-yellow-600 dark:text-yellow-500 flex-shrink-0">⚠️</span>
                <span class="text-yellow-900 dark:text-yellow-100">{{ warning }}</span>
              </div>
            </div>
          </div>

          <!-- Test Query Error -->
          <div v-if="testQueryError" class="rounded-lg border border-red-200 dark:border-red-800 bg-red-50 dark:bg-red-950/20 p-4">
            <div class="flex gap-2 text-sm">
              <span class="text-red-600 dark:text-red-500 flex-shrink-0">✗</span>
              <span class="text-red-900 dark:text-red-100">{{ testQueryError }}</span>
            </div>
          </div>
        </section>

        <!-- Threshold & Timing -->
        <section class="space-y-4">
          <div>
            <h3 class="text-sm font-semibold mb-3">Threshold & timing</h3>
            <div class="grid gap-4 lg:grid-cols-2">
              <div class="space-y-2">
                <Label for="alert-threshold-operator-inline">Threshold operator</Label>
                <Select :model-value="form.threshold_operator" :disabled="isDisabled" @update:model-value="(value: any) => (form.threshold_operator = value)">
                  <SelectTrigger id="alert-threshold-operator-inline">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="gt">Greater than (&gt;)</SelectItem>
                    <SelectItem value="gte">Greater than or equal (&ge;)</SelectItem>
                    <SelectItem value="lt">Less than (&lt;)</SelectItem>
                    <SelectItem value="lte">Less than or equal (&le;)</SelectItem>
                    <SelectItem value="eq">Equal (=)</SelectItem>
                    <SelectItem value="neq">Not equal (&ne;)</SelectItem>
                  </SelectContent>
                </Select>
              </div>
              <div class="space-y-2">
                <Label for="alert-threshold-value-inline">Threshold value</Label>
                <Input id="alert-threshold-value-inline" v-model.number="form.threshold_value" type="number" min="0" step="0.01" :disabled="isDisabled" placeholder="1" />
              </div>
              <div class="space-y-2">
                <Label for="alert-lookback-inline">
                  Lookback window (seconds)
                  <span class="text-xs font-normal text-muted-foreground ml-1">· Time range to query</span>
                </Label>
                <Input id="alert-lookback-inline" v-model.number="form.lookback_seconds" type="number" min="60" step="60" :disabled="isDisabled" placeholder="300" />
                <p class="text-xs text-muted-foreground">
                  How far back to look in logs (e.g., 300s = last 5 minutes)
                </p>
              </div>
              <div class="space-y-2">
                <Label for="alert-frequency-inline">
                  Evaluation frequency (seconds)
                  <span class="text-xs font-normal text-muted-foreground ml-1">· How often to check</span>
                </Label>
                <Input id="alert-frequency-inline" v-model.number="form.frequency_seconds" type="number" min="30" step="30" :disabled="isDisabled" placeholder="300" />
                <p class="text-xs text-muted-foreground">
                  How often this alert runs (e.g., 300s = every 5 minutes)
                </p>
              </div>
            </div>
          </div>
        </section>

        <!-- Notifications & Routing (Inline) -->
        <section class="space-y-6 border-t pt-4">
          <div>
            <h3 class="text-sm font-semibold flex items-center gap-2">
              <Bell class="h-4 w-4" />
              Notifications & Routing
            </h3>
            <p class="text-xs text-muted-foreground mt-1">Configure where alerts should be sent when triggered.</p>
          </div>

          <!-- Recipients -->
          <div class="space-y-3">
             <Label class="text-xs font-medium">Team Members <span class="font-normal text-muted-foreground ml-1">· Notify via email</span></Label>
             <div class="flex gap-2">
                <Select @update:model-value="addRecipient">
                  <SelectTrigger class="w-full">
                    <SelectValue placeholder="Select team member to notify..." />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem v-for="member in teamMembers" :key="member.user_id" :value="String(member.user_id)">
                      <div class="flex items-center gap-2">
                        <User class="h-3 w-3" />
                        <span>{{ member.user.name || member.user.email }}</span>
                        <span class="text-xs text-muted-foreground ml-1">({{ member.role }})</span>
                      </div>
                    </SelectItem>
                  </SelectContent>
                </Select>
             </div>
             
             <!-- Selected Recipients List -->
             <div v-if="form.recipient_user_ids.length > 0" class="flex flex-wrap gap-2">
                <Badge v-for="userId in form.recipient_user_ids" :key="userId" variant="secondary" class="flex items-center gap-1 font-normal">
                  <User class="h-3 w-3 opacity-50" />
                  <span>
                    {{ teamMembers.find(m => m.user_id === userId)?.user.name || teamMembers.find(m => m.user_id === userId)?.user.email || `User ${userId}` }}
                  </span>
                  <button type="button" @click="removeRecipient(userId)" class="ml-1 hover:text-destructive">
                    <X class="h-3 w-3" />
                  </button>
                </Badge>
             </div>
          </div>

          <!-- Webhooks -->
          <div class="space-y-3">
            <Label class="text-xs font-medium">Webhook URLs <span class="font-normal text-muted-foreground ml-1">· Send JSON payload</span></Label>
            <div class="flex gap-2">
              <Input v-model="newWebhookUrl" placeholder="https://api.example.com/hooks/..." @keydown.enter.prevent="addWebhook" />
              <Button type="button" variant="secondary" @click="addWebhook">
                <Plus class="h-4 w-4" />
              </Button>
            </div>
            
            <!-- Added Webhooks List -->
            <div v-if="form.webhook_urls.length > 0" class="space-y-2">
              <div v-for="url in form.webhook_urls" :key="url" class="flex items-center justify-between gap-2 border rounded-md px-3 py-2 text-sm">
                <span class="truncate font-mono text-xs">{{ url }}</span>
                <button type="button" @click="removeWebhook(url)" class="text-muted-foreground hover:text-destructive">
                  <X class="h-4 w-4" />
                </button>
              </div>
            </div>
          </div>

          <!-- Metadata -->
          <div class="grid gap-4 md:grid-cols-2">
            <!-- Labels -->
            <div class="space-y-3">
              <div class="flex items-center justify-between">
                <Label class="text-xs font-medium">Labels <span class="font-normal text-muted-foreground ml-1">· Grouping</span></Label>
                <Button type="button" variant="outline" size="sm" @click="addLabel" :disabled="isDisabled">
                  + Add Label
                </Button>
              </div>
              <div class="space-y-2">
                <div v-for="label in form.labels" :key="label.id" class="flex gap-2">
                  <Input v-model="label.key" placeholder="Key" class="flex-1" :disabled="isDisabled" />
                  <Input v-model="label.value" placeholder="Value" class="flex-1" :disabled="isDisabled" />
                  <Button type="button" variant="ghost" size="icon" @click="removeLabel(label.id)" :disabled="isDisabled">
                    <X class="h-4 w-4" />
                  </Button>
                </div>
                <p v-if="form.labels.length === 0" class="text-xs text-muted-foreground">No custom labels.</p>
              </div>
            </div>

            <!-- Annotations -->
            <div class="space-y-3">
              <div class="flex items-center justify-between">
                <Label class="text-xs font-medium">Annotations <span class="font-normal text-muted-foreground ml-1">· Context</span></Label>
                <Button type="button" variant="outline" size="sm" @click="addAnnotation" :disabled="isDisabled">
                  + Add Annotation
                </Button>
              </div>
              <div class="space-y-2">
                <div v-for="annotation in form.annotations" :key="annotation.id" class="flex gap-2">
                  <Input v-model="annotation.key" placeholder="Key" class="flex-1" :disabled="isDisabled" />
                  <Input v-model="annotation.value" placeholder="Value" class="flex-1" :disabled="isDisabled" />
                  <Button type="button" variant="ghost" size="icon" @click="removeAnnotation(annotation.id)" :disabled="isDisabled">
                    <X class="h-4 w-4" />
                  </Button>
                </div>
                <p v-if="form.annotations.length === 0" class="text-xs text-muted-foreground">No custom annotations.</p>
              </div>
            </div>
          </div>
        </section>

    <section class="space-y-4">
      <div class="flex items-center justify-between rounded-lg border bg-muted/20 p-4">
        <div>
          <h3 class="text-sm font-medium">Alert status</h3>
          <p class="text-xs text-muted-foreground mt-0.5">
            {{ form.is_active ? "This alert will evaluate on schedule" : "Disabled alerts are skipped until re-enabled" }}
          </p>
        </div>
        <Switch :checked="form.is_active" :disabled="isDisabled" @update:checked="(checked) => (form.is_active = Boolean(checked))" />
      </div>
    </section>

    <div class="flex items-center justify-end gap-2 pt-4">
      <Button type="submit" :disabled="!isValid || isDisabled">
        {{ isSubmitting ? "Saving..." : mode === "create" ? "Create alert" : "Save changes" }}
      </Button>
    </div>
  </form>
</template>
