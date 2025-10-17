<script setup lang="ts">
import { computed, reactive, ref, watch } from "vue";
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
import { useAlertsStore } from "@/stores/alerts";
import { alertsApi } from "@/api/alerts";
import type { Alert, CreateAlertRequest, UpdateAlertRequest, TestAlertQueryResponse } from "@/api/alerts";

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
  (e: "create", payload: CreateAlertRequest): void;
  (e: "update", payload: UpdateAlertRequest): void;
}>();

const alertsStore = useAlertsStore();

const form = reactive({
  name: "",
  description: "",
  query_type: "sql" as Alert["query_type"],
  query: "",
  lookback_seconds: 300,
  threshold_operator: "gt" as Alert["threshold_operator"],
  threshold_value: 1,
  frequency_seconds: 300,
  severity: "warning" as Alert["severity"],
  is_active: true,
  labels: [] as Array<{ id: number; key: string; value: string }>,
  annotations: [] as Array<{ id: number; key: string; value: string }>,
});

const labelCounter = ref(0);
const annotationCounter = ref(0);

const testQueryResult = ref<TestAlertQueryResponse | null>(null);
const isTestingQuery = ref(false);
const testQueryError = ref<string | null>(null);

const queryTemplates = [
  {
    name: "High Error Count",
    description: "Alert when error count exceeds threshold in time window",
    queryType: "sql" as const,
    query: `SELECT count(*) as value
FROM logs
WHERE severity = 'ERROR'
  AND timestamp >= now() - INTERVAL 5 MINUTE`,
  },
  {
    name: "Critical Logs",
    description: "Alert on any critical severity logs",
    queryType: "sql" as const,
    query: `SELECT count(*) as value
FROM logs
WHERE severity = 'CRITICAL'
  AND timestamp >= now() - INTERVAL 5 MINUTE`,
  },
  {
    name: "High Response Time",
    description: "Alert when average response time is high",
    queryType: "sql" as const,
    query: `SELECT avg(response_time) as value
FROM logs
WHERE timestamp >= now() - INTERVAL 5 MINUTE`,
  },
  {
    name: "Failed Requests",
    description: "Alert on HTTP 5xx status codes",
    queryType: "sql" as const,
    query: `SELECT count(*) as value
FROM logs
WHERE status_code >= 500
  AND timestamp >= now() - INTERVAL 5 MINUTE`,
  },
  {
    name: "Low Success Rate",
    description: "Alert when success rate drops below threshold",
    queryType: "sql" as const,
    query: `SELECT (countIf(status_code < 400) * 100.0 / count(*)) as value
FROM logs
WHERE timestamp >= now() - INTERVAL 5 MINUTE`,
  },
];

const isSubmitting = computed(() => {
  if (props.mode === "create") {
    return alertsStore.isLoadingOperation("createAlert");
  }
  return props.alert ? alertsStore.isLoadingOperation(`updateAlert-${props.alert.id}`) : false;
});

const isDisabled = computed(() => !props.teamId || !props.sourceId || isSubmitting.value);

