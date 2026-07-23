<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from "vue";
import { useDark } from "@vueuse/core";
import type { DateRange } from "reka-ui";
import { Play } from "lucide-vue-next";
import {
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
  SheetDescription,
} from "@/components/ui/sheet";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { DateTimePicker } from "@/components/date-time-picker";
import SqlMonacoEditor from "@/components/query-editor/SqlMonacoEditor.vue";
import TeamSourceSelector from "@/views/explore/components/TeamSourceSelector.vue";
import DashboardPanel from "./DashboardPanel.vue";
import PanelBuilderOptions from "./PanelBuilderOptions.vue";
import { useTeamsStore } from "@/stores/teams";
import { useDashboardsStore } from "@/stores/dashboards";
import { sourcesApi, asClickHouseConnection, type Source } from "@/api/sources";
import { supportsQueryLanguage } from "@/lib/queryMetadata";
import { isSuccessResponse } from "@/api/types";
import {
  timestampToCalendarDateTime,
  parseRelativeTimeString,
  calendarDateTimeToTimestamp,
} from "@/utils/time";
import type {
  DashboardPanel as PanelModel,
  DashboardPanelOptions,
  DashboardPanelType,
} from "@/api/dashboards";

// Full-height panel builder drawer. Draft-first: every field reads from and
// writes to `store.editDraft` via `store.updateDraftPanel` — there is no
// detached local copy of the panel config. DashboardView owns `open` +
// `panelId`; closing this drawer only exits panel-editing, never the
// dashboard's own edit mode.
interface Props {
  open: boolean;
  panelId: string | null;
}
const props = defineProps<Props>();
const emit = defineEmits<{
  (e: "update:open", value: boolean): void;
}>();

const teamsStore = useTeamsStore();
const store = useDashboardsStore();
const isDark = useDark();
const monacoTheme = computed(() => (isDark.value ? "logchef-dark" : "logchef-light"));

const PANEL_TYPES: { value: DashboardPanelType; label: string }[] = [
  { value: "timeseries", label: "Time series" },
  { value: "stat", label: "Stat" },
  { value: "breakdown", label: "Breakdown" },
  { value: "table", label: "Table" },
];

const panel = computed<PanelModel | null>(() => {
  const draft = store.editDraft;
  if (!draft || !props.panelId) return null;
  return draft.panels.find((p) => p.id === props.panelId) ?? null;
});

// The ONLY write path into the draft panel — patch-merges into the panel (and
// shallow-merges `options` specifically) via the store action, then reflows.
function patchPanel(
  patch: Partial<Omit<PanelModel, "options">> & { options?: Partial<DashboardPanelOptions> }
) {
  if (!props.panelId) return;
  store.updateDraftPanel(props.panelId, patch);
}
function patchOptions(patch: Partial<DashboardPanelOptions>) {
  patchPanel({ options: patch });
}

// Auto-close if the panel this drawer was pointed at disappears out from under
// it (e.g. removed on the canvas while the drawer was open).
watch(panel, (p) => {
  if (props.open && !p) emit("update:open", false);
});

// --- Team / source + schema plumbing ----------------------------------------
const teams = computed(() => teamsStore.teams || []);
const teamSources = computed(() => teamsStore.getTeamSourcesByTeamId(panel.value?.team_id ?? 0));

const sourceDetail = ref<Source | null>(null);
// Guards against a stale-response race: a rapid A -> B source switch can let
// A's async detail fetch resolve after B's has already started (and even
// after B's has landed), overwriting B's schema with A's - or forcing mode
// back to logchefql based on A's (stale) supportsLogsql. Every call captures
// its own token; only the call whose token still matches the latest one when
// it resolves is allowed to apply its result.
let sourceDetailRequestId = 0;

const supportsLogsql = computed(() =>
  sourceDetail.value ? supportsQueryLanguage(sourceDetail.value, "logsql") : false
);

