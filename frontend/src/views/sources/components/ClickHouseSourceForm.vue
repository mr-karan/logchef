<script setup lang="ts">
import { computed, ref } from "vue";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";
import { Separator } from "@/components/ui/separator";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog";
import { Code, ChevronsUpDown, Database, Plus } from "lucide-vue-next";
import type { ClickHouseSourceFormState } from "./sourceFormModels";
import { generateClickHouseSchema } from "./sourceFormModels";

const props = defineProps<{
  modelValue: ClickHouseSourceFormState;
  isEditMode: boolean;
  validationMessage?: string | null;
  validationError?: string | null;
  isValidated?: boolean;
  isValidating?: boolean;
}>();

const emit = defineEmits<{
  "update:modelValue": [value: ClickHouseSourceFormState];
  validate: [];
}>();

const isEditingSchema = ref(false);

function updateForm(patch: Partial<ClickHouseSourceFormState>) {
  emit("update:modelValue", { ...props.modelValue, ...patch });
}

function updateTableMode(value: string) {
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
    return "Validating...";
  }
  return props.modelValue.tableMode === "connect"
    ? "Validate Connection & Columns"
    : "Validate Connection";
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
        <h3 class="text-lg font-medium">ClickHouse Connection</h3>
        <div class="text-sm text-muted-foreground">
          Configure ClickHouse host, table, and auth
        </div>
      </div>

      <div class="grid gap-2">
        <Label for="host" class="required">Host and Port</Label>
        <Input
          id="host"
          :model-value="modelValue.host"
          placeholder="localhost:9000"
          @update:model-value="(value) => updateForm({ host: String(value) })"
        />
        <p class="text-sm text-muted-foreground">
          Enter the ClickHouse server host and port in `host:port` format.
        </p>
      </div>

      <div class="grid grid-cols-2 gap-4">
        <div class="grid gap-2">
          <Label for="database" class="required">Database</Label>
          <Input
            id="database"
            :model-value="modelValue.database"
            placeholder="default"
            @update:model-value="(value) => updateForm({ database: String(value) })"
          />
        </div>

        <div class="grid gap-2">
          <Label for="table_name" class="required">Table Name</Label>
          <Input
            id="table_name"
            :model-value="modelValue.tableName"
            placeholder="app_logs"
            @update:model-value="(value) => updateForm({ tableName: String(value) })"
          />
        </div>
      </div>
      <p class="text-sm text-muted-foreground">
        The database and table where LogChef reads or writes log data in ClickHouse.
      </p>

      <div class="space-y-4">
        <div class="flex items-center justify-between rounded-md bg-muted/50 p-3">
          <div class="space-y-0.5">
            <Label class="text-base">Authentication</Label>
            <p class="text-sm text-muted-foreground">
              Enable if your ClickHouse server requires credentials.
            </p>
          </div>
          <Switch
            :checked="modelValue.enableAuth"
            @update:checked="(checked) => updateForm({ enableAuth: checked })"
          />
        </div>

        <div
          v-show="modelValue.enableAuth"
          class="grid gap-4 border-l-2 border-primary/20 pl-3 md:grid-cols-2"
        >
          <div class="grid gap-2">
            <Label for="username" class="required">Username</Label>
            <Input
              id="username"
              :model-value="modelValue.username"
              placeholder="default"
              @update:model-value="(value) => updateForm({ username: String(value) })"
            />
          </div>

          <div class="grid gap-2">
            <Label for="password" class="required">Password</Label>
            <Input
              id="password"
              :model-value="modelValue.password"
              type="password"
              @update:model-value="(value) => updateForm({ password: String(value) })"
            />
          </div>
        </div>
      </div>
    </div>

    <div v-if="!isEditMode" class="space-y-4">
      <div class="flex items-center justify-between">
        <h3 class="text-lg font-medium">Table Configuration</h3>
        <div class="text-sm text-muted-foreground">
          Choose whether LogChef should create the table
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
              <Label for="create" class="cursor-pointer font-medium">Create New Table</Label>
            </div>
          </CardHeader>
          <CardContent class="space-y-4">
            <div class="flex items-start gap-4">
              <Plus class="mt-1 h-5 w-5 text-muted-foreground" />
              <div class="space-y-1">
                <p class="text-sm font-medium">Let LogChef create the table</p>
                <p class="text-sm text-muted-foreground">
                  Uses the default OTLP-friendly schema and retention settings.
                </p>
              </div>
            </div>

            <div class="mt-4 grid gap-2 border-t pt-4">
              <Label for="ttl_days">TTL Days</Label>
              <Input
                id="ttl_days"
                :model-value="modelValue.ttlDays"
                type="number"
                min="1"
                @update:model-value="(value) => updateForm({ ttlDays: String(value) })"
              />
              <p class="text-sm text-muted-foreground">
                Number of days to keep logs before automatic deletion.
              </p>
            </div>

            <Dialog>
              <DialogTrigger as-child>
                <Button variant="outline" class="flex w-full items-center justify-between">
                  <div class="flex items-center gap-2">
                    <Code class="h-4 w-4" />
                    <span>View Auto-Generated Schema</span>
                  </div>
                  <ChevronsUpDown class="h-4 w-4" />
                </Button>
              </DialogTrigger>
              <DialogContent class="sm:max-w-[800px]">
                <DialogHeader>
                  <DialogTitle>Table Schema</DialogTitle>
                  <DialogDescription>
                    Review or customize the CREATE TABLE statement before the source is created.
                  </DialogDescription>
                </DialogHeader>

                <div class="space-y-4 py-4">
                  <div class="flex items-center justify-between">
                    <div class="space-y-1">
                      <h4 class="text-sm font-medium leading-none">Schema Definition</h4>
                      <p class="text-sm text-muted-foreground">
                        This statement is only used when LogChef creates the table.
                      </p>
                    </div>
                    <div class="flex items-center gap-2">
                      <Button variant="outline" size="sm" :disabled="!modelValue.schema" @click="resetSchema">
                        Reset to Default
                      </Button>
                      <Button variant="outline" size="sm" @click="isEditingSchema = !isEditingSchema">
                        {{ isEditingSchema ? "Preview" : "Edit" }}
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
            <span class="px-4 text-sm text-muted-foreground">or</span>
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
              <Label for="connect" class="cursor-pointer font-medium">Connect Existing Table</Label>
            </div>
          </CardHeader>
          <CardContent class="space-y-4">
            <div class="flex items-start gap-4">
              <Database class="mt-1 h-5 w-5 text-muted-foreground" />
              <div class="space-y-1">
                <p class="text-sm font-medium">Use an existing table</p>
                <p class="text-sm text-muted-foreground">
                  Map timestamp and severity fields from a table where logs are already ingested.
                </p>
              </div>
            </div>

            <div v-if="modelValue.tableMode === 'connect'" class="mt-4 space-y-4 border-t pt-4">
              <div class="grid gap-2">
                <Label for="meta_ts_field" class="required">Timestamp Field Name</Label>
                <Input
                  id="meta_ts_field"
                  :model-value="modelValue.metaTSField"
                  placeholder="timestamp"
                  @update:model-value="(value) => updateForm({ metaTSField: String(value) })"
                />
              </div>

              <div class="grid gap-2">
                <Label for="meta_severity_field">Severity Field Name</Label>
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
        <h3 class="text-lg font-medium">Data Retention</h3>
        <div class="text-sm text-muted-foreground">
          Configure how long auto-created table data is retained
        </div>
      </div>
      <div class="grid gap-2">
        <Label for="ttl_days_edit">TTL Days</Label>
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
          {{ isValidated ? "Validated" : validateButtonText }}
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
