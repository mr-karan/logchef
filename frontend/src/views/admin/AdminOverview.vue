<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import { useI18n } from "vue-i18n";
import { Bot, Database, Users, UsersRound } from "lucide-vue-next";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { LoadingState, PageHeader } from "@/components/layout";
import { teamsApi, type TeamMembershipSummary } from "@/api/teams";
import { isSuccessResponse } from "@/api/types";
import { useUsersStore } from "@/stores/users";
import { useTeamsStore } from "@/stores/teams";
import { useSourcesStore } from "@/stores/sources";
import { useServiceAccountsStore } from "@/stores/serviceAccounts";

const { t } = useI18n();
const usersStore = useUsersStore();
const teamsStore = useTeamsStore();
const sourcesStore = useSourcesStore();
const serviceAccountsStore = useServiceAccountsStore();

const isLoading = ref(true);
const memberships = ref<TeamMembershipSummary[]>([]);
const membershipsLoaded = ref(false);

const humanUsers = computed(() =>
  usersStore.users.filter((user) => user.account_type !== "service"),
);
const adminUserCount = computed(() =>
  humanUsers.value.filter((user) => user.role === "admin").length,
);
const inactiveUserCount = computed(() =>
  humanUsers.value.filter((user) => user.status === "inactive").length,
);

const teams = computed(() => teamsStore.adminTeams);
const teamsWithoutAdminCount = computed(() => {
  if (!membershipsLoaded.value) return null;
  const teamsWithAdmin = new Set<number>();
  for (const membership of memberships.value) {
    if (membership.role === "admin") {
      teamsWithAdmin.add(membership.team_id);
    }
  }
  return teams.value.filter((team) => !teamsWithAdmin.has(team.id)).length;
});

const sources = computed(() => sourcesStore.sources);
const unreachableSourcesCount = computed(() =>
  sources.value.filter((source) => !source.is_connected).length,
);

const serviceAccounts = computed(() => serviceAccountsStore.accounts);

onMounted(async () => {
  try {
    const [, , , , membershipsRes] = await Promise.all([
      usersStore.loadUsers(true),
      teamsStore.loadAdminTeams(true),
      sourcesStore.loadAllSourcesForAdmin(),
      serviceAccountsStore.loadAccounts(true),
      teamsApi.listAllMemberships().catch(() => null),
    ]);
    if (membershipsRes && isSuccessResponse(membershipsRes) && Array.isArray(membershipsRes.data)) {
      memberships.value = membershipsRes.data;
      membershipsLoaded.value = true;
    }
  } finally {
    isLoading.value = false;
  }
});
</script>

<template>
  <div class="space-y-6">
    <PageHeader
      :title="t('admin.overviewTitle')"
      :description="t('admin.overviewDescription')"
    />

    <LoadingState v-if="isLoading" :label="t('admin.overviewLoading')" />

    <div v-else class="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
      <router-link to="/admin/users" class="group block">
        <Card class="h-full transition-colors group-hover:border-primary/40 group-hover:bg-muted/30">
          <CardHeader class="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle class="text-sm font-medium text-muted-foreground">
              {{ t("admin.navPeople") }}
            </CardTitle>
            <UsersRound class="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div class="text-2xl font-semibold tabular-nums">{{ humanUsers.length }}</div>
            <p class="mt-1 text-xs text-muted-foreground">
              {{ t("admin.overviewPeopleSummary", { admins: adminUserCount, inactive: inactiveUserCount }) }}
            </p>
          </CardContent>
        </Card>
      </router-link>

      <router-link to="/admin/teams" class="group block">
        <Card class="h-full transition-colors group-hover:border-primary/40 group-hover:bg-muted/30">
          <CardHeader class="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle class="text-sm font-medium text-muted-foreground">
              {{ t("ui.teams") }}
            </CardTitle>
            <Users class="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div class="text-2xl font-semibold tabular-nums">{{ teams.length }}</div>
            <p v-if="teamsWithoutAdminCount !== null" class="mt-1 text-xs text-muted-foreground">
              {{
                teamsWithoutAdminCount > 0
                  ? t("admin.overviewTeamsNoAdmin", { count: teamsWithoutAdminCount })
                  : t("admin.overviewTeamsAllHaveAdmin")
              }}
            </p>
          </CardContent>
        </Card>
      </router-link>

      <router-link to="/admin/sources" class="group block">
        <Card class="h-full transition-colors group-hover:border-primary/40 group-hover:bg-muted/30">
          <CardHeader class="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle class="text-sm font-medium text-muted-foreground">
              {{ t("ui.sources") }}
            </CardTitle>
            <Database class="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div class="text-2xl font-semibold tabular-nums">{{ sources.length }}</div>
            <p class="mt-1 text-xs text-muted-foreground">
              {{
                unreachableSourcesCount > 0
                  ? t("admin.overviewSourcesUnreachable", { count: unreachableSourcesCount })
                  : t("admin.overviewSourcesAllReachable")
              }}
            </p>
          </CardContent>
        </Card>
      </router-link>

      <router-link to="/admin/service-tokens" class="group block">
        <Card class="h-full transition-colors group-hover:border-primary/40 group-hover:bg-muted/30">
          <CardHeader class="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle class="text-sm font-medium text-muted-foreground">
              {{ t("admin.navServiceAccounts") }}
            </CardTitle>
            <Bot class="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div class="text-2xl font-semibold tabular-nums">{{ serviceAccounts.length }}</div>
            <p class="mt-1 text-xs text-muted-foreground">
              {{ t("admin.overviewServiceAccountsSummary") }}
            </p>
          </CardContent>
        </Card>
      </router-link>
    </div>
  </div>
</template>
