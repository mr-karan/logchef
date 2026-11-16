<script setup lang="ts">
import { computed, onActivated, onMounted, ref, watch } from "vue";
import { useIntervalFn, useNow } from "@vueuse/core";
import { useI18n } from "vue-i18n";
import { BellOff, Bell, Pencil, Plus, RefreshCcw, Search } from "lucide-vue-next";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import ConfirmDialog from "@/components/ui/ConfirmDialog.vue";
import ErrorAlert from "@/components/ui/ErrorAlert.vue";
import { EmptyState, LoadingState, PageHeader, RowActionsMenu, SearchInput, SortHeader } from "@/components/layout";
import AlertsTabs from "@/components/alerts/AlertsTabs.vue";
import SilenceForm from "@/components/alerts/SilenceForm.vue";
import TeamSourceSelector from "@/views/explore/components/TeamSourceSelector.vue";
import { useAlertsStore } from "@/stores/alerts";
import { useAuthStore } from "@/stores/auth";
import { useContextStore } from "@/stores/context";
import { useMetaStore } from "@/stores/meta";
import { usePreferencesStore } from "@/stores/preferences";
import { useSilencesStore } from "@/stores/silences";
import { useSourcesStore } from "@/stores/sources";
import { useTeamsStore } from "@/stores/teams";
import { useContextSync } from "@/composables/useContextSync";
import { useTableSearchSort } from "@/composables/useTableSearchSort";
import type { AlertSilence, AlertSilenceRequest } from "@/api/silences";
import { describeSilenceSchedule, silenceRequestFrom, silenceStatus, type SilenceStatus } from "@/utils/alertSilences";
import { formatAlertGroup } from "@/utils/alertGroups";

const { t, locale } = useI18n();
const alertsStore = useAlertsStore();
const authStore = useAuthStore();
const contextStore = useContextStore();
const metaStore = useMetaStore();
const preferences = usePreferencesStore();
const silencesStore = useSilencesStore();
const sourcesStore = useSourcesStore();
const teamsStore = useTeamsStore();

const { initialize: initializeContext, handleTeamChange, handleSourceChange } = useContextSync({ basePath: "/logs/alerts/silences" });

const currentTeamId = computed(() => contextStore.teamId);
const currentSourceId = computed(() => contextStore.sourceId);
const availableTeams = computed(() => teamsStore.teams || []);
const availableSources = computed(() => sourcesStore.teamSources || []);

const silences = computed(() => silencesStore.silencesOf(currentSourceId.value));
const rules = computed(() => alertsStore.alerts.filter((a) => a.source_id === currentSourceId.value));
const isLoading = computed(() => !!currentSourceId.value && silencesStore.isLoadingOperation(`fetchSilences-${currentSourceId.value}`));
const loadError = computed(() => (silencesStore.error ? { message: silencesStore.error.message } : null));

function ruleName(s: AlertSilence): string {
  if (s.alert_id == null) return t("silences.allRules");
  return rules.value.find((r) => r.id === s.alert_id)?.name ?? t("silences.ruleUnknown", { id: s.alert_id });
}

// The server sends user ids as numbers; the session user's id is typed as a string.
function isMine(s: AlertSilence): boolean {
  return s.created_by != null && authStore.user != null && String(s.created_by) === String(authStore.user.id);
}

function authorName(s: AlertSilence): string {
  if (s.created_by == null) return "";
  if (isMine(s)) return t("silences.authorYou");
  const member = currentTeamId.value ? teamsStore.getTeamMembersByTeamId(currentTeamId.value).find((m) => m.user_id === s.created_by) : undefined;
  return member ? member.full_name || member.email : t("silences.authorUnknown", { id: s.created_by });
}

const statusOrder: Record<SilenceStatus, number> = { active: 0, scheduled: 1, expired: 2 };
// Ticks so a status changes when a silence starts or ends while the page is open.
const now = useNow({ scheduler: (tick) => useIntervalFn(tick, 30_000) });
const status = (s: AlertSilence) => silenceStatus(s, now.value.getTime());

