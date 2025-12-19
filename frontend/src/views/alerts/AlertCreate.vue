<script setup lang="ts">
import { computed, onMounted, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import AlertForm from "@/components/alerts/AlertForm.vue";
import TeamSourceSelector from "@/views/explore/components/TeamSourceSelector.vue";
import { useAlertsStore } from "@/stores/alerts";
import { useContextStore } from "@/stores/context";
import { useSourcesStore } from "@/stores/sources";
import type { Alert, CreateAlertRequest } from "@/api/alerts";

const router = useRouter();
const route = useRoute();
const alertsStore = useAlertsStore();
const contextStore = useContextStore();
const sourcesStore = useSourcesStore();

const currentTeamId = computed(() => contextStore.teamId);
const currentSourceId = computed(() => contextStore.sourceId);

// For duplicating an existing alert
const duplicateAlertId = computed(() => {
  const id = route.query.duplicate;
  return id ? Number(id) : null;
});

const alertToDuplicate = ref<Alert | null>(null);
const isDuplicating = computed(() => duplicateAlertId.value !== null);

async function ensureSourcesLoaded() {
  if (!currentTeamId.value) {
    return;
  }
  if (!sourcesStore.teamSources.length) {
    await sourcesStore.loadTeamSources(currentTeamId.value);
  }
}

async function loadAlertForDuplication() {
  if (!duplicateAlertId.value || !currentTeamId.value || !currentSourceId.value) {
    alertToDuplicate.value = null;
    return;
  }
  // Find the alert in the store or fetch alerts first
  if (!alertsStore.alerts.length) {
    await alertsStore.fetchAlerts(currentTeamId.value, currentSourceId.value);
  }
  const found = alertsStore.alerts.find((a) => a.id === duplicateAlertId.value);
  if (found) {
    // Create a copy with modified name
    alertToDuplicate.value = {
      ...found,
      id: 0, // Reset ID for new alert
      name: `${found.name} (Copy)`,
    };
  }
}

onMounted(async () => {
  await ensureSourcesLoaded();
  await loadAlertForDuplication();
});

watch([currentTeamId, currentSourceId], async () => {
  await ensureSourcesLoaded();
  await loadAlertForDuplication();
});

watch(duplicateAlertId, async () => {
  await loadAlertForDuplication();
});

async function handleCreate(payload: CreateAlertRequest) {
  if (!currentTeamId.value || !currentSourceId.value) {
    return;
  }
  const result = await alertsStore.createAlert(currentTeamId.value, currentSourceId.value, payload);
  if (result.success && result.data) {
    // Remove the duplicate query param when navigating away
    const { duplicate, ...restQuery } = route.query;
    router.push({
      name: "AlertsOverview",
      query: restQuery,
    });
  }
}

function handleCancel() {
  const { duplicate, ...restQuery } = route.query;
  router.push({ name: "AlertsOverview", query: restQuery });
}
</script>

<template>
  <div class="space-y-6">
    <div class="flex items-start justify-between gap-4">
      <div class="space-y-1">
        <h1 class="text-2xl font-semibold tracking-tight">
          {{ isDuplicating ? "Duplicate Alert" : "Create Alert" }}
        </h1>
        <p class="text-muted-foreground">
          {{ isDuplicating
            ? "Create a new alert based on an existing configuration."
            : "Configure an alert rule for the currently selected team and source."
          }}
        </p>
      </div>
      <Button variant="outline" @click="handleCancel">Cancel</Button>
    </div>

    <Card>
      <CardHeader class="flex flex-col gap-2 space-y-0">
        <div>
          <CardTitle>Scope</CardTitle>
          <CardDescription>
            Alerts run against the selected team and source. Adjust the context here if needed.
          </CardDescription>
        </div>
        <TeamSourceSelector />
      </CardHeader>
    </Card>

    <Card>
      <CardContent v-if="currentTeamId && currentSourceId">
        <AlertForm
          :inline="true"
          mode="create"
          :team-id="currentTeamId"
          :source-id="currentSourceId"
          :alert="alertToDuplicate"
          :open="true"
          @create="handleCreate"
          @cancel="handleCancel"
        />
      </CardContent>
      <CardContent v-else>
        <p class="text-sm text-muted-foreground">
          Select a team and source to create an alert.
        </p>
      </CardContent>
    </Card>
  </div>
</template>
