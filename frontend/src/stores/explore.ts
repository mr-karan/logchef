import { defineStore } from "pinia";
import { computed, ref, watch } from "vue";
import { exploreApi, buildTailUrl, subscribeToTail } from "@/api/explore";
import type {
  QuerySharePayload,
  QueryShareResponse,
  LogContextRequest,
  LogContextResponse,
} from "@/api/explore";
import type { ResolvedSavedQuery, SavedQuery, SavedQueryContent } from "@/api/savedQueries";
import type { ExploreTarget } from "@/lib/exploreLocation";
import { useSourcesStore } from "./sources";
import { useTeamsStore } from "@/stores/teams";
import { useContextStore } from "@/stores/context";
import { useBaseStore } from "./base";
import { useExploreAIStore } from "./exploreAI";
import { usePreferencesStore } from "@/stores/preferences";
import { useVariables } from "@/composables/useVariables";
import { useVariableStore, type VariableState } from "@/stores/variables";
import { epochMs, resolveWindow, sameWindow, type ResolvedWindow, type TimeWindow } from '@/utils/timeWindow';
import { decodeDraftWindow, decodeStoredRange, encodeStoredRange, encodeWindowForUrl, type DraftTime } from '@/utils/timeWindowCodec';
import type { Source } from '@/api/sources';
import {
  getExploreModeForQueryLanguage,
  getNativeQueryLanguageForSource,
  hasSourceCapability,
  normalizeExploreMode,
  resolveSavedQueryMetadata,
  supportsQueryLanguage,
} from "@/lib/queryMetadata";

/** A query to run. An empty native query on a ClickHouse source runs the source's default SQL. */
export interface RunQuery {
  mode: 'logchefql' | 'native';
  text: string;
}

/** A query over a window with a row limit: what a run asks for and what the URL records. */
export interface RunRequest {
  query: RunQuery;
  window: TimeWindow;
  limit: number;
}

interface SavedQuerySnapshot {
  mode: "logchefql" | "native";
  queryContent: string;
  limit: number;
  window: TimeWindow;
}

type ExploreDraft = DraftTime & {
  mode: "logchefql" | "native";
  nativeQuery: string;
  showsDefaultSql: boolean;
  logchefqlCode: string;
  limit: number;
  variables: VariableState[];
};

// The non-time fields of a stored draft, any version. The time goes through decodeDraftWindow.
type StoredDraftFields = Partial<Omit<ExploreDraft, "version" | "window">> & { version?: unknown };

const DEFAULT_WINDOW: TimeWindow = { kind: "relative", spec: "15m" };

/** Everything needed to show an Explorer location: every id checked, every fetch done. */
export interface OpenedLocation {
  teamId: number;
  // Null when the team has no source.
  sourceId: number | null;
  content:
    | { kind: "draft" }
    | Extract<ExploreTarget, { kind: "query" }>
    | { kind: "saved"; saved: ResolvedSavedQuery; queryContent: SavedQueryContent }
    | { kind: "share"; share: QueryShareResponse };
}

export interface ExploreState {
  limit: number;
  // Null until the Explorer opens a location, a saved query or a share.
  window: TimeWindow | null;
  nativeQuery: string;
  showsDefaultSql: boolean;
  logchefqlCode: string;
  activeMode: "logchefql" | "native";
  selectedQueryId: string | null;
  activeShareToken: string | null;
  activeShareSnapshot: string | null;
  activeSavedQueryName: string | null;
  savedQuerySnapshot: SavedQuerySnapshot | null;
  queryTimeout: number;

  // Live tail (SSE). Kept apart from run results, so stopping live tail shows
  // the last results again.
  isLive: boolean;
  liveRows: Record<string, any>[];
  liveStatus: LiveTailStatus;
  liveError: string | null;
  liveEndReason: string | null;
  liveEndMessage: string | null;
  liveNotice: string | null;
  liveDroppedCount: number;
  liveTailAbortController: AbortController | null;
}

export type LiveTailStatus = "idle" | "connecting" | "streaming" | "ended" | "error";

// In-memory tail buffer cap (oldest rows dropped) until explore virtualization
// lands. Matches the ceiling documented in the live-tail spec (#69).
const MAX_LIVE_ROWS = 500;

