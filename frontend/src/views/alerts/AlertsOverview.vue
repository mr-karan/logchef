<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { storeToRefs } from "pinia";
import {
  BellRing,
  CalendarClock,
  Clock3,
  History,
  MoreHorizontal,
  Plus,
  RefreshCcw,
} from "lucide-vue-next";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import ErrorAlert from "@/components/ui/ErrorAlert.vue";
import TeamSourceSelector from "@/views/explore/components/TeamSourceSelector.vue";
import { useAlertsStore } from "@/stores/alerts";
import { useAlertHistoryStore } from "@/stores/alertHistory";
import { useContextStore } from "@/stores/context";
import { useTeamsStore } from "@/stores/teams";
import { useSourcesStore } from "@/stores/sources";
import { formatDate } from "@/utils/format";
import type { Alert } from "@/api/alerts";
import AlertForm from "@/components/alerts/AlertForm.vue";
import AlertHistoryDrawer from "@/components/alerts/AlertHistoryDrawer.vue";

const router = useRouter();
const route = useRoute();

const alertsStore = useAlertsStore();
const alertHistoryStore = useAlertHistoryStore();
const contextStore = useContextStore();
const teamsStore = useTeamsStore();
const sourcesStore = useSourcesStore();

const { alerts } = storeToRefs(alertsStore);

const isFormOpen = ref(false);
const formMode = ref<"create" | "edit">("create");
const editingAlert = ref<Alert | null>(null);

const isHistoryOpen = ref(false);
const historyAlert = ref<Alert | null>(null);

const showDeleteDialog = ref(false);
const alertToDelete = ref<Alert | null>(null);

const localState = reactive({
  pendingRouteAlertId: null as number | null,
});

const currentTeamId = computed(() => contextStore.teamId);
const currentSourceId = computed(() => contextStore.sourceId);

const isLoadingAlerts = computed(() => {
  if (!currentTeamId.value || !currentSourceId.value) return false;
  return alertsStore.isLoadingOperation(`fetchAlerts-${currentTeamId.value}-${currentSourceId.value}`);
});

const loadError = computed(() => {
  const error = alertsStore.error;
  if (!error) return null;
  if (typeof error === "object" && "operation" in error) {
    return error;
  }
  return null;
});

const emptyStateMessage = computed(() => {
  if (!currentTeamId.value) {
    return "Select a team to manage alerts.";
  }
  if (!currentSourceId.value) {
    return "Select a source to view its alert rules.";
  }
  if (isLoadingAlerts.value) {
    return "";
  }
  if (!alerts.value.length) {
    return "No alerts yet. Create your first alert to receive notifications when log conditions are met.";
  }
  return "";
});

function setRouteAlert(alertId: number | null) {
  const baseRoute = { name: "AlertsOverview" as const, params: {} as Record<string, string>, query: route.query };
  if (alertId) {
    router.replace({
      name: "AlertDetail",
      params: { alertID: String(alertId) },
      query: route.query,
    });
  } else {
    router.replace(baseRoute);
  }
}

function openCreateForm() {
  formMode.value = "create";
  editingAlert.value = null;
  isFormOpen.value = true;
}

function openEditForm(alert: Alert) {
  formMode.value = "edit";
  editingAlert.value = alert;
  alertsStore.setSelectedAlert(alert.id);
  isFormOpen.value = true;
}

function handleFormClose() {
  isFormOpen.value = false;
  editingAlert.value = null;
}

async function handleCreate(payload: Parameters<typeof alertsStore.createAlert>[2]) {
  if (!currentTeamId.value || !currentSourceId.value) return;
  const result = await alertsStore.createAlert(currentTeamId.value, currentSourceId.value, payload);
  if (result.success) {
    isFormOpen.value = false;
  }
}

