<script setup lang="ts">
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
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import type { TestAlertQueryResponse } from "@/api/alerts";
import type { AlertFormState, ConditionTemplate, QueryTemplate } from "@/composables/useAlertForm";

defineProps<{
  form: AlertFormState;
  disabled: boolean;
  supportsConditionEditor: boolean;
  nativeEditorLabel: string;
  nativeQueryLabel: string;
  nativeQueryPlaceholder: string;
  nativeQueryHelpText: string;
  generatedQueryLanguageLabel: string;
  aggregateFieldSuggestions: string[];
  conditionTemplates: ConditionTemplate[];
  queryTemplates: QueryTemplate[];
  conditionError: string | null;
  generatedQuery: string;
  isTestingQuery: boolean;
  testQueryResult: TestAlertQueryResponse | null;
  testQueryError: string | null;
  onApplyConditionTemplate: (template: ConditionTemplate) => void;
  onApplyTemplate: (template: QueryTemplate) => void;
  onTestQuery: () => void;
}>();
</script>

<template>
  <section class="space-y-4 rounded-lg border bg-muted/20 p-5">
    <div class="flex items-start justify-between gap-4">
      <div>
        <h3 class="text-sm font-semibold">Evaluation query</h3>
        <p class="text-xs text-muted-foreground mt-1">
          {{ form.editor_mode === 'condition'
            ? 'Write a simple filter condition. The time filter is auto-applied.'
            : `Write a ${nativeEditorLabel} query that returns a single numeric value.` }}
        </p>
      </div>
      <!-- Query Type Toggle -->
      <Tabs :model-value="form.editor_mode" @update:model-value="(v: any) => form.editor_mode = v" class="w-auto">
        <TabsList class="h-8">
          <TabsTrigger v-if="supportsConditionEditor" value="condition" class="text-xs px-3 h-7">LogchefQL</TabsTrigger>
          <TabsTrigger value="native" class="text-xs px-3 h-7">{{ nativeEditorLabel }}</TabsTrigger>
        </TabsList>
      </Tabs>
    </div>

    <!-- LogChefQL Mode -->
    <template v-if="form.editor_mode === 'condition'">
      <!-- Condition Templates -->
      <div class="space-y-2">
        <Label for="condition-template">Start from a template <span class="text-xs text-muted-foreground">(optional)</span></Label>
        <Select @update:model-value="(value: any) => onApplyConditionTemplate(conditionTemplates[parseInt(value)])">
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
              <SelectItem value="sum">sum(field) - Sum of a field</SelectItem>
              <SelectItem value="avg">avg(field) - Average of a field</SelectItem>
              <SelectItem value="min">min(field) - Minimum of a field</SelectItem>
              <SelectItem value="max">max(field) - Maximum of a field</SelectItem>
            </SelectContent>
          </Select>
        </div>
        <div v-if="form.aggregate_function !== 'count'" class="space-y-2">
          <Label for="aggregate-field" class="required">Field to aggregate</Label>
          <Input
            id="aggregate-field"
            v-model="form.aggregate_field"
            list="aggregate-field-suggestions"
            placeholder="numeric field, e.g. duration_ms"
          />
          <datalist id="aggregate-field-suggestions">
            <option v-for="name in aggregateFieldSuggestions" :key="name" :value="name" />
          </datalist>
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
            :disabled="!generatedQuery || disabled || isTestingQuery"
            @click="onTestQuery"
          >
            {{ isTestingQuery ? "Testing..." : "Test Query" }}
          </Button>
        </div>
        <Input
          id="alert-condition"
          v-model="form.condition_json"
          placeholder='severity = "ERROR" and status_code >= 500'
          :disabled="disabled"
          class="font-mono text-sm"
        />
        <p v-if="conditionError" class="text-xs text-destructive">{{ conditionError }}</p>
        <p class="text-xs text-muted-foreground">
          Examples: <code class="bg-muted px-1 rounded">severity = "ERROR"</code>,
          <code class="bg-muted px-1 rounded">status_code >= 500</code>,
          <code class="bg-muted px-1 rounded">message ~ "timeout"</code>
        </p>
      </div>

      <div v-if="generatedQuery" class="space-y-2">
        <Label class="text-xs text-muted-foreground">{{ generatedQueryLanguageLabel }} (read-only)</Label>
        <pre class="bg-muted/50 border rounded-md p-3 text-xs font-mono overflow-x-auto whitespace-pre-wrap">{{ generatedQuery }}</pre>
      </div>
    </template>

    <!-- Native Mode -->
    <template v-else>
      <!-- Query Templates -->
      <div class="space-y-2">
        <Label for="query-template">Start from a template <span class="text-xs text-muted-foreground">(optional)</span></Label>
        <Select @update:model-value="(value: any) => onApplyTemplate(queryTemplates[parseInt(value)])">
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
          <Label for="alert-query">{{ nativeQueryLabel }}</Label>
          <Button
            type="button"
            variant="outline"
            size="sm"
            :disabled="!form.query.trim() || disabled || isTestingQuery"
            @click="onTestQuery"
          >
            {{ isTestingQuery ? "Testing..." : "Test Query" }}
          </Button>
        </div>
        <Textarea
          id="alert-query"
          v-model="form.query"
          :placeholder="nativeQueryPlaceholder"
          :rows="6"
          :disabled="disabled"
          class="font-mono text-sm resize-none"
        />
        <p class="text-xs text-muted-foreground">
          {{ nativeQueryHelpText }}
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
</template>
