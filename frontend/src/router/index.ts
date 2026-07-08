import {
  createRouter,
  createWebHistory,
  type RouteRecordRaw,
} from "vue-router";
import { useAuthStore } from "@/stores/auth";
import { useMetaStore } from "@/stores/meta";
import { error } from "@/utils/debug";
import { contextRouterGuard } from "./contextGuard";
import ComponentLoadError from "@/views/error/ComponentLoadError.vue";

const lazy = (name: string, loader: () => Promise<unknown>) => () =>
  loader().catch((err) => {
    error("Router", `Failed to load ${name} component`, err);
    return { default: ComponentLoadError } as { default: typeof ComponentLoadError };
  });

// Routes whose stale query params should be stripped on navigation. The Library
// routes are intentionally NOT here: they use ?view=all for the All-queries
// browse mode, and collection navigation already pushes query:{} explicitly.
const QUERYLESS_ROUTE_NAMES = new Set([
  "SavedQueryRedirect",
]);

const routes: RouteRecordRaw[] = [
  {
    path: "/",
    redirect: "/logs/explore",
  },

  // Auth Section
  {
    path: "/auth",
    children: [
      {
        path: "login",
        name: "Login",
        component: lazy("Login", () => import("@/views/auth/Login.vue")),
        meta: {
          title: "Login",
          public: true,
          layout: "outer",
        },
        alias: "/login",
      },
      {
        path: "logout",
        name: "Logout",
        component: lazy("Logout", () => import("@/views/auth/Logout.vue")),
        meta: {
          title: "Logout",
          public: true,
          layout: "outer",
        },
        alias: "/logout",
      },
    ],
  },

  // Logs Section
  {
    path: "/logs",
    component: lazy("LogsLayout", () => import("@/views/explore/LogsLayout.vue")),
    meta: {
      requiresAuth: true,
    },
    children: [
      {
        path: "",
        redirect: "explore",
      },
      {
        path: "explore",
        name: "LogExplorer",
        component: lazy("LogExplorer", () => import("@/views/explore/LogExplorer.vue")),
        meta: { title: "Log Explorer" },
      },
      {
        path: "library",
        name: "Library",
        component: lazy("LibraryView", () => import("@/views/library/LibraryView.vue")),
        meta: { title: "Library" },
      },
      {
        path: "library/:collectionID",
        name: "LibraryCollection",
        component: lazy("LibraryView", () => import("@/views/library/LibraryView.vue")),
        meta: { title: "Library" },
      },
      {
        path: "alerts",
        name: "AlertsOverview",
        component: lazy("AlertsOverview", () => import("@/views/alerts/AlertsOverview.vue")),
        meta: { title: "Alerts" },
      },
      {
        path: "alerts/new",
        name: "AlertCreate",
        component: lazy("AlertCreate", () => import("@/views/alerts/AlertCreate.vue")),
        meta: { title: "Create Alert" },
      },
      {
        path: "alerts/:alertID",
        name: "AlertDetail",
        component: lazy("AlertDetail", () => import("@/views/alerts/AlertDetail.vue")),
        meta: { title: "Alert Detail" },
      },
      {
        path: "saved/:queryId",
        name: "SavedQueryRedirect",
        component: lazy("SavedQueryRedirect", () => import("@/views/collections/CollectionRedirect.vue")),
        meta: { title: "Loading Saved Query..." },
      },
    ],
  },

  // Dashboards Section (cross-team; each panel carries its own team/source, so
  // dashboards live at the top level rather than under the context-scoped /logs
  // tree — they still render inside the standard InnerApp sidebar shell).
  {
    path: "/dashboards",
    meta: {
      requiresAuth: true,
    },
    children: [
      {
        path: "",
        name: "Dashboards",
        component: lazy("DashboardsList", () => import("@/views/dashboards/DashboardsList.vue")),
        meta: { title: "Dashboards" },
      },
      {
        path: ":id",
        name: "DashboardView",
        component: lazy("DashboardView", () => import("@/views/dashboards/DashboardView.vue")),
        meta: { title: "Dashboard" },
      },
    ],
  },

  // Admin Section. Per-route gates: `requiresAdmin` blocks non-global admins;
  // `requiresAnyTeamAdmin` lets team admins through for their team's pages.
  // The parent has no role meta — children declare their own access.
  {
    path: "/admin",
    component: lazy("AccessLayout", () => import("@/views/access/AccessLayout.vue")),
    meta: {
      requiresAuth: true,
    },
    children: [
      {
        path: "",
        redirect: "users",
      },
      {
        path: "users",
        name: "ManageUsers",
        component: lazy("UsersList", () => import("@/views/access/users/UsersList.vue")),
        meta: { title: "Users", requiresAdmin: true },
      },
      {
        path: "service-tokens",
        name: "ServiceTokens",
        component: lazy("ServiceTokens", () => import("@/views/access/service-accounts/ServiceTokens.vue")),
        meta: { title: "Service Tokens", requiresAdmin: true },
      },
      {
        path: "teams",
        name: "Teams",
        component: lazy("TeamsList", () => import("@/views/access/teams/TeamsList.vue")),
        meta: { title: "Teams", requiresAnyTeamAdmin: true },
      },
      {
        path: "teams/:id",
        name: "TeamSettings",
        component: lazy("TeamSettings", () => import("@/views/access/teams/TeamSettings.vue")),
        meta: { title: "Team Settings", requiresAnyTeamAdmin: true },
      },
      {
        path: "sources",
        name: "Sources",
        component: lazy("ManageSources", () => import("@/views/sources/ManageSources.vue")),
        meta: { title: "Sources", requiresAdmin: true },
      },
      {
        path: "sources/new",
        name: "NewSource",
        component: lazy("AddSource", () => import("@/views/sources/AddSource.vue")),
        meta: { title: "New Source", requiresAdmin: true },
      },
      {
        path: "sources/:sourceId/edit",
        name: "EditSource",
        component: lazy("AddSource", () => import("@/views/sources/AddSource.vue")),
        meta: { title: "Edit Source", requiresAdmin: true },
      },
      {
        path: "sources/stats",
        name: "SourceInspection",
        component: lazy("SourceInspection", () => import("@/views/sources/SourceStats.vue")),
        meta: { title: "Source Inspection", requiresAdmin: true },
      },
      {
        path: "settings",
        name: "AdminSettings",
        component: lazy("AdminSettings", () => import("@/views/admin/AdminSettings.vue")),
        meta: { title: "System Settings", requiresAdmin: true },
      },
    ],
  },

  // Settings Section (user-scoped)
  {
    path: "/settings",
    component: lazy("SettingsLayout", () => import("@/views/settings/SettingsLayout.vue")),
    meta: {
      requiresAuth: true,
    },
    children: [
      {
        path: "",
        redirect: "profile",
      },
      {
        path: "profile",
        name: "Profile",
        component: lazy("UserProfile", () => import("@/views/settings/UserProfile.vue")),
        meta: { title: "Profile" },
      },
      {
        path: "preferences",
        name: "Preferences",
        component: lazy("UserPreferences", () => import("@/views/settings/UserPreferences.vue")),
        meta: { title: "User Preferences" },
      },
    ],
  },

  // Error Pages
  {
    path: "/forbidden",
    name: "Forbidden",
    component: lazy("Forbidden", () => import("@/views/error/Forbidden.vue")),
    meta: {
      title: "Access Denied",
      public: true,
    },
  },
  {
    path: "/:pathMatch(.*)*",
    name: "NotFound",
    component: lazy("NotFound", () => import("@/views/error/NotFound.vue")),
    meta: {
      title: "Not Found",
      public: true,
    },
  },
];