const { search, rows, sortKey, sortDir, toggleSort } = useTableSearchSort(silences, {
  searchKeys: ["comment", ruleName, authorName, (s) => formatAlertGroup(s.matchers)],
  sortAccessors: {
    rule: (s) => ruleName(s).toLowerCase(),
    status: (s) => statusOrder[status(s)],
    author: (s) => authorName(s).toLowerCase(),
  },
  initialSort: { key: "rule", dir: "asc" },
  persistKey: "alert-silences",
});

// Only the creator and global admins may change a silence.
function canEdit(s: AlertSilence): boolean {
  return authStore.user?.role === "admin" || isMine(s);
}

function statusVariant(value: SilenceStatus): "default" | "secondary" | "outline" {
  if (value === "active") return "default";
  if (value === "scheduled") return "secondary";
  return "outline";
}

const formOpen = ref(false);
const editing = ref<AlertSilence | null>(null);
const preset = ref<AlertSilenceRequest | null>(null);

function openNew() {
  editing.value = null;
  preset.value = null;
  formOpen.value = true;
}

function openEdit(s: AlertSilence) {
  editing.value = s;
  preset.value = null;
  formOpen.value = true;
}

function openDuplicate(s: AlertSilence) {
  editing.value = null;
  preset.value = silenceRequestFrom(s, Date.now());
  formOpen.value = true;
}

const deleteTarget = ref<AlertSilence | null>(null);
const deleteOpen = ref(false);
function confirmDelete(s: AlertSilence) {
  deleteTarget.value = s;
  deleteOpen.value = true;
}
async function doDelete() {
  if (deleteTarget.value) await silencesStore.deleteSilence(deleteTarget.value);
  deleteTarget.value = null;
}

function load() {
  const teamId = currentTeamId.value;
  const sourceId = currentSourceId.value;
  if (teamId) void teamsStore.listTeamMembers(teamId);
  if (!teamId || !sourceId) return;
  void silencesStore.fetchSilences(sourceId);
  void alertsStore.fetchAlerts(sourceId);
}

watch(() => [currentTeamId.value, currentSourceId.value] as const, load, { immediate: true });
// LogsLayout keeps this page alive; a return shows the current state.
let activated = false;
onActivated(() => {
  if (activated) load();
  activated = true;
});
onMounted(initializeContext);
</script>

