<script setup lang="ts">
import { useI18n } from "vue-i18n";
import { severityLabels, alertStateLabels } from "@/i18n/alertLabels";

import { computed, onMounted, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { storeToRefs } from "pinia";
import {
  Bell,
  BellRing,
  Clock3,
  History,
  Pencil,
  Plus,
  RefreshCcw,
  Search,
} from "lucide-vue-next";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Switch } from "@/components/ui/switch";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import ErrorAlert from "@/components/ui/ErrorAlert.vue";
import ConfirmDialog from "@/components/ui/ConfirmDialog.vue";
import {
  EmptyState,
  LoadingState,
  PageHeader,
  RowActionsMenu,
  SearchInput,
  SortHeader,
} from "@/components/layout";
import TeamSourceSelector from "@/views/explore/components/TeamSourceSelector.vue";
import { useAlertsStore } from "@/stores/alerts";
import { useContextStore } from "@/stores/context";
import { useTeamsStore } from "@/stores/teams";
import { useSourcesStore } from "@/stores/sources";
import { useMetaStore } from "@/stores/meta";
import { usePreferencesStore } from "@/stores/preferences";
import { epochMs, formatInZone } from "@/utils/timeWindow";
import { normalizeDateString } from "@/utils/format";
import { useContextSync } from "@/composables/useContextSync";
import { useTableSearchSort } from "@/composables/useTableSearchSort";
import type { Alert } from "@/api/alerts";

const { t, locale } = useI18n();

const router = useRouter();
const route = useRoute();

const alertsStore = useAlertsStore();
const contextStore = useContextStore();
const teamsStore = useTeamsStore();
const sourcesStore = useSourcesStore();
const metaStore = useMetaStore();
const preferencesStore = usePreferencesStore();

const {
  initialize: initializeContext,
  handleTeamChange,
  handleSourceChange,
} = useContextSync({ basePath: '/logs/alerts' });

const { alerts } = storeToRefs(alertsStore);

const { search, rows: sortedAlerts, sortKey, sortDir, toggleSort } = useTableSearchSort(alerts, {
  searchKeys: ["name", "description"],
  sortAccessors: {
    active: (a) => (a.is_active ? 1 : 0),
    name: (a) => a.name.toLowerCase(),
    status: (a) => (a.last_state === "firing" ? 1 : 0),
    lastTriggered: (a) => (a.last_triggered_at ? new Date(a.last_triggered_at).getTime() || 0 : 0),
  },
  initialSort: { key: "name", dir: "asc" },
  persistKey: "alerts",
});

const showDeleteDialog = ref(false);
const alertToDelete = ref<Alert | null>(null);


const currentTeamId = computed(() => contextStore.teamId);
const currentSourceId = computed(() => contextStore.sourceId);
const availableTeams = computed(() => teamsStore.teams || []);
const availableSources = computed(() => sourcesStore.teamSources || []);

const isLoadingAlerts = computed(() => {
  if (!currentSourceId.value) return false;
  return alertsStore.isLoadingOperation(`fetchAlerts-${currentSourceId.value}`);
});

const loadError = computed((): { message: string; operation?: string } | null => {
  const error = alertsStore.error;
  if (!error) return null;
  // Convert APIErrorResponse to the format expected by ErrorAlert
  return { 
    message: error.message,
    operation: undefined 
  };
});

const emptyStateMessage = computed(() => {
  if (!currentTeamId.value) {
    return t('ui.selectATeamToManageAlerts');
  }
  if (!currentSourceId.value) {
    return t('ui.selectASourceToViewItsAlertRules');
  }
  if (!alerts.value.length) {
    return t('ui.noAlertsYetCreateYourFirstAlertToReceiveNotificationsWhenLog');
  }
  return "";
});


function openCreateForm() {
  router.push({ name: "AlertCreate", query: route.query });
}

function openEditForm(alert: Alert) {
  router.push({ name: "AlertDetail", params: { alertID: alert.id }, query: route.query });
}

function confirmDelete(alert: Alert) {
  alertToDelete.value = alert;
  showDeleteDialog.value = true;
}

async function handleDelete() {
  if (!alertToDelete.value) return;
  await alertsStore.deleteAlert(alertToDelete.value.id);
  alertToDelete.value = null;
}

async function toggleAlert(alert: Alert) {
  await alertsStore.toggleAlertActivity(alert.id, !alert.is_active);
}

function openHistory(alert: Alert) {
  router.push({ name: "AlertDetail", params: { alertID: alert.id }, query: { ...route.query, tab: "history" } });
}

function duplicateAlert(alert: Alert) {
  router.push({ name: "AlertCreate", query: { ...route.query, duplicate: String(alert.id) } });
}

async function retryLoad() {
  // sourceId is optional — without one the API returns every alert the
  // caller can see across all their team-mediated sources.
  await alertsStore.fetchAlerts(currentSourceId.value ?? undefined);
}

function refreshAlerts() {
  retryLoad();
}

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

