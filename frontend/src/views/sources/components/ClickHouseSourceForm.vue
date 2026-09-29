<script setup lang="ts">
import { computed, ref } from "vue";
import { useI18n } from "vue-i18n";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";
import { Separator } from "@/components/ui/separator";
import {
  Accordion,
  AccordionContent,
  AccordionItem,
  AccordionTrigger,
} from "@/components/ui/accordion";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog";
import { Code, ChevronsUpDown, Database, Plus } from "lucide-vue-next";
import type {
  ClickHouseSettingsFormState,
  ClickHouseSourceFormState,
} from "./sourceFormModels";
import { generateClickHouseSchema } from "./sourceFormModels";

const props = defineProps<{
  modelValue: ClickHouseSourceFormState;
  isEditMode: boolean;
  validationMessage?: string | null;
  validationError?: string | null;
  isValidated?: boolean;
  isValidating?: boolean;
}>();

const { t } = useI18n();

const emit = defineEmits<{
  "update:modelValue": [value: ClickHouseSourceFormState];
  validate: [];
}>();

const isEditingSchema = ref(false);

function updateForm(patch: Partial<ClickHouseSourceFormState>) {
  emit("update:modelValue", { ...props.modelValue, ...patch });
}

function updateSettings(patch: Partial<ClickHouseSettingsFormState>) {
  updateForm({ settings: { ...props.modelValue.settings, ...patch } });
}

// Sanitize a number input so blanks stay blank (unset) and negatives are
// clamped to "" rather than sending a negative value to the backend.
function sanitizeNonNegative(value: unknown): string {
  const raw = String(value ?? "").trim();
  if (raw === "") {
    return "";
  }
  const parsed = Number(raw);
  if (!Number.isFinite(parsed) || parsed < 0) {
    return "";
  }
  return raw;
}

function updateTableMode(value: unknown) {
  updateForm({ tableMode: value === "connect" ? "connect" : "create" });
}

const generatedSchema = computed(() => generateClickHouseSchema(props.modelValue));
const actualSchema = computed(() => props.modelValue.schema || generatedSchema.value);
const editableSchema = computed({
  get: () => actualSchema.value,
  set: (value: string) => updateForm({ schema: value }),
});

const validateButtonText = computed(() => {
  if (props.isValidating) {
    return t("sources.validating");
  }
  return props.modelValue.tableMode === "connect"
    ? t("sources.validateConnectionColumns")
    : t("sources.validateConnection");
});

function resetSchema() {
  updateForm({ schema: "" });
  isEditingSchema.value = false;
}
</script>

