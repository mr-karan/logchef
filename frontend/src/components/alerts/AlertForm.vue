<script setup lang="ts">
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
          {{ mode === "create" ? "Create alert" : "Edit alert" }}
        </DialogTitle>
        <DialogDescription>
          Configure the evaluation query, thresholds, and delivery targets for this alert rule.
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
          :generated-query-language-label="generatedQueryLanguageLabel"
          :aggregate-field-suggestions="aggregateFieldSuggestions"
          :condition-templates="conditionTemplates"
          :query-templates="queryTemplates"
          :condition-error="conditionError"
          :generated-query="generatedQuery"
          :is-testing-query="isTestingQuery"
          :test-query-result="testQueryResult"
          :test-query-error="testQueryError"
          :on-apply-condition-template="applyConditionTemplate"
          :on-apply-template="applyTemplate"
          :on-test-query="handleTestQuery"
        />

        <AlertScheduleSection :form="form" :disabled="isDisabled" />

        <AlertNotificationSection
          :form="form"
          :disabled="isDisabled"
          :team-members="teamMembers"
          v-model:new-webhook-url="newWebhookUrl"
          :on-add-recipient="addRecipient"
          :on-remove-recipient="removeRecipient"
          :on-add-webhook="addWebhook"
          :on-remove-webhook="removeWebhook"
          :on-add-label="addLabel"
          :on-remove-label="removeLabel"
          :on-add-annotation="addAnnotation"
          :on-remove-annotation="removeAnnotation"
        />

        <!-- Alert Status -->
        <section class="space-y-4">
          <div class="flex items-center justify-between rounded-lg border bg-muted/20 p-4">
            <div>
              <h3 class="text-sm font-medium">Alert status</h3>
              <p class="text-xs text-muted-foreground mt-0.5">
                {{ form.is_active ? "This alert will evaluate on schedule" : "Disabled alerts are skipped until re-enabled" }}
              </p>
            </div>
            <Switch :model-value="form.is_active" :disabled="isDisabled" @update:model-value="(checked) => (form.is_active = Boolean(checked))" />
          </div>
        </section>

        <DialogFooter class="pt-4">
          <Button type="button" variant="ghost" @click="handleClose" :disabled="isSubmitting">
            Cancel
          </Button>
          <Button type="submit" :disabled="!isValid || isDisabled">
            {{ isSubmitting ? "Saving..." : mode === "create" ? "Create alert" : "Save changes" }}
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
      :generated-query-language-label="generatedQueryLanguageLabel"
      :aggregate-field-suggestions="aggregateFieldSuggestions"
      :condition-templates="conditionTemplates"
      :query-templates="queryTemplates"
      :condition-error="conditionError"
      :generated-query="generatedQuery"
      :is-testing-query="isTestingQuery"
      :test-query-result="testQueryResult"
      :test-query-error="testQueryError"
      :on-apply-condition-template="applyConditionTemplate"
      :on-apply-template="applyTemplate"
      :on-test-query="handleTestQuery"
    />

    <AlertScheduleSection :form="form" :disabled="isDisabled" />

    <AlertNotificationSection
      :form="form"
      :disabled="isDisabled"
      :team-members="teamMembers"
      v-model:new-webhook-url="newWebhookUrl"
      :on-add-recipient="addRecipient"
      :on-remove-recipient="removeRecipient"
      :on-add-webhook="addWebhook"
      :on-remove-webhook="removeWebhook"
      :on-add-label="addLabel"
      :on-remove-label="removeLabel"
      :on-add-annotation="addAnnotation"
      :on-remove-annotation="removeAnnotation"
    />

    <!-- Alert Status -->
    <section class="space-y-4">
      <div class="flex items-center justify-between rounded-lg border bg-muted/20 p-4">
        <div>
          <h3 class="text-sm font-medium">Alert status</h3>
          <p class="text-xs text-muted-foreground mt-0.5">
            {{ form.is_active ? "This alert will evaluate on schedule" : "Disabled alerts are skipped until re-enabled" }}
          </p>
        </div>
        <Switch :model-value="form.is_active" :disabled="isDisabled" @update:model-value="(checked) => (form.is_active = Boolean(checked))" />
      </div>
    </section>

    <div class="flex items-center justify-end gap-2 pt-4">
      <Button type="submit" :disabled="!isValid || isDisabled">
        {{ isSubmitting ? "Saving..." : mode === "create" ? "Create alert" : "Save changes" }}
      </Button>
    </div>
  </form>
</template>