const router = createRouter({
  history: createWebHistory(),
  routes,
  scrollBehavior(_to, _from, savedPosition) {
    return savedPosition ?? { top: 0 };
  },
});

router.beforeEach(async (to) => {
  const authStore = useAuthStore();
  const isAuthenticated = authStore.isAuthenticated;
  const isAdmin = authStore.user?.role === "admin";
  const isPublic = to.matched.some((record) => record.meta.public);

  if (!isAuthenticated && !isPublic) {
    return {
      name: "Login",
      query: { redirect: to.fullPath },
    };
  }

  if (isAuthenticated && to.name === "Login") {
    return { path: "/" };
  }

  if (
    QUERYLESS_ROUTE_NAMES.has(String(to.name)) &&
    Object.keys(to.query).length > 0
  ) {
    return {
      path: to.path,
      hash: to.hash,
      query: {},
      replace: true,
    };
  }

  // If the server has alerting disabled, redirect any /logs/alerts* deep link
  // to the explorer. The layered defenses in each alert view and the alert
  // stores handle bookmarked URLs that arrive before meta has loaded.
  const metaStore = useMetaStore();
  if (!metaStore.alertsEnabled && to.path.startsWith("/logs/alerts")) {
    return { path: "/logs/explore" };
  }

  if (to.matched.some((record) => record.meta.requiresAdmin) && !isAdmin) {
    return { name: "Forbidden" };
  }

  if (to.matched.some((record) => record.meta.requiresAnyTeamAdmin) && !isAdmin) {
    // Lazy-import to avoid a circular store dependency at module init.
    const { useTeamsStore } = await import("@/stores/teams");
    if (!useTeamsStore().isAnyTeamAdmin) {
      return { name: "Forbidden" };
    }
  }

  if (isAuthenticated && !isPublic) {
    contextRouterGuard(to);
  }
});

export default router;
