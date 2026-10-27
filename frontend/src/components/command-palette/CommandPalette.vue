<script setup lang="ts">
import { computed, nextTick, onMounted, onScopeDispose, ref, shallowRef, watch } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { useI18n } from 'vue-i18n';
import { CommandDialog, CommandGroup } from '@/components/ui/command';
import { Kbd } from '@/components/ui/kbd';
import { useAskChefStore } from '@/stores/askChef';
import { useExploreStore } from '@/stores/explore';
import { useExploreRuns } from '@/stores/exploreRuns';
import { exploreWindowQuery, alertHistoryExploreQuery } from '@/utils/alertExplore';
import { loadAlertInvestigation } from './alertInvestigation';
import { useAuthStore } from '@/stores/auth';
import { useContextStore } from '@/stores/context';
import { useMetaStore } from '@/stores/meta';
import { useSourcesStore } from '@/stores/sources';
import { usePreferencesStore } from '@/stores/preferences';
import { useThemeStore } from '@/stores/theme';
import { useTeamPermissions } from '@/composables/useTeamPermissions';
import { useToast } from '@/composables/useToast';
import { isAbortError } from '@/api/error-handler';
import { readRecentSources, type RecentSource } from '@/lib/recentSources';
import { navigateFromPalette } from '@/lib/exploreHandoff';
import { platformModifier } from '@/lib/platformModifier';
import { EXPLORE_PATH } from '@/lib/exploreLocation';
import { epochMs } from '@/utils/timeWindow';
import { createCatalog } from './catalog';
import { routePages } from './pages';
import { buildPaletteView, type KeyName, type PaletteContext, type PaletteEffect } from './palette';
import { resolveTextSearch } from './textSearch';
import type { InputEdit, Span } from './parse';
import PaletteInput from './PaletteInput.vue';
import PaletteRow from './PaletteRow.vue';

const props = defineProps<{ open: boolean; cancellation?: AbortSignal }>();
const emit = defineEmits<{ close: [] }>();
const { t, locale } = useI18n();
const router = useRouter();
const route = useRoute();
const auth = useAuthStore();
const askChef = useAskChefStore();
const explore = useExploreStore();
const runs = useExploreRuns();
const context = useContextStore();
const meta = useMetaStore();
const sources = useSourcesStore();
const preferences = usePreferencesStore();
const theme = useThemeStore();
const { isAnyTeamAdmin } = useTeamPermissions();
const { toast } = useToast();
const catalog = createCatalog();
const pages = routePages(router);
const recents = shallowRef<readonly RecentSource[]>([]);
const now = ref(epochMs(Date.now()));
const text = ref('');
const selection = ref<Span>({ start: 0, end: 0 });
const selectedId = ref<string | null>(null);
const input = ref<InstanceType<typeof PaletteInput> | null>(null);
const list = ref<HTMLElement | null>(null);
type Activity = { readonly kind: 'idle' } | { readonly kind: 'searching' | 'asking'; readonly controller: AbortController };
const activity = shallowRef<Activity>({ kind: 'idle' });
const error = ref<string | null>(null);
const modifier = platformModifier;
let clock: ReturnType<typeof setInterval> | null = null;
let generation = 0;
const keys: Record<KeyName, string> = { up: '↑', down: '↓', enter: '↵', modEnter: `${modifier} ↵`, modShiftEnter: `${modifier} ⇧ ↵`, tab: 'Tab', esc: 'esc' };
const paletteContext = computed<PaletteContext>(() => ({
  schemaFields: route.name === 'LogExplorer' && sources.currentSourceDetails?.id === context.sourceId
    ? new Set(sources.currentSourceDetails.columns?.map(column => column.name) ?? []) : new Set(),
  askChefEnabled: meta.askChefEnabled,
  explorer: (() => {
    if (route.name !== 'LogExplorer') return null;
    const captured = askChef.explorerContext();
    if (!captured || captured.teamId !== context.teamId || captured.sourceId !== context.sourceId) return null;
    const shown = runs.shown?.teamId === captured.teamId && runs.shown.source.id === captured.sourceId ? runs.shown : null;
    return { context: captured, window: shown?.window ?? explore.editorRun.window, hasResults: Boolean(shown?.results?.rows.length) };
  })(),
  viewer: { isAdmin: auth.user?.role === 'admin', isAnyTeamAdmin: isAnyTeamAdmin.value, alertsEnabled: meta.alertsEnabled, oauthEnabled: Boolean(meta.oauthIssuer) },
  routeName: typeof route.name === 'string' ? route.name : null,
  current: context.teamId !== null && context.sourceId !== null ? { teamId: context.teamId, sourceId: context.sourceId } : null,
  recents: recents.value, theme: theme.preference, timezone: preferences.preferences.timezone, zone: preferences.zone,
  now: now.value, locale: locale.value, pages,
  t: (key, named, plural) => plural === undefined ? t(key, named ?? {}) : t(key, named ?? {}, plural),
}));
const view = computed(() => buildPaletteView(text.value, selection.value, paletteContext.value, catalog.snapshot.value, selectedId.value));
const rows = computed(() => view.value.groups.flatMap(group => group.items));
const selectable = computed(() => rows.value.filter(row => row.enter || row.tab));
const selected = computed(() => rows.value.find(row => row.id === selectedId.value) ?? null);
const activeId = computed(() => selected.value ? `palette-option-${rows.value.indexOf(selected.value)}` : undefined);
watch(selectable, items => {
  if (!items.some(row => row.id === selectedId.value)) selectedId.value = items[0]?.id ?? null;
}, { immediate: true });
watch(() => view.value.needsTeamSources, ids => { if (props.open) for (const id of ids) catalog.ensureTeamSources(id); });
watch(selectedId, () => { void nextTick(() => list.value?.querySelector('[data-active="true"]')?.scrollIntoView({ block: 'nearest' })); });

