<script setup lang="ts">
import { useI18n } from "vue-i18n";

import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectLabel,
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
    <div class="grid gap-4 lg:grid-cols-3">
      <div class="space-y-2 lg:col-span-2">
        <Label for="alert-name">{{ t('ui.alertName') }}</Label>
        <Input id="alert-name" v-model="form.name" :placeholder="t('ui.highErrorRateAlert')" :disabled="disabled" />
      </div>
      <div class="space-y-2">
        <Label for="alert-severity">{{ t('ui.severity') }}</Label>
        <Select :model-value="form.severity" :disabled="disabled" @update:model-value="(value: any) => (form.severity = value)">
          <SelectTrigger id="alert-severity">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectGroup>
              <SelectLabel>{{ t('ui.severity') }}</SelectLabel>
              <SelectItem value="info">{{ t('ui.info') }}</SelectItem>
              <SelectItem value="warning">{{ t('ui.warning') }}</SelectItem>
              <SelectItem value="critical">{{ t('ui.critical') }}</SelectItem>
            </SelectGroup>
          </SelectContent>
        </Select>
      </div>
    </div>
    <div class="space-y-2">
      <Label for="alert-description">{{ t('ui.description') }} <span class='text-xs text-muted-foreground'>{{ t('ui.optional2') }}</span></Label>
      <Textarea id="alert-description" v-model="form.description" :placeholder="t('ui.provideContextAboutWhenThisAlertShouldFireAndWhatActionTo')" :rows="2" :disabled="disabled" />
    </div>
  </section>
</template>
