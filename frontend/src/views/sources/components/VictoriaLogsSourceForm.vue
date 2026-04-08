<script setup lang="ts">
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import type { VictoriaLogsSourceFormState } from "./sourceFormModels";

const props = defineProps<{
  modelValue: VictoriaLogsSourceFormState;
  isEditMode: boolean;
  validationMessage?: string | null;
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

function updateAuthMode(value: string) {
  const authMode = value === "basic" || value === "bearer" ? value : "none";
  updateForm({ authMode });
}
</script>

<template>
  <div class="space-y-6">
    <div class="space-y-4">
      <div class="flex items-center justify-between">
        <h3 class="text-lg font-medium">VictoriaLogs Connection</h3>
        <div class="text-sm text-muted-foreground">
          Configure the VictoriaLogs API endpoint and tenant scope
        </div>
      </div>

      <div class="grid gap-2">
        <Label for="victorialogs_base_url" class="required">Base URL</Label>
        <Input
          id="victorialogs_base_url"
          :model-value="modelValue.baseURL"
          placeholder="https://logs.example.com"
          @update:model-value="(value) => updateForm({ baseURL: String(value) })"
        />
        <p class="text-sm text-muted-foreground">
          Base VictoriaLogs endpoint, including scheme and optional path prefix.
        </p>
      </div>

      <div class="grid gap-2 md:max-w-sm">
        <Label for="victorialogs_auth_mode">Authentication</Label>
        <Select
          :model-value="modelValue.authMode"
          @update:model-value="updateAuthMode"
        >
          <SelectTrigger id="victorialogs_auth_mode">
            <SelectValue placeholder="Select auth mode" />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="none">No Auth</SelectItem>
            <SelectItem value="basic">Basic Auth</SelectItem>
            <SelectItem value="bearer">Bearer Token</SelectItem>
          </SelectContent>
        </Select>
      </div>

      <div v-if="modelValue.authMode === 'basic'" class="grid gap-4 rounded-md border border-border/60 p-4 md:grid-cols-2">
        <div class="grid gap-2">
          <Label for="victorialogs_username" class="required">Username</Label>
          <Input
            id="victorialogs_username"
            :model-value="modelValue.username"
            placeholder="logchef"
            @update:model-value="(value) => updateForm({ username: String(value) })"
          />
        </div>

        <div class="grid gap-2">
          <Label for="victorialogs_password" :class="{ required: !isEditMode }">Password</Label>
          <Input
            id="victorialogs_password"
            :model-value="modelValue.password"
            type="password"
            placeholder="Enter password"
            @update:model-value="(value) => updateForm({ password: String(value) })"
          />
          <p v-if="isEditMode" class="text-xs text-muted-foreground">
            Leave blank to keep the existing password unchanged.
          </p>
        </div>
      </div>

      <div v-if="modelValue.authMode === 'bearer'" class="grid gap-2 rounded-md border border-border/60 p-4">
        <Label for="victorialogs_token" :class="{ required: !isEditMode }">Bearer Token</Label>
        <Input
          id="victorialogs_token"
          :model-value="modelValue.token"
          type="password"
          placeholder="Enter bearer token"
          @update:model-value="(value) => updateForm({ token: String(value) })"
        />
        <p v-if="isEditMode" class="text-xs text-muted-foreground">
          Leave blank to keep the existing token unchanged.
        </p>
      </div>
    </div>

    <div class="space-y-4">
      <div class="flex items-center justify-between">
        <h3 class="text-lg font-medium">Tenant Scope</h3>
        <div class="text-sm text-muted-foreground">
          Optional multi-tenant and source-scoped filters
        </div>
      </div>

      <div class="grid gap-4 md:grid-cols-2">
        <div class="grid gap-2">
          <Label for="victorialogs_account_id">Account ID</Label>
          <Input
            id="victorialogs_account_id"
            :model-value="modelValue.accountID"
            placeholder="12"
            @update:model-value="(value) => updateForm({ accountID: String(value) })"
          />
        </div>

        <div class="grid gap-2">
          <Label for="victorialogs_project_id">Project ID</Label>
          <Input
            id="victorialogs_project_id"
            :model-value="modelValue.projectID"
            placeholder="34"
            @update:model-value="(value) => updateForm({ projectID: String(value) })"
          />
        </div>
      </div>

      <div class="grid gap-2">
        <Label for="victorialogs_scope_query">Immutable Scope Query</Label>
        <Textarea
          id="victorialogs_scope_query"
          :model-value="modelValue.scopeQuery"
          placeholder='{app="payments"} kubernetes.namespace:=prod'
          rows="3"
          @update:model-value="(value) => updateForm({ scopeQuery: String(value) })"
        />
        <p class="text-sm text-muted-foreground">
          This scope is prepended server-side to every query for the datasource.
        </p>
      </div>
    </div>

    <div class="space-y-4">
      <div class="flex items-center justify-between">
        <h3 class="text-lg font-medium">Field Mapping</h3>
        <div class="text-sm text-muted-foreground">
          Tell LogChef which fields represent time and severity
        </div>
      </div>

      <div class="grid gap-4 md:grid-cols-2">
        <div class="grid gap-2">
          <Label for="victorialogs_meta_ts_field" class="required">Timestamp Field</Label>
          <Input
            id="victorialogs_meta_ts_field"
            :model-value="modelValue.metaTSField"
            placeholder="_time"
            @update:model-value="(value) => updateForm({ metaTSField: String(value) })"
          />
        </div>

        <div class="grid gap-2">
          <Label for="victorialogs_meta_severity_field">Severity Field</Label>
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
        <div class="text-sm font-medium">Validate Connection</div>
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
          {{ isValidated ? "Validated" : "Validate Connection" }}
        </Button>
      </div>

      <div
        v-if="validationMessage"
        class="rounded-md border border-green-200 bg-green-50 p-3 text-sm text-green-800"
      >
        {{ validationMessage }}
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
