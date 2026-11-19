<script setup lang="ts">
import { useI18n } from "vue-i18n";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { LOKI_SELECTOR_SUGGESTION, type LokiParser, type LokiSourceFormState } from "./sourceFormModels";

const { t } = useI18n();

const props = defineProps<{
  modelValue: LokiSourceFormState;
  isEditMode: boolean;
  validationMessage?: string | null;
  validationError?: string | null;
  isValidated?: boolean;
  isValidating?: boolean;
}>();

const emit = defineEmits<{
  "update:modelValue": [value: LokiSourceFormState];
  validate: [];
}>();

// Select has no empty value, so "none" stands for the empty parser.
const NO_PARSER = "none";

function updateForm(patch: Partial<LokiSourceFormState>) {
  emit("update:modelValue", { ...props.modelValue, ...patch });
}

function updateAuthMode(value: unknown) {
  const authMode = value === "basic" || value === "bearer" ? value : "none";
  updateForm({ authMode });
}

function updateParser(value: unknown) {
  const parser: LokiParser = value === "json" || value === "logfmt" ? value : "";
  updateForm({ parser });
}
</script>

<template>
  <div class="space-y-6">
    <section class="space-y-4 border-t pt-6">
      <div class="space-y-1">
        <h2 class="text-base font-medium">{{ t("sources.lokiConnection") }}</h2>
        <div class="text-sm text-muted-foreground">
          {{ t("sources.lokiWholeTenantNote") }}
        </div>
      </div>

      <div class="grid gap-2">
        <Label for="loki_base_url" class="required">{{ t("sources.baseURL") }}</Label>
        <Input
          id="loki_base_url"
          :model-value="modelValue.baseURL"
          :disabled="isEditMode"
          placeholder="https://loki.example.com"
          @update:model-value="(value) => updateForm({ baseURL: String(value) })"
        />
      </div>

      <div class="grid gap-2 md:max-w-sm">
        <Label for="loki_tenant_id">{{ t("sources.tenantID") }}</Label>
        <Input
          id="loki_tenant_id"
          :model-value="modelValue.tenantID"
          :disabled="isEditMode"
          placeholder="team-a"
          @update:model-value="(value) => updateForm({ tenantID: String(value) })"
        />
      </div>

      <div class="grid gap-2 md:max-w-sm">
        <Label for="loki_auth_mode">{{ t("sources.authentication") }}</Label>
        <Select :model-value="modelValue.authMode" @update:model-value="updateAuthMode">
          <SelectTrigger id="loki_auth_mode">
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
          <Label for="loki_username" class="required">{{ t("sources.username") }}</Label>
          <Input
            id="loki_username"
            :model-value="modelValue.username"
            @update:model-value="(value) => updateForm({ username: String(value) })"
          />
        </div>
        <div class="grid gap-2">
          <Label for="loki_password" :class="{ required: !isEditMode }">{{ t("sources.password") }}</Label>
          <Input
            id="loki_password"
            :model-value="modelValue.password"
            type="password"
            :placeholder="t('sources.enterPassword')"
            @update:model-value="(value) => updateForm({ password: String(value) })"
          />
          <p v-if="isEditMode" class="text-xs text-muted-foreground">{{ t("sources.keepPassword") }}</p>
        </div>
      </div>

      <div v-if="modelValue.authMode === 'bearer'" class="grid gap-2 rounded-md border border-border/60 p-4">
        <Label for="loki_token" :class="{ required: !isEditMode }">{{ t("sources.bearerToken") }}</Label>
        <Input
          id="loki_token"
          :model-value="modelValue.token"
          type="password"
          :placeholder="t('sources.enterBearerToken')"
          @update:model-value="(value) => updateForm({ token: String(value) })"
        />
        <p v-if="isEditMode" class="text-xs text-muted-foreground">{{ t("sources.keepToken") }}</p>
      </div>
    </section>

    <section class="space-y-4 border-t pt-6">
      <h2 class="text-base font-medium">{{ t("sources.lokiStreams") }}</h2>

      <div class="grid gap-2">
        <Label for="loki_selector" class="required">{{ t("sources.defaultStreamSelector") }}</Label>
        <Input
          id="loki_selector"
          class="font-mono"
          :model-value="modelValue.selector"
          :disabled="isEditMode"
          :placeholder="LOKI_SELECTOR_SUGGESTION"
          @update:model-value="(value) => updateForm({ selector: String(value) })"
        />
        <p class="text-sm text-muted-foreground">{{ t("sources.lokiSelectorDescription") }}</p>
      </div>

      <div class="grid gap-2 md:max-w-sm">
        <Label for="loki_parser">{{ t("sources.parser") }}</Label>
        <Select :model-value="modelValue.parser || NO_PARSER" @update:model-value="updateParser">
          <SelectTrigger id="loki_parser">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem :value="NO_PARSER">{{ t("sources.parserNone") }}</SelectItem>
            <SelectItem value="json">JSON</SelectItem>
            <SelectItem value="logfmt">logfmt</SelectItem>
          </SelectContent>
        </Select>
      </div>

      <div class="grid gap-2 md:max-w-sm">
        <Label for="loki_principal">{{ t("sources.accessPrincipal") }}</Label>
        <Input
          id="loki_principal"
          :model-value="modelValue.principal"
          :disabled="isEditMode"
          placeholder="payments-readonly"
          @update:model-value="(value) => updateForm({ principal: String(value) })"
        />
        <p class="text-sm text-muted-foreground">{{ t("sources.lokiPrincipalDescription") }}</p>
      </div>
    </section>

    <section class="space-y-4 border-t pt-6">
      <div class="space-y-1">
        <h2 class="text-base font-medium">{{ t("sources.fieldMapping") }}</h2>
        <div class="text-sm text-muted-foreground">{{ t("sources.fieldMappingDescription") }}</div>
      </div>
      <div class="grid gap-4 md:grid-cols-2">
        <div class="grid gap-2">
          <Label for="loki_meta_ts_field" class="required">{{ t("sources.timestampField") }}</Label>
          <Input
            id="loki_meta_ts_field"
            :model-value="modelValue.metaTSField"
            placeholder="_time"
            @update:model-value="(value) => updateForm({ metaTSField: String(value) })"
          />
        </div>
        <div class="grid gap-2">
          <Label for="loki_meta_severity_field">{{ t("sources.severityField") }}</Label>
          <Input
            id="loki_meta_severity_field"
            :model-value="modelValue.metaSeverityField"
            placeholder="detected_level"
            @update:model-value="(value) => updateForm({ metaSeverityField: String(value) })"
          />
        </div>
      </div>
    </section>

    <div v-if="!isEditMode" class="space-y-4 border-t pt-4">
      <div class="flex flex-wrap items-center justify-between gap-3">
        <div class="text-sm font-medium">{{ t("sources.validateConnection") }}</div>
        <Button
          type="button"
          variant="outline"
          size="sm"
          :disabled="isValidating || isValidated"
          @click="emit('validate')"
        >
          <span v-if="isValidated" class="mr-2">✓</span>
          {{ isValidated ? t("sources.validated") : t("sources.validateConnection") }}
        </Button>
      </div>
      <div
        v-if="validationMessage"
        class="rounded-md border border-emerald-500/30 bg-emerald-500/10 p-3 text-sm text-emerald-700 dark:text-emerald-400"
      >
        {{ validationMessage }}
      </div>
      <div
        v-if="validationError"
        class="rounded-md border border-destructive/20 bg-destructive/10 p-3 text-sm text-destructive"
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
