<script setup lang="ts">
import { computed, onMounted, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import AlertForm from "@/components/alerts/AlertForm.vue";
import TeamSourceSelector from "@/views/explore/components/TeamSourceSelector.vue";
import { useAlertsStore } from "@/stores/alerts";
import { useContextStore } from "@/stores/context";
import { useSourcesStore } from "@/stores/sources";
import type { CreateAlertRequest } from "@/api/alerts";

const router = useRouter();
const route = useRoute();
const alertsStore = useAlertsStore();
const contextStore = useContextStore();
const sourcesStore = useSourcesStore();

const currentTeamId = computed(() => contextStore.teamId);
const currentSourceId = computed(() => contextStore.sourceId);
async function ensureSourcesLoaded() {
  if (!currentTeamId.value) {
    return;
  }
  if (!sourcesStore.teamSources.length) {
    await sourcesStore.loadTeamSources(currentTeamId.value);
  }
}

onMounted(async () => {
  await ensureSourcesLoaded();
});

watch([currentTeamId, currentSourceId], async () => {
  await ensureSourcesLoaded();
});

async function handleCreate(payload: CreateAlertRequest) {
  if (!currentTeamId.value || !currentSourceId.value) {
    return;
  }
  const result = await alertsStore.createAlert(currentTeamId.value, currentSourceId.value, payload);
  if (result.success && result.data) {
    router.push({
      name: "AlertDetail",
      params: { alertID: result.data.id },
      query: route.query,
    });
  }
}

function handleCancel() {
  router.push({ name: "AlertsOverview", query: route.query });
}
</script>

<template>
  <div class="space-y-6">
    <div class="flex items-start justify-between gap-4">
      <div class="space-y-1">
        <h1 class="text-2xl font-semibold tracking-tight">Create Alert</h1>
        <p class="text-muted-foreground">
          Configure an alert rule for the currently selected team and source.
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
          :alert="null"
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