function formatFrequency(alert: Alert) {
  const minutes = Math.round(alert.frequency_seconds / 60);
  if (minutes < 1) {
    return `${alert.frequency_seconds}s`;
  }
  if (minutes === 1) {
    return "1m";
  }
  if (minutes < 60) {
    return `${minutes}m`;
  }
  const hours = Math.round(minutes / 60);
  if (hours === 1) {
    return "1h";
  }
  return `${hours}h`;
}

function formatThreshold(alert: Alert) {
  const ops: Record<string, string> = {
    gt: ">",
    gte: "≥",
    lt: "<",
    lte: "≤",
    eq: "=",
    neq: "≠",
  };
  return `${ops[alert.threshold_operator] || alert.threshold_operator} ${alert.threshold_value}`;
}

function formatDate(dateStr: string): string {
  const normalizedDateStr = normalizeDateString(dateStr);
  const date = new Date(normalizedDateStr);
  if (Number.isNaN(date.getTime())) return dateStr;
  return formatInZone(epochMs(date), preferencesStore.zone, { month: "short", day: "numeric", year: "numeric" }, locale.value);
}

function formatRelativeTime(dateStr: string | null | undefined): string {
  if (!dateStr || dateStr === "") return t('ui.never');
  const normalizedDateStr = normalizeDateString(dateStr);
  const date = new Date(normalizedDateStr);
  if (isNaN(date.getTime())) return t('ui.never');
  const now = new Date();
  const diffMs = now.getTime() - date.getTime();
  
  if (diffMs < 0) return t('ui.justNow');
  
  const diffMins = Math.floor(diffMs / 60000);
  const diffHours = Math.floor(diffMs / 3600000);
  const diffDays = Math.floor(diffMs / 86400000);

  if (diffMins < 1) return t('ui.justNow');
  const relative = new Intl.RelativeTimeFormat(locale.value, { numeric: "auto" });
  if (diffMins < 60) return relative.format(-diffMins, "minute");
  if (diffHours < 24) return relative.format(-diffHours, "hour");
  if (diffDays < 30) return relative.format(-diffDays, "day");
  return formatDate(dateStr);
}

async function handleContextChange(teamId: number | null, sourceId: number | null) {
  if (!teamId) {
    alertsStore.clearAlerts();
    return;
  }
  if (teamId && sourceId) {
    await alertsStore.fetchAlerts(sourceId);
  } else {
    alertsStore.clearAlerts();
  }
}

watch(
  () => [currentTeamId.value, currentSourceId.value] as const,
  async ([teamId, sourceId], oldValue) => {
    if (oldValue) {
      const [prevTeam, prevSource] = oldValue;
      if (teamId === prevTeam && sourceId === prevSource) {
        return;
      }
    }
    await handleContextChange(teamId, sourceId);
  },
  { immediate: true }
);

onMounted(async () => {
  await initializeContext();
});
</script>

