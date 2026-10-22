<script setup lang="ts">
import { useI18n } from "vue-i18n";
import { severityLabels, alertStateLabels } from "@/i18n/alertLabels";

import { computed, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { ArrowLeft, Bell, Trash2, CheckCircle2, AlertCircle, AlertTriangle, Clock, History, ScrollText } from "lucide-vue-next";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
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
import { EmptyState, LoadingState } from "@/components/layout";
import type { Alert, AlertHistoryEntry, UpdateAlertRequest } from "@/api/alerts";

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

function formatEventTime(timestamp: string): string {
  const date = new Date(timestamp);
  if (Number.isNaN(date.getTime())) return timestamp;
  return formatInZone(epochMs(date), preferencesStore.zone, { dateStyle: "medium", timeStyle: "short" }, locale.value);
}

const alertId = computed(() => Number(route.params.alertID));
const currentTab = ref<"edit" | "history">(route.query.tab === "history" ? "history" : "edit");
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
  router.push({ name: "AlertsOverview", query: route.query });
}

async function handleUpdate(payload: UpdateAlertRequest) {
  if (!alert.value) return;
  // Alerts are no longer team-scoped — drop the team/source guards.
  await alertsStore.updateAlert(undefined, undefined, alert.value.id, payload);
}

function confirmDelete() {
  showDeleteDialog.value = true;
}

async function handleDelete() {
  if (!alert.value) return;
  const result = await alertsStore.deleteAlert(undefined, undefined, alert.value.id);
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
  await alertHistoryStore.loadHistory(alertId.value);
}

async function handleResolve(_historyId: number, message: string) {
  if (!alertId.value) return;
  const result = await alertHistoryStore.resolveAlert(
    undefined,
    undefined,
    alertId.value,
    { message }
  );
  if (result.success) {
    await loadHistory();
  }
}

watch(
  currentTab,
  async (tab) => {
    if (tab === "history" && !historyEntries.value.length) {
      await loadHistory();
    }
  }
);

watch(alertId, (id) => {
  alertHistoryStore.setCurrentContext(id || null);
  if (!id) return;
  if (!alert.value) {
    void alertsStore.refreshAlert(undefined, undefined, id);
  }
  if (currentTab.value === "history") {
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
        <Button variant="ghost" size="icon" @click="goBack">
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
      <TabsContent value="history">
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
                        : entry.status === 'resolved' || entry.resolved_at
                        ? 'bg-green-500 text-white'
                        : entry.status === 'error'
                        ? 'bg-yellow-500 text-white'
                        : 'bg-muted text-muted-foreground'
                    ]"
                  >
                    <AlertCircle v-if="entry.status === 'triggered' && !entry.resolved_at" class="h-3.5 w-3.5" />
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
                        v-if="entry.status === 'triggered' && !entry.resolved_at"
                        variant="outline"
                        size="sm"
                        @click="handleResolve(entry.id, 'Manually resolved')"
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
