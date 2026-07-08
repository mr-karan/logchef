<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { useDark } from "@vueuse/core";
import { X, Play } from "lucide-vue-next";
import {
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
  SheetDescription,
  SheetFooter,
} from "@/components/ui/sheet";
import {
  Select,
  SelectTrigger,
  SelectValue,
  SelectContent,
  SelectItem,
} from "@/components/ui/select";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import SqlMonacoEditor from "@/components/query-editor/SqlMonacoEditor.vue";
import DashboardPanel from "./DashboardPanel.vue";
import { useTeamsStore } from "@/stores/teams";
import { useDashboardsStore } from "@/stores/dashboards";
import { sourcesApi, asClickHouseConnection, type Source } from "@/api/sources";
import { supportsQueryLanguage } from "@/lib/queryMetadata";
import { isSuccessResponse } from "@/api/types";
import type {
  DashboardPanel as PanelModel,
  DashboardPanelType,
  PanelQueryLanguage,
} from "@/api/dashboards";

interface Props {
  open: boolean;
  /** The panel being edited, or null to create a new one. */
  panel: PanelModel | null;
}
const props = defineProps<Props>();
const emit = defineEmits<{
  (e: "update:open", value: boolean): void;
  (e: "save", panel: PanelModel): void;
}>();

const teamsStore = useTeamsStore();
const store = useDashboardsStore();
const isDark = useDark();
const monacoTheme = computed(() => (isDark.value ? "logchef-dark" : "logchef-light"));

const PANEL_TYPES: { value: DashboardPanelType; label: string }[] = [
  { value: "timeseries", label: "Time series" },
  { value: "stat", label: "Stat" },
  { value: "table", label: "Table" },
];

function newPanelId(): string {
  const rand =
    typeof crypto !== "undefined" && "randomUUID" in crypto
      ? crypto.randomUUID().slice(0, 8)
      : Math.random().toString(36).slice(2, 10);
  return `p-${rand}`;
}

// --- Local form state -------------------------------------------------------
const panelId = ref<string>(newPanelId());
const title = ref("");
const type = ref<DashboardPanelType>("timeseries");
const teamId = ref<number>(0);
const sourceId = ref<number>(0);
const query = ref("");
// mode is the panel's own two-state toggle: LogchefQL (default) or native LogsQL
// (VictoriaLogs only). clickhouse-sql native is intentionally never offered here.
const mode = ref<"logchefql" | "native">("logchefql");
const groupBy = ref("");
const limit = ref<number>(50);
const columns = ref<string[]>([]);
const columnInput = ref("");

// The full detail of the currently-selected source (for schema/capabilities).
const sourceDetail = ref<Source | null>(null);
const isLoadingSource = ref(false);

const teams = computed(() => teamsStore.teams || []);
const teamSources = computed(() => teamsStore.getTeamSourcesByTeamId(teamId.value));

const supportsLogsql = computed(() =>
  sourceDetail.value ? supportsQueryLanguage(sourceDetail.value, "logsql") : false
);

const queryLanguage = computed<PanelQueryLanguage>(() =>
  mode.value === "native" && supportsLogsql.value ? "logsql" : "logchefql"
);

// SqlMonacoEditor has no LogsQL grammar; the explorer feeds native queries to the
// clickhouse-sql editor too, so we mirror that here for the LogsQL case.
const editorLanguage = computed<"logchefql" | "clickhouse-sql">(() =>
  mode.value === "native" ? "clickhouse-sql" : "logchefql"
);

const editorSchema = computed<Record<string, { type: string }>>(() => {
  const cols = sourceDetail.value?.columns ?? [];
  const map: Record<string, { type: string }> = {};
  for (const c of cols) {
    if (c.name && c.type) map[c.name] = { type: c.type };
  }
  return map;
});

const tableName = computed(
  () => asClickHouseConnection(sourceDetail.value?.connection)?.table_name ?? ""
);

// Column suggestions for group_by / column subset (mirrors AlertForm's approach:
// numeric-only would be wrong for a group dimension, so offer all field names).
const fieldSuggestions = computed(() => (sourceDetail.value?.columns ?? []).map((c) => c.name));

const canSave = computed(() => teamId.value > 0 && sourceId.value > 0);

const sheetTitle = computed(() => (props.panel ? "Edit panel" : "Add panel"));

// --- Sync form when the sheet opens -----------------------------------------
watch(
  () => props.open,
  async (open) => {
    if (!open) {
      store.clearPreview();
      return;
    }
    if (teams.value.length === 0) {
      await teamsStore.loadUserTeams();
    }
    const p = props.panel;
    panelId.value = p?.id ?? newPanelId();
    title.value = p?.title ?? "";
    type.value = p?.type ?? "timeseries";
    teamId.value = p?.team_id ?? 0;
    sourceId.value = p?.source_id ?? 0;
    query.value = p?.query ?? "";
    mode.value = p?.query_language === "logsql" ? "native" : "logchefql";
    groupBy.value = p?.options?.group_by ?? "";
    limit.value = p?.options?.limit ?? 50;
    columns.value = [...(p?.options?.columns ?? [])];
    columnInput.value = "";
    store.clearPreview();
    if (teamId.value > 0) {
      await teamsStore.listTeamSources(teamId.value);
    }
    if (sourceId.value > 0) {
      await loadSourceDetail(sourceId.value);
    } else {
      sourceDetail.value = null;
    }
  },
  { immediate: true }
);