const isValid = computed(() => {
  return (
    !!form.name.trim() &&
    !!form.query.trim() &&
    form.threshold_value !== undefined &&
    form.frequency_seconds > 0 &&
    form.lookback_seconds > 0
  );
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
    form.query_type = "sql";
    form.query = "";
    form.lookback_seconds = 300;
    form.threshold_operator = "gt";
    form.threshold_value = 1;
    form.frequency_seconds = 300;
    form.severity = "warning";
    form.is_active = true;
    form.labels = [];
    form.annotations = [];
    labelCounter.value = 0;
    annotationCounter.value = 0;
    return;
  }
  form.name = alert.name;
  form.description = alert.description ?? "";
  form.query_type = alert.query_type;
  form.query = alert.query;
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

function applyTemplate(template: typeof queryTemplates[0]) {
  form.query_type = template.queryType;
  form.query = template.query;
  testQueryResult.value = null;
  testQueryError.value = null;
}

watch(
  () => props.alert,
  (alert) => {
    if (props.mode === "edit") {
      resetForm(alert || null);
    }
  },
  { immediate: true }
);

watch(
  () => props.open,
  (open) => {
    if (open) {
      if (props.mode === "create") {
        resetForm(null);
      }
    }
  },
  { immediate: false }
);

// Clear test results when query or threshold parameters change
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
  if (props.mode === "create") {
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
    const payload: CreateAlertRequest = {
      name: form.name.trim(),
      description: form.description.trim(),
      query_type: form.query_type,
      query: form.query.trim(),
      lookback_seconds: Number(form.lookback_seconds),
      threshold_operator: form.threshold_operator,
      threshold_value: Number(form.threshold_value),
      frequency_seconds: Number(form.frequency_seconds),
      severity: form.severity,
      is_active: form.is_active,
      labels: labelsRecord,
      annotations: annotationsRecord,
    };
    emit("create", payload);
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
  const payload: UpdateAlertRequest = {
    name: form.name.trim(),
    description: form.description.trim(),
    query_type: form.query_type,
    query: form.query.trim(),
    lookback_seconds: Number(form.lookback_seconds),
    threshold_operator: form.threshold_operator,
    threshold_value: Number(form.threshold_value),
    frequency_seconds: Number(form.frequency_seconds),
    severity: form.severity,
    is_active: form.is_active,
    labels: labelsRecord,
    annotations: annotationsRecord,
  };
  emit("update", payload);
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
          Configure the evaluation query, thresholds, and room delivery targets for this alert rule.
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
          <div>
            <h3 class="text-sm font-semibold">Evaluation query</h3>
            <p class="text-xs text-muted-foreground mt-1">Write a SQL query that returns a single numeric value. Include time filters in your WHERE clause.</p>
          </div>

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
              placeholder="SELECT count(*) as value FROM logs WHERE severity = 'ERROR' AND timestamp >= now() - INTERVAL 5 MINUTE"
              :rows="6"
              :disabled="isDisabled"
              class="font-mono text-sm resize-none"
            />
          </div>

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

        <!-- Labels & Annotations (for Alertmanager routing) -->
        <section class="space-y-4">
          <div>
            <h3 class="text-sm font-semibold">Alertmanager routing <span class="text-xs font-normal text-muted-foreground ml-1">(optional)</span></h3>
            <p class="text-xs text-muted-foreground mt-1">Add custom labels and annotations to control Alertmanager routing, grouping, and notification templates.</p>
          </div>

          <!-- Labels -->
          <div class="space-y-3">
            <div class="flex items-center justify-between">
              <Label class="text-xs font-medium">Labels <span class="font-normal text-muted-foreground ml-1">· Used for routing and grouping</span></Label>
              <Button type="button" variant="outline" size="sm" @click="addLabel" :disabled="isDisabled">
                + Add Label
              </Button>
            </div>
            <div v-if="form.labels.length > 0" class="space-y-2">
              <div v-for="label in form.labels" :key="label.id" class="flex gap-2">
                <Input
                  v-model="label.key"
                  placeholder="Key (e.g., env)"
                  class="flex-1"
                  :disabled="isDisabled"
                />
                <Input
                  v-model="label.value"
                  placeholder="Value (e.g., production)"
                  class="flex-1"
                  :disabled="isDisabled"
                />
                <Button type="button" variant="ghost" size="icon" @click="removeLabel(label.id)" :disabled="isDisabled">
                  ×
                </Button>
              </div>
            </div>
            <p v-else class="text-xs text-muted-foreground">No custom labels. Labels like alertname, severity, team_id are added automatically.</p>
          </div>

          <!-- Annotations -->
          <div class="space-y-3">
            <div class="flex items-center justify-between">
              <Label class="text-xs font-medium">Annotations <span class="font-normal text-muted-foreground ml-1">· Additional context for notifications</span></Label>
              <Button type="button" variant="outline" size="sm" @click="addAnnotation" :disabled="isDisabled">
                + Add Annotation
              </Button>
            </div>
            <div v-if="form.annotations.length > 0" class="space-y-2">
              <div v-for="annotation in form.annotations" :key="annotation.id" class="flex gap-2">
                <Input
                  v-model="annotation.key"
                  placeholder="Key (e.g., runbook_url)"
                  class="flex-1"
                  :disabled="isDisabled"
                />
                <Input
                  v-model="annotation.value"
                  placeholder="Value (e.g., https://docs.example.com/runbook)"
                  class="flex-1"
                  :disabled="isDisabled"
                />
                <Button type="button" variant="ghost" size="icon" @click="removeAnnotation(annotation.id)" :disabled="isDisabled">
                  ×
                </Button>
              </div>
            </div>
            <p v-else class="text-xs text-muted-foreground">No custom annotations. Common annotations: summary, description, dashboard_url, runbook_url.</p>
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
      <div>
        <h3 class="text-sm font-semibold">Evaluation query</h3>
        <p class="text-xs text-muted-foreground mt-1">Write a SQL query that returns a single numeric value. Include time filters in your WHERE clause.</p>
      </div>

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
          placeholder="SELECT count(*) as value FROM logs WHERE severity = 'ERROR' AND timestamp >= now() - INTERVAL 5 MINUTE"
          :rows="6"
          :disabled="isDisabled"
          class="font-mono text-sm resize-none"
        />
      </div>

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

    <!-- Labels & Annotations (for Alertmanager routing) - Inline Mode -->
    <section class="space-y-4">
      <div>
        <h3 class="text-sm font-semibold">Alertmanager routing <span class="text-xs font-normal text-muted-foreground ml-1">(optional)</span></h3>
        <p class="text-xs text-muted-foreground mt-1">Add custom labels and annotations to control Alertmanager routing, grouping, and notification templates.</p>
      </div>

      <!-- Labels -->
      <div class="space-y-3">
        <div class="flex items-center justify-between">
          <Label class="text-xs font-medium">Labels <span class="font-normal text-muted-foreground ml-1">· Used for routing and grouping</span></Label>
          <Button type="button" variant="outline" size="sm" @click="addLabel" :disabled="isDisabled">
            + Add Label
          </Button>
        </div>
        <div v-if="form.labels.length > 0" class="space-y-2">
          <div v-for="label in form.labels" :key="label.id" class="flex gap-2">
            <Input
              v-model="label.key"
              placeholder="Key (e.g., env)"
              class="flex-1"
              :disabled="isDisabled"
            />
            <Input
              v-model="label.value"
              placeholder="Value (e.g., production)"
              class="flex-1"
              :disabled="isDisabled"
            />
            <Button type="button" variant="ghost" size="icon" @click="removeLabel(label.id)" :disabled="isDisabled">
              ×
            </Button>
          </div>
        </div>
        <p v-else class="text-xs text-muted-foreground">No custom labels. Labels like alertname, severity, team_id are added automatically.</p>
      </div>

      <!-- Annotations -->
      <div class="space-y-3">
        <div class="flex items-center justify-between">
          <Label class="text-xs font-medium">Annotations <span class="font-normal text-muted-foreground ml-1">· Additional context for notifications</span></Label>
          <Button type="button" variant="outline" size="sm" @click="addAnnotation" :disabled="isDisabled">
            + Add Annotation
          </Button>
        </div>
        <div v-if="form.annotations.length > 0" class="space-y-2">
          <div v-for="annotation in form.annotations" :key="annotation.id" class="flex gap-2">
            <Input
              v-model="annotation.key"
              placeholder="Key (e.g., runbook_url)"
              class="flex-1"
              :disabled="isDisabled"
            />
            <Input
              v-model="annotation.value"
              placeholder="Value (e.g., https://docs.example.com/runbook)"
              class="flex-1"
              :disabled="isDisabled"
            />
            <Button type="button" variant="ghost" size="icon" @click="removeAnnotation(annotation.id)" :disabled="isDisabled">
              ×
            </Button>
          </div>
        </div>
        <p v-else class="text-xs text-muted-foreground">No custom annotations. Common annotations: summary, description, dashboard_url, runbook_url.</p>
      </div>
    </section>

    <!-- Alert Status - Inline Mode -->
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
        {{ isSubmitting ? "Saving..." : "Save changes" }}
      </Button>
    </div>
  </form>
</template>
