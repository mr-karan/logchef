<script setup lang="ts">
import { useI18n } from "vue-i18n";

import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Switch } from "@/components/ui/switch";
import type { Alert } from "@/api/alerts";
import AlertBasicInfoSection from "./AlertBasicInfoSection.vue";
import AlertQuerySection from "./AlertQuerySection.vue";
import AlertScheduleSection from "./AlertScheduleSection.vue";
import AlertNotificationSection from "./AlertNotificationSection.vue";
import { useAlertForm, type ExtendedUpdateAlertRequest, type FormCreatePayload } from "@/composables/useAlertForm";

const { t } = useI18n();

// Extended types for local usage until API types are updated.
// The form doesn't include source_id — the parent adds it from context.
export type { FormCreatePayload, ExtendedUpdateAlertRequest };

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
  (e: "create", payload: FormCreatePayload): void;
  (e: "update", payload: ExtendedUpdateAlertRequest): void;
}>();

const {
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
} = useAlertForm(props, emit);

function handleClose() {
  emit("cancel");
}
</script>

<template>
  <Dialog v-if="!inline" :open="open" @update:open="(value) => !value && handleClose()">
    <DialogContent class="max-h-[90vh] max-w-4xl overflow-y-auto">
      <DialogHeader>
        <DialogTitle>
          {{ mode === "create" ? t('ui.createAlert') : t('ui.editAlert') }}
        </DialogTitle>
        <DialogDescription>
          {{ t('ui.configureTheEvaluationQueryThresholdsAndDeliveryTargetsForThisAlertRule') }}
        </DialogDescription>
      </DialogHeader>

      <form class="space-y-6" @submit.prevent="handleSubmit">
        <AlertBasicInfoSection :form="form" :disabled="isDisabled" />

        <AlertQuerySection
          :form="form"
          :disabled="isDisabled"
          :supports-condition-editor="supportsConditionEditor"
          :native-editor-label="nativeEditorLabel"
          :native-query-label="nativeQueryLabel"
          :native-query-placeholder="nativeQueryPlaceholder"
          :native-query-help-text="nativeQueryHelpText"
          :aggregate-field-suggestions="aggregateFieldSuggestions"
          :condition-templates="conditionTemplates"
          :query-templates="queryTemplates"
          :lookback-warning="lookbackWarning"
          :condition-group-by="conditionGroupBy"
          :is-testing-query="isTestingQuery"
          :test-query-result="testQueryResult"
          :test-query-error="testQueryError"
          :backtest-request="backtestRequest"
          :on-apply-condition-template="applyConditionTemplate"
          :on-apply-template="applyTemplate"
          :on-test-query="handleTestQuery"
        />

        <AlertScheduleSection :form="form" :disabled="isDisabled" />

        <AlertNotificationSection
          :form="form"
          :disabled="isDisabled"
          :team-members="teamMembers"
          :team-id="teamId"
          :on-add-label="addLabel"
          :on-remove-label="removeLabel"
          :on-add-annotation="addAnnotation"
          :on-remove-annotation="removeAnnotation"
        />

        <!-- Alert Status -->
        <section class="space-y-4">
          <div class="flex items-center justify-between rounded-lg border bg-muted/20 p-4">
            <div>
              <h3 class='text-sm font-medium'>{{ t('ui.alertStatus') }}</h3>
              <p class="text-xs text-muted-foreground mt-0.5">
                {{ form.is_active ? t('ui.thisAlertWillEvaluateOnSchedule') : t('ui.disabledAlertsAreSkippedUntilReEnabled') }}
              </p>
            </div>
            <Switch :aria-label="form.is_active ? t('ui.disableAlert') : t('ui.enableAlert')" :model-value="form.is_active" :disabled="isDisabled" @update:model-value="(checked) => (form.is_active = Boolean(checked))" />
          </div>
        </section>

        <DialogFooter class="pt-4">
          <Button type="button" variant="ghost" @click="handleClose" :disabled="isSubmitting">
            {{ t('ui.cancel') }}
          </Button>
          <Button type="submit" :disabled="!isValid || isDisabled">
            {{ isSubmitting ? t('ui.saving2') : mode === "create" ? t('ui.createAlert') : t('ui.saveChanges2') }}
          </Button>
        </DialogFooter>
      </form>
    </DialogContent>
  </Dialog>

  <!-- Inline mode (no dialog wrapper) -->
  <form v-else class="space-y-6" @submit.prevent="handleSubmit">
    <AlertBasicInfoSection :form="form" :disabled="isDisabled" />

    <AlertQuerySection
      :form="form"
      :disabled="isDisabled"
      :supports-condition-editor="supportsConditionEditor"
      :native-editor-label="nativeEditorLabel"
      :native-query-label="nativeQueryLabel"
      :native-query-placeholder="nativeQueryPlaceholder"
      :native-query-help-text="nativeQueryHelpText"
      :aggregate-field-suggestions="aggregateFieldSuggestions"
      :condition-templates="conditionTemplates"
      :query-templates="queryTemplates"
      :lookback-warning="lookbackWarning"
      :condition-group-by="conditionGroupBy"
      :is-testing-query="isTestingQuery"
      :test-query-result="testQueryResult"
      :test-query-error="testQueryError"
      :backtest-request="backtestRequest"
      :on-apply-condition-template="applyConditionTemplate"
      :on-apply-template="applyTemplate"
      :on-test-query="handleTestQuery"
    />

    <AlertScheduleSection :form="form" :disabled="isDisabled" />

    <AlertNotificationSection
      :form="form"
      :disabled="isDisabled"
      :team-members="teamMembers"
      :team-id="teamId"
      :on-add-label="addLabel"
      :on-remove-label="removeLabel"
      :on-add-annotation="addAnnotation"
      :on-remove-annotation="removeAnnotation"
    />

    <!-- Alert Status -->
    <section class="space-y-4">
      <div class="flex items-center justify-between rounded-lg border bg-muted/20 p-4">
        <div>
          <h3 class='text-sm font-medium'>{{ t('ui.alertStatus') }}</h3>
          <p class="text-xs text-muted-foreground mt-0.5">
            {{ form.is_active ? t('ui.thisAlertWillEvaluateOnSchedule') : t('ui.disabledAlertsAreSkippedUntilReEnabled') }}
          </p>
        </div>
        <Switch :aria-label="form.is_active ? t('ui.disableAlert') : t('ui.enableAlert')" :model-value="form.is_active" :disabled="isDisabled" @update:model-value="(checked) => (form.is_active = Boolean(checked))" />
      </div>
    </section>

    <div class="flex items-center justify-end gap-2 pt-4">
      <Button type="submit" :disabled="!isValid || isDisabled">
        {{ isSubmitting ? t('ui.saving2') : mode === "create" ? t('ui.createAlert') : t('ui.saveChanges2') }}
      </Button>
    </div>
  </form>
</template>