async function loadSourceDetail(id: number) {
  if (!teamId.value || !id) {
    sourceDetail.value = null;
    return;
  }
  isLoadingSource.value = true;
  try {
    const resp = await sourcesApi.getTeamSource(teamId.value, id);
    sourceDetail.value = isSuccessResponse(resp) ? resp.data : null;
  } catch {
    // Fall back to the (leaner) list entry so pickers still work.
    sourceDetail.value = teamSources.value.find((s) => s.id === id) ?? null;
  } finally {
    isLoadingSource.value = false;
    // A source that doesn't support LogsQL can't stay in native mode.
    if (mode.value === "native" && !supportsLogsql.value) {
      mode.value = "logchefql";
    }
  }
}

async function onTeamChange(value: unknown) {
  const id = Number(value);
  if (Number.isNaN(id)) return;
  teamId.value = id;
  sourceId.value = 0;
  sourceDetail.value = null;
  store.clearPreview();
  await teamsStore.listTeamSources(id);
}

async function onSourceChange(value: unknown) {
  const id = Number(value);
  if (Number.isNaN(id)) return;
  sourceId.value = id;
  store.clearPreview();
  await loadSourceDetail(id);
}

function addColumn() {
  const raw = columnInput.value.trim().replace(/,$/, "").trim();
  if (raw && !columns.value.includes(raw)) {
    columns.value = [...columns.value, raw];
  }
  columnInput.value = "";
}

function removeColumn(name: string) {
  columns.value = columns.value.filter((c) => c !== name);
}

function buildPanel(): PanelModel {
  const options: PanelModel["options"] = {};
  if (type.value === "timeseries" && groupBy.value.trim()) {
    options.group_by = groupBy.value.trim();
  }
  if (type.value === "table") {
    if (limit.value > 0) options.limit = Number(limit.value);
    if (columns.value.length > 0) options.columns = [...columns.value];
  }
  return {
    id: panelId.value,
    title: title.value.trim(),
    type: type.value,
    team_id: teamId.value,
    source_id: sourceId.value,
    query: query.value,
    query_language: queryLanguage.value,
    options,
  };
}

const previewState = computed(() => store.previewState ?? undefined);
const isPreviewing = computed(() => store.previewState?.status === "loading");

function runPreview() {
  if (!canSave.value) return;
  void store.previewPanel(buildPanel());
}

function onSave() {
  if (!canSave.value) return;
  emit("save", buildPanel());
  emit("update:open", false);
}

function onOpenChange(value: boolean) {
  emit("update:open", value);
}
</script>