<template>
  <div class="space-y-6">
    <div class="space-y-4">
      <div class="flex items-center justify-between">
        <h3 class="text-lg font-medium">{{ t("sources.clickHouseConnection") }}</h3>
        <div class="text-sm text-muted-foreground">
          {{ t("sources.clickHouseConnectionDescription") }}
        </div>
      </div>

      <div class="grid gap-2">
        <Label for="host" class="required">{{ t("sources.hostAndPort") }}</Label>
        <Input
          id="host"
          :model-value="modelValue.host"
          placeholder="localhost:9000"
          @update:model-value="(value) => updateForm({ host: String(value) })"
        />
        <p class="text-sm text-muted-foreground">
          {{ t("sources.hostAndPortDescription") }}
        </p>
      </div>

      <div class="grid grid-cols-2 gap-4">
        <div class="grid gap-2">
          <Label for="database" class="required">{{ t("sources.database") }}</Label>
          <Input
            id="database"
            :model-value="modelValue.database"
            placeholder="default"
            @update:model-value="(value) => updateForm({ database: String(value) })"
          />
        </div>

        <div class="grid gap-2">
          <Label for="table_name" class="required">{{ t("sources.tableName") }}</Label>
          <Input
            id="table_name"
            :model-value="modelValue.tableName"
            placeholder="app_logs"
            @update:model-value="(value) => updateForm({ tableName: String(value) })"
          />
        </div>
      </div>
      <p class="text-sm text-muted-foreground">
        {{ t("sources.databaseTableDescription") }}
      </p>

      <div class="space-y-4">
        <div class="flex items-center justify-between rounded-md bg-muted/50 p-3">
          <div class="space-y-0.5">
            <Label class="text-base">{{ t("sources.authentication") }}</Label>
            <p class="text-sm text-muted-foreground">
              {{ t("sources.clickHouseAuthenticationDescription") }}
            </p>
          </div>
          <Switch
            :model-value="modelValue.enableAuth"
            @update:model-value="(checked: boolean) => updateForm({ enableAuth: checked })"
          />
        </div>

        <div
          v-show="modelValue.enableAuth"
          class="grid gap-4 border-l-2 border-primary/20 pl-3 md:grid-cols-2"
        >
          <div class="grid gap-2">
            <Label for="username" class="required">{{ t("sources.username") }}</Label>
            <Input
              id="username"
              :model-value="modelValue.username"
              placeholder="default"
              @update:model-value="(value) => updateForm({ username: String(value) })"
            />
          </div>

          <div class="grid gap-2">
            <Label for="password" :class="{ required: !isEditMode }">{{ t("sources.password") }}</Label>
            <Input
              id="password"
              :model-value="modelValue.password"
              type="password"
              :placeholder="isEditMode ? t('sources.keepCurrentPassword') : ''"
              @update:model-value="(value) => updateForm({ password: String(value) })"
            />
          </div>
        </div>
      </div>
    </div>

    <Accordion type="single" collapsible class="w-full">
      <AccordionItem value="advanced-query-settings" class="border rounded-md px-4">
        <AccordionTrigger class="text-base font-medium">
          {{ t("sources.advancedQuerySettings") }}
        </AccordionTrigger>
        <AccordionContent class="space-y-4 pt-2">
          <p class="text-sm text-muted-foreground">
            {{ t("sources.advancedQuerySettingsDescription") }}
          </p>

          <div class="grid gap-4 md:grid-cols-2">
            <div class="grid gap-2">
              <Label for="ch_max_execution_time">{{ t("sources.maxExecutionTime") }}</Label>
              <Input
                id="ch_max_execution_time"
                :model-value="modelValue.settings.maxExecutionTime"
                type="number"
                min="0"
                :placeholder="t('sources.unset')"
                @update:model-value="(value) => updateSettings({ maxExecutionTime: sanitizeNonNegative(value) })"
              />
            </div>

            <div class="grid gap-2">
              <Label for="ch_max_result_rows">{{ t("sources.maxResultRows") }}</Label>
              <Input
                id="ch_max_result_rows"
                :model-value="modelValue.settings.maxResultRows"
                type="number"
                min="0"
                :placeholder="t('sources.unset')"
                @update:model-value="(value) => updateSettings({ maxResultRows: sanitizeNonNegative(value) })"
              />
            </div>

            <div class="grid gap-2">
              <Label for="ch_max_result_bytes">{{ t("sources.maxResultBytes") }}</Label>
              <Input
                id="ch_max_result_bytes"
                :model-value="modelValue.settings.maxResultBytes"
                type="number"
                min="0"
                :placeholder="t('sources.unset')"
                @update:model-value="(value) => updateSettings({ maxResultBytes: sanitizeNonNegative(value) })"
              />
            </div>

            <div class="grid gap-2">
              <Label for="ch_max_rows_to_read">{{ t("sources.maxRowsToRead") }}</Label>
              <Input
                id="ch_max_rows_to_read"
                :model-value="modelValue.settings.maxRowsToRead"
                type="number"
                min="0"
                :placeholder="t('sources.unset')"
                @update:model-value="(value) => updateSettings({ maxRowsToRead: sanitizeNonNegative(value) })"
              />
            </div>

            <div class="grid gap-2">
              <Label for="ch_max_bytes_to_read">{{ t("sources.maxBytesToRead") }}</Label>
              <Input
                id="ch_max_bytes_to_read"
                :model-value="modelValue.settings.maxBytesToRead"
                type="number"
                min="0"
                :placeholder="t('sources.unset')"
                @update:model-value="(value) => updateSettings({ maxBytesToRead: sanitizeNonNegative(value) })"
              />
            </div>
          </div>

          <div class="grid gap-4 md:grid-cols-2">
            <div class="grid gap-2">
              <Label for="ch_result_overflow_mode">{{ t("sources.resultOverflowMode") }}</Label>
              <Select
                :model-value="modelValue.settings.resultOverflowMode || 'default'"
                @update:model-value="(value) => updateSettings({ resultOverflowMode: value === 'default' ? '' : String(value) })"
              >
                <SelectTrigger id="ch_result_overflow_mode">
                  <SelectValue :placeholder="t('sources.default')" />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="default">{{ t("sources.default") }}</SelectItem>
                  <SelectItem value="throw">{{ t("sources.throwError") }}</SelectItem>
                  <SelectItem value="break">{{ t("sources.truncate") }}</SelectItem>
                </SelectContent>
              </Select>
              <p class="text-sm text-muted-foreground">
                {{ t("sources.resultOverflowDescription") }}
              </p>
            </div>

            <div class="grid gap-2">
              <Label for="ch_readonly">{{ t("sources.readOnlyMode") }}</Label>
              <Select
                :model-value="modelValue.settings.readonly || 'default'"
                @update:model-value="(value) => updateSettings({ readonly: value === 'default' ? '' : String(value) })"
              >
                <SelectTrigger id="ch_readonly">
                  <SelectValue :placeholder="t('sources.default')" />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="default">{{ t("sources.default") }}</SelectItem>
                  <SelectItem value="2">{{ t("sources.readOnly2") }}</SelectItem>
                </SelectContent>
              </Select>
              <p class="text-sm text-muted-foreground">
                {{ t("sources.readOnlyDescription") }}
              </p>
            </div>
          </div>
        </AccordionContent>
      </AccordionItem>
    </Accordion>

    <div v-if="!isEditMode" class="space-y-4">
      <div class="flex items-center justify-between">
          <h3 class="text-lg font-medium">{{ t("sources.tableConfiguration") }}</h3>
        <div class="text-sm text-muted-foreground">
          {{ t("sources.tableConfigurationDescription") }}
        </div>
      </div>

      <RadioGroup
        :model-value="modelValue.tableMode"
        class="grid grid-cols-[1fr_auto_1fr] items-start gap-4"
        @update:model-value="updateTableMode"
      >
        <Card
          :class="{ 'border-primary shadow-sm': modelValue.tableMode === 'create', 'border-muted-foreground/20': modelValue.tableMode !== 'create' }"
          class="cursor-pointer transition-all hover:border-primary/70"
          @click="updateForm({ tableMode: 'create' })"
        >
          <CardHeader>
            <div class="flex items-center gap-2">
              <RadioGroupItem value="create" id="create" />
              <Label for="create" class="cursor-pointer font-medium">{{ t("sources.createNewTable") }}</Label>
            </div>
          </CardHeader>
          <CardContent class="space-y-4">
            <div class="flex items-start gap-4">
              <Plus class="mt-1 h-5 w-5 text-muted-foreground" />
              <div class="space-y-1">
                <p class="text-sm font-medium">{{ t("sources.letLogChefCreateTable") }}</p>
                <p class="text-sm text-muted-foreground">
                  {{ t("sources.defaultSchemaDescription") }}
                </p>
              </div>
            </div>

            <div class="mt-4 grid gap-2 border-t pt-4">
              <Label for="ttl_days">{{ t("sources.ttlDays") }}</Label>
              <Input
                id="ttl_days"
                :model-value="modelValue.ttlDays"
                type="number"
                min="1"
                @update:model-value="(value) => updateForm({ ttlDays: String(value) })"
              />
              <p class="text-sm text-muted-foreground">
                {{ t("sources.retentionDaysDescription") }}
              </p>
            </div>

            <Dialog>
              <DialogTrigger as-child>
                <Button variant="outline" class="flex w-full items-center justify-between">
                  <div class="flex items-center gap-2">
                    <Code class="h-4 w-4" />
                    <span>{{ t("sources.viewGeneratedSchema") }}</span>
                  </div>
                  <ChevronsUpDown class="h-4 w-4" />
                </Button>
              </DialogTrigger>
              <DialogContent class="sm:max-w-[800px]">
                <DialogHeader>
                  <DialogTitle>{{ t("sources.tableSchema") }}</DialogTitle>
                  <DialogDescription>
                    {{ t("sources.reviewTableSchema") }}
                  </DialogDescription>
                </DialogHeader>

                <div class="space-y-4 py-4">
                  <div class="flex items-center justify-between">
                    <div class="space-y-1">
                      <h4 class="text-sm font-medium leading-none">{{ t("sources.schemaDefinition") }}</h4>
                      <p class="text-sm text-muted-foreground">
                        {{ t("sources.schemaCreateOnly") }}
                      </p>
                    </div>
                    <div class="flex items-center gap-2">
                      <Button variant="outline" size="sm" :disabled="!modelValue.schema" @click="resetSchema">
                        {{ t("sources.resetToDefault") }}
                      </Button>
                      <Button variant="outline" size="sm" @click="isEditingSchema = !isEditingSchema">
                        {{ isEditingSchema ? t("sources.preview") : t("sources.edit") }}
                      </Button>
                    </div>
                  </div>

                  <div v-if="!isEditingSchema" class="rounded-md bg-muted p-4">
                    <pre class="whitespace-pre-wrap text-sm text-muted-foreground">{{ actualSchema }}</pre>
                  </div>
                  <Textarea
                    v-else
                    v-model="editableSchema"
                    class="font-mono text-sm"
                    rows="20"
                  />
                </div>
              </DialogContent>
            </Dialog>
          </CardContent>
        </Card>

        <div class="flex h-full flex-col items-center justify-center">
          <div class="flex flex-col items-center gap-2">
            <Separator orientation="vertical" class="h-8" />
            <span class="px-4 text-sm text-muted-foreground">{{ t("sources.or") }}</span>
            <Separator orientation="vertical" class="h-8" />
          </div>
        </div>

        <Card
          :class="{ 'border-primary shadow-sm': modelValue.tableMode === 'connect', 'border-muted-foreground/20': modelValue.tableMode !== 'connect' }"
          class="cursor-pointer transition-all hover:border-primary/70"
          @click="updateForm({ tableMode: 'connect' })"
        >
          <CardHeader>
            <div class="flex items-center gap-2">
              <RadioGroupItem value="connect" id="connect" />
              <Label for="connect" class="cursor-pointer font-medium">{{ t("sources.connectExistingTable") }}</Label>
            </div>
          </CardHeader>
          <CardContent class="space-y-4">
            <div class="flex items-start gap-4">
              <Database class="mt-1 h-5 w-5 text-muted-foreground" />
              <div class="space-y-1">
                <p class="text-sm font-medium">{{ t("sources.useExistingTable") }}</p>
                <p class="text-sm text-muted-foreground">
                  {{ t("sources.existingTableDescription") }}
                </p>
              </div>
            </div>

            <div v-if="modelValue.tableMode === 'connect'" class="mt-4 space-y-4 border-t pt-4">
              <div class="grid gap-2">
                <Label for="meta_ts_field" class="required">{{ t("sources.timestampFieldName") }}</Label>
                <Input
                  id="meta_ts_field"
                  :model-value="modelValue.metaTSField"
                  placeholder="timestamp"
                  @update:model-value="(value) => updateForm({ metaTSField: String(value) })"
                />
              </div>

              <div class="grid gap-2">
                <Label for="meta_severity_field">{{ t("sources.severityFieldName") }}</Label>
                <Input
                  id="meta_severity_field"
                  :model-value="modelValue.metaSeverityField"
                  placeholder="severity_text"
                  @update:model-value="(value) => updateForm({ metaSeverityField: String(value) })"
                />
              </div>
            </div>
          </CardContent>
        </Card>
      </RadioGroup>
    </div>

    <div v-if="isEditMode" class="space-y-4">
      <div class="flex items-center justify-between">
          <h3 class="text-lg font-medium">{{ t("sources.dataRetention") }}</h3>
        <div class="text-sm text-muted-foreground">
          {{ t("sources.dataRetentionDescription") }}
        </div>
      </div>
      <div class="grid gap-2">
        <Label for="ttl_days_edit">{{ t("sources.ttlDays") }}</Label>
        <Input
          id="ttl_days_edit"
          :model-value="modelValue.ttlDays"
          type="number"
          min="1"
          class="max-w-xs"
          @update:model-value="(value) => updateForm({ ttlDays: String(value) })"
        />
      </div>
    </div>

    <div v-if="!isEditMode && modelValue.tableMode === 'connect'" class="space-y-4 border-t pt-4">
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
          {{ isValidated ? t("sources.validated") : validateButtonText }}
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
