<script setup lang="ts">
import { computed, onMounted, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { ArrowLeft, History, Pencil, Trash2 } from "lucide-vue-next";
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
import { useToast } from "@/composables/useToast";
import { useAlertsStore } from "@/stores/alerts";
import { useAlertHistoryStore } from "@/stores/alertHistory";
import { useContextStore } from "@/stores/context";
import AlertForm from "@/components/alerts/AlertForm.vue";
import type { Alert, UpdateAlertRequest } from "@/api/alerts";

const route = useRoute();
const router = useRouter();
const { toast } = useToast();

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
  return alertHistoryStore.entriesByAlert[alertId.value] || [];
});

const isLoadingHistory = computed(() => {
  return alertHistoryStore.isLoadingOperation(`loadHistory-${currentTeamId.value}-${currentSourceId.value}-${alertId.value}`);
});

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

function goBack() {
  router.push({ name: "AlertsOverview", query: route.query });
}

async function handleUpdate(payload: UpdateAlertRequest) {
  if (!currentTeamId.value || !currentSourceId.value || !alert.value) return;
  const result = await alertsStore.updateAlert(
    currentTeamId.value,
    currentSourceId.value,
    alert.value.id,
    payload
  );
  if (result.success) {
    toast({
      title: "Alert updated",
      description: "Your alert has been successfully updated.",
    });
  }
}

function confirmDelete() {
  showDeleteDialog.value = true;
}

async function handleDelete() {
  if (!alert.value || !currentTeamId.value || !currentSourceId.value) return;
  const result = await alertsStore.deleteAlert(currentTeamId.value, currentSourceId.value, alert.value.id);
  showDeleteDialog.value = false;
  if (result.success) {
    toast({
      title: "Alert deleted",
      description: `Alert "${alert.value.name}" has been deleted.`,
    });
    goBack();
  }
}

async function loadHistory() {
  if (!currentTeamId.value || !currentSourceId.value || !alertId.value) return;
  await alertHistoryStore.loadHistory(currentTeamId.value, currentSourceId.value, alertId.value);
}

async function handleResolve(historyId: number, message: string) {
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
    <div class="flex items-start justify-between gap-4">
      <div class="flex items-center gap-3">
        <Button variant="ghost" size="icon" @click="goBack">
          <ArrowLeft class="h-5 w-5" />
        </Button>
        <div class="space-y-1">
          <div class="flex items-center gap-2">
            <h1 class="text-2xl font-semibold tracking-tight">{{ alert?.name || "Alert" }}</h1>
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
      <div class="flex items-center gap-2">
        <Button variant="outline" @click="confirmDelete" :disabled="!alert">
          <Trash2 class="-ml-1 mr-2 h-4 w-4" />
          Delete
        </Button>
      </div>
    </div>

    <div v-if="!alert" class="rounded-lg border border-dashed py-12 text-center">
      <p class="text-sm text-muted-foreground">Alert not found or still loading...</p>
      <Button class="mt-4" @click="goBack">Go back</Button>
    </div>

    <Card v-else>
      <CardHeader>
        <Tabs v-model="currentTab" class="w-full">
          <TabsList>
            <TabsTrigger value="edit" class="gap-2">
              <Pencil class="h-4 w-4" />
              Edit Configuration
            </TabsTrigger>
            <TabsTrigger value="history" class="gap-2">
              <History class="h-4 w-4" />
              History
            </TabsTrigger>
          </TabsList>
        </Tabs>
      </CardHeader>
      <CardContent>
        <TabsContent value="edit" class="mt-0">
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
        </TabsContent>
        <TabsContent value="history" class="mt-0">
          <div v-if="isLoadingHistory" class="py-8 text-center text-sm text-muted-foreground">
            Loading history...
          </div>
          <div v-else-if="!historyEntries.length" class="rounded-lg border border-dashed py-12 text-center">
            <History class="mx-auto h-12 w-12 text-muted-foreground" />
            <h3 class="mt-4 text-lg font-semibold">No history yet</h3>
            <p class="mt-1 text-sm text-muted-foreground">
              This alert hasn't been triggered yet.
            </p>
          </div>
          <div v-else class="space-y-4">
            <div
              v-for="entry in historyEntries"
              :key="entry.id"
              class="rounded-lg border p-4 space-y-3"
            >
              <div class="flex items-start justify-between">
                <div class="space-y-1">
                  <div class="flex items-center gap-2">
                    <Badge :variant="entry.status === 'triggered' ? 'destructive' : 'secondary'">
                      {{ entry.status }}
                    </Badge>
                    <span class="text-sm text-muted-foreground">
                      {{ new Date(entry.triggered_at).toLocaleString() }}
                    </span>
                  </div>
                  <div v-if="entry.value_text" class="text-sm">
                    Value: <span class="font-mono font-medium">{{ entry.value_text }}</span>
                  </div>
                </div>
                <Button
                  v-if="entry.status === 'triggered' && !entry.resolved_at"
                  variant="outline"
                  size="sm"
                  @click="handleResolve(entry.id, 'Manually resolved')"
                >
                  Resolve
                </Button>
              </div>
              <div v-if="entry.message" class="text-sm text-muted-foreground">
                {{ entry.message }}
              </div>
              <div v-if="entry.resolved_at" class="text-xs text-muted-foreground">
                Resolved at {{ new Date(entry.resolved_at).toLocaleString() }}
              </div>
            </div>
          </div>
        </TabsContent>
      </CardContent>
    </Card>

    <AlertDialog :open="showDeleteDialog" @update:open="showDeleteDialog = $event">
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>Delete alert</AlertDialogTitle>
          <AlertDialogDescription>
            Are you sure you want to delete the alert "{{ alert?.name }}"? This action cannot be undone.
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>Cancel</AlertDialogCancel>
          <AlertDialogAction variant="destructive" @click="handleDelete">Delete</AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  </div>
</template>