<template>
  <Sheet :open="open" @update:open="onOpenChange">
    <SheetContent side="right" class="w-[560px] max-w-[92vw] overflow-y-auto flex flex-col">
      <SheetHeader>
        <SheetTitle>{{ sheetTitle }}</SheetTitle>
        <SheetDescription>
          Configure the panel's source, query, and visualization. Use Preview to test it.
        </SheetDescription>
      </SheetHeader>

      <div class="flex-1 space-y-4 py-4">
        <!-- Title -->
        <div class="space-y-1.5">
          <Label for="panel-title">Title</Label>
          <Input id="panel-title" v-model="title" placeholder="e.g. 5xx by service" />
        </div>

        <!-- Type -->
        <div class="space-y-1.5">
          <Label>Type</Label>
          <div class="grid grid-cols-3 gap-1.5">
            <Button
              v-for="t in PANEL_TYPES"
              :key="t.value"
              type="button"
              size="sm"
              :variant="type === t.value ? 'default' : 'outline'"
              class="h-8 text-xs"
              @click="type = t.value"
            >
              {{ t.label }}
            </Button>
          </div>
        </div>

        <!-- Team + Source -->
        <div class="grid grid-cols-2 gap-3">
          <div class="space-y-1.5">
            <Label>Team</Label>
            <Select :model-value="teamId ? teamId.toString() : ''" @update:model-value="onTeamChange">
              <SelectTrigger class="h-8 text-sm">
                <SelectValue placeholder="Select team" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem v-for="team in teams" :key="team.id" :value="team.id.toString()">
                  {{ team.name }}
                </SelectItem>
              </SelectContent>
            </Select>
          </div>
          <div class="space-y-1.5">
            <Label>Source</Label>
            <Select
              :model-value="sourceId ? sourceId.toString() : ''"
              :disabled="!teamId || teamSources.length === 0"
              @update:model-value="onSourceChange"
            >
              <SelectTrigger class="h-8 text-sm">
                <SelectValue placeholder="Select source" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem v-for="src in teamSources" :key="src.id" :value="src.id.toString()">
                  {{ src.name }}
                </SelectItem>
              </SelectContent>
            </Select>
          </div>
        </div>

        <!-- Query -->
        <div class="space-y-1.5">
          <div class="flex items-center justify-between">
            <Label>Query</Label>
            <div v-if="supportsLogsql" class="flex items-center gap-1 rounded-md border p-0.5">
              <button
                type="button"
                class="rounded px-2 py-0.5 text-xs"
                :class="mode === 'logchefql' ? 'bg-primary text-primary-foreground' : 'text-muted-foreground'"
                @click="mode = 'logchefql'"
              >
                LogchefQL
              </button>
              <button
                type="button"
                class="rounded px-2 py-0.5 text-xs"
                :class="mode === 'native' ? 'bg-primary text-primary-foreground' : 'text-muted-foreground'"
                @click="mode = 'native'"
              >
                LogsQL
              </button>
            </div>
          </div>
          <div class="h-40 rounded-md border overflow-hidden" :class="{ 'opacity-60 pointer-events-none': !sourceId }">
            <SqlMonacoEditor
              v-if="sourceId"
              :key="`${sourceId}-${editorLanguage}`"
              :value="query"
              :theme="monacoTheme"
              :language="editorLanguage"
              :schema="editorSchema"
              :team-id="teamId"
              :source-id="sourceId"
              :table-name="tableName"
              :is-executing="isPreviewing"
              :visible="open"
              @change="(v: string) => (query = v)"
              @submit="runPreview"
            />
            <div v-else class="flex h-full items-center justify-center text-xs text-muted-foreground">
              Pick a team and source to write a query
            </div>
          </div>
        </div>

        <!-- Type-specific options -->
        <div v-if="type === 'timeseries'" class="space-y-1.5">
          <Label for="panel-groupby">Group by <span class="text-muted-foreground">(optional)</span></Label>
          <Input
            id="panel-groupby"
            v-model="groupBy"
            list="panel-field-suggestions"
            placeholder="e.g. service"
          />
          <datalist id="panel-field-suggestions">
            <option v-for="name in fieldSuggestions" :key="name" :value="name" />
          </datalist>
        </div>

        <div v-else-if="type === 'table'" class="space-y-3">
          <div class="space-y-1.5">
            <Label for="panel-limit">Row limit</Label>
            <Input id="panel-limit" v-model.number="limit" type="number" min="1" max="1000" class="w-32" />
          </div>
          <div class="space-y-1.5">
            <Label for="panel-columns">Columns <span class="text-muted-foreground">(optional; all if empty)</span></Label>
            <div class="flex flex-wrap gap-1.5">
              <span
                v-for="col in columns"
                :key="col"
                class="inline-flex items-center gap-1 rounded bg-muted px-2 py-0.5 text-xs"
              >
                {{ col }}
                <button type="button" class="text-muted-foreground hover:text-foreground" @click="removeColumn(col)">
                  <X class="h-3 w-3" />
                </button>
              </span>
            </div>
            <Input
              id="panel-columns"
              v-model="columnInput"
              list="panel-field-suggestions"
              placeholder="Type a column and press Enter"
              @keydown.enter.prevent="addColumn"
              @keydown="(e: KeyboardEvent) => e.key === ',' && (e.preventDefault(), addColumn())"
            />
            <datalist id="panel-field-suggestions">
              <option v-for="name in fieldSuggestions" :key="name" :value="name" />
            </datalist>
          </div>
        </div>

        <!-- Preview -->
        <div class="space-y-1.5">
          <div class="flex items-center justify-between">
            <Label>Preview</Label>
            <Button
              type="button"
              variant="outline"
              size="sm"
              class="h-7 gap-1.5 text-xs"
              :disabled="!canSave || isPreviewing"
              @click="runPreview"
            >
              <Play class="h-3.5 w-3.5" />
              {{ isPreviewing ? "Running…" : "Preview" }}
            </Button>
          </div>
          <div v-if="previewState" class="h-52">
            <DashboardPanel :panel="buildPanel()" :height-px="208" :state="previewState" />
          </div>
          <div
            v-else
            class="flex h-24 items-center justify-center rounded-md border border-dashed text-xs text-muted-foreground"
          >
            Run a preview to see this panel's data
          </div>
        </div>
      </div>

      <SheetFooter class="mt-2">
        <Button variant="outline" @click="onOpenChange(false)">Cancel</Button>
        <Button :disabled="!canSave" @click="onSave">
          {{ props.panel ? "Update panel" : "Add panel" }}
        </Button>
      </SheetFooter>
    </SheetContent>
  </Sheet>
</template>