<template>
  <EmptyState
    v-if="!metaStore.alertsEnabled"
    :icon="Bell"
    :title="t('ui.alertingIsDisabled')"
    :description="t('ui.alertingIsDisabledOnThisServerAskYourAdministratorToSetAlerts')"
  />
  <div v-else class="space-y-6">
    <PageHeader :title="t('silences.title')">
      <template #actions>
        <Button variant="outline" :disabled="!currentSourceId || isLoading" @click="load">
          <RefreshCcw class="-ml-1 mr-2 h-4 w-4" />
          {{ t('ui.refresh') }}
        </Button>
        <Button :disabled="!currentSourceId" @click="openNew">
          <Plus class="-ml-1 mr-2 h-4 w-4" />
          {{ t('silences.newSilence') }}
        </Button>
      </template>
    </PageHeader>

    <AlertsTabs />

    <Card>
      <CardHeader class="flex items-start justify-between gap-4 space-y-0">
        <CardTitle>{{ t('ui.scope') }}</CardTitle>
        <TeamSourceSelector
          :current-team-id="currentTeamId"
          :current-source-id="currentSourceId"
          :available-teams="availableTeams"
          :available-sources="availableSources"
          @update:team="handleTeamChange"
          @update:source="handleSourceChange"
        />
      </CardHeader>
      <CardContent>
        <div v-if="loadError" class="mb-4">
          <ErrorAlert :error="loadError" :title="t('silences.loadFailed')" @retry="load" />
        </div>
        <EmptyState v-if="!currentTeamId" :icon="BellOff" :description="t('ui.selectATeamToManageAlerts')" />
        <EmptyState v-else-if="!currentSourceId" :icon="BellOff" :description="t('silences.selectSource')" />
        <LoadingState v-else-if="isLoading && !silences.length" :label="t('silences.loading')" />
        <EmptyState v-else-if="!silences.length" :icon="BellOff" :title="t('silences.empty')" :description="t('silences.emptyDescription')">
          <template #action>
            <Button @click="openNew">
              <Plus class="-ml-1 mr-2 h-4 w-4" />
              {{ t('silences.newSilence') }}
            </Button>
          </template>
        </EmptyState>
        <div v-else class="space-y-4">
          <SearchInput v-model="search" :placeholder="t('silences.search')" />
          <EmptyState
            v-if="!rows.length"
            :icon="Search"
            :title="t('silences.noMatches', { search })"
            :description="t('ui.tryADifferentNameOrClearTheSearch')"
          />
          <Table v-else>
            <TableHeader>
              <TableRow>
                <TableHead><SortHeader column="rule" :sort-key="sortKey" :sort-dir="sortDir" @toggle="toggleSort">{{ t('silences.rule') }}</SortHeader></TableHead>
                <TableHead>{{ t('silences.matchers') }}</TableHead>
                <TableHead>{{ t('silences.schedule') }}</TableHead>
                <TableHead><SortHeader column="status" :sort-key="sortKey" :sort-dir="sortDir" @toggle="toggleSort">{{ t('ui.status') }}</SortHeader></TableHead>
                <TableHead><SortHeader column="author" :sort-key="sortKey" :sort-dir="sortDir" @toggle="toggleSort">{{ t('silences.author') }}</SortHeader></TableHead>
                <TableHead class="w-24 text-right">{{ t('ui.actions') }}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              <TableRow v-for="s in rows" :key="s.id" :data-silence-id="s.id">
                <TableCell class="py-3">
                  <div class="font-medium">{{ ruleName(s) }}</div>
                  <p class="text-sm text-muted-foreground line-clamp-2">{{ s.comment }}</p>
                </TableCell>
                <TableCell class="py-3 font-mono text-xs">
                  {{ formatAlertGroup(s.matchers) || t('silences.anyGroup') }}
                </TableCell>
                <TableCell class="py-3 text-sm">{{ describeSilenceSchedule(s, t, locale, preferences.zone) }}</TableCell>
                <TableCell class="py-3">
                  <Badge :variant="statusVariant(status(s))">{{ t(`silences.status_${status(s)}`) }}</Badge>
                </TableCell>
                <TableCell class="py-3 text-sm">{{ authorName(s) }}</TableCell>
                <TableCell class="py-3 text-right">
                  <div class="inline-flex items-center gap-0.5">
                    <Button v-if="canEdit(s)" variant="ghost" size="icon" class="h-8 w-8" :title="t('ui.edit')" :aria-label="t('ui.edit')" @click="openEdit(s)">
                      <Pencil class="h-4 w-4" />
                    </Button>
                    <RowActionsMenu
                      :on-duplicate="() => openDuplicate(s)"
                      :on-delete="canEdit(s) ? () => confirmDelete(s) : undefined"
                    />
                  </div>
                </TableCell>
              </TableRow>
            </TableBody>
          </Table>
        </div>
      </CardContent>
    </Card>

    <SilenceForm
      v-if="currentSourceId"
      v-model:open="formOpen"
      :source-id="currentSourceId"
      :rules="rules"
      :silence="editing"
      :preset="preset"
    />
    <ConfirmDialog
      v-model:open="deleteOpen"
      :title="t('silences.deleteTitle')"
      :description="t('silences.deleteDescription')"
      :confirm-text="t('ui.delete')"
      destructive
      @confirm="doDelete"
      @cancel="deleteTarget = null"
    />
  </div>
</template>