<template>
  <EmptyState
    v-if="!metaStore.alertsEnabled"
    :icon="Bell"
    :title="t('ui.alertingIsDisabled')"
    :description="t('ui.alertingIsDisabledOnThisServerAskYourAdministratorToSetAlerts')"
  />
  <div v-else class="space-y-6">
    <PageHeader
      :title="t('ui.alertRules')"
      :description="t('ui.monitorLogActivityAndReceiveNotificationsWhenYourThresholdsAreCrossed')"
    >
      <template #actions>
        <Button variant="outline" @click="refreshAlerts" :disabled="isLoadingAlerts">
          <RefreshCcw class="-ml-1 mr-2 h-4 w-4" />
          {{ t('ui.refresh') }}
        </Button>
        <Button :variant="alerts.length ? 'default' : 'outline'" @click="openCreateForm" :disabled="!currentTeamId || !currentSourceId">
          <Plus class="-ml-1 mr-2 h-4 w-4" />
          {{ t('ui.newAlert') }}
        </Button>
      </template>
    </PageHeader>

    <Card>
      <CardHeader class="flex items-start justify-between gap-4 space-y-0">
        <div>
          <CardTitle>{{ t('ui.scope') }}</CardTitle>
          <CardDescription>
            {{ t('ui.alertsRunAgainstTheSelectedTeamAndSourceSwitchContextFromHere') }}
          </CardDescription>
        </div>
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
          <ErrorAlert :error="loadError" :title="t('ui.failedToLoadAlerts')" @retry="retryLoad" />
        </div>
        <LoadingState v-if="isLoadingAlerts" :label="t('ui.loadingAlerts')" />
        <EmptyState
          v-else-if="emptyStateMessage"
          :icon="BellRing"
          :title="t('ui.alerts')"
          :description="emptyStateMessage"
        >
          <template v-if="currentTeamId && currentSourceId && !alerts.length" #action>
            <Button @click="openCreateForm">
              <Plus class="-ml-1 mr-2 h-4 w-4" />
              {{ t('ui.createAlert') }}
            </Button>
          </template>
        </EmptyState>

        <div v-else class="space-y-4">
          <SearchInput v-model="search" :placeholder="t('alerts.searchAlerts')" />
          <EmptyState
            v-if="!sortedAlerts.length"
            :icon="Search"
            :title="t('alerts.noMatches', { search })"
            :description="t('ui.tryADifferentNameOrClearTheSearch')"
          />
          <Table v-else>
            <TableHeader>
              <TableRow>
                <TableHead class="w-12">
                  <SortHeader column="active" :sort-key="sortKey" :sort-dir="sortDir" @toggle="toggleSort">{{ t('ui.active') }}</SortHeader>
                </TableHead>
                <TableHead>
                  <SortHeader column="name" :sort-key="sortKey" :sort-dir="sortDir" @toggle="toggleSort">{{ t('ui.alert') }}</SortHeader>
                </TableHead>
                <TableHead>{{ t('ui.condition') }}</TableHead>
                <TableHead>
                  <SortHeader column="status" :sort-key="sortKey" :sort-dir="sortDir" @toggle="toggleSort">{{ t('ui.status') }}</SortHeader>
                </TableHead>
                <TableHead>
                  <SortHeader column="lastTriggered" :sort-key="sortKey" :sort-dir="sortDir" @toggle="toggleSort">{{ t('ui.lastTriggered') }}</SortHeader>
                </TableHead>
                <TableHead class="w-24 text-right">{{ t('ui.actions') }}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              <TableRow v-for="alert in sortedAlerts" :key="alert.id" :class="{ 'opacity-50': !alert.is_active }" class="group">
                <!-- Active Toggle -->
                <TableCell class="py-3">
                  <Switch 
                    :model-value="alert.is_active"
                    @update:model-value="toggleAlert(alert)"
                    :title="alert.is_active ? t('ui.disableAlert') : t('ui.enableAlert')"
                  />
                </TableCell>
                <!-- Alert Name & Description -->
                <TableCell class="py-3">
                  <div class="space-y-1 min-w-0">
                    <div class="flex items-center gap-2">
                      <router-link
                        :to="{ name: 'AlertDetail', params: { alertID: alert.id }, query: route.query }"
                        class="font-medium hover:underline cursor-pointer"
                      >
                        {{ alert.name }}
                      </router-link>
                      <Badge :variant="mapSeverityVariant(alert.severity)" class="capitalize shrink-0 text-xs">
                        {{ t(severityLabels[alert.severity]) }}
                      </Badge>
                    </div>
                    <p v-if="alert.description" class="text-sm text-muted-foreground line-clamp-1">
                      {{ alert.description }}
                    </p>
                  </div>
                </TableCell>
                <!-- Condition: Threshold + Frequency -->
                <TableCell class="py-3">
                  <div class="text-sm space-y-0.5">
                    <div class="font-mono text-xs bg-muted px-1.5 py-0.5 rounded inline-block">
                      {{ formatThreshold(alert) }}
                    </div>
                    <div class="text-xs text-muted-foreground flex items-center gap-1">
                      <Clock3 class="h-3 w-3" />
                      {{ t('ui.every') }} {{ formatFrequency(alert) }}
                    </div>
                  </div>
                </TableCell>
                <!-- Status: Firing/Resolved -->
                <TableCell class="py-3">
                  <div class="flex items-center gap-2">
                    <span 
                      class="h-2 w-2 rounded-full shrink-0"
                      :class="alert.last_state === 'firing' ? 'bg-red-500 animate-pulse' : 'bg-green-500'"
                    />
                    <span class="text-sm">{{ t(alertStateLabels[alert.last_state]) }}</span>
                  </div>
                </TableCell>
                <!-- Last Triggered -->
                <TableCell class="py-3">
                  <div class="text-sm">
                    <div :class="alert.last_triggered_at ? 'font-medium' : 'text-muted-foreground'">
                      {{ formatRelativeTime(alert.last_triggered_at) }}
                    </div>
                  </div>
                </TableCell>
                <!-- Actions -->
                <TableCell class="py-3 text-right">
                  <div class="inline-flex items-center gap-0.5">
                    <Button variant='ghost' size="icon" class='h-8 w-8' @click="openEditForm(alert)" :title="t('ui.edit')">
                      <Pencil class="h-4 w-4" />
                    </Button>
                    <Button variant='ghost' size="icon" class='h-8 w-8' @click="openHistory(alert)" :title="t('ui.history')">
                      <History class="h-4 w-4" />
                    </Button>
                    <RowActionsMenu @duplicate="duplicateAlert(alert)" @delete="confirmDelete(alert)" />
                  </div>
                </TableCell>
              </TableRow>
            </TableBody>
          </Table>
        </div>
      </CardContent>
    </Card>

    <ConfirmDialog
      v-model:open="showDeleteDialog"
      :title="t('ui.deleteAlert3')"
      :description="t('alerts.confirmDelete', { name: alertToDelete?.name ?? '' })"
      :confirm-text="t('ui.delete')"
      destructive
      @confirm="handleDelete"
      @cancel="alertToDelete = null"
    />
  </div>
</template>