const DRAFT_STORAGE_PREFIX = "logchef.explore.draft";

function cloneVariables(variables: VariableState[]): VariableState[] {
  return variables.map((variable) => ({
    ...variable,
    value: Array.isArray(variable.value) ? [...variable.value] : variable.value,
    defaultValue: Array.isArray(variable.defaultValue)
      ? [...variable.defaultValue]
      : variable.defaultValue,
    options: variable.options?.map((option) => ({ ...option })),
  }));
}

export const useExploreStore = defineStore("explore", () => {
  const contextStore = useContextStore();
  const sourcesStore = useSourcesStore();
  const preferencesStore = usePreferencesStore();
  const selectedStreams = ref<Record<string, string[]>>({});
  const streamFilters = computed(() => Object.entries(selectedStreams.value)
    .filter(([, values]) => values.length > 0)
    .map(([field, values]) => JSON.stringify({ [field]: values })));
  const aiStore = useExploreAIStore();
  
  const state = useBaseStore<ExploreState>({
    limit: 100,
    window: null,
    nativeQuery: "",
    showsDefaultSql: false,
    logchefqlCode: "",
    activeMode: "logchefql",
    selectedQueryId: null,
    activeShareToken: null,
    activeShareSnapshot: null,
    activeSavedQueryName: null,
    savedQuerySnapshot: null,
    queryTimeout: 30,
    isLive: false,
    liveRows: [],
    liveStatus: "idle",
    liveError: null,
    liveEndReason: null,
    liveEndMessage: null,
    liveNotice: null,
    liveDroppedCount: 0,
    liveTailAbortController: null,
  });

  const sourceId = computed(() => contextStore.sourceId || 0);
  const hasValidSource = computed(() => !!sourceId.value);
  const hasWindow = computed(() => state.data.value.window !== null);

  const timeWindow = computed<TimeWindow>(() => state.data.value.window ?? DEFAULT_WINDOW);
  /** The editor's window as instants now. A run resolves its window once, when it starts. */
  const resolveNow = (): ResolvedWindow => resolveWindow(timeWindow.value, preferencesStore.zone, epochMs(Date.now()));

  function assignWindow(window: TimeWindow) {
    state.data.value.window = window;
  }
  const canExecuteQuery = computed(() => hasValidSource.value && hasWindow.value);

  const getCurrentSource = () => sourcesStore.currentSourceDetails ?? null;
  const supportsLogchefQLForSource = (source = getCurrentSource()) => supportsQueryLanguage(source, "logchefql");
  const getDefaultModeForSource = (source = getCurrentSource()): "logchefql" | "native" =>
    supportsLogchefQLForSource(source) ? "logchefql" : "native";
  const normalizeModeForSource = (
    mode: "logchefql" | "native",
    source = getCurrentSource(),
  ): "logchefql" | "native" => (mode === "logchefql" && !supportsLogchefQLForSource(source) ? "native" : mode);

  const templateDialect = computed(() => state.data.value.activeMode === "logchefql"
    ? "logchefql" : getCurrentSource() ? getNativeQueryLanguageForSource(getCurrentSource()) : null);

  // Live tail: the current source must advertise the capability, and the tail
  // endpoint only accepts LogchefQL (either backend) or native logsql
  // (VictoriaLogs). Native clickhouse-sql is rejected 400 server-side, so the
  // toggle stays disabled there.
  const supportsLiveTail = computed(() => hasSourceCapability(getCurrentSource(), "live_tail"));
  const canArmLiveTail = computed(() => {
    if (!supportsLiveTail.value || !hasValidSource.value) return false;
    if (state.data.value.activeMode === "logchefql") return true;
    return getNativeQueryLanguageForSource(getCurrentSource()) === "logsql";
  });

  const variableStore = useVariableStore();
  let suppressSharedVariableTracking = false;

  function currentDraftKey(): string | null {
    const teamId = useTeamsStore().currentTeamId;
    if (!teamId || !sourceId.value) {
      return null;
    }
    return `${DRAFT_STORAGE_PREFIX}.${teamId}.${sourceId.value}`;
  }

  function persistDraft() {
    const key = currentDraftKey();
    if (!key) {
      return;
    }

    const { activeMode, nativeQuery, showsDefaultSql, logchefqlCode, limit } = state.data.value;
    const draft: ExploreDraft = {
      version: 2,
      window: state.data.value.window,
      mode: activeMode,
      nativeQuery,
      showsDefaultSql,
      logchefqlCode,
      limit,
      variables: cloneVariables(variableStore.allVariables),
    };

    try {
      localStorage.setItem(key, JSON.stringify(draft));
    } catch (error) {
      console.warn("Failed to persist query draft:", error);
    }
  }

  function restoreDraftForCurrentContext(): boolean {
    const key = currentDraftKey();
    if (!key) {
      return false;
    }

    try {
      const rawDraft = localStorage.getItem(key);
      if (!rawDraft) {
        return false;
      }

      const draft = JSON.parse(rawDraft) as StoredDraftFields;
      if (draft.version !== 1 && draft.version !== 2) {
        return false;
      }
      const window = decodeDraftWindow(draft, preferencesStore.zone);

      state.data.value.activeMode = draft.mode ? normalizeExploreMode(draft.mode) : "logchefql";
      state.data.value.nativeQuery = draft.nativeQuery || "";
      state.data.value.showsDefaultSql = draft.showsDefaultSql === true && state.data.value.nativeQuery === draft.nativeQuery;
      state.data.value.logchefqlCode = draft.logchefqlCode || "";
      if (draft.limit && draft.limit > 0) {
        state.data.value.limit = draft.limit;
      }
      if (window) {
        assignWindow(window);
      }

      if (Array.isArray(draft.variables)) {
        variableStore.setAllVariable(draft.variables);
      } else {
        const { ensureVariablesFromSql } = useVariables();
        if (templateDialect.value) ensureVariablesFromSql(state.data.value.activeMode === "native" ? state.data.value.nativeQuery : state.data.value.logchefqlCode, templateDialect.value);
      }
      return true;
    } catch (error) {
      console.warn("Failed to restore query draft:", error);
      return false;
    }
  }

  function buildCurrentShareSnapshot(): string {
    const { activeMode, nativeQuery, logchefqlCode, limit, window } = state.data.value;
    return JSON.stringify({
      sourceId: sourceId.value,
      mode: activeMode,
      query: activeMode === "native" ? nativeQuery : logchefqlCode,
      limit,
      window,
      variables: cloneVariables(variableStore.allVariables),
    });
  }

  function clearActiveShareSelection() {
    state.data.value.activeShareToken = null;
    state.data.value.activeShareSnapshot = null;
  }

  function clearActiveSavedQuerySelection() {
    state.data.value.selectedQueryId = null;
    state.data.value.activeSavedQueryName = null;
    state.data.value.savedQuerySnapshot = null;
  }

  function clearShareSelectionIfDirty() {
    if (!state.data.value.activeShareToken || !state.data.value.activeShareSnapshot) {
      return;
    }

    if (buildCurrentShareSnapshot() !== state.data.value.activeShareSnapshot) {
      clearActiveShareSelection();
    }
  }

  const editorQuery = computed<RunQuery>(() => ({
    mode: state.data.value.activeMode,
    text: state.data.value.activeMode === 'native' ? state.data.value.nativeQuery : state.data.value.logchefqlCode,
  }));
  const editorRun = computed<RunRequest>(() => ({ query: editorQuery.value, window: timeWindow.value, limit: state.data.value.limit }));

  const hasDivergedFromSavedQuery = computed(() => {
    const { savedQuerySnapshot, selectedQueryId, activeMode, logchefqlCode, nativeQuery, limit } = state.data.value;
    
    if (!selectedQueryId || !savedQuerySnapshot) {
      return false;
    }

    const currentContent = activeMode === 'logchefql' ? logchefqlCode : nativeQuery;
    if (activeMode !== savedQuerySnapshot.mode || currentContent !== savedQuerySnapshot.queryContent) {
      return true;
    }

    if (limit !== savedQuerySnapshot.limit) {
      return true;
    }

    return !sameWindow(timeWindow.value, savedQuerySnapshot.window);
  });

  /** The URL parameters that reopen `run`: the selected saved query or share when it is the editor's, else the query itself. */
  function queryParameters({ query, window, limit }: RunRequest = editorRun.value) {
    const { selectedQueryId, activeShareToken } = state.data.value;
    const teamsStore = useTeamsStore();

    const params: Record<string, string> = {};

    if (teamsStore.currentTeamId) {
      params.team = teamsStore.currentTeamId.toString();
    }

    if (sourceId.value) {
      params.source = sourceId.value.toString();
    }

    const isEditorQuery = query.mode === editorQuery.value.mode && query.text === editorQuery.value.text;
    if (isEditorQuery && selectedQueryId && !hasDivergedFromSavedQuery.value) {
      params.id = selectedQueryId;
      return params;
    }

    if (isEditorQuery && activeShareToken) {
      params.share = activeShareToken;
      return params;
    }

    Object.assign(params, encodeWindowForUrl(window));
    params.limit = limit.toString();
    // The query is always written, even when empty, so Back and copied links reopen it.
    params.mode = query.mode;
    if (query.mode === 'native') {
      params.sql = query.text;
    } else {
      params.q = query.text;
    }

    return params;
  }
  const urlQueryParameters = computed(() => queryParameters());

  /** Does not run. The Explorer navigator's setWindow runs and records it. */
  function setWindow(window: TimeWindow) {
    assignWindow(window);
    clearShareSelectionIfDirty();
    persistDraft();
  }

  /** Does not run. The Explorer navigator's setLimit runs and records it. */
  function setLimit(newLimit: number) {
    if (newLimit > 0) {
      state.data.value.limit = newLimit;
      clearShareSelectionIfDirty();
      persistDraft();
    }
  }

  function setQueryTimeout(timeout: number) {
    if (timeout > 0 && timeout <= 3600) {
      state.data.value.queryTimeout = timeout;
    }
  }

  function setLogchefqlCode(code: string) {
    state.data.value.logchefqlCode = code;
    clearShareSelectionIfDirty();
    persistDraft();
  }

  function setNativeQuery(sql: string) {
    if (sql === state.data.value.nativeQuery) return;
    state.data.value.nativeQuery = sql;
    state.data.value.showsDefaultSql = false;
    clearShareSelectionIfDirty();
    persistDraft();
  }

  /** Shows the default SQL a run generated. */
  function showDefaultSql(sql: string) {
    state.data.value.nativeQuery = sql;
    state.data.value.showsDefaultSql = true;
    clearShareSelectionIfDirty();
    persistDraft();
  }

  function setActiveMode(mode: 'logchefql' | 'native') {
    const currentMode = state.data.value.activeMode;
    const normalizedMode = normalizeModeForSource(mode);
    if (normalizedMode === currentMode) return;
    state.data.value.activeMode = normalizedMode;
    clearShareSelectionIfDirty();
    persistDraft();
  }

  // Mode and content the source can run. Raw LogchefQL is no native query,
  // so a source without LogchefQL drops it and runs its default SQL instead.
  function fitModeToSource(source: Source | null) {
    if (!source) return;
    if (!supportsLogchefQLForSource(source) && state.data.value.activeMode === 'logchefql') {
      state.data.value.logchefqlCode = '';
      state.data.value.nativeQuery = '';
      state.data.value.showsDefaultSql = false;
      state.data.value.activeMode = 'native';
      return;
    }
    state.data.value.activeMode = normalizeModeForSource(state.data.value.activeMode, source);
  }

  function openDraft() {
    if (!state.data.value.window) assignWindow(DEFAULT_WINDOW);
    state.data.value.logchefqlCode = '';
    state.data.value.nativeQuery = '';
    state.data.value.activeMode = getDefaultModeForSource();
    restoreDraftForCurrentContext();
  }

  function openQuery({ mode, text, window, limit }: Extract<ExploreTarget, { kind: 'query' }>) {
    assignWindow(window ?? DEFAULT_WINDOW);
    if (limit && limit > 0) state.data.value.limit = limit;
    if (mode) state.data.value.activeMode = mode;
    state.data.value.logchefqlCode = state.data.value.activeMode === 'logchefql' ? text : '';
    state.data.value.nativeQuery = state.data.value.activeMode === 'native' ? text : '';
    if (text && templateDialect.value) useVariables().ensureVariablesFromSql(text, templateDialect.value);
  }

  function openSavedQuery(saved: ResolvedSavedQuery, content: SavedQueryContent) {
    const metadata = resolveSavedQueryMetadata({
      query_language: saved.query_language,
      editor_mode: saved.editor_mode,
      source_type: sourcesStore.currentSourceDetails?.source_type ?? "clickhouse",
      query_languages: sourcesStore.currentSourceDetails?.query_languages ?? [],
      saved_query_editor_modes: sourcesStore.currentSourceDetails?.saved_query_editor_modes ?? [],
    });
    // Route content by the RESOLVED active mode, not the query's own
    // language. When the source can't honour the saved language,
    // normalizeModeForSource coerces the mode; routing by the raw language
    // would drop the content into the hidden editor and run default SQL
    // instead. Clear the opposite slot so no stale content lingers behind the
    // hidden editor. See #105.
    const resolvedMode = normalizeModeForSource(getExploreModeForQueryLanguage(metadata.queryLanguage));

    state.data.value.activeMode = resolvedMode;
    state.data.value.activeSavedQueryName = saved.name;
    state.data.value.selectedQueryId = saved.id.toString();

    const queryContent = content.content || '';
    if (resolvedMode === 'logchefql') {
      state.data.value.logchefqlCode = queryContent;
      state.data.value.nativeQuery = '';
    } else {
      state.data.value.nativeQuery = queryContent;
      state.data.value.logchefqlCode = '';
    }

    const limit = content.limit || 100;
    state.data.value.limit = limit;

    // A saved query without a time range keeps the current window.
    const window = decodeStoredRange(content.timeRange, {
      format: "saved-query",
      version: typeof content.version === "number" ? content.version : 1,
      prefZone: preferencesStore.zone,
    }) ?? timeWindow.value;
    assignWindow(window);

    state.data.value.savedQuerySnapshot = { mode: resolvedMode, queryContent, limit, window };

    // Ensure variables from the query content are initialized in the variable store
    if (Array.isArray(content.variables)) {
      const normalizedVariables = content.variables.map((variable) => {
        const hasValue = variable.value !== '' && variable.value !== null && variable.value !== undefined;
        if (!hasValue && variable.defaultValue !== undefined && variable.defaultValue !== null && variable.defaultValue !== '') {
          return { ...variable, value: variable.defaultValue };
        }
        return variable;
      });
      variableStore.setAllVariable(normalizedVariables);
    } else {
      const { ensureVariablesFromSql } = useVariables();
      if (templateDialect.value) ensureVariablesFromSql(queryContent, templateDialect.value);
    }
  }

  function openShare(share: QueryShareResponse) {
    const payload = share.payload;

    state.data.value.activeShareToken = share.token;
    state.data.value.activeMode = payload.mode ? normalizeExploreMode(payload.mode) : "logchefql";

    if (state.data.value.activeMode === "native") {
      state.data.value.nativeQuery = payload.query || "";
      state.data.value.logchefqlCode = "";
    } else {
      state.data.value.logchefqlCode = payload.query || "";
      state.data.value.nativeQuery = "";
    }

    if (payload.limit > 0) {
      state.data.value.limit = payload.limit;
    }
    assignWindow(
      decodeStoredRange(payload.time_range, { format: "share", version: payload.version, timezone: payload.timezone }) ?? timeWindow.value,
    );

    suppressSharedVariableTracking = true;
    try {
      if (Array.isArray(payload.variables)) {
        variableStore.setAllVariable(payload.variables as VariableState[]);
      } else {
        const { ensureVariablesFromSql } = useVariables();
        if (templateDialect.value) ensureVariablesFromSql(payload.query || "", templateDialect.value);
      }
    } finally {
      suppressSharedVariableTracking = false;
    }
    state.data.value.activeShareSnapshot = buildCurrentShareSnapshot();
    persistDraft();
  }

  /**
   * Shows one location's team, source and content. Returns whether it has
   * something to run.
   */
  function open(location: OpenedLocation): boolean {
    // Stream filters name another source's streams.
    if (location.sourceId !== contextStore.sourceId) selectedStreams.value = {};
    contextStore.selectTeam(location.teamId);
    if (location.sourceId) contextStore.selectSource(location.sourceId);
    else contextStore.clearSource();
    if (location.sourceId) contextStore.rememberCurrentSource();
    clearActiveShareSelection();
    clearActiveSavedQuerySelection();
    state.data.value.showsDefaultSql = false;

    const { content } = location;
    if (content.kind === 'draft') openDraft();
    else if (content.kind === 'query') openQuery(content);
    else if (content.kind === 'saved') openSavedQuery(content.saved, content.queryContent);
    else openShare(content.share);
    fitModeToSource(getCurrentSource());

    clearError();
    // A link that names an empty SQL query opens it without running default SQL.
    const { activeMode, nativeQuery } = state.data.value;
    return location.sourceId !== null && !(content.kind === 'query' && activeMode === 'native' && !nativeQuery);
  }

  function _clearQueryContent() {
    state.data.value.showsDefaultSql = false;
    state.data.value.logchefqlCode = '';
    state.data.value.nativeQuery = '';
    state.data.value.activeMode = getDefaultModeForSource();
    state.data.value.selectedQueryId = null;
    clearActiveShareSelection();
    state.data.value.activeSavedQueryName = null;
    state.data.value.savedQuerySnapshot = null;
  }

  function resetQueryToDefaults() {
    assignWindow(DEFAULT_WINDOW);
    state.data.value.limit = 100;
    _clearQueryContent();
  }

  // --- Live tail ----------------------------------------------------------

  function appendLiveRows(rows: Record<string, any>[]) {
    if (!rows?.length) return;
    // Backend batches are oldest-first; the tail view shows newest-at-top, so
    // reverse each batch before prepending. Cap the buffer, dropping the oldest.
    const reversed = [...rows].reverse();
    const merged = [...reversed, ...state.data.value.liveRows];
    state.data.value.liveRows =
      merged.length > MAX_LIVE_ROWS ? merged.slice(0, MAX_LIVE_ROWS) : merged;
  }

  /**
   * Abort any in-flight tail stream and leave live mode. Idempotent. Run
   * results are untouched, so the last results show again when the tail view
   * is hidden.
   */
  function stopLiveTail() {
    if (state.data.value.liveTailAbortController) {
      state.data.value.liveTailAbortController.abort();
      state.data.value.liveTailAbortController = null;
    }
    state.data.value.isLive = false;
    state.data.value.liveStatus = "idle";
  }

  function startLiveTail() {
    const source = getCurrentSource();
    if (!canArmLiveTail.value || !source) return;

    const currentTeamId = useTeamsStore().currentTeamId;
    const sid = sourceId.value;
    if (!currentTeamId || !sid) return;

    let queryLanguage: "logchefql" | "logsql";
    let queryText: string;
    if (state.data.value.activeMode === "logchefql") {
      queryLanguage = "logchefql";
      queryText = state.data.value.logchefqlCode || "";
    } else {
      // canArmLiveTail already guaranteed native === logsql here.
      queryLanguage = "logsql";
      queryText = state.data.value.nativeQuery || "";
    }

    // Abort any prior stream and reset the buffer for a fresh session.
    stopLiveTail();
    state.data.value.liveRows = [];
    state.data.value.liveNotice = null;
    state.data.value.liveDroppedCount = 0;
    state.data.value.liveEndReason = null;
    state.data.value.liveEndMessage = null;
    state.data.value.liveError = null;
    state.data.value.isLive = true;
    state.data.value.liveStatus = "connecting";

    const controller = new AbortController();
    state.data.value.liveTailAbortController = controller;
    const url = buildTailUrl(currentTeamId, sid, queryText, queryLanguage);

    const isActive = () => state.data.value.liveTailAbortController === controller;

    subscribeToTail(url, controller.signal, {
      onOpen: () => {
        if (isActive()) state.data.value.liveStatus = "streaming";
      },
      onRows: (rows) => {
        if (isActive()) appendLiveRows(rows);
      },
      onNotice: (notice) => {
        if (!isActive()) return;
        state.data.value.liveNotice = notice.message || "Live tail rate-limited";
        // Prefer the server's cumulative count when present (newer backends).
        // Older backends only send the human message, so fall back to
        // extracting the per-notice count and accumulating client-side.
        if (typeof notice.dropped_total === "number" && Number.isFinite(notice.dropped_total)) {
          state.data.value.liveDroppedCount = notice.dropped_total;
        } else {
          const match = /(\d+)/.exec(notice.message || "");
          if (match) state.data.value.liveDroppedCount += parseInt(match[1], 10);
        }
      },
      onEnd: (end) => {
        if (!isActive()) return;
        // TTL / completion / abnormal close: keep isLive true so the view can
        // offer "resume", but the stream itself is finished — release the
        // controller. `reason` may be a value this build doesn't recognize
        // (e.g. a new VL connection-lost reason) — it's rendered as opaque
        // text, so unknown values degrade gracefully rather than crashing.
        state.data.value.liveStatus = "ended";
        state.data.value.liveEndReason = end.reason || "ended";
        state.data.value.liveEndMessage = end.message || null;
        state.data.value.liveTailAbortController = null;
      },
    })
      .then(() => {
        if (isActive()) state.data.value.liveTailAbortController = null;
      })
      .catch((err) => {
        // Aborts (toggle off, source/query change, unmount) resolve silently;
        // guard against stale controllers finishing after a restart.
        if (controller.signal.aborted || !isActive()) return;
        state.data.value.liveStatus = "error";
        state.data.value.liveError = err instanceof Error ? err.message : String(err);
        state.data.value.liveTailAbortController = null;
      });
  }

  function toggleLiveTail() {
    if (state.data.value.isLive) {
      stopLiveTail();
    } else {
      startLiveTail();
    }
  }

  /** Marks the shown query as the saved query it was just saved as. */
  function markSavedQuery(saved: Pick<SavedQuery, 'id' | 'name' | 'query_content' | 'query_language'>) {
    state.data.value.selectedQueryId = String(saved.id);
    state.data.value.activeSavedQueryName = saved.name;
    state.data.value.showsDefaultSql = false;
    const content: SavedQueryContent = JSON.parse(saved.query_content);
    state.data.value.savedQuerySnapshot = {
      mode: normalizeModeForSource(getExploreModeForQueryLanguage(saved.query_language)),
      queryContent: content.content || '',
      limit: content.limit || 100,
      window: decodeStoredRange(content.timeRange, {
        format: 'saved-query', version: content.version, prefZone: preferencesStore.zone,
      }) ?? timeWindow.value,
    };
    persistDraft();
  }

  async function getLogContext(sourceId: number, params: LogContextRequest) {
    const operationKey = `getLogContext-${sourceId}`;
    return await state.withLoading(operationKey, async () => {
      if (!sourceId) {
        return state.handleError(
          { status: "error", message: "Source ID is required", error_type: "ValidationError" },
          operationKey
        );
      }
      const teamsStore = useTeamsStore();
      const currentTeamId = teamsStore.currentTeamId;
      if (!currentTeamId) {
        return state.handleError(
          { status: "error", message: "No team selected.", error_type: "ValidationError" },
          operationKey
        );
      }
      return await state.callApi<LogContextResponse>({
        apiCall: () => exploreApi.getLogContext(sourceId, params, currentTeamId),
        operationKey: operationKey,
        showToast: false,
      });
    });
  }

  function buildQuerySharePayload(): QuerySharePayload {
    const { activeMode, nativeQuery, logchefqlCode, limit, window } = state.data.value;
    const stored = window ? encodeStoredRange(window) : null;
    return {
      version: 2,
      mode: activeMode === "native" ? "sql" : "logchefql",
      query: activeMode === "native" ? nativeQuery : logchefqlCode,
      limit,
      timezone: preferencesStore.zone,
      variables: variableStore.allVariables as unknown as QuerySharePayload["variables"],
      ...(stored ? { time_range: stored.timeRange } : {}),
    };
  }

  /** Creates a share link for the editor's query. markShare selects it while the editor still holds what was shared. */
  async function createQueryShare() {
    const currentTeamId = useTeamsStore().currentTeamId;
    if (!currentTeamId || !sourceId.value) {
      throw new Error("Team and source are required to share a query");
    }

    const payload = buildQuerySharePayload();
    if (payload.mode === "sql" && !payload.query.trim()) {
      throw new Error("Query is required to create a share link");
    }

    const snapshot = buildCurrentShareSnapshot();
    const share = (await exploreApi.createQueryShare(sourceId.value, payload, currentTeamId)).data;
    return share ? { ...share, snapshot } : null;
  }

  function markShare({ token, snapshot }: { token: string; snapshot: string }) {
    state.data.value.activeShareToken = token;
    state.data.value.activeShareSnapshot = snapshot;
    state.data.value.showsDefaultSql = false;
    clearShareSelectionIfDirty();
    persistDraft();
  }

  function clearError() {
    state.error.value = null;
  }

  function setQueryError(message: string) {
    state.error.value = { status: "error", message, error_type: "ValidationError" };
  }

  // AI delegation
  async function generateAiSql(naturalLanguageQuery: string, currentQuery?: string) {
    // The concrete target language is derived server-side from the source
    // backend + this editor mode; forward the explorer's current mode.
    const mode = state.data.value.activeMode === 'logchefql' ? 'logchefql' : 'native';
    const result = await aiStore.generateAiSql(naturalLanguageQuery, currentQuery, mode);

    if (result.success && aiStore.generatedAiQuery?.mode === 'native') {
      setNativeQuery(aiStore.generatedAiQuery.query);
    }

    return result;
  }

  function clearAiSqlState() {
    aiStore.clearState();
  }

  watch(
    () => variableStore.allVariables,
    () => {
      if (suppressSharedVariableTracking || !state.data.value.activeShareToken) {
        return;
      }

      clearShareSelectionIfDirty();
      persistDraft();
    },
    { deep: true }
  );
  
  // Exit paths that MUST abort an in-flight tail. Toggle-off, route change and
  // unmount are handled by the caller (they invoke stopLiveTail directly); these
  // watchers cover source change and any query/mode edit while live.
  watch(
    () => sourceId.value,
    () => {
      if (state.data.value.isLive) stopLiveTail();
    }
  );
  watch(
    () =>
      [
        state.data.value.activeMode,
        state.data.value.logchefqlCode,
        state.data.value.nativeQuery,
      ] as const,
    () => {
      if (state.data.value.isLive) stopLiveTail();
    }
  );

  return {
    // State
    sourceId,
    selectedStreams,
    streamFilters,
    limit: computed(() => state.data.value.limit),
    queryTimeout: computed(() => state.data.value.queryTimeout),
    window: timeWindow,
    resolveNow,
    hasWindow,
    nativeQuery: computed(() => state.data.value.nativeQuery),
    showsDefaultSql: computed(() => state.data.value.showsDefaultSql),
    logchefqlCode: computed(() => state.data.value.logchefqlCode),
    activeMode: computed(() => state.data.value.activeMode),
    templateDialect,
    error: state.error,
    selectedQueryId: computed(() => state.data.value.selectedQueryId),
    activeShareToken: computed(() => state.data.value.activeShareToken),
    activeSavedQueryName: computed(() => state.data.value.activeSavedQueryName),

    // AI state (delegated)
    isGeneratingAISQL: computed(() => aiStore.isGeneratingAISQL),
    aiSqlError: computed(() => aiStore.aiSqlError),
    generatedAiQuery: computed(() => aiStore.generatedAiQuery),

    hasDivergedFromSavedQuery,
    editorQuery,
    editorRun,
    queryParameters,
    urlQueryParameters,
    canExecuteQuery,

    // Actions
    open,
    setWindow,
    setLimit,
    setQueryTimeout,
    setNativeQuery,
    showDefaultSql,
    setActiveMode,
    setLogchefqlCode,
    clearActiveSavedQuerySelection,
    markSavedQuery,
    resetQueryToDefaults,
    persistDraft,
    getLogContext,
    createQueryShare,
    markShare,
    clearError,
    setQueryError,
    generateAiSql,
    clearAiSqlState,

    // Live tail state
    isLive: computed(() => state.data.value.isLive),
    liveRows: computed(() => state.data.value.liveRows),
    liveStatus: computed(() => state.data.value.liveStatus),
    liveError: computed(() => state.data.value.liveError),
    liveEndReason: computed(() => state.data.value.liveEndReason),
    liveEndMessage: computed(() => state.data.value.liveEndMessage),
    liveNotice: computed(() => state.data.value.liveNotice),
    liveDroppedCount: computed(() => state.data.value.liveDroppedCount),
    supportsLiveTail,
    canArmLiveTail,

    // Live tail actions
    startLiveTail,
    stopLiveTail,
    toggleLiveTail,
  };
});