async function handleUpdate(payload: Parameters<typeof alertsStore.updateAlert>[3]) {
  if (!currentTeamId.value || !currentSourceId.value || !editingAlert.value) return;
  const result = await alertsStore.updateAlert(currentTeamId.value, currentSourceId.value, editingAlert.value.id, payload);
  if (result.success) {
    isFormOpen.value = false;
    editingAlert.value = null;
  }
}

function confirmDelete(alert: Alert) {
  alertToDelete.value = alert;
  showDeleteDialog.value = true;
}

async function handleDelete() {
  if (!alertToDelete.value || !currentTeamId.value || !currentSourceId.value) return;
  await alertsStore.deleteAlert(currentTeamId.value, currentSourceId.value, alertToDelete.value.id);
  showDeleteDialog.value = false;
  alertToDelete.value = null;
}

function cancelDelete() {
  showDeleteDialog.value = false;
  alertToDelete.value = null;
}

async function toggleAlert(alert: Alert) {
  if (!currentTeamId.value || !currentSourceId.value) return;
  await alertsStore.toggleAlertActivity(currentTeamId.value, currentSourceId.value, alert.id, !alert.is_active);
}

function openHistory(alert: Alert) {
  historyAlert.value = alert;
  alertsStore.setSelectedAlert(alert.id);
  isHistoryOpen.value = true;
  setRouteAlert(alert.id);
  if (currentTeamId.value && currentSourceId.value) {
    alertHistoryStore.loadHistory(currentTeamId.value, currentSourceId.value, alert.id);
  }
}

function closeHistory() {
  isHistoryOpen.value = false;
  historyAlert.value = null;
  setRouteAlert(null);
}

async function retryLoad() {
  if (!currentTeamId.value || !currentSourceId.value) return;
  await alertsStore.fetchAlerts(currentTeamId.value, currentSourceId.value);
}

function refreshAlerts() {
  retryLoad();
}

function mapSeverityVariant(severity: Alert["severity"]) {
  switch (severity) {
    case "critical":
      return "destructive";
    case "warning":
      return "warning";
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
    return "Every minute";
  }
  if (minutes < 60) {
    return `Every ${minutes} minutes`;
  }
  const hours = Math.round(minutes / 60);
  if (hours === 1) {
    return "Every hour";
  }
  return `Every ${hours} hours`;
}

async function ensureDataLoaded() {
  if (!teamsStore.userTeams.length) {
    await teamsStore.loadUserTeams();
  }
  if (contextStore.teamId && !sourcesStore.teamSources.length) {
    await sourcesStore.loadTeamSources(contextStore.teamId);
  }
}

async function handleContextChange(teamId: number | null, sourceId: number | null) {
  if (!teamId) {
    alertsStore.clearAlerts();
    return;
  }
  if (teamId && sourceId) {
    await alertsStore.fetchAlerts(teamId, sourceId);
  } else {
    alertsStore.clearAlerts();
  }
}

