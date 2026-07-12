<script setup lang="ts">
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

defineProps<{
  form: AlertFormState;
  disabled: boolean;
}>();
</script>

<template>
  <section class="space-y-4">
    <div>
      <h3 class="text-sm font-semibold mb-3">Threshold & timing</h3>
      <div class="grid gap-4 lg:grid-cols-2">
        <div class="space-y-2">
          <Label for="alert-threshold-operator">Threshold operator</Label>
          <Select :model-value="form.threshold_operator" :disabled="disabled" @update:model-value="(value: any) => (form.threshold_operator = value)">
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
          <Input id="alert-threshold-value" v-model.number="form.threshold_value" type="number" min="0" step="0.01" :disabled="disabled" placeholder="1" />
        </div>
        <div class="space-y-2">
          <Label for="alert-lookback">
            Lookback window (seconds)
            <span class="text-xs font-normal text-muted-foreground ml-1">· Time range to query</span>
          </Label>
          <Input id="alert-lookback" v-model.number="form.lookback_seconds" type="number" min="60" step="60" :disabled="disabled" placeholder="300" />
          <p class="text-xs text-muted-foreground">
            How far back to look in logs (e.g., 300s = last 5 minutes)
          </p>
        </div>
        <div class="space-y-2">
          <Label for="alert-frequency">
            Evaluation frequency (seconds)
            <span class="text-xs font-normal text-muted-foreground ml-1">· How often to check</span>
          </Label>
          <Input id="alert-frequency" v-model.number="form.frequency_seconds" type="number" min="30" step="30" :disabled="disabled" placeholder="300" />
          <p class="text-xs text-muted-foreground">
            How often this alert runs (e.g., 300s = every 5 minutes)
          </p>
        </div>
      </div>
    </div>
  </section>
</template>