function stopRequests() {
  if (clock !== null) clearInterval(clock);
  clock = null;
  catalog.close();
  if (activity.value.kind === 'searching') {
    activity.value.controller.abort();
    activity.value = { kind: 'idle' };
  }
}
watch(() => props.open, open => {
  if (!open) { stopRequests(); return; }
  text.value = '';
  selection.value = { start: 0, end: 0 };
  error.value = null;
  now.value = epochMs(Date.now());
  clock = setInterval(() => { now.value = epochMs(Date.now()); }, 1000);
  const userId = String(auth.user?.id ?? '');
  recents.value = readRecentSources(userId);
  catalog.open({ userId, isAdmin: auth.user?.role === 'admin', alertsEnabled: meta.alertsEnabled,
    priorityTeams: [...(context.teamId === null ? [] : [context.teamId]), ...recents.value.map(recent => recent.teamId)] });
  void nextTick(() => input.value?.focus());
}, { immediate: true });
watch(() => auth.user?.id, () => { stopRequests(); catalog.clear(); emit('close'); });
onScopeDispose(() => { stopRequests(); catalog.clear(); });

async function editInput(edit: InputEdit) {
  text.value = edit.text;
  selection.value = { start: edit.caret, end: edit.caret };
  error.value = null;
  await input.value?.focus(edit);
}
function change(value: string, span: Span) {
  cancelActivity();
  text.value = value;
  selection.value = span;
  selectedId.value = null;
  error.value = null;
}

