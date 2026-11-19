<script setup lang="ts">
import { useI18n } from "vue-i18n";
import { severityLabels, alertStateLabels, instanceStateLabels } from "@/i18n/alertLabels";

import { computed, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { ArrowLeft, Bell, BellOff, Trash2, CheckCircle2, AlertCircle, AlertTriangle, Clock, History, ScrollText } from "lucide-vue-next";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import ConfirmDialog from "@/components/ui/ConfirmDialog.vue";
import { useAlertsStore } from "@/stores/alerts";
import { useAlertHistoryStore } from "@/stores/alertHistory";
import { useAskChefStore } from "@/stores/askChef";
import { useContextStore } from "@/stores/context";
import { useMetaStore } from "@/stores/meta";
import { usePreferencesStore } from "@/stores/preferences";
import { epochMs, formatInZone } from "@/utils/timeWindow";
import { useTeamsStore } from "@/stores/teams";
import { useToast } from "@/composables/useToast";
import { alertHistoryExploreQuery } from "@/utils/alertExplore";
import AlertDeliveryStatus from "@/components/alerts/AlertDeliveryStatus.vue";
import AlertForm from "@/components/alerts/AlertForm.vue";
import SilenceForm from "@/components/alerts/SilenceForm.vue";
import { useSilencesStore } from "@/stores/silences";
import { activeSilencesFor, groupMatchers } from "@/utils/alertSilences";
import { useIntervalFn, useNow } from "@vueuse/core";
import type { AlertSilenceRequest } from "@/api/silences";
import { EmptyState, LoadingState } from "@/components/layout";
import type { Alert, AlertHistoryEntry, AlertInstance, UpdateAlertRequest } from "@/api/alerts";
import { formatAlertGroup } from "@/utils/alertGroups";

const { t, locale } = useI18n();

const route = useRoute();
const router = useRouter();

const alertsStore = useAlertsStore();
const alertHistoryStore = useAlertHistoryStore();
const contextStore = useContextStore();
const metaStore = useMetaStore();
const askChef = useAskChefStore();
const teamsStore = useTeamsStore();
const { toast } = useToast();
const preferencesStore = usePreferencesStore();
const silencesStore = useSilencesStore();

function formatEventTime(timestamp: string): string {
  const date = new Date(timestamp);
  if (Number.isNaN(date.getTime())) return timestamp;
  return formatInZone(epochMs(date), preferencesStore.zone, { dateStyle: "medium", timeStyle: "short" }, locale.value);
}

const alertId = computed(() => Number(route.params.alertID));
const currentTab = computed({
  get: () => route.query.tab === "history" ? "history" : "edit",
  set: (tab: string | number) => {
    const { tab: _tab, ...query } = route.query;
    void router.replace({ query: tab === "history" ? { ...query, tab } : query });
  },
});
const showDeleteDialog = ref(false);

const alert = computed(() => {
  return alertsStore.alerts.find((a) => a.id === alertId.value) || null;
});

const currentTeamId = computed(() => contextStore.teamId);
const currentSourceId = computed(() => contextStore.sourceId);

const historyEntries = computed(() => {
  // Only return entries if they belong to the current alert
  if (alertHistoryStore.currentAlertId !== alertId.value) return [];
  return alertHistoryStore.entries;
});

const instances = computed(() => {
  if (alertHistoryStore.currentAlertId !== alertId.value) return [];
  return alertHistoryStore.instances;
});

const isLoadingHistory = computed(() => {
  return alertHistoryStore.isLoadingOperation(`loadHistory-${alertId.value}`);
});

function mapSeverityVariant(severity: Alert["severity"]): "destructive" | "outline" | "secondary" {
  switch (severity) {
    case "critical":
      return "destructive";
    case "warning":
      return "outline";
    default:
      return "secondary";
  }
}

function goBack() {
  const { tab: _tab, ...query } = route.query;
  router.push({ name: "AlertsOverview", query });
}

async function handleUpdate(payload: UpdateAlertRequest) {
  if (!alert.value) return;
  await alertsStore.updateAlert(alert.value.id, payload);
}

function confirmDelete() {
  showDeleteDialog.value = true;
}

async function handleDelete() {
  if (!alert.value) return;
  const result = await alertsStore.deleteAlert(alert.value.id);
  if (result.success) {
    goBack();
  }
}

// Alerts are not team-scoped, but Explorer needs a team that has the source.
// The current team is tried first, since it usually has it.
async function findTeamWithSource(sourceId: number): Promise<number | null> {
  if (!teamsStore.userTeams.length) {
    await teamsStore.loadUserTeams();
  }
  const teamIds = new Set([currentTeamId.value, ...teamsStore.userTeams.map((team) => team.id)]);
  for (const teamId of teamIds) {
    if (teamId == null) continue;
    if (!teamsStore.getTeamSourcesByTeamId(teamId).length) {
      await teamsStore.listTeamSources(teamId);
    }
    if (teamsStore.getTeamSourcesByTeamId(teamId).some((source) => source.id === sourceId)) {
      return teamId;
    }
  }
  return null;
}

async function viewLogs(entry: AlertHistoryEntry): Promise<{ alert: Alert; teamId: number } | null> {
  // The alerts list can reload while teams load, so keep this alert.
  const current = alert.value;
  if (!current) return null;
  const teamId = await findTeamWithSource(current.source_id);
  if (teamId == null) {
    toast({ title: t("alerts.viewLogsNoTeam"), variant: "destructive" });
    return null;
  }
  const failure = await router.push({ name: "LogExplorer", query: alertHistoryExploreQuery(current, entry, teamId) });
  return failure ? null : { alert: current, teamId };
}

async function investigate(entry: AlertHistoryEntry) {
  const opened = await viewLogs(entry);
  if (opened) askChef.investigateAlert(opened.alert, entry, opened.teamId);
}

async function loadHistory() {
  if (!alertId.value) return;
  await Promise.all([
    alertHistoryStore.loadHistory(alertId.value),
    alertHistoryStore.loadInstances(alertId.value),
  ]);
}

// The silenced badge needs the source's silences, and the alert can load after the tab opens.
watch(() => (currentTab.value === "history" ? alert.value?.source_id : undefined), (sourceId) => {
  if (sourceId) void silencesStore.fetchSilences(sourceId);
}, { immediate: true });

// Ticks so the badge drops when a silence ends while the page is open.
const now = useNow({ scheduler: (tick) => useIntervalFn(tick, 30_000) });
function isSilenced(instance: AlertInstance): boolean {
  return !!alert.value && activeSilencesFor(silencesStore.silencesOf(alert.value.source_id), alert.value, instance.group, now.value.getTime()).length > 0;
}

// Silence opens the silence form for this rule and group, for one hour from now.
const silenceOpen = ref(false);
const silencePreset = ref<AlertSilenceRequest | null>(null);
function openSilence(instance: AlertInstance) {
  if (!alert.value) return;
  const now = Date.now();
  silencePreset.value = {
    source_id: alert.value.source_id,
    alert_id: alert.value.id,
    matchers: groupMatchers(alert.value, instance.group),
    comment: "",
    schedule_kind: "once",
    starts_at: new Date(now).toISOString(),
    ends_at: new Date(now + 3_600_000).toISOString(),
    weekly: null,
  };
  silenceOpen.value = true;
}

async function handleResolve(groupKey: string, message: string) {
  if (!alertId.value) return;
  const result = await alertHistoryStore.resolveAlert(
    alertId.value,
    { message, group_key: groupKey }
  );
  if (result.success) {
    await loadHistory();
  }
}

// An episode stays open while its group fires or has no data.
function isOpen(entry: AlertHistoryEntry): boolean {
  return (entry.status === "triggered" || entry.status === "no_data") && !entry.resolved_at;
}

function instanceVariant(state: AlertInstance["state"]): "destructive" | "outline" | "secondary" {
  if (state === "firing") return "destructive";
  if (state === "no_data") return "outline";
  return "secondary";
}

function groupLabel(group: AlertInstance["group"] | undefined): string {
  return formatAlertGroup(group) || t("alerts.wholeResult");
}

watch([alertId, currentTab], ([id, tab]) => {
  alertHistoryStore.setCurrentContext(id || null);
  if (!id) return;
  if (!alert.value) {
    void alertsStore.refreshAlert(id);
  }
  if (tab === "history") {
    void loadHistory();
  }
}, { immediate: true });
</script>

<template>
  <EmptyState
    v-if="!metaStore.alertsEnabled"
    :icon="Bell"
    :title="t('ui.alertingIsDisabled')"
    :description="t('ui.alertingIsDisabledOnThisServerAskYourAdministratorToSetAlerts')"
  />
  <div v-else class="space-y-6">
    <!-- Header Section -->
    <div class="flex items-start justify-between gap-4">
      <div class="flex items-start gap-3">
        <Button variant="ghost" size="icon" :aria-label="t('ui.goBack')" @click="goBack">
          <ArrowLeft class="h-5 w-5" />
        </Button>
        <div class="space-y-1">
          <div class="flex items-center gap-2 flex-wrap">
            <h1 class="text-2xl font-bold tracking-tight">{{ alert?.name || t('ui.alert') }}</h1>
            <Badge v-if="alert" :variant="mapSeverityVariant(alert.severity)" class="capitalize">
              {{ t(severityLabels[alert.severity]) }}
            </Badge>
            <Badge v-if="alert && !alert.is_active" variant='outline'>{{ t('ui.disabled') }}</Badge>
          </div>
          <p v-if="alert?.description" class="text-muted-foreground">
            {{ alert.description }}
          </p>
        </div>
      </div>
      <Button variant="outline" @click="confirmDelete" :disabled="!alert">
        <Trash2 class="mr-2 h-4 w-4" />
        {{ t('ui.delete') }}
      </Button>
    </div>

    <!-- Loading State -->
    <EmptyState v-if="!alert" :icon="Bell" :description="t('ui.alertNotFoundOrStillLoading')">
      <template #action>
        <Button variant="outline" @click="goBack">{{ t('ui.goBack') }}</Button>
      </template>
    </EmptyState>

    <!-- Main Content with Tabs -->
    <Tabs v-else v-model="currentTab" class="space-y-6">
      <TabsList>
        <TabsTrigger value="edit">{{ t('ui.configuration') }}</TabsTrigger>
        <TabsTrigger value="history">{{ t('ui.history') }}</TabsTrigger>
      </TabsList>

      <!-- Edit Configuration Tab -->
      <TabsContent value="edit">
        <Card>
          <CardHeader>
            <CardTitle>{{ t('ui.alertConfiguration') }}</CardTitle>
          </CardHeader>
          <CardContent>
            <AlertForm
              :open="true"
              mode="edit"
              :team-id="currentTeamId"
              :source-id="currentSourceId"
              :alert="alert"
              @cancel="goBack"
              @update="handleUpdate"
              :inline="true"
            />
          </CardContent>
        </Card>
      </TabsContent>

      <!-- History Tab -->
      <TabsContent value="history" class="space-y-6">
        <Card>
          <CardHeader>
            <CardTitle>{{ t('alerts.groups') }}</CardTitle>
          </CardHeader>
          <CardContent>
            <p v-if="!instances.length" class="text-sm text-muted-foreground">{{ t('alerts.noGroupFiring') }}</p>
            <Table v-else>
              <TableHeader>
                <TableRow>
                  <TableHead>{{ t('alerts.group') }}</TableHead>
                  <TableHead>{{ t('ui.status') }}</TableHead>
                  <TableHead>{{ t('panelStyle.value') }}</TableHead>
                  <TableHead>{{ t('alerts.since') }}</TableHead>
                  <TableHead class="text-right">{{ t('ui.actions') }}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                <TableRow v-for="instance in instances" :key="instance.group_key">
                  <TableCell class="font-mono text-xs">{{ groupLabel(instance.group) }}</TableCell>
                  <TableCell>
                    <div class="flex items-center gap-1.5">
                      <Badge :variant="instanceVariant(instance.state)" class="text-xs">
                        {{ t(instanceStateLabels[instance.state]) }}
                      </Badge>
                      <Badge v-if="isSilenced(instance)" variant="outline" class="text-xs gap-1">
                        <BellOff class="h-3 w-3" />{{ t('silences.silenced') }}
                      </Badge>
                    </div>
                  </TableCell>
                  <TableCell class="tabular-nums">{{ instance.value ?? '' }}</TableCell>
                  <TableCell class="text-sm text-muted-foreground">{{ formatEventTime(instance.state_since) }}</TableCell>
                  <TableCell class="text-right space-x-2">
                    <Button variant="outline" size="sm" @click="openSilence(instance)">
                      {{ t('silences.silence') }}
                    </Button>
                    <Button
                      v-if="instance.state !== 'ok'"
                      variant="outline"
                      size="sm"
                      @click="handleResolve(instance.group_key, 'Manually resolved')"
                    >
                      {{ t('ui.resolve') }}
                    </Button>
                  </TableCell>
                </TableRow>
              </TableBody>
            </Table>
          </CardContent>
        </Card>
        <Card>
          <CardHeader>
            <CardTitle>{{ t('ui.alertHistory') }}</CardTitle>
          </CardHeader>
          <CardContent>
            <!-- Loading State -->
            <LoadingState v-if="isLoadingHistory" :label="t('ui.loadingHistory')" />

            <!-- Empty State -->
            <EmptyState
              v-else-if="!historyEntries.length"
              :icon="History"
              :title="t('ui.noHistoryYet')"
              :description="t('ui.thisAlertHasnTBeenTriggeredYetHistoryWillAppearHereOnce')"
            />

            <!-- Timeline -->
            <div v-else class="relative space-y-4 py-2">
              <!-- Timeline Line -->
              <div class="absolute left-4 top-0 bottom-0 w-px bg-border"></div>

              <!-- Timeline Entries -->
              <div
                v-for="entry in historyEntries"
                :key="entry.id"
                class="relative pl-12 pb-4"
              >
                <!-- Timeline Node -->
                <div class="absolute left-4 -translate-x-1/2 flex items-center justify-center">
                  <div
                    :class="[
                      'flex items-center justify-center rounded-full p-1 ring-4 ring-background',
                      entry.status === 'triggered' && !entry.resolved_at
                        ? 'bg-destructive text-destructive-foreground'
                        : entry.status === 'no_data' && !entry.resolved_at
                        ? 'bg-amber-500 text-white'
                        : entry.status === 'resolved' || entry.resolved_at
                        ? 'bg-green-500 text-white'
                        : entry.status === 'error'
                        ? 'bg-yellow-500 text-white'
                        : 'bg-muted text-muted-foreground'
                    ]"
                  >
                    <AlertCircle v-if="isOpen(entry)" class="h-3.5 w-3.5" />
                    <CheckCircle2 v-else-if="entry.status === 'resolved' || entry.resolved_at" class="h-3.5 w-3.5" />
                    <AlertTriangle v-else-if="entry.status === 'error'" class="h-3.5 w-3.5" />
                    <Clock v-else class="h-3.5 w-3.5" />
                  </div>
                </div>

                <!-- Timeline Content -->
                <div class="rounded-lg border bg-card p-3 space-y-2">
                  <!-- Header -->
                  <div class="flex items-start justify-between gap-4">
                    <div class="space-y-1 flex-1">
                      <div class="flex items-center gap-2 flex-wrap">
                        <Badge
                          :variant="entry.status === 'triggered' ? 'destructive' : entry.status === 'error' ? 'outline' : 'secondary'"
                          class="capitalize text-xs"
                        >
                          {{ t(alertStateLabels[entry.status]) }}
                        </Badge>
                        <span class="text-sm text-muted-foreground">
                          {{ formatEventTime(entry.triggered_at) }}
                        </span>
                        <code v-if="entry.group_key" class="text-xs font-mono bg-muted px-1.5 py-0.5 rounded">
                          {{ groupLabel(entry.group) }}
                        </code>
                        <!-- Value Display Inline -->
                        <span v-if="entry.value != null" class="text-sm text-muted-foreground">
                          {{ t('ui.value') }} <code class='font-mono font-semibold'>{{ entry.value }}</code>
                        </span>
                      </div>
                      <!-- Message -->
                      <div v-if="entry.message" class="text-sm text-muted-foreground">
                        {{ entry.message }}
                      </div>
                    </div>

                    <div class="flex items-center gap-2">
                      <Button
                        v-if="entry.status !== 'error'"
                        variant="ghost"
                        size="sm"
                        @click="viewLogs(entry)"
                      >
                        <ScrollText class="mr-1.5 h-4 w-4" />
                        {{ t('alerts.viewLogs') }}
                      </Button>
                      <Button
                        v-if="entry.status !== 'error' && metaStore.askChefEnabled"
                        variant="ghost"
                        size="sm"
                        @click="investigate(entry)"
                      >
                        <span class="mr-1.5 text-violet-600" aria-hidden="true">✦</span>
                        {{ t('askChef.investigate') }}
                      </Button>
                      <Button
                        v-if="isOpen(entry)"
                        variant="outline"
                        size="sm"
                        @click="handleResolve(entry.group_key, 'Manually resolved')"
                      >
                        {{ t('ui.resolve') }}
                      </Button>
                    </div>
                  </div>

                  <AlertDeliveryStatus :entry="entry" />

                  <!-- Resolution Info -->
                  <div v-if="entry.resolved_at" class="text-xs text-muted-foreground">
                    {{ t('ui.resolved') }} {{ formatEventTime(entry.resolved_at) }}
                  </div>
                </div>
              </div>
            </div>
          </CardContent>
        </Card>
      </TabsContent>
    </Tabs>

    <SilenceForm
      v-if="alert"
      v-model:open="silenceOpen"
      :source-id="alert.source_id"
      :rules="[alert]"
      :silence="null"
      :preset="silencePreset"
    />

    <!-- Delete Confirmation Dialog -->
    <ConfirmDialog
      v-model:open="showDeleteDialog"
      :title="t('ui.deleteAlert')"
      :description="t('alerts.confirmDeleteHistory', { name: alert?.name ?? '' })"
      :confirm-text="t('ui.deleteAlert2')"
      destructive
      @confirm="handleDelete"
    />
  </div>
</template>