// mode is the panel's own two-state toggle: LogchefQL (default) or native
// LogsQL (VictoriaLogs only). clickhouse-sql native is intentionally never
// offered here — see PanelEditorSheet's original note, preserved by design.
const mode = computed<"logchefql" | "native">({
  get: () => (panel.value?.query_language === "logsql" ? "native" : "logchefql"),
  set: (v) => {
    patchPanel({ query_language: v === "native" && supportsLogsql.value ? "logsql" : "logchefql" });
  },
});

// SqlMonacoEditor has no LogsQL grammar; feed native queries to the
// clickhouse-sql editor too, mirroring the explorer's approach for LogsQL.
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

const fieldSuggestions = computed(() => (sourceDetail.value?.columns ?? []).map((c) => c.name));

async function loadSourceDetail(teamId: number, sourceId: number) {
  const requestId = ++sourceDetailRequestId;
  if (!teamId || !sourceId) {
    sourceDetail.value = null;
    return;
  }
  try {
    const resp = await sourcesApi.getTeamSource(teamId, sourceId);
    if (requestId !== sourceDetailRequestId) return; // a newer call superseded this one
    sourceDetail.value = isSuccessResponse(resp) ? resp.data : null;
  } catch {
    if (requestId !== sourceDetailRequestId) return;
    sourceDetail.value = teamSources.value.find((s) => s.id === sourceId) ?? null;
  } finally {
    if (requestId !== sourceDetailRequestId) return;
    // A source that doesn't support LogsQL can't stay in native mode.
    if (mode.value === "native" && !supportsLogsql.value) {
      mode.value = "logchefql";
    }
  }
}

async function onTeamChange(teamId: number) {
  patchPanel({ team_id: teamId, source_id: 0 });
  sourceDetail.value = null;
  store.clearPreview();
  await teamsStore.listTeamSources(teamId);
}

async function onSourceChange(sourceId: number) {
  const teamId = panel.value?.team_id ?? 0;
  const previousSourceId = panel.value?.source_id ?? 0;
  // A query written against one source's schema (tables/columns) is unlikely
  // to still be valid for a different source - clear it rather than silently
  // keep a stale, probably-invalid query pointed at the newly picked source.
  const shouldClearQuery = previousSourceId > 0 && previousSourceId !== sourceId;
  patchPanel(shouldClearQuery ? { source_id: sourceId, query: "" } : { source_id: sourceId });
  store.clearPreview();
  await loadSourceDetail(teamId, sourceId);
}

// Load teams/sources/schema whenever the drawer opens on a panel (including a
// panel switch while already open).
watch(
  () => [props.open, props.panelId] as const,
  async ([open]) => {
    if (!open) return;
    if (teams.value.length === 0) {
      await teamsStore.loadUserTeams();
    }
    const p = panel.value;
    if (p?.team_id) {
      await teamsStore.listTeamSources(p.team_id);
    }
    if (p?.team_id && p?.source_id) {
      await loadSourceDetail(p.team_id, p.source_id);
    } else {
      sourceDetail.value = null;
    }
  },
  { immediate: true }
);

// --- Live preview: instant config, debounced data fetch ---------------------
const canPreview = computed(() => !!panel.value && panel.value.team_id > 0 && panel.value.source_id > 0);
const isPreviewing = computed(() => store.previewState?.status === "loading");
const previewState = computed(() => store.previewState ?? undefined);

let debounceTimer: ReturnType<typeof setTimeout> | null = null;
let lastPreviewedPanelId: string | null = null;

function clearDebounce() {
  if (debounceTimer !== null) {
    clearTimeout(debounceTimer);
    debounceTimer = null;
  }
}

function schedulePreview(delayMs: number) {
  clearDebounce();
  const p = panel.value;
  if (!p || p.team_id <= 0 || p.source_id <= 0) {
    store.clearPreview();
    return;
  }
  // Abort any in-flight preview immediately so a stale request can't resolve
  // into previewState during the debounce window (keeps the last result shown).
  store.abortPreview();
  debounceTimer = setTimeout(() => {
    debounceTimer = null;
    void store.previewPanel(p);
  }, delayMs);
}