async function dispatch(effect: PaletteEffect | null) {
  if (!effect || activity.value.kind !== 'idle') return;
  error.value = null;
  const userId = auth.user?.id;
  const attempt = ++generation;
  try {
    switch (effect.kind) {
      case 'edit': return void await editInput(effect.edit);
      case 'retry': return catalog.retry(effect.list);
      case 'navigate':
        await navigateFromPalette(router, effect.to);
        if (attempt === generation) emit('close');
        return;
      case 'askChef': {
        if (!meta.askChefEnabled) return;
        const controller = new AbortController();
        const signal = controller.signal;
        activity.value = { kind: 'asking', controller };
        if (effect.navigation === 'scoped') {
          const landed = await navigateFromPalette(router, { path: EXPLORE_PATH, query: exploreWindowQuery(effect.context) }, true);
          if (signal.aborted || attempt !== generation) return;
          if (!landed) { showAskError('palette.ask.landingFailed'); return; }
        }
        if (!meta.askChefEnabled || auth.user?.id !== userId) return;
        emit('close');
        void askChef.openFromPalette(effect.context, effect.question);
        return;
      }
      case 'investigateAlert': {
        if (!meta.askChefEnabled) return;
        const originPath = route.path;
        const controller = new AbortController();
        const signal = controller.signal;
        activity.value = { kind: 'asking', controller };
        const result = await loadAlertInvestigation(effect.alertId, effect.teamId, signal);
        if (signal.aborted || route.path !== originPath || attempt !== generation || !meta.askChefEnabled || auth.user?.id !== userId) return;
        if (result.kind === 'failed') { showAskError(`palette.ask.${result.reason}`); return; }
        const landed = await navigateFromPalette(router, { path: EXPLORE_PATH, query: alertHistoryExploreQuery(result.alert, result.entry, result.teamId) }, true);
        if (signal.aborted || attempt !== generation) return;
        if (!landed) { showAskError('palette.ask.landingFailed'); return; }
        if (!meta.askChefEnabled || auth.user?.id !== userId) return;
        emit('close');
        askChef.investigateAlert(result.alert, result.entry, result.teamId);
        return;
      }
      case 'searchText': {
        const controller = new AbortController();
        activity.value = { kind: 'searching', controller };
        try {
          const cached = context.teamId === effect.target.teamId ? sources.currentSourceDetails : null;
          const result = await resolveTextSearch(effect.target, cached, controller.signal);
          if (controller.signal.aborted || !props.open) return;
          if (result.kind !== 'ready') { error.value = t(result.kind === 'noFields' ? 'palette.noTextFields' : 'palette.searchFailed'); return; }
          await navigateFromPalette(router, { path: EXPLORE_PATH, query: { team: String(effect.target.teamId), source: String(effect.target.sourceId), mode: 'logchefql', q: result.query, t: effect.target.range } });
          emit('close');
        } catch (cause) {
          if (!controller.signal.aborted) throw cause;
        } finally {
          if (activity.value.kind === 'searching' && activity.value.controller === controller) activity.value = { kind: 'idle' };
        }
        return;
      }
      case 'setTheme': await preferences.updatePreferences({ theme: effect.mode }); break;
      case 'setTimezone': await preferences.updatePreferences({ timezone: effect.timezone }); break;
      case 'copyLink': await navigator.clipboard.writeText(window.location.href); toast({ title: t('palette.linkCopied') }); break;
      case 'openDocs': window.open('https://logchef.app', '_blank', 'noopener,noreferrer'); break;
      case 'signOut': catalog.clear(); await auth.logout(); break;
      default: { const exhaustive: never = effect; return exhaustive; }
    }
    if (attempt === generation) emit('close');
  } catch (cause) {
    if (attempt !== generation) return;
    if (!isAbortError(cause)) {
      if (effect.kind === 'askChef' || effect.kind === 'investigateAlert') showAskError('palette.ask.failed');
      else if (props.open) error.value = t('palette.searchFailed');
    }
  } finally {
    if (attempt === generation) activity.value = { kind: 'idle' };
  }
}
function cancelActivity() {
  generation++;
  if (activity.value.kind !== 'idle') activity.value.controller.abort();
  activity.value = { kind: 'idle' };
}
function cancelPending(event: KeyboardEvent) {
  if (event.key === 'Escape' && activity.value.kind === 'asking') cancelActivity();
}
function dismiss() {
  cancelActivity();
  emit('close');
}
watch(() => props.cancellation, (signal, _previous, onCleanup) => {
  signal?.addEventListener('abort', cancelActivity, { once: true });
  if (signal?.aborted) cancelActivity();
  onCleanup(() => signal?.removeEventListener('abort', cancelActivity));
}, { immediate: true });
onMounted(() => window.addEventListener('keydown', cancelPending, true));
onScopeDispose(() => {
  window.removeEventListener('keydown', cancelPending, true);
  cancelActivity();
});
function showAskError(key: string) {
  if (props.open) error.value = t(key);
  else toast({ title: t(key), variant: 'destructive' });
}
function keydown(event: KeyboardEvent) {
  if (event.isComposing || event.keyCode === 229) { event.stopPropagation(); return; }
  if (!['ArrowDown', 'ArrowUp', 'Enter', 'Tab'].includes(event.key)) return;
  event.preventDefault();
  event.stopPropagation();
  if (event.repeat && event.key === 'Enter') return;
  if (event.key === 'Tab') {
    if (!event.shiftKey && selected.value?.tab) void editInput(selected.value.tab);
  } else if (event.key === 'Enter') void dispatch(event.metaKey || event.ctrlKey ? event.shiftKey ? view.value.askEnter : view.value.modEnter : selected.value?.enter ?? null);
  else {
    const items = selectable.value;
    const index = items.findIndex(row => row.id === selectedId.value);
    selectedId.value = items[(index + (event.key === 'ArrowDown' ? 1 : -1) + items.length) % items.length]?.id ?? null;
  }
}
</script>

