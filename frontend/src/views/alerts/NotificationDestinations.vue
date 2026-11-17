<script setup lang="ts">
import { computed, onActivated, onMounted, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { Bell, Mail, Pencil, Plus, RefreshCcw, Search, Send, Slack, Webhook } from "lucide-vue-next";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import ConfirmDialog from "@/components/ui/ConfirmDialog.vue";
import ErrorAlert from "@/components/ui/ErrorAlert.vue";
import { EmptyState, LoadingState, PageHeader, RowActionsMenu, SearchInput, SortHeader } from "@/components/layout";
import AlertsTabs from "@/components/alerts/AlertsTabs.vue";
import DestinationForm from "@/components/alerts/DestinationForm.vue";
import { useAuthStore } from "@/stores/auth";
import { useContextStore } from "@/stores/context";
import { useMetaStore } from "@/stores/meta";
import { useNotificationDestinationsStore } from "@/stores/notificationDestinations";
import { useTeamsStore } from "@/stores/teams";
import { useContextSync } from "@/composables/useContextSync";
import { useTableSearchSort } from "@/composables/useTableSearchSort";
import { useToast } from "@/composables/useToast";
import type { NotificationDestination } from "@/api/notificationDestinations";

const { t } = useI18n();
const authStore = useAuthStore();
const contextStore = useContextStore();
const metaStore = useMetaStore();
const store = useNotificationDestinationsStore();
const teamsStore = useTeamsStore();
const { toast } = useToast();

const { initialize: initializeContext } = useContextSync({ basePath: "/logs/alerts/destinations" });

const teamId = computed(() => contextStore.teamId);
const isLoading = computed(() => store.isLoadingOperation("fetchDestinations"));
const loadError = computed(() => (store.error ? { message: store.error.message } : null));

function teamName(id: number | undefined): string {
  return teamsStore.userTeams.find((team) => team.id === id)?.name ?? t("destinations.teamUnknown", { id });
}

function scopeLabel(d: NotificationDestination): string {
  if (d.scope === "team") return teamName(d.team_id);
  return d.scope === "global" ? t("destinations.scopeGlobal") : t("destinations.scopePrivate");
}

function typeLabel(d: NotificationDestination): string {
  if (d.type === "email") return t("destinations.typeEmail");
  return d.type === "slack" ? t("destinations.typeSlack") : t("destinations.typeWebhook");
}

function target(d: NotificationDestination): string {
  if (d.type === "email") return t("destinations.recipientCount", (d.config.user_ids?.length ?? 0) + (d.config.addresses?.length ?? 0));
  return d.secret_hint ?? "";
}

const { search, rows, sortKey, sortDir, toggleSort } = useTableSearchSort(() => store.destinations, {
  searchKeys: ["name", "type", scopeLabel, target],
  sortAccessors: {
    name: (d) => d.name.toLowerCase(),
    type: (d) => d.type,
    scope: (d) => scopeLabel(d).toLowerCase(),
    active: (d) => (d.is_active ? 0 : 1),
  },
  initialSort: { key: "name", dir: "asc" },
  persistKey: "notification-destinations",
});

// The server's edit rule: global admins edit everything; the creator edits a
// private or team destination; team owners and admins edit a team one.
function canEdit(d: NotificationDestination): boolean {
  const user = authStore.user;
  if (!user) return false;
  if (user.role === "admin") return true;
  if (d.scope === "global") return false;
  if (d.created_by != null && String(d.created_by) === String(user.id)) return true;
  if (d.scope !== "team") return false;
  const role: string | undefined = teamsStore.userTeams.find((team) => team.id === d.team_id)?.role;
  return role === "owner" || role === "admin";
}

const formOpen = ref(false);
// Remounts the form for each open, since it reads its starting values once.
const formKey = ref(0);
const editing = ref<NotificationDestination | null>(null);
const copyOf = ref<NotificationDestination | null>(null);

function openForm(destination: NotificationDestination | null, copy: NotificationDestination | null) {
  editing.value = destination;
  copyOf.value = copy;
  formKey.value++;
  formOpen.value = true;
}

async function sendTest(d: NotificationDestination) {
  const result = await store.testDestination({ id: d.id });
  if (!result.success || !result.data) {
    toast({ title: t("destinations.testFailed"), description: result.error?.message, variant: "destructive" });
    return;
  }
  const failed = result.data.results.filter((r) => !r.ok);
  if (failed.length) {
    toast({ title: t("destinations.testFailed"), description: failed.map((r) => `${r.target}: ${r.error ?? ""}`).join("\n"), variant: "destructive" });
  } else {
    toast({ title: t("destinations.testDone", { name: d.name }), variant: "success" });
  }
}

const deleteTarget = ref<NotificationDestination | null>(null);
const deleteOpen = ref(false);
function confirmDelete(d: NotificationDestination) {
  deleteTarget.value = d;
  deleteOpen.value = true;
}
async function doDelete() {
  if (deleteTarget.value) await store.deleteDestination(deleteTarget.value.id);
  deleteTarget.value = null;
}

function load() {
  void store.fetchDestinations();
  if (!teamsStore.userTeams.length) void teamsStore.loadUserTeams();
}

// The form lists the members of the team its destination belongs to: the
// destination's own team when it has one, else the current team.
const formTeamId = computed(() => {
  const base = editing.value ?? copyOf.value;
  return base?.scope === "team" && base.team_id ? base.team_id : teamId.value;
});
const formMembers = computed(() => (formTeamId.value ? teamsStore.getTeamMembersByTeamId(formTeamId.value) : []));
// Only teams the user can read: a 403 on this GET would open the Forbidden page.
watch([formTeamId, () => teamsStore.userTeams.length], ([id]) => {
  const readable = authStore.user?.role === "admin" || teamsStore.userTeams.some((team) => team.id === id);
  if (id && readable) void teamsStore.listTeamMembers(id);
}, { immediate: true });

load();
// LogsLayout keeps this page alive; a return shows the current list.
let activated = false;
onActivated(() => {
  if (activated) load();
  activated = true;
});
onMounted(initializeContext);
</script>

<template>
  <EmptyState
    v-if="!metaStore.alertsEnabled"
    :icon="Bell"
    :title="t('ui.alertingIsDisabled')"
    :description="t('ui.alertingIsDisabledOnThisServerAskYourAdministratorToSetAlerts')"
  />
  <div v-else class="space-y-6">
    <PageHeader :title="t('alerts.destinations')">
      <template #actions>
        <Button variant="outline" :disabled="isLoading" @click="load">
          <RefreshCcw class="-ml-1 mr-2 h-4 w-4" />
          {{ t('ui.refresh') }}
        </Button>
        <Button @click="openForm(null, null)">
          <Plus class="-ml-1 mr-2 h-4 w-4" />
          {{ t('alerts.newDestination') }}
        </Button>
      </template>
    </PageHeader>

    <AlertsTabs />

    <Card>
      <CardContent class="pt-6">
        <div v-if="loadError" class="mb-4">
          <ErrorAlert :error="loadError" :title="t('alerts.loadDestinationsFailed')" @retry="load" />
        </div>
        <LoadingState v-if="isLoading && !store.destinations.length" :label="t('destinations.loading')" />
        <EmptyState
          v-else-if="!store.destinations.length"
          :icon="Send"
          :title="t('destinations.empty')"
          :description="t('destinations.emptyDescription')"
        >
          <template #action>
            <Button @click="openForm(null, null)">
              <Plus class="-ml-1 mr-2 h-4 w-4" />
              {{ t('alerts.newDestination') }}
            </Button>
          </template>
        </EmptyState>
        <div v-else class="space-y-4">
          <SearchInput v-model="search" :placeholder="t('destinations.search')" />
          <EmptyState
            v-if="!rows.length"
            :icon="Search"
            :title="t('destinations.noMatches', { search })"
            :description="t('ui.tryADifferentNameOrClearTheSearch')"
          />
          <Table v-else>
            <TableHeader>
              <TableRow>
                <TableHead><SortHeader column="name" :sort-key="sortKey" :sort-dir="sortDir" @toggle="toggleSort">{{ t('destinations.name') }}</SortHeader></TableHead>
                <TableHead><SortHeader column="type" :sort-key="sortKey" :sort-dir="sortDir" @toggle="toggleSort">{{ t('destinations.type') }}</SortHeader></TableHead>
                <TableHead><SortHeader column="scope" :sort-key="sortKey" :sort-dir="sortDir" @toggle="toggleSort">{{ t('destinations.scope') }}</SortHeader></TableHead>
                <TableHead>{{ t('destinations.target') }}</TableHead>
                <TableHead><SortHeader column="active" :sort-key="sortKey" :sort-dir="sortDir" @toggle="toggleSort">{{ t('ui.status') }}</SortHeader></TableHead>
                <TableHead class="w-32 text-right">{{ t('ui.actions') }}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              <TableRow v-for="d in rows" :key="d.id" :data-destination-id="d.id">
                <TableCell class="py-3 font-medium">{{ d.name }}</TableCell>
                <TableCell class="py-3 text-sm">
                  <span class="inline-flex items-center gap-1.5">
                    <Mail v-if="d.type === 'email'" class="h-3.5 w-3.5 text-muted-foreground" />
                    <Slack v-else-if="d.type === 'slack'" class="h-3.5 w-3.5 text-muted-foreground" />
                    <Webhook v-else class="h-3.5 w-3.5 text-muted-foreground" />
                    {{ typeLabel(d) }}
                  </span>
                </TableCell>
                <TableCell class="py-3 text-sm">{{ scopeLabel(d) }}</TableCell>
                <TableCell class="py-3 text-sm text-muted-foreground">{{ target(d) }}</TableCell>
                <TableCell class="py-3">
                  <Badge :variant="d.is_active ? 'secondary' : 'outline'">{{ d.is_active ? t('ui.active') : t('ui.disabled') }}</Badge>
                </TableCell>
                <TableCell class="py-3 text-right">
                  <div class="inline-flex items-center gap-0.5">
                    <Button v-if="canEdit(d)" variant="ghost" size="icon" class="h-8 w-8" :title="t('ui.edit')" :aria-label="t('ui.edit')" @click="openForm(d, null)">
                      <Pencil class="h-4 w-4" />
                    </Button>
                    <Button
                      variant="ghost"
                      size="icon"
                      class="h-8 w-8"
                      :title="t('destinations.test')"
                      :aria-label="t('destinations.test')"
                      :disabled="store.isLoadingOperation('testDestination')"
                      @click="sendTest(d)"
                    >
                      <Send class="h-4 w-4" />
                    </Button>
                    <RowActionsMenu
                      :on-duplicate="() => openForm(null, d)"
                      :on-delete="canEdit(d) ? () => confirmDelete(d) : undefined"
                    />
                  </div>
                </TableCell>
              </TableRow>
            </TableBody>
          </Table>
        </div>
      </CardContent>
    </Card>

    <Dialog v-model:open="formOpen">
      <DialogContent class="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{{ editing ? t('destinations.editDestination') : t('alerts.newDestination') }}</DialogTitle>
        </DialogHeader>
        <DestinationForm
          :key="formKey"
          :team-members="formMembers"
          :team-id="teamId"
          :destination="editing"
          :copy-of="copyOf"
          @saved="formOpen = false"
          @cancel="formOpen = false"
        />
      </DialogContent>
    </Dialog>
    <ConfirmDialog
      v-model:open="deleteOpen"
      :title="t('destinations.deleteTitle')"
      :description="t('destinations.deleteDescription', { name: deleteTarget?.name ?? '' })"
      :confirm-text="t('ui.delete')"
      destructive
      @confirm="doDelete"
      @cancel="deleteTarget = null"
    />
  </div>
</template>
