<script setup lang="ts">
import { useI18n } from "vue-i18n";

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

const { t } = useI18n();

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
        <h3 class='text-sm font-semibold'>{{ t('ui.evaluationQuery') }}</h3>
        <p class="text-xs text-muted-foreground mt-1">
          {{ form.editor_mode === 'condition'
            ? t('ui.writeASimpleFilterConditionTheTimeFilterIsAutoApplied')
            : t('alerts.nativeQueryHint', { language: nativeEditorLabel }) }}
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
        <Label for="condition-template">{{ t('ui.startFromATemplate') }} <span class='text-xs text-muted-foreground'>{{ t('ui.optional2') }}</span></Label>
        <Select @update:model-value="(value: any) => onApplyConditionTemplate(conditionTemplates[parseInt(value)])">
          <SelectTrigger id="condition-template">
            <SelectValue :placeholder="t('ui.chooseATemplate')" />
          </SelectTrigger>
          <SelectContent>
            <SelectGroup>
              <SelectLabel>{{ t('ui.conditionTemplates') }}</SelectLabel>
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
          <Label for="aggregate-function">{{ t('ui.aggregateFunction') }}</Label>
          <Select :model-value="form.aggregate_function" @update:model-value="(v: any) => form.aggregate_function = v">
            <SelectTrigger id="aggregate-function">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="count">count(*) · {{ t('alerts.aggregateCount') }}</SelectItem>
              <SelectItem value="sum">sum(field) · {{ t('alerts.aggregateSum') }}</SelectItem>
              <SelectItem value="avg">avg(field) · {{ t('alerts.aggregateAverage') }}</SelectItem>
              <SelectItem value="min">min(field) · {{ t('alerts.aggregateMinimum') }}</SelectItem>
              <SelectItem value="max">max(field) · {{ t('alerts.aggregateMaximum') }}</SelectItem>
            </SelectContent>
          </Select>
        </div>
        <div v-if="form.aggregate_function !== 'count'" class="space-y-2">
          <Label for="aggregate-field" class="required">{{ t('ui.fieldToAggregate') }}</Label>
          <Input
            id="aggregate-field"
            v-model="form.aggregate_field"
            list="aggregate-field-suggestions"
            :placeholder="t('ui.numericFieldEGDurationMs')"
          />
          <datalist id="aggregate-field-suggestions">
            <option v-for="name in aggregateFieldSuggestions" :key="name" :value="name" />
          </datalist>
        </div>
      </div>

      <!-- Condition Input -->
      <div class="space-y-2">
        <div class="flex items-center justify-between">
          <Label for="alert-condition">{{ t('ui.filterCondition') }}</Label>
          <Button
            type="button"
            variant="outline"
            size="sm"
            :disabled="!generatedQuery || disabled || isTestingQuery"
            @click="onTestQuery"
          >
            {{ isTestingQuery ? t('ui.testing') : t('ui.testQuery') }}
          </Button>
        </div>
        <Input
          id="alert-condition"
          v-model="form.condition_json"
          placeholder="severity = &quot;ERROR&quot; and status_code >= 500"
          :disabled="disabled"
          class="font-mono text-sm"
        />
        <p v-if="conditionError" class="text-xs text-destructive">{{ conditionError }}</p>
        <p class="text-xs text-muted-foreground">
          {{ t('ui.examples2') }} <code class='bg-muted px-1 rounded'>severity = "ERROR"</code>,
          <code class="bg-muted px-1 rounded">status_code >= 500</code>,
          <code class="bg-muted px-1 rounded">message ~ "timeout"</code>
        </p>
      </div>

      <div v-if="generatedQuery" class="space-y-2">
        <Label class='text-xs text-muted-foreground'>{{ generatedQueryLanguageLabel }} {{ t('ui.readOnly') }}</Label>
        <pre class="bg-muted/50 border rounded-md p-3 text-xs font-mono overflow-x-auto whitespace-pre-wrap">{{ generatedQuery }}</pre>
      </div>
    </template>

    <!-- Native Mode -->
    <template v-else>
      <!-- Query Templates -->
      <div class="space-y-2">
        <Label for="query-template">{{ t('ui.startFromATemplate') }} <span class='text-xs text-muted-foreground'>{{ t('ui.optional2') }}</span></Label>
        <Select @update:model-value="(value: any) => onApplyTemplate(queryTemplates[parseInt(value)])">
          <SelectTrigger id="query-template">
            <SelectValue :placeholder="t('ui.chooseATemplate')" />
          </SelectTrigger>
          <SelectContent>
            <SelectGroup>
              <SelectLabel>{{ t('ui.queryTemplates') }}</SelectLabel>
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
            {{ isTestingQuery ? t('ui.testing') : t('ui.testQuery') }}
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
          <h4 class='text-sm font-medium'>{{ t('ui.testResult') }}</h4>
          <div class="flex items-baseline gap-3">
            <span class="text-2xl font-semibold tabular-nums">{{ testQueryResult.value }}</span>
            <span class="text-sm text-muted-foreground">
              {{ testQueryResult.threshold_met ? t('ui.thresholdMet') : t('ui.thresholdNotMet') }}
            </span>
          </div>
        </div>
        <div class="text-right space-y-1">
          <div class='text-xs text-muted-foreground'>{{ t('ui.executionTime') }}</div>
          <div class='text-sm font-medium tabular-nums'>{{ testQueryResult.execution_time_ms }}{{ t('ui.ms') }}</div>
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
