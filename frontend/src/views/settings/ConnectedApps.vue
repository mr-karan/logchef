<script setup lang="ts">
import { onMounted, ref } from "vue";
import { useI18n } from "vue-i18n";
import { Calendar, Clock, PlugZap, Trash2 } from "lucide-vue-next";
import { PageHeader, PageSection, EmptyState, LoadingState } from "@/components/layout";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import ConfirmDialog from "@/components/ui/ConfirmDialog.vue";
import { useToast } from "@/composables/useToast";
import { oauthApi, type ConnectedApp } from "@/api/oauth";
import { formatDate } from "@/utils/format";

const { t } = useI18n();
const { toast } = useToast();

const apps = ref<ConnectedApp[]>([]);
const isLoading = ref(true);
const appToRevoke = ref<ConnectedApp | null>(null);

async function load() {
  isLoading.value = true;
  try {
    const response = await oauthApi.listConnectedApps();
    apps.value = response.status === "success" && response.data ? response.data : [];
  } finally {
    isLoading.value = false;
  }
}

async function confirmRevoke() {
  const app = appToRevoke.value;
  appToRevoke.value = null;
  if (!app) return;
  try {
    await oauthApi.revokeConnectedApp(app.id);
    toast({ title: t("pages.connectedAppRevoked", { client: app.client.name }), variant: "success" });
    await load();
  } catch {
    // The API interceptor already reported the error.
  }
}

onMounted(load);
</script>

<template>
  <div class="space-y-6">
    <PageHeader :title="t('routes.connectedApps')" :description="t('pages.connectedAppsDescription')" />

    <PageSection>
      <LoadingState v-if="isLoading" />
      <EmptyState
        v-else-if="apps.length === 0"
        :icon="PlugZap"
        :title="t('pages.connectedAppsEmpty')"
        :description="t('pages.connectedAppsEmptyDescription')"
      />
      <div v-else class="space-y-3" data-testid="connected-apps">
        <div
          v-for="app in apps"
          :key="app.id"
          class="flex items-center justify-between gap-4 rounded-lg border p-4"
        >
          <div class="min-w-0 flex-1 space-y-2">
            <div class="flex flex-wrap items-center gap-2">
              <h4 class="font-medium">{{ app.client.name }}</h4>
              <Badge variant="secondary" class="text-xs">
                {{ app.resource_kind === "api" ? t("pages.oauthResourceApi") : t("pages.oauthResourceMcp") }}
              </Badge>
              <Badge v-for="scope in app.scopes" :key="scope" variant="outline" class="font-mono text-xs">{{ scope }}</Badge>
            </div>
            <div class="flex flex-wrap items-center gap-4 text-sm text-muted-foreground">
              <span class="flex items-center gap-1">
                <Calendar class="h-3 w-3" />
                {{ t("ui.created") }} {{ formatDate(app.created_at) }}
              </span>
              <span class="flex items-center gap-1">
                <Clock class="h-3 w-3" />
                <template v-if="app.last_used_at">{{ t("ui.lastUsed") }} {{ formatDate(app.last_used_at) }}</template>
                <template v-else>{{ t("ui.neverUsed") }}</template>
              </span>
            </div>
          </div>
          <Button
            variant="ghost"
            size="sm"
            class="text-destructive hover:text-destructive"
            :aria-label="t('pages.connectedAppRevoke')"
            @click="appToRevoke = app"
          >
            <Trash2 class="mr-1 h-4 w-4" />
            {{ t("pages.connectedAppRevoke") }}
          </Button>
        </div>
      </div>
    </PageSection>

    <ConfirmDialog
      :open="appToRevoke !== null"
      :title="t('pages.connectedAppRevokeTitle')"
      :description="appToRevoke ? t('pages.connectedAppRevokeDescription', { client: appToRevoke.client.name }) : undefined"
      :confirm-text="t('pages.connectedAppRevoke')"
      destructive
      @update:open="(v) => { if (!v) appToRevoke = null }"
      @confirm="confirmRevoke"
    />
  </div>
</template>