<template>
  <CommandDialog :open="open" :title="t('palette.title')" :description="t('palette.description')" class="top-[max(24px,12vh)] w-[640px] max-w-[calc(100vw-24px)] translate-y-0 gap-0 rounded-lg border p-0 shadow-2xl sm:max-w-[640px]" @update:open="value => { if (!value) dismiss(); }">
    <PaletteInput ref="input" :text="text" :tokens="view.tokens" :query="view.mode === 'query'" :ask="view.mode === 'ask'" :ghost="view.ghost?.text ?? ''" :label="t('palette.placeholder')" :active-id="activeId" @change="change" @selection="selection = $event" @key="keydown" />
    <div ref="list" class="min-h-0 overflow-hidden">
      <div id="palette-results" role="listbox" @mousedown.prevent :aria-label="t('palette.title')" class="max-h-[min(60vh,520px)] overflow-y-auto overflow-x-hidden p-1.5">
        <CommandGroup v-for="group in view.groups" :key="group.id" class="p-0">
          <template v-if="group.heading" #heading><span class="flex justify-between gap-2 text-[10px] uppercase tracking-wide"><span>{{ group.heading }}</span><span>{{ group.count }}</span></span></template>
          <div v-for="item in group.items" :id="`palette-option-${rows.indexOf(item)}`" :key="item.id" role="option" :aria-selected="selectedId === item.id" :aria-disabled="!item.enter" :data-active="selectedId === item.id" class="relative flex min-h-[34px] cursor-default select-none items-center gap-3 rounded px-2.5 py-2 text-[12px] outline-none" :class="[selectedId === item.id ? 'bg-accent text-accent-foreground ring-1 ring-inset ring-primary/25' : '', item.shape === 'run' ? 'my-0.5 min-h-[58px]' : '']" @pointermove="() => { if (item.enter || item.tab) selectedId = item.id; }" @mousedown.prevent @click="dispatch(item.enter)">
            <PaletteRow :item="item" :selected="selectedId === item.id" :modifier="modifier" />
          </div>
        </CommandGroup>
        <p v-if="rows.length === 0" role="status" class="px-4 py-8 text-center text-xs text-muted-foreground">{{ t('palette.empty') }}</p>
      </div>
    </div>
    <p v-if="error" role="alert" class="border-t px-4 py-2 text-xs text-destructive">{{ error }}</p>
    <p v-if="activity.kind !== 'idle'" role="status" class="border-t px-4 py-2 text-xs text-muted-foreground">{{ t(activity.kind === 'asking' ? 'palette.ask.opening' : 'palette.searching') }}</p>
    <div class="flex flex-wrap items-center gap-x-3 gap-y-2 border-t bg-muted/25 px-3 py-2 text-[10px] text-muted-foreground">
      <span v-for="(hint, index) in view.footer.hints" :key="index" class="inline-flex items-center gap-1"><Kbd v-for="key in hint.keys" :key="key" class="text-[10px]">{{ keys[key] }}</Kbd>{{ hint.label }}<span v-if="hint.detail" class="font-mono text-orange-600 dark:text-orange-400">{{ hint.detail }}</span></span>
      <span class="ml-auto hidden sm:inline"><template v-if="view.footer.status.kind === 'syntax'"><span class="font-mono text-blue-500">@team/source</span> · <span class="font-mono text-orange-500">last:</span></template><template v-else-if="view.footer.status.kind === 'completing'">{{ view.footer.status.label }} <span class="font-mono">{{ view.footer.status.syntax }}</span></template><template v-else>{{ view.footer.status.text }}</template></span>
    </div>
  </CommandDialog>
</template>
