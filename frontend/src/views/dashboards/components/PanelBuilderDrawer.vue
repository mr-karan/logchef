<script setup lang="ts">
import { useI18n } from "vue-i18n";

import { computed, onBeforeUnmount, ref, watch } from "vue";
import { useThemeStore } from "@/stores/theme";
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
import { usePreferencesStore } from "@/stores/preferences";
import type { DashboardQueryPanel, DashboardViz } from "@/api/dashboards";
import { execSignature } from "@/utils/dashboardRuntime";
import { panelBuilderKind, type PanelBuilderKind } from "@/utils/dashboardPanels";
import { suggestViz, withSuggestedRoles } from "@/utils/panelCharts";
import type { DraftPanelPatch } from "@/stores/dashboards";

const { t } = useI18n();

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
const themeStore = useThemeStore();
const isDark = computed(() => themeStore.isDark);
const monacoTheme = computed(() => (isDark.value ? "logchef-dark" : "logchef-light"));

const PANEL_TYPES: { value: PanelBuilderKind; label: string }[] = [
  { value: "chart", get label() { return t("ui.timeSeries"); } },
  { value: "stat", get label() { return t('ui.stat'); } },
  { value: "breakdown", get label() { return t('ui.breakdown'); } },
  { value: "table", get label() { return t('ui.table'); } },
];

// A draft never holds a locked panel: only a creator or an admin edits, and
// they see every panel. The filter keeps the type honest.
const panel = computed<DashboardQueryPanel | null>(() => {
  const draft = store.editDraft;
  if (!draft || !props.panelId) return null;
  const found = draft.panels.find((p) => p.id === props.panelId) ?? null;
  return found && !found.locked ? found : null;
});
const panelKind = computed(() => (panel.value ? panelBuilderKind(panel.value.viz) : "chart"));

// The ONLY write path into the draft panel via the store action, then reflows.
function patchPanel(patch: DraftPanelPatch) {
  if (!props.panelId) return;
  store.updateDraftPanel(props.panelId, patch);
}
let suggestNextVisualization = false;
function switchKind(kind: PanelBuilderKind) {
  if (!props.panelId) return;
  suggestNextVisualization = false;
  previousCountPanel = null;
  store.switchDraftPanelKind(props.panelId, kind);
  const p = panel.value;
  const frame = store.previewState?.querySignature === (p && execSignature(p)) ? store.previewState.frame : undefined;
  if (p?.query.mode === "result" && frame) patchPanel({ viz: withSuggestedRoles(frame, p.viz) });
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

let previousCountPanel: DashboardQueryPanel | null = null;

const mode = computed<"logchefql" | "native">({
  get: () => panel.value?.query.language === "logchefql" ? "logchefql" : "native",
  set: (v) => {
    const language = v === "native" ? supportsLogsql.value ? "logsql" : "clickhouse-sql" : "logchefql";
    if (language === "logchefql" && previousCountPanel && panel.value?.id === previousCountPanel.id && panel.value.source_id === previousCountPanel.source_id) {
      const previous = previousCountPanel;
      previousCountPanel = null;
      suggestNextVisualization = false;
      patchPanel({ query: { ...previous.query, language, text: panel.value.query.text, limit: undefined }, viz: previous.viz });
      return;
    }
    if (language === "clickhouse-sql" && panel.value?.query.mode === "count") {
      suggestNextVisualization = true;
      previousCountPanel = panel.value;
      patchPanel({ query: { mode: "result", language, interval: undefined, group_by: undefined, translate_limit: undefined }, viz: { type: "stat", reduce: "last", ...(panel.value.viz.type === "stat" ? { format: panel.value.viz.format } : {}) } });
      return;
    }
    patchPanel({ query: language === "logchefql" ? { language } : { language, translate_limit: undefined } });
  },
});
function setQueryMode(mode: "count" | "result") {
  const p = panel.value;
  if (!p || p.query.mode === mode || (mode === "count" && p.query.language === "clickhouse-sql")) return;
  const { language, text } = p.query;
  previousCountPanel = null;
  suggestNextVisualization = mode === "result";
  const viz = { ...p.viz };
  if (viz.type === "chart") { delete viz.x; delete viz.y; delete viz.series; }
  else if (viz.type === "stat") { delete viz.value; delete viz.order_by; viz.reduce = mode === "count" ? "sum" : "last"; }
  else if (viz.type !== "table") { delete viz.label; delete viz.value; }
  delete viz.columns;
  patchPanel({ query: mode === "result" ? { mode, language, text, interval: undefined, group_by: undefined, translate_limit: undefined } : { mode, language, text, limit: undefined, interval: "auto" }, viz });
}

function setVisualization(viz: DashboardViz) {
  previousCountPanel = null;
  suggestNextVisualization = false;
  patchPanel({ viz });
}

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
    if (sourceDetail.value && panel.value?.query.language === "logsql" && !supportsLogsql.value) mode.value = "logchefql";
    else if (sourceDetail.value && panel.value?.query.language === "clickhouse-sql" && supportsLogsql.value) mode.value = "native";
  } catch {
    if (requestId !== sourceDetailRequestId) return;
    sourceDetail.value = teamSources.value.find((s) => s.id === sourceId) ?? null;
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
  patchPanel(shouldClearQuery ? { source_id: sourceId, query: { text: "" } } : { source_id: sourceId });
  store.clearPreview();
  await loadSourceDetail(teamId, sourceId);
}

