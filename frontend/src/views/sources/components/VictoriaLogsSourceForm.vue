<script setup lang="ts">
import { useI18n } from "vue-i18n";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { Switch } from "@/components/ui/switch";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import type { VictoriaLogsSourceFormState } from "./sourceFormModels";

const { t } = useI18n();

const props = defineProps<{
  modelValue: VictoriaLogsSourceFormState;
  isEditMode: boolean;
  validationMessage?: string | null;
  validationError?: string | null;
  isValidated?: boolean;
  isValidating?: boolean;
}>();

const emit = defineEmits<{
  "update:modelValue": [value: VictoriaLogsSourceFormState];
  validate: [];
}>();

function updateForm(patch: Partial<VictoriaLogsSourceFormState>) {
  emit("update:modelValue", { ...props.modelValue, ...patch });
}

function updateAuthMode(value: unknown) {
  const authMode = value === "basic" || value === "bearer" ? value : "none";
  updateForm({ authMode });
}
</script>

<template>
  <div class="space-y-6">
    <fieldset class="flex flex-col gap-4 rounded-md border p-4">
      <legend class="px-1 text-sm font-medium">{{ t('sources.windowedSearch') }}</legend>
      <div class="flex items-center gap-3">
        <Switch
          id="windowed_search"
          :model-value="modelValue.windowedEnabled"
          @update:model-value="value => updateForm({ windowedEnabled: value })"
        />
        <Label for="windowed_search">{{ t('sources.enableWindowedSearch') }}</Label>
      </div>
      <p class="text-sm text-muted-foreground">{{ t('sources.windowedSearchDescription') }}</p>
      <template v-if="modelValue.windowedEnabled">
        <div class="grid gap-4 md:grid-cols-2">
          <div class="flex flex-col gap-2">
            <Label for="window_seconds">{{ t('sources.maxWindowSeconds') }}</Label>
            <Input
              id="window_seconds" type="number" min="60" max="86400"
              :model-value="modelValue.maxWindowSeconds"
              @update:model-value="value => updateForm({ maxWindowSeconds: String(value) })"
            />
          </div>
          <div class="flex flex-col gap-2">
            <Label for="window_concurrency">{{ t('sources.windowConcurrency') }}</Label>
            <Input
              id="window_concurrency" type="number" min="1" max="8"
              :model-value="modelValue.concurrency"
              @update:model-value="value => updateForm({ concurrency: String(value) })"
            />
          </div>
          <div class="flex flex-col gap-2">
            <Label for="sidebar_lookback">{{ t('sources.sidebarLookbackSeconds') }}</Label>
            <Input
              id="sidebar_lookback" type="number" min="60" max="10800"
              :model-value="modelValue.sidebarLookbackSeconds"
              @update:model-value="value => updateForm({ sidebarLookbackSeconds: String(value) })"
            />
          </div>
          <div class="flex flex-col gap-2">
            <Label for="schema_lookback">{{ t('sources.schemaLookbackSeconds') }}</Label>
            <Input
              id="schema_lookback" type="number" min="60" max="86400"
              :model-value="modelValue.schemaLookbackSeconds"
              @update:model-value="value => updateForm({ schemaLookbackSeconds: String(value) })"
            />
          </div>
          <div class="flex flex-col gap-2">
            <Label for="sidebar_cap">{{ t('sources.sidebarValuesCap') }}</Label>
            <Input
              id="sidebar_cap" type="number" min="100" max="100000"
              :model-value="modelValue.sidebarValuesCap"
              @update:model-value="value => updateForm({ sidebarValuesCap: String(value) })"
            />
          </div>
        </div>
        <div class="flex flex-col gap-2">
          <Label for="stream_fields">{{ t('sources.streamFields') }}</Label>
          <Input
            id="stream_fields" :model-value="modelValue.streamFields"
            @update:model-value="value => updateForm({ streamFields: String(value) })"
          />
          <p class="text-xs text-muted-foreground">{{ t('sources.streamFieldsDescription') }}</p>
        </div>
        <div class="flex items-center gap-3">
          <Switch
            id="window_histogram" :model-value="modelValue.histogramEnabled"
            @update:model-value="value => updateForm({ histogramEnabled: value })"
          />
          <Label for="window_histogram">{{ t('sources.windowHistogram') }}</Label>
        </div>
      </template>
    </fieldset>
    <div class="space-y-4">
      <div class="flex items-center justify-between">
        <h3 class="text-lg font-medium">{{ t("sources.victoriaLogsConnection") }}</h3>
        <div class="text-sm text-muted-foreground">
          {{ t("sources.victoriaLogsConnectionDescription") }}
        </div>
      </div>

      <div class="grid gap-2">
        <Label for="victorialogs_base_url" class="required">{{ t("sources.baseURL") }}</Label>
        <Input
          id="victorialogs_base_url"
          :model-value="modelValue.baseURL"
          placeholder="https://logs.example.com"
          @update:model-value="(value) => updateForm({ baseURL: String(value) })"
        />
        <p class="text-sm text-muted-foreground">
          {{ t("sources.baseURLDescription") }}
        </p>
      </div>

      <div class="grid gap-2 md:max-w-sm">
        <Label for="victorialogs_auth_mode">{{ t("sources.authentication") }}</Label>
        <Select
          :model-value="modelValue.authMode"
          @update:model-value="updateAuthMode"
        >
          <SelectTrigger id="victorialogs_auth_mode">
            <SelectValue :placeholder="t('sources.selectAuthMode')" />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="none">{{ t("sources.noAuth") }}</SelectItem>
            <SelectItem value="basic">{{ t("sources.basicAuth") }}</SelectItem>
            <SelectItem value="bearer">{{ t("sources.bearerToken") }}</SelectItem>
          </SelectContent>
        </Select>
      </div>

      <div v-if="modelValue.authMode === 'basic'" class="grid gap-4 rounded-md border border-border/60 p-4 md:grid-cols-2">
        <div class="grid gap-2">
          <Label for="victorialogs_username" class="required">{{ t("sources.username") }}</Label>
          <Input
            id="victorialogs_username"
            :model-value="modelValue.username"
            placeholder="logchef"
            @update:model-value="(value) => updateForm({ username: String(value) })"
          />
        </div>

        <div class="grid gap-2">
          <Label for="victorialogs_password" :class="{ required: !isEditMode }">{{ t("sources.password") }}</Label>
          <Input
            id="victorialogs_password"
            :model-value="modelValue.password"
            type="password"
            :placeholder="t('sources.enterPassword')"
            @update:model-value="(value) => updateForm({ password: String(value) })"
          />
          <p v-if="isEditMode" class="text-xs text-muted-foreground">
            {{ t("sources.keepPassword") }}
          </p>
        </div>
      </div>

      <div v-if="modelValue.authMode === 'bearer'" class="grid gap-2 rounded-md border border-border/60 p-4">
        <Label for="victorialogs_token" :class="{ required: !isEditMode }">{{ t("sources.bearerToken") }}</Label>
        <Input
          id="victorialogs_token"
          :model-value="modelValue.token"
          type="password"
          :placeholder="t('sources.enterBearerToken')"
          @update:model-value="(value) => updateForm({ token: String(value) })"
        />
        <p v-if="isEditMode" class="text-xs text-muted-foreground">
          {{ t("sources.keepToken") }}
        </p>
      </div>
    </div>

    <div class="space-y-4">
      <div class="flex items-center justify-between">
        <h3 class="text-lg font-medium">{{ t("sources.tenantScope") }}</h3>
        <div class="text-sm text-muted-foreground">
          {{ t("sources.tenantScopeDescription") }}
        </div>
      </div>

      <div class="grid gap-4 md:grid-cols-2">
        <div class="grid gap-2">
          <Label for="victorialogs_account_id">{{ t("sources.accountID") }}</Label>
          <Input
            id="victorialogs_account_id"
            :model-value="modelValue.accountID"
            placeholder="12"
            @update:model-value="(value) => updateForm({ accountID: String(value) })"
          />
        </div>

        <div class="grid gap-2">
          <Label for="victorialogs_project_id">{{ t("sources.projectID") }}</Label>
          <Input
            id="victorialogs_project_id"
            :model-value="modelValue.projectID"
            placeholder="34"
            @update:model-value="(value) => updateForm({ projectID: String(value) })"
          />
        </div>
      </div>
      <p class="text-sm text-muted-foreground">
        {{ t("sources.accountProjectValidation") }}
      </p>

      <div class="grid gap-2">
        <Label for="victorialogs_scope_query">{{ t("sources.immutableScopeQuery") }}</Label>
        <Textarea
          id="victorialogs_scope_query"
          :model-value="modelValue.scopeQuery"
          placeholder='{app="payments"} kubernetes.namespace:=prod'
          rows="3"
          @update:model-value="(value) => updateForm({ scopeQuery: String(value) })"
        />
        <p class="text-sm text-muted-foreground">
          {{ t("sources.scopeAppliedToQueries") }} <code>{app="payments"}</code> {{ t("sources.or") }} <code>kubernetes.namespace:="prod"</code>.
        </p>
      </div>
    </div>

    <div class="space-y-4">
      <div class="flex items-center justify-between">
        <h3 class="text-lg font-medium">{{ t("sources.fieldMapping") }}</h3>
        <div class="text-sm text-muted-foreground">
          {{ t("sources.fieldMappingDescription") }}
        </div>
      </div>

      <div class="grid gap-4 md:grid-cols-2">
        <div class="grid gap-2">
          <Label for="victorialogs_meta_ts_field" class="required">{{ t("sources.timestampField") }}</Label>
          <Input
            id="victorialogs_meta_ts_field"
            :model-value="modelValue.metaTSField"
            placeholder="_time"
            @update:model-value="(value) => updateForm({ metaTSField: String(value) })"
          />
        </div>

        <div class="grid gap-2">
          <Label for="victorialogs_meta_severity_field">{{ t("sources.severityField") }}</Label>
          <Input
            id="victorialogs_meta_severity_field"
            :model-value="modelValue.metaSeverityField"
            placeholder="level"
            @update:model-value="(value) => updateForm({ metaSeverityField: String(value) })"
          />
        </div>
      </div>
    </div>

    <div v-if="!isEditMode" class="space-y-4 border-t pt-4">
      <div class="flex items-center justify-between">
        <div class="text-sm font-medium">{{ t("sources.validateConnection") }}</div>
        <Button
          type="button"
          variant="outline"
          size="sm"
          :disabled="isValidating || isValidated"
          @click="emit('validate')"
        >
          <span v-if="isValidating" class="mr-2">
            <svg class="h-4 w-4 animate-spin text-primary" xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24">
              <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
              <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
            </svg>
          </span>
          <span v-else-if="isValidated" class="mr-2">✓</span>
          {{ isValidated ? t("sources.validated") : t("sources.validateConnection") }}
        </Button>
      </div>

      <div
        v-if="validationMessage"
        class="rounded-md border border-green-200 bg-green-50 p-3 text-sm text-green-800"
      >
        {{ validationMessage }}
      </div>

      <div
        v-if="validationError"
        class="rounded-md border border-red-200 bg-red-50 p-3 text-sm text-red-800"
      >
        {{ validationError }}
      </div>
    </div>
  </div>
</template>

<style scoped>
.required::after {
  content: " *";
  color: hsl(var(--destructive));
}
</style>
