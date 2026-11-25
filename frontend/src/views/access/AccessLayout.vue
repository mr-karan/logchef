<script setup lang="ts">
import { computed, type Component } from "vue";
import { useRoute } from "vue-router";
import { useI18n } from "vue-i18n";
import {
  Activity,
  Database,
  KeyRound,
  LayoutGrid,
  ScanSearch,
  ScrollText,
  Users,
  UsersRound,
  Wrench,
} from "lucide-vue-next";
import { cn } from "@/lib/utils";
import { useTeamPermissions } from "@/composables/useTeamPermissions";

interface AdminNavItem {
  label: string;
  to: string;
  icon: Component;
  match: (path: string) => boolean;
}

interface AdminNavGroup {
  id: string;
  label: string;
  items: AdminNavItem[];
}

const { t } = useI18n();
const route = useRoute();
const { isGlobalAdmin } = useTeamPermissions();

const activePath = computed(() => route.path);

const isOverviewActive = computed(() => activePath.value === "/admin");

const navGroups = computed<AdminNavGroup[]>(() => {
  if (!isGlobalAdmin.value) {
    return [
      {
        id: "access",
        label: t("admin.navGroupAccess"),
        items: [
          {
            label: t("ui.teams"),
            to: "/admin/teams",
            icon: Users,
            match: (path) => path === "/admin/teams" || path.startsWith("/admin/teams/"),
          },
        ],
      },
    ];
  }

  return [
    {
      id: "access",
      label: t("admin.navGroupAccess"),
      items: [
        {
          label: t("admin.navPeople"),
          to: "/admin/users",
          icon: UsersRound,
          match: (path) => path === "/admin/users" || path.startsWith("/admin/users/"),
        },
        {
          label: t("ui.teams"),
          to: "/admin/teams",
          icon: Users,
          match: (path) => path === "/admin/teams" || path.startsWith("/admin/teams/"),
        },
        {
          label: t("admin.navServiceAccounts"),
          to: "/admin/service-tokens",
          icon: KeyRound,
          match: (path) => path === "/admin/service-tokens" || path.startsWith("/admin/service-tokens/"),
        },
      ],
    },
    {
      id: "data",
      label: t("admin.navGroupData"),
      items: [
        {
          label: t("ui.sources"),
          to: "/admin/sources",
          icon: Database,
          match: (path) =>
            (path === "/admin/sources" || path.startsWith("/admin/sources/")) &&
            !path.startsWith("/admin/sources/stats"),
        },
        {
          label: t("routes.sourceInspection"),
          to: "/admin/sources/stats",
          icon: ScanSearch,
          match: (path) => path === "/admin/sources/stats" || path.startsWith("/admin/sources/stats/"),
        },
      ],
    },
    {
      id: "monitoring",
      label: t("admin.navGroupMonitoring"),
      items: [
        {
          label: t("ui.queryActivity"),
          to: "/admin/query-activity",
          icon: Activity,
          match: (path) => path === "/admin/query-activity" || path.startsWith("/admin/query-activity/"),
        },
        {
          label: t("ui.auditLog"),
          to: "/admin/audit-log",
          icon: ScrollText,
          match: (path) => path === "/admin/audit-log" || path.startsWith("/admin/audit-log/"),
        },
      ],
    },
    {
      id: "system",
      label: t("admin.navGroupSystem"),
      items: [
        {
          label: t("admin.navSettings"),
          to: "/admin/settings",
          icon: Wrench,
          match: (path) => path === "/admin/settings" || path.startsWith("/admin/settings/"),
        },
      ],
    },
  ];
});
</script>

<template>
  <div class="grid gap-6 lg:grid-cols-[200px_1fr]">
    <aside class="h-fit rounded-md border bg-card p-2">
      <nav :aria-label="t('ui.administration')" class="space-y-3">
        <router-link
          v-if="isGlobalAdmin"
          to="/admin"
          class="block"
        >
          <div
            :class="
              cn(
                'flex items-center gap-2 rounded-md px-2.5 py-1.5 text-sm transition-colors',
                isOverviewActive
                  ? 'bg-accent text-accent-foreground font-medium'
                  : 'text-muted-foreground hover:bg-muted/60 hover:text-foreground',
              )
            "
          >
            <LayoutGrid class="h-4 w-4 shrink-0" />
            <span class="truncate">{{ t("admin.navOverview") }}</span>
          </div>
        </router-link>

        <div v-for="group in navGroups" :key="group.id" class="space-y-0.5">
          <div class="px-2.5 pb-1 pt-1 text-[11px] font-medium uppercase tracking-wider text-muted-foreground">
            {{ group.label }}
          </div>
          <router-link
            v-for="item in group.items"
            :key="item.to"
            :to="item.to"
            class="block"
          >
            <div
              :class="
                cn(
                  'flex items-center gap-2 rounded-md px-2.5 py-1.5 text-sm transition-colors',
                  item.match(activePath)
                    ? 'bg-accent text-accent-foreground font-medium'
                    : 'text-muted-foreground hover:bg-muted/60 hover:text-foreground',
                )
              "
            >
              <component :is="item.icon" class="h-4 w-4 shrink-0" />
              <span class="truncate">{{ item.label }}</span>
            </div>
          </router-link>
        </div>
      </nav>
    </aside>

    <section class="min-w-0">
      <router-view />
    </section>
  </div>
</template>