function syncRouteAlert(alertIdParam: unknown) {
  if (!alertIdParam) {
    localState.pendingRouteAlertId = null;
    if (isHistoryOpen.value) {
      closeHistory();
    }
    return;
  }
  const parsed = Number(alertIdParam);
  if (Number.isNaN(parsed)) {
    localState.pendingRouteAlertId = null;
    return;
  }
  localState.pendingRouteAlertId = parsed;
  const existing = alerts.value.find((alert) => alert.id === parsed);
  if (existing) {
    openHistory(existing);
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

watch(
  () => alerts.value.length,
  () => {
    if (localState.pendingRouteAlertId) {
      const alert = alerts.value.find((a) => a.id === localState.pendingRouteAlertId);
      if (alert) {
        openHistory(alert);
        localState.pendingRouteAlertId = null;
      }
    }
  }
);

watch(
  () => route.params.alertID,
  (value) => {
    syncRouteAlert(value);
  },
  { immediate: true }
);

onMounted(async () => {
  await ensureDataLoaded();
  if (!contextStore.teamId && teamsStore.teams.length > 0) {
    router.replace({
      name: route.name ? (route.name as string) : "AlertsOverview",
      params: route.params,
      query: { ...route.query, team: String(teamsStore.teams[0].id) },
    });
  }
});
</script>

<template>
  <div class="space-y-6">
    <div class="flex items-start justify-between gap-4">
      <div class="space-y-1">
        <h1 class="text-2xl font-semibold tracking-tight">Alert Rules</h1>
        <p class="text-muted-foreground">
          Monitor log activity and receive notifications when your thresholds are crossed.
        </p>
      </div>
      <div class="flex items-center gap-2">
        <Button variant="outline" @click="refreshAlerts" :disabled="isLoadingAlerts">
          <RefreshCcw class="-ml-1 mr-2 h-4 w-4" />
          Refresh
        </Button>
        <Button @click="openCreateForm" :disabled="!currentTeamId || !currentSourceId">
          <Plus class="-ml-1 mr-2 h-4 w-4" />
          New Alert
        </Button>
      </div>
    </div>

    <Card>
      <CardHeader class="flex items-start justify-between gap-4 space-y-0">
        <div>
          <CardTitle>Scope</CardTitle>
          <CardDescription>
            Alerts run against the selected team and source. Switch context from here.
          </CardDescription>
        </div>
        <TeamSourceSelector />
      </CardHeader>
      <CardContent>
        <div v-if="loadError" class="mb-4">
          <ErrorAlert :error="loadError" title="Failed to load alerts" @retry="retryLoad" />
        </div>
        <div v-if="emptyStateMessage && !isLoadingAlerts" class="rounded-lg border border-dashed py-12 text-center">
          <div class="mx-auto flex h-12 w-12 items-center justify-center rounded-full bg-primary/10 text-primary">
            <BellRing class="h-6 w-6" />
          </div>
          <h3 class="mt-4 text-lg font-semibold">Alerts</h3>
          <p class="mt-1 text-sm text-muted-foreground">
            {{ emptyStateMessage }}
          </p>
          <Button v-if="currentTeamId && currentSourceId && !alerts.length" class="mt-4" @click="openCreateForm">
            <Plus class="-ml-1 mr-2 h-4 w-4" />
            Create alert
          </Button>
        </div>

        <div v-else>
          <div v-if="isLoadingAlerts" class="py-8 text-center text-sm text-muted-foreground">
            Loading alerts…
          </div>
          <Table v-else class="overflow-hidden rounded-lg border">
            <TableHeader>
              <TableRow>
                <TableHead>Name</TableHead>
                <TableHead>Severity</TableHead>
                <TableHead>Query</TableHead>
                <TableHead>Threshold</TableHead>
                <TableHead>Schedule</TableHead>
                <TableHead>Rooms</TableHead>
                <TableHead>Last Triggered</TableHead>
                <TableHead class="text-right">Actions</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              <TableRow v-for="alert in alerts" :key="alert.id" :class="{ 'opacity-60': !alert.is_active }">
                <TableCell class="font-medium">
                  <div class="flex items-center gap-2">
                    <span>{{ alert.name }}</span>
                    <Badge v-if="!alert.is_active" variant="outline">Disabled</Badge>
                  </div>
                  <p v-if="alert.description" class="text-xs text-muted-foreground">
                    {{ alert.description }}
                  </p>
                </TableCell>
                <TableCell>
                  <Badge :variant="mapSeverityVariant(alert.severity)" class="capitalize">
                    {{ alert.severity }}
                  </Badge>
                </TableCell>
                <TableCell class="text-sm text-muted-foreground">
                  <div class="flex flex-col gap-1">
                    <span class="font-medium capitalize">{{ alert.query_type === "sql" ? "SQL" : "Log condition" }}</span>
                    <code class="break-all rounded bg-muted px-2 py-1 text-xs">{{ alert.query }}</code>
                  </div>
                </TableCell>
                <TableCell class="text-sm">
                  <div class="flex flex-col">
                    <span class="font-medium">Value {{ alert.threshold_operator }} {{ alert.threshold_value }}</span>
                    <span class="text-xs text-muted-foreground">Lookback: {{ alert.lookback_seconds }}s</span>
                  </div>
                </TableCell>
                <TableCell class="text-sm">
                  <div class="flex items-center gap-2">
                    <Clock3 class="h-4 w-4 text-muted-foreground" />
                    <span>{{ formatFrequency(alert) }}</span>
                  </div>
                </TableCell>
                <TableCell>
                  <div class="flex flex-col gap-2">
                    <div v-if="!alert.rooms.length" class="text-xs text-muted-foreground">
                      No rooms configured
                    </div>
                    <div v-else class="flex flex-wrap gap-2">
                      <Badge v-for="room in alert.rooms" :key="room.id" variant="outline" class="flex items-center gap-2">
                        <span class="text-sm font-medium">{{ room.name }}</span>
                        <span class="text-[11px] uppercase tracking-wide text-muted-foreground">
                          {{ room.channel_types.length ? room.channel_types.join(", ") : "email" }}
                        </span>
                        <span class="text-[11px] text-muted-foreground">{{ room.member_count }} members</span>
                      </Badge>
                    </div>
                  </div>
                </TableCell>
                <TableCell class="text-sm">
                  <div class="flex flex-col">
                    <span>{{ formatDate(alert.last_triggered_at || alert.last_evaluated_at || alert.updated_at) }}</span>
                    <span class="text-xs text-muted-foreground">Evaluated {{ formatDate(alert.last_evaluated_at || alert.updated_at) }}</span>
                  </div>
                </TableCell>
                <TableCell class="text-right">
                  <DropdownMenu>
                    <DropdownMenuTrigger as-child>
                      <Button variant="ghost" size="icon">
                        <MoreHorizontal class="h-4 w-4" />
                        <span class="sr-only">Open alert actions</span>
                      </Button>
                    </DropdownMenuTrigger>
                    <DropdownMenuContent align="end" class="w-48">
                      <DropdownMenuItem @click="openEditForm(alert)">
                        Edit alert
                      </DropdownMenuItem>
                      <DropdownMenuItem @click="openHistory(alert)">
                        <History class="mr-2 h-4 w-4" />
                        View history
                      </DropdownMenuItem>
                      <DropdownMenuItem @click="toggleAlert(alert)">
                        <CalendarClock class="mr-2 h-4 w-4" />
                        {{ alert.is_active ? "Disable" : "Enable" }}
                      </DropdownMenuItem>
                      <DropdownMenuSeparator />
                      <DropdownMenuItem class="text-destructive focus:text-destructive" @click="confirmDelete(alert)">
                        Delete
                      </DropdownMenuItem>
                    </DropdownMenuContent>
                  </DropdownMenu>
                </TableCell>
              </TableRow>
            </TableBody>
          </Table>
        </div>
      </CardContent>
    </Card>

    <AlertForm
      v-if="isFormOpen"
      :open="isFormOpen"
      :mode="formMode"
      :team-id="currentTeamId"
      :source-id="currentSourceId"
      :alert="formMode === 'edit' ? editingAlert : null"
      @cancel="handleFormClose"
      @create="handleCreate"
      @update="handleUpdate"
    />

    <AlertHistoryDrawer
      v-if="historyAlert"
      :open="isHistoryOpen"
      :alert="historyAlert"
      :team-id="currentTeamId"
      :source-id="currentSourceId"
      @close="closeHistory"
    />

    <AlertDialog :open="showDeleteDialog" @update:open="showDeleteDialog = $event">
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>Delete alert</AlertDialogTitle>
          <AlertDialogDescription>
            Are you sure you want to delete the alert "{{ alertToDelete?.name }}"? This action cannot be undone.
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel @click="cancelDelete">Cancel</AlertDialogCancel>
          <AlertDialogAction variant="destructive" @click="handleDelete">Delete</AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  </div>
</template>
