<script setup lang="ts">
import { computed, onMounted, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { ArrowLeft, Trash2, CheckCircle2, AlertCircle, AlertTriangle, Clock, History } from "lucide-vue-next";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
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
import { useAlertsStore } from "@/stores/alerts";
import { useAlertHistoryStore } from "@/stores/alertHistory";
import { useContextStore } from "@/stores/context";
import AlertForm from "@/components/alerts/AlertForm.vue";
import type { Alert, UpdateAlertRequest } from "@/api/alerts";

const route = useRoute();
const router = useRouter();

const alertsStore = useAlertsStore();
const alertHistoryStore = useAlertHistoryStore();
const contextStore = useContextStore();

const alertId = computed(() => Number(route.params.alertID));
const currentTab = ref<"edit" | "history">("edit");
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
  if (!currentTeamId.value || !currentSourceId.value || !alert.value) return;
  await alertsStore.updateAlert(
    currentTeamId.value,
    currentSourceId.value,
    alert.value.id,
    payload
  );
}

function confirmDelete() {
  showDeleteDialog.value = true;
}

async function handleDelete() {
  if (!alert.value || !currentTeamId.value || !currentSourceId.value) return;
  const result = await alertsStore.deleteAlert(currentTeamId.value, currentSourceId.value, alert.value.id);
  showDeleteDialog.value = false;
  if (result.success) {
    goBack();
  }
}

async function loadHistory() {
  if (!alertId.value) return;
  await alertHistoryStore.loadHistory(alertId.value);
}

async function handleResolve(_historyId: number, message: string) {
  if (!currentTeamId.value || !currentSourceId.value || !alertId.value) return;
  const result = await alertHistoryStore.resolveAlert(
    currentTeamId.value,
    currentSourceId.value,
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
  },
  { immediate: true }
);

onMounted(async () => {
  if (!alert.value && currentTeamId.value && currentSourceId.value) {
    await alertsStore.fetchAlerts(currentTeamId.value, currentSourceId.value);
  }
  if (route.query.tab === "history") {
    currentTab.value = "history";
    await loadHistory();
  }
});
</script>

<template>
  <div class="space-y-6">
    <!-- Header Section -->
    <div class="flex items-start justify-between gap-4">
      <div class="flex items-start gap-3">
        <Button variant="ghost" size="icon" @click="goBack">
          <ArrowLeft class="h-5 w-5" />
        </Button>
        <div class="space-y-1">
          <div class="flex items-center gap-2 flex-wrap">
            <h1 class="text-2xl font-bold tracking-tight">{{ alert?.name || "Alert" }}</h1>
            <Badge v-if="alert" :variant="mapSeverityVariant(alert.severity)" class="capitalize">
              {{ alert.severity }}
            </Badge>
            <Badge v-if="alert && !alert.is_active" variant="outline">Disabled</Badge>
          </div>
          <p v-if="alert?.description" class="text-muted-foreground">
            {{ alert.description }}
          </p>
        </div>
      </div>
      <Button variant="outline" @click="confirmDelete" :disabled="!alert">
        <Trash2 class="mr-2 h-4 w-4" />
        Delete
      </Button>
    </div>

    <!-- Loading State -->
    <div v-if="!alert" class="rounded-lg border border-dashed py-12 text-center">
      <p class="text-sm text-muted-foreground">Alert not found or still loading...</p>
      <Button class="mt-4" variant="outline" @click="goBack">Go back</Button>
    </div>

    <!-- Main Content with Tabs -->
    <Tabs v-else v-model="currentTab" class="space-y-6">
      <TabsList>
        <TabsTrigger value="edit">Configuration</TabsTrigger>
        <TabsTrigger value="history">History</TabsTrigger>
      </TabsList>

      <!-- Edit Configuration Tab -->
      <TabsContent value="edit">
        <Card>
          <CardHeader>
            <CardTitle>Alert Configuration</CardTitle>
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
            <CardTitle>Alert History</CardTitle>
          </CardHeader>
          <CardContent>
            <!-- Loading State -->
            <div v-if="isLoadingHistory" class="py-8 text-center">
              <div class="animate-spin rounded-full h-8 w-8 border-b-2 border-primary mx-auto mb-3"></div>
              <p class="text-sm text-muted-foreground">Loading history...</p>
            </div>

            <!-- Empty State -->
            <div v-else-if="!historyEntries.length" class="py-12 text-center">
              <div class="flex justify-center mb-4">
                <div class="rounded-full bg-muted p-3">
                  <History class="h-8 w-8 text-muted-foreground" />
                </div>
              </div>
              <h3 class="text-lg font-semibold mb-2">No history yet</h3>
              <p class="text-sm text-muted-foreground">
                This alert hasn't been triggered yet. History will appear here once the alert fires.
              </p>
            </div>

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
                          {{ entry.status }}
                        </Badge>
                        <span class="text-sm text-muted-foreground">
                          {{ new Date(entry.triggered_at).toLocaleString('en-US', {
                            dateStyle: 'medium',
                            timeStyle: 'short'
                          }) }}
                        </span>
                        <!-- Value Display Inline -->
                        <span v-if="entry.value != null" class="text-sm text-muted-foreground">
                          · Value: <code class="font-mono font-semibold">{{ entry.value }}</code>
                        </span>
                      </div>
                      <!-- Message -->
                      <div v-if="entry.message" class="text-sm text-muted-foreground">
                        {{ entry.message }}
                      </div>
                    </div>

                    <!-- Resolve Button -->
                    <Button
                      v-if="entry.status === 'triggered' && !entry.resolved_at"
                      variant="outline"
                      size="sm"
                      @click="handleResolve(entry.id, 'Manually resolved')"
                    >
                      Resolve
                    </Button>
                  </div>

                  <!-- Resolution Info -->
                  <div v-if="entry.resolved_at" class="text-xs text-muted-foreground">
                    Resolved {{ new Date(entry.resolved_at).toLocaleString('en-US', {
                      dateStyle: 'medium',
                      timeStyle: 'short'
                    }) }}
                  </div>
                </div>
              </div>
            </div>
          </CardContent>
        </Card>
      </TabsContent>
    </Tabs>

    <!-- Delete Confirmation Dialog -->
    <AlertDialog :open="showDeleteDialog" @update:open="showDeleteDialog = $event">
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>Delete alert?</AlertDialogTitle>
          <AlertDialogDescription>
            Are you sure you want to delete <strong>{{ alert?.name }}</strong>? This action cannot be undone and all associated history will be permanently deleted.
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>Cancel</AlertDialogCancel>
          <AlertDialogAction variant="destructive" @click="handleDelete">
            Delete Alert
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  </div>
</template>