function runNow() {
  schedulePreview(0);
}

// A signature of everything that affects the executed query/result. Config
// changes (title, type, query, options, team/source) reflect on the draft —
// and therefore the canvas tile behind this drawer — the instant they're
// patched; only the resulting backend fetch is debounced (~400ms), so rapid
// typing never fires a request per keystroke. Switching to a different panel
// (or first opening the drawer) previews immediately instead of waiting.
const previewSignature = computed(() => {
  const p = panel.value;
  if (!p) return "";
  return JSON.stringify({
    type: p.type,
    team: p.team_id,
    source: p.source_id,
    query: p.query,
    lang: p.query_language,
    options: p.options ?? {},
    range: store.effectiveRange,
  });
});

watch(
  () => (props.open ? ([props.panelId, previewSignature.value] as const) : null),
  (curr) => {
    if (!curr || !curr[0]) {
      clearDebounce();
      store.clearPreview();
      lastPreviewedPanelId = null;
      return;
    }
    const [id] = curr;
    const isSwitch = id !== lastPreviewedPanelId;
    lastPreviewedPanelId = id;
    schedulePreview(isSwitch ? 0 : 400);
  },
  { immediate: true }
);

onBeforeUnmount(() => {
  clearDebounce();
  store.clearPreview();
});

// --- Preview pane pixel height (unovis charts need a real px number) --------
const previewEl = ref<HTMLElement | null>(null);
const previewHeightPx = ref(320);
let previewResizeObserver: ResizeObserver | null = null;

watch(previewEl, (el) => {
  previewResizeObserver?.disconnect();
  previewResizeObserver = null;
  if (!el) return;
  previewHeightPx.value = el.clientHeight;
  previewResizeObserver = new ResizeObserver((entries) => {
    for (const entry of entries) previewHeightPx.value = entry.contentRect.height;
  });
  previewResizeObserver.observe(el);
});

onBeforeUnmount(() => {
  previewResizeObserver?.disconnect();
  previewResizeObserver = null;
});

// --- Embedded time-range control (bound to the dashboard store's range) ----
const dateTimePickerRef = ref<InstanceType<typeof DateTimePicker> | null>(null);

const pickerModel = computed<DateRange>(() => {
  const range = store.effectiveRange;
  return {
    start: timestampToCalendarDateTime(range.start),
    end: timestampToCalendarDateTime(range.end),
  } as DateRange;
});

const selectedQuickRange = computed(() => (store.timeRelative ? `Last ${store.timeRelative}` : null));

function handleRangeChange(value: any) {
  const quick = dateTimePickerRef.value?.selectedQuickRange as string | null | undefined;
  if (quick) {
    const relative = quick.replace(/^Last\s+/i, "").trim();
    try {
      parseRelativeTimeString(relative);
      store.setRelativeTime(relative);
      return;
    } catch {
      // not a parseable relative preset — fall through to absolute
    }
  }
  if (value?.start && value?.end) {
    store.setAbsoluteRange(calendarDateTimeToTimestamp(value.start), calendarDateTimeToTimestamp(value.end));
  }
}

function onOpenChange(value: boolean) {
  emit("update:open", value);
}
</script>

