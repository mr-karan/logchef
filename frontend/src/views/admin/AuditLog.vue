<script setup lang="ts">
import { computed, onMounted, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { useRoute, useRouter, type LocationQueryRaw, type RouteLocationRaw } from "vue-router";
import { ChevronDown, ChevronRight, Download, History, RefreshCw, ScrollText } from "lucide-vue-next";
import { EmptyState, LoadingState, PageHeader, SearchInput, SortHeader } from "@/components/layout";
import { DateTimePicker } from "@/components/date-time-picker";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { NativeSelect, NativeSelectOption } from "@/components/ui/native-select";
import { SearchableSelect } from "@/components/ui/searchable-select";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import {
  auditApi,
  type AuditEvent,
  type AuditEventFilters,
  type AuditOutcome,
} from "@/api/admin";
import { usersApi } from "@/api/users";
import { teamsApi } from "@/api/teams";
import { sourcesApi } from "@/api/sources";
import { isSuccessResponse, type APIResponse } from "@/api/types";
import { formatErrorMessage, hasShownErrorToast, isCanceledError, showErrorToast } from "@/api/error-handler";
import type { User } from "@/types";
import { useTableSearchSort } from "@/composables/useTableSearchSort";
import { usePreferencesStore } from "@/stores/preferences";
import { epochMs, formatInZone, resolveWindow, toRfc3339, type TimeWindow } from "@/utils/timeWindow";
import { decodeWindowFromUrl, encodeWindowForUrl } from "@/utils/timeWindowCodec";

// The server-side filters live in the URL, so a link such as
// /admin/audit-log?target_type=source&target_id=7 opens one object's history.
const FILTER_KEYS = ["action", "actor_id", "outcome", "target_type", "target_id", "order"] as const;
const WINDOW_KEYS = ["t", "time", "start", "end", "start_time", "end_time"] as const;
const DEFAULT_WINDOW: TimeWindow = { kind: "relative", spec: "7d" };

const { t, locale } = useI18n();
const route = useRoute();
const router = useRouter();
const preferencesStore = usePreferencesStore();

const events = ref<AuditEvent[]>([]);
const nextCursor = ref("");
const isLoading = ref(true);
const isLoadingMore = ref(false);
const isExporting = ref(false);
const error = ref<string | null>(null);
const lookupError = ref<string | null>(null);
const expanded = ref(new Set<string>());
const users = ref<User[]>([]);
const sourceIds = ref(new Set<number>());
const teamIds = ref(new Set<number>());
const seenActions = ref(new Set<string>());
// The filters of the loaded pages, so Load more and Download read the same
// range. Null while a load is pending or after it failed, so Download then
// reads the URL.
const loadedFilters = ref<AuditEventFilters | null>(null);
let controller: AbortController | null = null;

function queryString(key: string): string {
  const value = route.query[key];
  return typeof value === "string" ? value : "";
}

const timeWindow = computed<TimeWindow>(() => {
  const query = Object.fromEntries(WINDOW_KEYS.map((key) => [key, queryString(key) || undefined]));
  return decodeWindowFromUrl(query) ?? DEFAULT_WINDOW;
});
const resolvedWindow = computed(() => resolveWindow(timeWindow.value, preferencesStore.zone, epochMs(Date.now())));
const order = computed<"asc" | "desc">(() => (queryString("order") === "asc" ? "asc" : "desc"));
const outcome = computed(() => queryString("outcome"));
const actorId = computed(() => queryString("actor_id"));
const actionInput = ref("");
const targetTypeInput = ref("");
const targetIdInput = ref("");
const hasServerFilters = computed(() => [...FILTER_KEYS, ...WINDOW_KEYS].some((key) => key !== "order" && queryString(key)));

function currentFilters(): AuditEventFilters {
  const range = resolveWindow(timeWindow.value, preferencesStore.zone, epochMs(Date.now()));
  const rawOutcome = outcome.value;
  const parsedOutcome: AuditOutcome | undefined = rawOutcome === "success" || rawOutcome === "failure" ? rawOutcome : undefined;
  return {
    from: toRfc3339(range.start),
    to: toRfc3339(range.end),
    action: queryString("action") || undefined,
    actor_id: actorId.value || undefined,
    outcome: parsedOutcome,
    target_type: queryString("target_type") || undefined,
    target_id: queryString("target_id") || undefined,
    order: order.value === "asc" ? "asc" : undefined,
  };
}

function setQuery(changes: Record<string, string | null>) {
  const query: LocationQueryRaw = { ...route.query };
  for (const [key, value] of Object.entries(changes)) {
    if (value) query[key] = value;
    else delete query[key];
  }
  router.replace({ query });
}

function setWindow(window: TimeWindow) {
  setQuery({ ...Object.fromEntries(WINDOW_KEYS.map((key) => [key, null])), ...encodeWindowForUrl(window) });
}

function applyAction() {
  setQuery({ action: actionInput.value.trim() || null });
}

function applyTarget() {
  const type = targetTypeInput.value.trim();
  setQuery({ target_type: type || null, target_id: type ? targetIdInput.value.trim() || null : null });
}

function toggleOrder() {
  setQuery({ order: order.value === "desc" ? "asc" : null });
}

function rememberActions(page: AuditEvent[]) {
  const next = new Set(seenActions.value);
  for (const event of page) next.add(event.action);
  seenActions.value = next;
}

async function fetchPage(filters: AuditEventFilters, cursor: string) {
  controller?.abort();
  controller = new AbortController();
  const response = await auditApi.listEvents(filters, cursor, { signal: controller.signal });
  if (!isSuccessResponse(response) || !response.data) {
    throw response;
  }
  rememberActions(response.data.events);
  return response.data;
}

async function loadEvents() {
  const filters = currentFilters();
  isLoading.value = true;
  isLoadingMore.value = false;
  error.value = null;
  expanded.value = new Set();
  loadedFilters.value = null;
  try {
    const page = await fetchPage(filters, "");
    events.value = page.events;
    nextCursor.value = page.next_cursor;
    loadedFilters.value = filters;
    isLoading.value = false;
  } catch (err: unknown) {
    if (isCanceledError(err)) return;
    events.value = [];
    nextCursor.value = "";
    error.value = formatErrorMessage(err, t("admin.auditLoadFailed"));
    isLoading.value = false;
  }
}

async function loadMore() {
  const filters = loadedFilters.value;
  if (!filters || !nextCursor.value) return;
  isLoadingMore.value = true;
  try {
    const page = await fetchPage(filters, nextCursor.value);
    events.value = [...events.value, ...page.events];
    nextCursor.value = page.next_cursor;
    isLoadingMore.value = false;
  } catch (err: unknown) {
    if (isCanceledError(err)) return;
    error.value = formatErrorMessage(err, t("admin.auditLoadFailed"));
    isLoadingMore.value = false;
  }
}

async function download() {
  const filters = loadedFilters.value ?? currentFilters();
  isExporting.value = true;
  try {
    const blob = await auditApi.exportEvents(filters);
    const url = URL.createObjectURL(blob);
    const link = document.createElement("a");
    link.href = url;
    link.download = `logchef-audit-${new Date().toISOString().replace(/[:.]/g, "-")}.ndjson`;
    document.body.appendChild(link);
    link.click();
    link.remove();
    URL.revokeObjectURL(url);
  } catch (err: unknown) {
    if (!hasShownErrorToast(err)) showErrorToast(err, formatErrorMessage(err, t("admin.auditExportFailed")));
  } finally {
    isExporting.value = false;
  }
}

function lookupIds<T extends { id: number }>(response: APIResponse<T[]>): Set<number> {
  if (!isSuccessResponse(response)) throw response;
  return new Set((response.data ?? []).map((item) => item.id));
}

// Users fill the actor filter; sources and teams decide which targets still
// exist and so get a link.
async function loadLookups() {
  try {
    const [usersResponse, sourcesResponse, teamsResponse] = await Promise.all([
      usersApi.listUsers(),
      sourcesApi.listAllSourcesForAdmin(),
      teamsApi.listAllTeams(),
    ]);
    if (!isSuccessResponse(usersResponse)) throw usersResponse;
    users.value = usersResponse.data ?? [];
    sourceIds.value = lookupIds(sourcesResponse);
    teamIds.value = lookupIds(teamsResponse);
  } catch (err: unknown) {
    lookupError.value = formatErrorMessage(err, t("admin.auditLookupFailed"));
  }
}

watch(
  () => [...FILTER_KEYS, ...WINDOW_KEYS].map(queryString),
  () => {
    actionInput.value = queryString("action");
    targetTypeInput.value = queryString("target_type");
    targetIdInput.value = queryString("target_id");
    loadEvents();
  },
  { immediate: true },
);

onMounted(loadLookups);

const { search, rows } = useTableSearchSort(events, {
  searchKeys: ["actor_label", "action", "target_label"],
});
const displayRows = computed(() => rows.value.map((event) => ({ event, link: targetLink(event) })));

const actorItems = computed(() => [
  { value: "", label: t("admin.auditAnyActor") },
  ...users.value.map((user) => ({ value: String(user.id), label: user.email, sublabel: user.full_name })),
]);

// Suggests every action seen so far and its resource, so typing "source."
// selects all source events. The server owns the list of actions.
const actionSuggestions = computed(() => {
  const suggestions = new Set<string>();
  for (const action of seenActions.value) {
    const dot = action.indexOf(".");
    if (dot > 0) suggestions.add(action.slice(0, dot + 1));
    suggestions.add(action);
  }
  return [...suggestions].sort();
});

function formatTime(value: string): string {
  const ms = Date.parse(value);
  if (Number.isNaN(ms)) return value;
  return formatInZone(epochMs(ms), preferencesStore.zone, { dateStyle: "medium", timeStyle: "medium" }, locale.value);
}

function actorLabel(event: AuditEvent): string {
  return event.actor_label || event.actor_type;
}

function targetLabel(event: AuditEvent): string {
  return event.target_label || event.target_id;
}

function targetLink(event: AuditEvent): RouteLocationRaw | null {
  const id = Number(event.target_id);
  if (!Number.isInteger(id)) return null;
  if (event.target_type === "source" && sourceIds.value.has(id)) {
    return { name: "EditSource", params: { sourceId: id } };
  }
  if (event.target_type === "team" && teamIds.value.has(id)) {
    return { name: "TeamSettings", params: { id } };
  }
  return null;
}

function historyLink(event: AuditEvent): RouteLocationRaw {
  return { name: "AdminAuditLog", query: { target_type: event.target_type, target_id: event.target_id } };
}

// An empty string shows as "" so that a cleared field is visible.
function formatValue(value: unknown): string {
  return typeof value === "string" && value !== "" ? value : JSON.stringify(value);
}

function toggleRow(id: string) {
  const next = new Set(expanded.value);
  if (next.has(id)) next.delete(id);
  else next.add(id);
  expanded.value = next;
}

function eventDetails(event: AuditEvent): { label: string; value: string }[] {
  return [
    { label: t("admin.auditReason"), value: event.reason },
    { label: t("admin.auditRoute"), value: [event.http_method, event.route].filter(Boolean).join(" ") },
    { label: t("admin.auditStatus"), value: event.status ? String(event.status) : "" },
    { label: t("admin.auditAuthMethod"), value: event.auth_method },
    { label: t("admin.auditCredential"), value: event.credential_id },
    { label: t("admin.auditClient"), value: event.client_id },
    { label: t("admin.auditUserAgent"), value: event.user_agent },
    { label: t("admin.auditEventId"), value: event.id },
  ].filter((row) => row.value);
}

const COLUMN_COUNT = 7;
</script>

<template>
  <div class="space-y-6">
    <PageHeader :title="t('routes.auditLog')">
      <template #actions>
        <Button variant="ghost" size="sm" as-child>
          <router-link :to="{ name: 'AdminQueryActivity' }">{{ t('routes.queryActivity') }}</router-link>
        </Button>
        <Button variant="outline" size="sm" :disabled="isLoading" @click="loadEvents">
          <RefreshCw :class="['mr-2 h-3.5 w-3.5', isLoading && 'animate-spin']" />
          {{ t('ui.refresh') }}
        </Button>
        <Button variant="outline" size="sm" :disabled="isExporting" @click="download">
          <Download class="mr-2 h-3.5 w-3.5" />
          {{ t('admin.auditDownload') }}
        </Button>
      </template>
    </PageHeader>

    <div class="flex flex-wrap items-end gap-3">
      <DateTimePicker
        :model-value="timeWindow"
        :resolved="resolvedWindow"
        :zone-preference="preferencesStore.preferences.timezone"
        @update:model-value="setWindow"
        @update:zone-preference="preferencesStore.updatePreferences({ timezone: $event })"
      />
      <div class="w-56">
        <Input
          v-model="actionInput"
          list="audit-action-suggestions"
          class="h-9 font-mono text-xs"
          :placeholder="t('admin.auditActionPlaceholder')"
          :aria-label="t('admin.auditAction')"
          @change="applyAction"
        />
        <datalist id="audit-action-suggestions">
          <option v-for="action in actionSuggestions" :key="action" :value="action" />
        </datalist>
      </div>
      <SearchableSelect
        :model-value="actorId"
        :items="actorItems"
        :placeholder="t('admin.auditAnyActor')"
        :search-placeholder="t('admin.auditSearchActors')"
        trigger-class="h-9 w-56"
        @update:model-value="setQuery({ actor_id: $event || null })"
      />
      <NativeSelect
        :model-value="outcome"
        class="h-9 w-36"
        :aria-label="t('admin.auditOutcome')"
        @update:model-value="setQuery({ outcome: typeof $event === 'string' && $event ? $event : null })"
      >
        <NativeSelectOption value="">{{ t('admin.auditAnyOutcome') }}</NativeSelectOption>
        <NativeSelectOption value="success">{{ t('admin.auditOutcomeSuccess') }}</NativeSelectOption>
        <NativeSelectOption value="failure">{{ t('admin.auditOutcomeFailure') }}</NativeSelectOption>
      </NativeSelect>
      <div class="flex items-center gap-1">
        <Input
          v-model="targetTypeInput"
          class="h-9 w-36 font-mono text-xs"
          :placeholder="t('admin.auditTargetType')"
          :aria-label="t('admin.auditTargetType')"
          @change="applyTarget"
        />
        <Input
          v-model="targetIdInput"
          class="h-9 w-28 font-mono text-xs"
          :disabled="!targetTypeInput.trim()"
          :placeholder="t('admin.auditTargetId')"
          :aria-label="t('admin.auditTargetId')"
          @change="applyTarget"
        />
      </div>
    </div>

    <div
      v-for="message in [lookupError, error].filter(Boolean)"
      :key="message ?? ''"
      class="rounded-md border border-destructive/50 bg-destructive/10 p-4 text-sm text-destructive"
    >
      {{ message }}
    </div>

    <LoadingState v-if="isLoading" :label="t('admin.auditLoading')" />

    <template v-else-if="!error || events.length">
      <SearchInput v-model="search" :placeholder="t('admin.auditSearch')" />

      <EmptyState
        v-if="!events.length && !hasServerFilters"
        :icon="ScrollText"
        :title="t('admin.auditNoEvents')"
      />
      <EmptyState
        v-else-if="!rows.length"
        :icon="ScrollText"
        :title="t('admin.auditNoMatch')"
      />

      <div v-else class="overflow-hidden rounded-lg border bg-card shadow-sm">
        <div class="overflow-x-auto">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead class="w-8" />
                <TableHead>
                  <SortHeader column="time" sort-key="time" :sort-dir="order" @toggle="toggleOrder">
                    {{ t('admin.time') }}
                  </SortHeader>
                </TableHead>
                <TableHead>{{ t('admin.auditActor') }}</TableHead>
                <TableHead>{{ t('admin.auditAction') }}</TableHead>
                <TableHead>{{ t('admin.auditTarget') }}</TableHead>
                <TableHead>{{ t('admin.auditOutcome') }}</TableHead>
                <TableHead>{{ t('admin.auditIp') }}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              <template v-for="{ event, link } in displayRows" :key="event.id">
                <TableRow class="cursor-pointer" data-audit-row @click="toggleRow(event.id)">
                  <TableCell class="w-8 text-muted-foreground">
                    <component :is="expanded.has(event.id) ? ChevronDown : ChevronRight" class="h-4 w-4" />
                  </TableCell>
                  <TableCell class="whitespace-nowrap text-sm tabular-nums">{{ formatTime(event.occurred_at) }}</TableCell>
                  <TableCell class="text-sm">
                    <span>{{ actorLabel(event) }}</span>
                    <Badge v-if="event.auth_method !== 'session'" variant="outline" class="ml-2 font-mono text-[10px] font-normal">
                      {{ event.auth_method }}
                    </Badge>
                  </TableCell>
                  <TableCell><code class="text-xs">{{ event.action }}</code></TableCell>
                  <TableCell class="text-sm">
                    <span class="mr-1 text-xs text-muted-foreground">{{ event.target_type }}</span>
                    <router-link
                      v-if="link"
                      :to="link"
                      class="text-primary hover:underline"
                      @click.stop
                    >{{ targetLabel(event) }}</router-link>
                    <span v-else>{{ targetLabel(event) }}</span>
                  </TableCell>
                  <TableCell>
                    <Badge :variant="event.outcome === 'failure' ? 'destructive' : 'secondary'" class="font-normal">
                      {{ event.outcome === 'failure' ? t('admin.auditOutcomeFailure') : event.outcome === 'success' ? t('admin.auditOutcomeSuccess') : event.outcome }}
                    </Badge>
                  </TableCell>
                  <TableCell class="font-mono text-xs text-muted-foreground">{{ event.ip }}</TableCell>
                </TableRow>
                <TableRow v-if="expanded.has(event.id)" class="bg-muted/30 hover:bg-muted/30" data-audit-detail>
                  <TableCell :colspan="COLUMN_COUNT" class="whitespace-normal px-10 py-4">
                    <div class="grid gap-6 lg:grid-cols-2">
                      <div class="space-y-2">
                        <h3 class="text-xs font-semibold uppercase text-muted-foreground">{{ t('admin.auditChanges') }}</h3>
                        <p v-if="!Object.keys(event.changes ?? {}).length" class="text-sm text-muted-foreground">{{ t('admin.auditNoChanges') }}</p>
                        <dl v-else class="space-y-1 text-sm">
                          <div v-for="(change, field) in event.changes" :key="field" class="flex flex-wrap items-baseline gap-2">
                            <dt class="font-mono text-xs font-medium">{{ field }}</dt>
                            <dd class="flex flex-wrap items-baseline gap-2 font-mono text-xs">
                              <span v-if="'from' in change" class="text-destructive line-through">{{ formatValue(change.from) }}</span>
                              <span v-if="'from' in change && 'to' in change" class="text-muted-foreground">→</span>
                              <span v-if="'to' in change" class="text-emerald-600 dark:text-emerald-400">{{ formatValue(change.to) }}</span>
                            </dd>
                          </div>
                        </dl>
                        <template v-if="Object.keys(event.detail ?? {}).length">
                          <h3 class="pt-2 text-xs font-semibold uppercase text-muted-foreground">{{ t('admin.auditDetail') }}</h3>
                          <dl class="space-y-1 text-sm">
                            <div v-for="(value, key) in event.detail" :key="key" class="flex flex-wrap items-baseline gap-2">
                              <dt class="font-mono text-xs font-medium">{{ key }}</dt>
                              <dd class="font-mono text-xs">{{ formatValue(value) }}</dd>
                            </div>
                          </dl>
                        </template>
                      </div>
                      <div class="space-y-2">
                        <dl class="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1 text-sm">
                          <template v-for="row in eventDetails(event)" :key="row.label">
                            <dt class="text-xs text-muted-foreground">{{ row.label }}</dt>
                            <dd class="break-all font-mono text-xs">{{ row.value }}</dd>
                          </template>
                        </dl>
                        <router-link
                          v-if="event.target_type && event.target_id"
                          :to="historyLink(event)"
                          class="inline-flex items-center gap-1 text-xs text-primary hover:underline"
                        >
                          <History class="h-3.5 w-3.5" />
                          {{ t('admin.auditTargetHistory') }}
                        </router-link>
                      </div>
                    </div>
                  </TableCell>
                </TableRow>
              </template>
            </TableBody>
          </Table>
        </div>
      </div>

      <div v-if="nextCursor" class="flex justify-center">
        <Button variant="outline" size="sm" :disabled="isLoadingMore" @click="loadMore">
          {{ isLoadingMore ? t('admin.auditLoadingMore') : t('admin.auditLoadMore') }}
        </Button>
      </div>
    </template>
  </div>
</template>
