<script setup lang="ts">
import { computed, onMounted, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { storeToRefs } from "pinia";
import {
  BellRing,
  Clock3,
  Copy,
  History,
  MoreHorizontal,
  Pencil,
  Plus,
  RefreshCcw,
  Trash2,
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
import { useContextStore } from "@/stores/context";
import { useTeamsStore } from "@/stores/teams";
import { useSourcesStore } from "@/stores/sources";
import type { Alert } from "@/api/alerts";

const router = useRouter();
const route = useRoute();

const alertsStore = useAlertsStore();
const contextStore = useContextStore();
const teamsStore = useTeamsStore();
const sourcesStore = useSourcesStore();

const { alerts } = storeToRefs(alertsStore);

const showDeleteDialog = ref(false);
const alertToDelete = ref<Alert | null>(null);


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
  router.push({ name: "AlertDetail", params: { alertID: alert.id }, query: { ...route.query, tab: "history" } });
}

function duplicateAlert(alert: Alert) {
  router.push({ name: "AlertCreate", query: { ...route.query, duplicate: String(alert.id) } });
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
  const date = new Date(dateStr);
  return date.toLocaleDateString(undefined, { month: "short", day: "numeric", year: "numeric" });
}

function formatRelativeTime(dateStr: string | null | undefined): string {
  if (!dateStr || dateStr === "") return "Never";
  const date = new Date(dateStr);
  if (isNaN(date.getTime())) return "Never";
  const now = new Date();
  const diffMs = now.getTime() - date.getTime();
  const diffMins = Math.floor(diffMs / 60000);
  const diffHours = Math.floor(diffMs / 3600000);
  const diffDays = Math.floor(diffMs / 86400000);

  if (diffMins < 1) return "Just now";
  if (diffMins < 60) return `${diffMins}m ago`;
  if (diffHours < 24) return `${diffHours}h ago`;
  if (diffDays < 30) return `${diffDays}d ago`;
  return formatDate(dateStr);
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
          <Table v-else>
            <TableHeader>
              <TableRow>
                <TableHead class="w-12">Active</TableHead>
                <TableHead>Alert</TableHead>
                <TableHead>Condition</TableHead>
                <TableHead>Status</TableHead>
                <TableHead>Last Triggered</TableHead>
                <TableHead class="w-24 text-right">Actions</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              <TableRow v-for="alert in alerts" :key="alert.id" :class="{ 'opacity-50': !alert.is_active }" class="group">
                <!-- Active Toggle -->
                <TableCell class="py-3">
                  <Switch 
                    :checked="alert.is_active" 
                    @update:checked="toggleAlert(alert)"
                    :title="alert.is_active ? 'Disable alert' : 'Enable alert'"
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
                        {{ alert.severity }}
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
                      Every {{ formatFrequency(alert) }}
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
                    <span class="text-sm capitalize">{{ alert.last_state }}</span>
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
                    <Button variant="ghost" size="icon" class="h-8 w-8" @click="openEditForm(alert)" title="Edit">
                      <Pencil class="h-4 w-4" />
                    </Button>
                    <Button variant="ghost" size="icon" class="h-8 w-8" @click="openHistory(alert)" title="History">
                      <History class="h-4 w-4" />
                    </Button>
                    <DropdownMenu>
                      <DropdownMenuTrigger as-child>
                        <Button variant="ghost" size="icon" class="h-8 w-8">
                          <MoreHorizontal class="h-4 w-4" />
                        </Button>
                      </DropdownMenuTrigger>
                      <DropdownMenuContent align="end" class="w-40">
                        <DropdownMenuItem @click="duplicateAlert(alert)">
                          <Copy class="mr-2 h-4 w-4" />
                          Duplicate
                        </DropdownMenuItem>
                        <DropdownMenuSeparator />
                        <DropdownMenuItem class="text-destructive focus:text-destructive" @click="confirmDelete(alert)">
                          <Trash2 class="mr-2 h-4 w-4" />
                          Delete
                        </DropdownMenuItem>
                      </DropdownMenuContent>
                    </DropdownMenu>
                  </div>
                </TableCell>
              </TableRow>
            </TableBody>
          </Table>
        </div>
      </CardContent>
    </Card>

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