<template>
  <Sheet :open="open" @update:open="onOpenChange">
    <SheetContent
      side="right"
      class="w-[min(1280px,96vw)] max-w-none sm:max-w-none gap-0 overflow-hidden p-0 flex flex-col"
    >
      <SheetHeader class="sr-only">
        <SheetTitle>{{ panel ? `Edit panel — ${panel.title || "Untitled"}` : "Panel builder" }}</SheetTitle>
        <SheetDescription>
          Configure the panel's source, query, visualization, and time range. Changes apply to the
          dashboard draft immediately; the dashboard's own Save/Cancel controls persist or discard them.
        </SheetDescription>
      </SheetHeader>

      <template v-if="panel">
        <!-- Header: title, type, close -->
        <div class="flex flex-wrap items-center gap-3 border-b px-4 py-3 pr-12 shrink-0">
          <Input
            :model-value="panel.title"
            aria-label="Title"
            placeholder="Panel title"
            class="h-8 max-w-xs font-medium"
            @update:model-value="(v) => patchPanel({ title: String(v ?? '') })"
          />
          <div class="flex items-center gap-1 rounded-md border p-0.5">
            <Button
              v-for="t in PANEL_TYPES"
              :key="t.value"
              type="button"
              size="sm"
              :variant="panel.type === t.value ? 'default' : 'ghost'"
              class="h-7 px-2.5 text-xs"
              @click="patchPanel({ type: t.value })"
            >
              {{ t.label }}
            </Button>
          </div>
          <div class="ml-auto flex items-center gap-2">
            <DateTimePicker
              ref="dateTimePickerRef"
              :model-value="pickerModel"
              :selected-quick-range="selectedQuickRange"
              @update:model-value="handleRangeChange"
            />
            <Button
              type="button"
              variant="outline"
              size="sm"
              class="h-8 gap-1.5 text-xs"
              :disabled="!canPreview || isPreviewing"
              @click="runNow"
            >
              <Play class="h-3.5 w-3.5" />
              {{ isPreviewing ? "Running…" : "Run" }}
            </Button>
          </div>
        </div>

        <!-- Body: large live preview + query/options rail -->
        <div class="flex-1 min-h-0 flex flex-col gap-3 p-4">
          <div ref="previewEl" class="flex-[3] min-h-0 rounded-md border overflow-hidden">
            <div
              v-if="!canPreview"
              class="flex h-full items-center justify-center px-6 text-center text-xs text-muted-foreground"
            >
              Pick a team and source, then write a query to see a live preview.
            </div>
            <DashboardPanel v-else :panel="panel" :height-px="previewHeightPx" :state="previewState" />
          </div>

          <div class="flex-[2] min-h-0 grid grid-cols-1 gap-3 lg:grid-cols-[1fr_300px]">
            <!-- Query column -->
            <div class="flex min-h-0 flex-col gap-2">
              <TeamSourceSelector
                :current-team-id="panel.team_id || null"
                :current-source-id="panel.source_id || null"
                :available-teams="teams"
                :available-sources="teamSources"
                @update:team="onTeamChange"
                @update:source="onSourceChange"
              />
              <div class="flex items-center justify-between shrink-0">
                <Label class="text-xs text-muted-foreground">Query</Label>
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
              <div
                class="flex-1 min-h-0 rounded-md border overflow-hidden"
                :class="{ 'opacity-60 pointer-events-none': !panel.source_id }"
              >
                <SqlMonacoEditor
                  v-if="panel.source_id"
                  :key="`${panel.source_id}-${editorLanguage}`"
                  :value="panel.query"
                  :theme="monacoTheme"
                  :language="editorLanguage"
                  :schema="editorSchema"
                  :team-id="panel.team_id"
                  :source-id="panel.source_id"
                  :table-name="tableName"
                  :is-executing="isPreviewing"
                  :visible="open"
                  @change="(v: string) => patchPanel({ query: v })"
                  @submit="runNow"
                />
                <div v-else class="flex h-full items-center justify-center text-xs text-muted-foreground">
                  Pick a team and source to write a query
                </div>
              </div>
            </div>

            <!-- Options column -->
            <div class="flex min-h-0 flex-col gap-2 overflow-y-auto pl-0.5">
              <Label class="text-xs text-muted-foreground">Options</Label>
              <PanelBuilderOptions
                :type="panel.type"
                :options="panel.options ?? {}"
                :field-suggestions="fieldSuggestions"
                @update:options="patchOptions"
              />
            </div>
          </div>
        </div>
      </template>
    </SheetContent>
  </Sheet>
</template>
