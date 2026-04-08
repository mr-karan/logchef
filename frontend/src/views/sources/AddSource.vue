<script setup lang="ts">
import { computed, onMounted, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";
import { Textarea } from "@/components/ui/textarea";
import { useToast } from "@/composables/useToast";
import { TOAST_DURATION } from "@/lib/constants";
import { useSourcesStore } from "@/stores/sources";
import type { Source } from "@/api/sources";
import ClickHouseSourceForm from "./components/ClickHouseSourceForm.vue";
import VictoriaLogsSourceForm from "./components/VictoriaLogsSourceForm.vue";
import {
  buildClickHouseConnection,
  buildClickHouseValidationRequest,
  buildVictoriaLogsConnection,
  buildVictoriaLogsValidationRequest,
  clickHouseFormStateFromSource,
  createDefaultClickHouseFormState,
  createDefaultVictoriaLogsFormState,
  generateClickHouseSchema,
  serializeConnectionSnapshot,
  sourceTypeFromSource,
  victoriaLogsFormStateFromSource,
  type ClickHouseSourceFormState,
  type DatasourceType,
  type VictoriaLogsSourceFormState,
} from "./components/sourceFormModels";

const router = useRouter();
const route = useRoute();
const { toast } = useToast();
const sourcesStore = useSourcesStore();

const isEditMode = computed(() => Boolean(route.params.sourceId));
const editingSourceId = computed(() => (route.params.sourceId ? Number(route.params.sourceId) : null));
const duplicateFromId = computed(() => {
  const raw = route.query.duplicateFrom;
  return typeof raw === "string" ? Number(raw) : null;
});

const isLoadingSource = ref(false);
const isSubmitting = ref(false);
const formError = ref<string | null>(null);

const sourceType = ref<DatasourceType>("clickhouse");
const sourceName = ref("");
const description = ref("");
const clickHouseForm = ref<ClickHouseSourceFormState>(createDefaultClickHouseFormState());
const victoriaLogsForm = ref<VictoriaLogsSourceFormState>(createDefaultVictoriaLogsFormState());
const victoriaLogsTTLDays = ref("0");

const isValidating = ref(false);
const validationMessage = ref<string | null>(null);
const isValidated = ref(false);
const originalConnectionSnapshot = ref("");

const currentConnectionSnapshot = computed(() =>
  sourceType.value === "clickhouse"
    ? serializeConnectionSnapshot(buildClickHouseConnection(clickHouseForm.value))
    : serializeConnectionSnapshot(buildVictoriaLogsConnection(victoriaLogsForm.value))
);

const activeValidationFingerprint = computed(() =>
  sourceType.value === "clickhouse"
    ? JSON.stringify(buildClickHouseValidationRequest(clickHouseForm.value))
    : JSON.stringify(buildVictoriaLogsValidationRequest(victoriaLogsForm.value))
);

const activeSchema = computed(() => generateClickHouseSchema(clickHouseForm.value));
const hasConnectionChanges = computed(() => currentConnectionSnapshot.value !== originalConnectionSnapshot.value);

const pageTitle = computed(() => {
  if (isEditMode.value) {
    return "Edit Source";
  }
  if (duplicateFromId.value) {
    return "Duplicate Source";
  }
  return "Add Source";
});

const pageDescription = computed(() => {
  if (isEditMode.value) {
    return "Update datasource configuration and connection settings.";
  }
  if (duplicateFromId.value) {
    return "Create a new datasource using an existing configuration as the starting point.";
  }
  return "Create a datasource and choose the backend LogChef should query.";
});

const submitButtonText = computed(() => {
  if (isSubmitting.value) {
    return isEditMode.value ? "Updating..." : "Creating...";
  }
  if (isEditMode.value) {
    return "Update Source";
  }
  if (sourceType.value === "clickhouse" && clickHouseForm.value.tableMode === "connect" && !isValidated.value) {
    return "Validate & Import";
  }
  if (sourceType.value === "clickhouse" && clickHouseForm.value.tableMode === "connect") {
    return "Import Source";
  }
  return "Create Source";
});

const isValid = computed(() => {
  if (!sourceName.value.trim()) {
    return false;
  }

  if (sourceType.value === "clickhouse") {
    const state = clickHouseForm.value;
    if (!state.host.trim() || !state.database.trim() || !state.tableName.trim()) {
      return false;
    }
    if (state.enableAuth && !state.username.trim()) {
      return false;
    }
    if (state.enableAuth && (!isEditMode.value || hasConnectionChanges.value) && !state.password) {
      return false;
    }
    if (state.tableMode === "connect" && !state.metaTSField.trim()) {
      return false;
    }
    if (state.tableMode === "create" && !state.ttlDays.trim()) {
      return false;
    }
    return true;
  }

  const state = victoriaLogsForm.value;
  if (!state.baseURL.trim() || !state.metaTSField.trim()) {
    return false;
  }
  if (state.authMode === "basic") {
    if (!state.username.trim()) {
      return false;
    }
    if ((!isEditMode.value || hasConnectionChanges.value) && !state.password) {
      return false;
    }
  }
  if (state.authMode === "bearer" && (!isEditMode.value || hasConnectionChanges.value) && !state.token) {
    return false;
  }
  return true;
});

watch(activeValidationFingerprint, (_next, previous) => {
  if (previous === undefined) {
    return;
  }
  isValidated.value = false;
  validationMessage.value = null;
});

watch(sourceType, () => {
  formError.value = null;
  isValidated.value = false;
  validationMessage.value = null;
});

function handleSourceTypeChange(value: string) {
  sourceType.value = value === "victorialogs" ? "victorialogs" : "clickhouse";
}

function setOriginalConnectionSnapshot() {
  originalConnectionSnapshot.value = currentConnectionSnapshot.value;
}

async function loadSourceForPrefill(sourceId: number): Promise<Source | null> {
  await sourcesStore.loadAllSourcesForAdmin();
  return sourcesStore.sources.find((source) => source.id === sourceId) || null;
}

function prefillFormFromSource(source: Source, isCopy = false) {
  sourceType.value = sourceTypeFromSource(source);
  sourceName.value = isCopy ? `${source.name} (Copy)` : source.name;
  description.value = source.description || "";

  if (sourceType.value === "clickhouse") {
    clickHouseForm.value = clickHouseFormStateFromSource(source);
    victoriaLogsForm.value = createDefaultVictoriaLogsFormState();
    victoriaLogsTTLDays.value = "0";
  } else {
    victoriaLogsForm.value = victoriaLogsFormStateFromSource(source);
    victoriaLogsTTLDays.value = String(source.ttl_days ?? 0);
    clickHouseForm.value = createDefaultClickHouseFormState();
  }

  formError.value = null;
  isValidated.value = false;
  validationMessage.value = null;
  setOriginalConnectionSnapshot();
}

async function handleValidateConnection() {
  isValidating.value = true;
  isValidated.value = false;
  validationMessage.value = null;

  try {
    const request =
      sourceType.value === "clickhouse"
        ? buildClickHouseValidationRequest(clickHouseForm.value)
        : buildVictoriaLogsValidationRequest(victoriaLogsForm.value);

    const result = await sourcesStore.validateSourceConnection(request);
    if (result.success && result.data) {
      validationMessage.value = result.data.message;
      isValidated.value = true;
    }
  } catch (error) {
    console.error("Validation error:", error);
  } finally {
    isValidating.value = false;
  }
}

async function submitForm() {
  if (!isValid.value) {
    toast({
      title: "Error",
      description: "Please fill in all required fields",
      variant: "destructive",
      duration: TOAST_DURATION.ERROR,
    });
    return;
  }

  if (!isEditMode.value && sourceType.value === "clickhouse" && clickHouseForm.value.tableMode === "connect" && !isValidated.value) {
    await handleValidateConnection();
    if (!isValidated.value) {
      return;
    }
  }

  if (isEditMode.value && sourceType.value === "victorialogs" && hasConnectionChanges.value) {
    const authMode = victoriaLogsForm.value.authMode;
    if (authMode === "basic" && !victoriaLogsForm.value.password) {
      formError.value = "Re-enter the VictoriaLogs password before saving connection changes.";
      return;
    }
    if (authMode === "bearer" && !victoriaLogsForm.value.token) {
      formError.value = "Re-enter the VictoriaLogs bearer token before saving connection changes.";
      return;
    }
  }

  isSubmitting.value = true;
  formError.value = null;

  try {
    if (isEditMode.value && editingSourceId.value) {
      const updatePayload: Record<string, unknown> = {
        name: sourceName.value.trim(),
        description: description.value.trim(),
      };

      if (sourceType.value === "clickhouse") {
        updatePayload.ttl_days = Number(clickHouseForm.value.ttlDays || 0);
        updatePayload.meta_ts_field = clickHouseForm.value.metaTSField.trim();
        updatePayload.meta_severity_field = clickHouseForm.value.metaSeverityField.trim();
        if (hasConnectionChanges.value) {
          updatePayload.connection = buildClickHouseConnection(clickHouseForm.value);
        }
      } else {
        updatePayload.ttl_days = Number(victoriaLogsTTLDays.value || 0);
        updatePayload.meta_ts_field = victoriaLogsForm.value.metaTSField.trim();
        updatePayload.meta_severity_field = victoriaLogsForm.value.metaSeverityField.trim();
        if (hasConnectionChanges.value) {
          updatePayload.connection = buildVictoriaLogsConnection(victoriaLogsForm.value);
        }
      }

      const result = await sourcesStore.updateSource(editingSourceId.value, updatePayload);
      if (result.success) {
        toast({
          title: "Success",
          description: "Source updated successfully",
          duration: TOAST_DURATION.SUCCESS,
        });
        router.push({ name: "Sources" });
      } else {
        formError.value = result.error || "Failed to update source";
      }
      return;
    }

    const payload =
      sourceType.value === "clickhouse"
        ? {
            name: sourceName.value.trim(),
            source_type: "clickhouse",
            meta_is_auto_created: clickHouseForm.value.tableMode === "create",
            meta_ts_field: clickHouseForm.value.metaTSField.trim(),
            meta_severity_field: clickHouseForm.value.metaSeverityField.trim(),
            connection: buildClickHouseConnection(clickHouseForm.value),
            description: description.value.trim(),
            ttl_days: Number(clickHouseForm.value.ttlDays || 0),
            schema: clickHouseForm.value.tableMode === "create"
              ? clickHouseForm.value.schema || activeSchema.value
              : undefined,
          }
        : {
            name: sourceName.value.trim(),
            source_type: "victorialogs",
            meta_is_auto_created: false,
            meta_ts_field: victoriaLogsForm.value.metaTSField.trim(),
            meta_severity_field: victoriaLogsForm.value.metaSeverityField.trim(),
            connection: buildVictoriaLogsConnection(victoriaLogsForm.value),
            description: description.value.trim(),
            ttl_days: Number(victoriaLogsTTLDays.value || 0),
          };

    const result = await sourcesStore.createSource(payload);
    if (result.success) {
      router.push({ name: "Sources" });
    } else {
      formError.value = result.error || "Failed to create source";
    }
  } catch (error) {
    console.error("Error saving source:", error);
    formError.value = error instanceof Error ? error.message : "Unknown error";
  } finally {
    isSubmitting.value = false;
  }
}

onMounted(async () => {
  if (isEditMode.value && editingSourceId.value) {
    isLoadingSource.value = true;
    try {
      const source = await loadSourceForPrefill(editingSourceId.value);
      if (!source) {
        toast({
          title: "Error",
          description: "Source not found",
          variant: "destructive",
          duration: TOAST_DURATION.ERROR,
        });
        router.push({ name: "Sources" });
        return;
      }
      prefillFormFromSource(source, false);
    } catch (error) {
      console.error("Error loading source for editing:", error);
      toast({
        title: "Error",
        description: "Failed to load source data",
        variant: "destructive",
        duration: TOAST_DURATION.ERROR,
      });
    } finally {
      isLoadingSource.value = false;
    }
    return;
  }

  if (duplicateFromId.value) {
    isLoadingSource.value = true;
    try {
      const source = await loadSourceForPrefill(duplicateFromId.value);
      if (source) {
        prefillFormFromSource(source, true);
      } else {
        toast({
          title: "Warning",
          description: "Could not find source to duplicate",
          variant: "destructive",
          duration: TOAST_DURATION.ERROR,
        });
      }
    } catch (error) {
      console.error("Error loading source for duplication:", error);
      toast({
        title: "Error",
        description: "Failed to load source data for duplication",
        variant: "destructive",
        duration: TOAST_DURATION.ERROR,
      });
    } finally {
      isLoadingSource.value = false;
      setOriginalConnectionSnapshot();
    }
    return;
  }

  setOriginalConnectionSnapshot();
});
</script>

<template>
  <div class="container mx-auto max-w-4xl px-4 py-8">
    <Card>
      <CardHeader>
        <CardTitle>{{ pageTitle }}</CardTitle>
        <CardDescription>{{ pageDescription }}</CardDescription>
      </CardHeader>
      <CardContent>
        <div v-if="isLoadingSource" class="flex items-center justify-center py-12">
          <div class="flex items-center gap-3">
            <svg class="h-5 w-5 animate-spin text-primary" xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24">
              <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
              <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
            </svg>
            <span class="text-muted-foreground">Loading source data...</span>
          </div>
        </div>

        <form v-else class="space-y-6" @submit.prevent="submitForm">
          <div class="space-y-6">
            <div class="space-y-4">
              <div class="flex items-center justify-between">
                <h3 class="text-lg font-medium">Basic Information</h3>
                <div class="text-sm text-muted-foreground">
                  Define source identity and provider
                </div>
              </div>

              <div v-if="!isEditMode" class="space-y-3">
                <Label class="required">Datasource Type</Label>
                <RadioGroup
                  :model-value="sourceType"
                  class="grid gap-3 md:grid-cols-2"
                  @update:model-value="handleSourceTypeChange"
                >
                  <Card
                    :class="{ 'border-primary shadow-sm': sourceType === 'clickhouse', 'border-muted-foreground/20': sourceType !== 'clickhouse' }"
                    class="cursor-pointer transition-all hover:border-primary/70"
                    @click="sourceType = 'clickhouse'"
                  >
                    <CardContent class="flex items-start gap-3 p-5">
                      <RadioGroupItem value="clickhouse" id="source_type_clickhouse" />
                      <div class="space-y-1">
                        <Label for="source_type_clickhouse" class="cursor-pointer font-medium">ClickHouse</Label>
                        <p class="text-sm text-muted-foreground">
                          Native LogChefQL and SQL with optional auto-created tables.
                        </p>
                      </div>
                    </CardContent>
                  </Card>

                  <Card
                    :class="{ 'border-primary shadow-sm': sourceType === 'victorialogs', 'border-muted-foreground/20': sourceType !== 'victorialogs' }"
                    class="cursor-pointer transition-all hover:border-primary/70"
                    @click="sourceType = 'victorialogs'"
                  >
                    <CardContent class="flex items-start gap-3 p-5">
                      <RadioGroupItem value="victorialogs" id="source_type_victorialogs" />
                      <div class="space-y-1">
                        <Label for="source_type_victorialogs" class="cursor-pointer font-medium">VictoriaLogs</Label>
                        <p class="text-sm text-muted-foreground">
                          Native LogsQL against VictoriaLogs with tenant and scope controls.
                        </p>
                      </div>
                    </CardContent>
                  </Card>
                </RadioGroup>
              </div>

              <div v-else class="grid gap-2 md:max-w-sm">
                <Label>Datasource Type</Label>
                <Input :model-value="sourceType === 'clickhouse' ? 'ClickHouse' : 'VictoriaLogs'" disabled />
              </div>

              <div class="grid gap-2">
                <Label for="source_name" class="required">Source Name</Label>
                <Input
                  id="source_name"
                  v-model="sourceName"
                  placeholder="My Application Logs"
                  maxlength="50"
                />
              </div>

              <div class="grid gap-2">
                <Label for="description">Description</Label>
                <Textarea
                  id="description"
                  v-model="description"
                  rows="2"
                  maxlength="500"
                  placeholder="Optional description of what this source contains"
                />
              </div>
            </div>

            <ClickHouseSourceForm
              v-if="sourceType === 'clickhouse'"
              v-model="clickHouseForm"
              :is-edit-mode="isEditMode"
              :is-validating="isValidating"
              :is-validated="isValidated"
              :validation-message="validationMessage"
              @validate="handleValidateConnection"
            />

            <VictoriaLogsSourceForm
              v-else
              v-model="victoriaLogsForm"
              :is-edit-mode="isEditMode"
              :is-validating="isValidating"
              :is-validated="isValidated"
              :validation-message="validationMessage"
              @validate="handleValidateConnection"
            />
          </div>

          <div
            v-if="formError"
            class="rounded-md border border-destructive/20 bg-destructive/10 p-3 text-sm text-destructive"
          >
            {{ formError }}
          </div>

          <div class="mt-6 flex justify-end space-x-4 border-t pt-6">
            <Button type="button" variant="outline" @click="router.push({ name: 'Sources' })">
              Cancel
            </Button>
            <Button
              type="submit"
              size="lg"
              :disabled="isSubmitting || !isValid || (!isEditMode && sourceType === 'clickhouse' && clickHouseForm.tableMode === 'connect' && !isValidated)"
              :class="{ 'opacity-50': !isValid || (!isEditMode && sourceType === 'clickhouse' && clickHouseForm.tableMode === 'connect' && !isValidated) }"
            >
              <span v-if="isSubmitting" class="mr-2">
                <svg class="h-4 w-4 animate-spin text-white" xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24">
                  <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
                  <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
                </svg>
              </span>
              {{ submitButtonText }}
            </Button>
          </div>
        </form>
      </CardContent>
    </Card>
  </div>
</template>

<style scoped>
.required::after {
  content: " *";
  color: hsl(var(--destructive));
}
</style>
