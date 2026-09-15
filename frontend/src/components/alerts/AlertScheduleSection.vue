<script setup lang="ts">
import { useI18n } from "vue-i18n";

import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import type { AlertFormState } from "@/composables/useAlertForm";

const { t } = useI18n();

defineProps<{
  form: AlertFormState;
  disabled: boolean;
}>();
</script>

<template>
  <section class="space-y-4">
    <div>
      <h3 class='text-sm font-semibold mb-3'>{{ t('ui.thresholdTiming') }}</h3>
      <div class="grid gap-4 lg:grid-cols-2">
        <div class="space-y-2">
          <Label for="alert-threshold-operator">{{ t('ui.thresholdOperator') }}</Label>
          <Select :model-value="form.threshold_operator" :disabled="disabled" @update:model-value="(value: any) => (form.threshold_operator = value)">
            <SelectTrigger id="alert-threshold-operator">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="gt">{{ t('ui.greaterThan') }}</SelectItem>
              <SelectItem value="gte">{{ t('ui.greaterThanOrEqual') }}</SelectItem>
              <SelectItem value="lt">{{ t('ui.lessThan') }}</SelectItem>
              <SelectItem value="lte">{{ t('ui.lessThanOrEqual') }}</SelectItem>
              <SelectItem value="eq">{{ t('ui.equal') }}</SelectItem>
              <SelectItem value="neq">{{ t('ui.notEqual2') }}</SelectItem>
            </SelectContent>
          </Select>
        </div>
        <div class="space-y-2">
          <Label for="alert-threshold-value">{{ t('ui.thresholdValue') }}</Label>
          <Input id="alert-threshold-value" v-model.number="form.threshold_value" type="number" min="0" step="0.01" :disabled="disabled" placeholder="1" />
        </div>
        <div class="space-y-2">
          <Label for="alert-lookback">
            {{ t('ui.lookbackWindowSeconds') }}
            <span class='text-xs font-normal text-muted-foreground ml-1'>{{ t('ui.timeRangeToQuery') }}</span>
          </Label>
          <Input id="alert-lookback" v-model.number="form.lookback_seconds" type="number" min="60" step="60" :disabled="disabled" placeholder="300" />
          <p class="text-xs text-muted-foreground">
            {{ t('ui.howFarBackToLookInLogsEG300sLast5') }}
          </p>
        </div>
        <div class="space-y-2">
          <Label for="alert-frequency">
            {{ t('ui.evaluationFrequencySeconds') }}
            <span class='text-xs font-normal text-muted-foreground ml-1'>{{ t('ui.howOftenToCheck') }}</span>
          </Label>
          <Input id="alert-frequency" v-model.number="form.frequency_seconds" type="number" min="30" step="30" :disabled="disabled" placeholder="300" />
          <p class="text-xs text-muted-foreground">
            {{ t('ui.howOftenThisAlertRunsEG300sEvery5Minutes') }}
          </p>
        </div>
      </div>
    </div>
  </section>
</template>