// Load teams/sources/schema whenever the drawer opens on a panel (including a
// panel switch while already open).
watch(
  () => [props.open, props.panelId] as const,
  async ([open]) => {
    suggestNextVisualization = false;
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
watch(() => previewState.value?.frame, (frame) => {
  const p = panel.value;
  if (!frame || p?.query.mode !== "result" || previewState.value?.querySignature !== execSignature(p)) return;
  const viz = suggestNextVisualization ? suggestViz(frame) : withSuggestedRoles(frame, p.viz);
  suggestNextVisualization = false;
  patchPanel({ viz });
});

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
    fetch: execSignature(p),
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
const preferencesStore = usePreferencesStore();

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
        <SheetTitle>{{ panel ? `Edit panel — ${panel.title || "Untitled"}` : t('ui.panelBuilder') }}</SheetTitle>
        <SheetDescription>
          {{ t('ui.configureThePanelSSourceQueryVisualizationAndTimeRangeChangesApply') }}
        </SheetDescription>
      </SheetHeader>

      <template v-if="panel">
        <!-- Header: title, type, close -->
        <div class="flex flex-wrap items-center gap-3 border-b px-4 py-3 pr-12 shrink-0">
          <Input
            :model-value="panel.title"
            :aria-label="t('ui.title')"
            :placeholder="t('ui.panelTitle')"
            class="h-8 max-w-xs font-medium"
            @update:model-value="(v) => patchPanel({ title: String(v ?? '') })"
          />
          <div class="flex items-center gap-1 rounded-md border p-0.5">
            <Button
              v-for="t in PANEL_TYPES"
              :key="t.value"
              type="button"
              size="sm"
              :variant="panelKind === t.value ? 'default' : 'ghost'"
              class="h-7 px-2.5 text-xs"
              @click="switchKind(t.value)"
            >
              {{ t.label }}
            </Button>
          </div>
          <div class="ml-auto flex items-center gap-2">
            <DateTimePicker
              :model-value="store.window"
              :resolved="store.effectiveRange"
              :zone-preference="preferencesStore.preferences.timezone"
              @update:model-value="store.setWindow"
              @update:zone-preference="preferencesStore.updatePreferences({ timezone: $event })"
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
              {{ isPreviewing ? t('ui.running') : t('ui.run') }}
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
              {{ t('ui.pickATeamAndSourceThenWriteAQueryToSeeA') }}
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
                <div class="flex items-center gap-2"><Label class='text-xs text-muted-foreground'>{{ t('ui.query') }}</Label>
                  <button v-for="queryMode in (['count', 'result'] as const)" :key="queryMode" type="button" class="rounded border px-2 py-0.5 text-xs aria-pressed:bg-primary aria-pressed:text-primary-foreground" :disabled="queryMode === 'count' && (panelKind === 'table' || panel.query.language === 'clickhouse-sql')" :aria-pressed="panel.query.mode === queryMode" @click="setQueryMode(queryMode)">{{ queryMode }}</button></div>
                <div v-if="sourceDetail" class="flex items-center gap-1 rounded-md border p-0.5">
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
                    {{ supportsLogsql ? 'LogsQL' : 'ClickHouse SQL' }}
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
                  :value="panel.query.text"
                  :theme="monacoTheme"
                  :language="editorLanguage"
                  :schema="editorSchema"
                  :team-id="panel.team_id"
                  :source-id="panel.source_id"
                  :table-name="tableName"
                  :is-executing="isPreviewing"
                  :visible="open"
                  @change="(v: string) => patchPanel({ query: { text: v } })"
                  @submit="runNow"
                />
                <div v-else class="flex h-full items-center justify-center text-xs text-muted-foreground">
                  {{ t('ui.pickATeamAndSourceToWriteAQuery') }}
                </div>
              </div>
            </div>

            <!-- Options column -->
            <div class="flex min-h-0 flex-col gap-2 overflow-y-auto pl-0.5">
              <Label class='text-xs text-muted-foreground'>{{ t('ui.options') }}</Label>
              <PanelBuilderOptions
                :frame="previewState?.frame"
                :query="panel.query"
                :viz="panel.viz"
                :field-suggestions="fieldSuggestions"
                @update:query="(patch) => patchPanel({ query: patch })"
                @update:viz="setVisualization"
              />
            </div>
          </div>
        </div>
      </template>
    </SheetContent>
  </Sheet>
</template>
