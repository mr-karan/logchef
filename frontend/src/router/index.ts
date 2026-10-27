import {
  createRouter,
  createWebHistory,
  type RouteRecordRaw,
} from "vue-router";
import { useAuthStore } from "@/stores/auth";
import { useMetaStore } from "@/stores/meta";
import { basePath } from "@/utils/basePath";
import { error } from "@/utils/debug";
import { meetsRequirements, routeRequirements, type RouteRequirement, type Viewer } from "./access";
import { contextRouterGuard } from "./contextGuard";
import { usePendingNavigation } from "./navigation";
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
          title: "routes.login",
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
          title: "routes.logout",
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
        meta: { title: "routes.logExplorer", palette: { icon: "explore", contextual: true } },
      },
      {
        path: "library",
        name: "Library",
        component: lazy("LibraryView", () => import("@/views/library/LibraryView.vue")),
        meta: { title: "routes.library", palette: { icon: "library" } },
      },
      {
        path: "library/:collectionID",
        name: "LibraryCollection",
        component: lazy("LibraryView", () => import("@/views/library/LibraryView.vue")),
        meta: { title: "routes.library" },
      },
      {
        path: "alerts",
        name: "AlertsOverview",
        component: lazy("AlertsOverview", () => import("@/views/alerts/AlertsOverview.vue")),
        meta: { title: "routes.alerts", palette: { icon: "alerts", contextual: true } },
      },
      {
        path: "alerts/new",
        name: "AlertCreate",
        component: lazy("AlertCreate", () => import("@/views/alerts/AlertCreate.vue")),
        meta: { title: "routes.createAlert", palette: { icon: "newAlert" } },
      },
      {
        path: "alerts/:alertID",
        name: "AlertDetail",
        component: lazy("AlertDetail", () => import("@/views/alerts/AlertDetail.vue")),
        meta: { title: "routes.alertDetail" },
      },
      {
        path: "saved/:queryId",
        name: "SavedQueryRedirect",
        component: lazy("SavedQueryRedirect", () => import("@/views/collections/CollectionRedirect.vue")),
        meta: { title: "routes.loadingSavedQuery" },
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
        meta: { title: "routes.dashboards", palette: { icon: "dashboards" } },
      },
      {
        path: ":id",
        name: "DashboardView",
        component: lazy("DashboardView", () => import("@/views/dashboards/DashboardView.vue")),
        meta: { title: "routes.dashboard" },
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
        meta: { title: "routes.users", requiresAdmin: true, palette: { icon: "users" } },
      },
      {
        path: "service-tokens",
        name: "ServiceTokens",
        component: lazy("ServiceTokens", () => import("@/views/access/service-accounts/ServiceTokens.vue")),
        meta: { title: "routes.serviceTokens", requiresAdmin: true, palette: { icon: "serviceTokens" } },
      },
      {
        path: "teams",
        name: "Teams",
        component: lazy("TeamsList", () => import("@/views/access/teams/TeamsList.vue")),
        meta: { title: "routes.teams", requiresAnyTeamAdmin: true, palette: { icon: "teams" } },
      },
      {
        path: "teams/:id",
        name: "TeamSettings",
        component: lazy("TeamSettings", () => import("@/views/access/teams/TeamSettings.vue")),
        meta: { title: "routes.teamSettings", requiresAnyTeamAdmin: true },
      },
      {
        path: "sources",
        name: "Sources",
        component: lazy("ManageSources", () => import("@/views/sources/ManageSources.vue")),
        meta: { title: "routes.sources", requiresAdmin: true, palette: { icon: "sources" } },
      },
      {
        path: "sources/new",
        name: "NewSource",
        component: lazy("AddSource", () => import("@/views/sources/AddSource.vue")),
        meta: { title: "routes.newSource", requiresAdmin: true, palette: { icon: "newSource" } },
      },
      {
        path: "sources/:sourceId/edit",
        name: "EditSource",
        component: lazy("AddSource", () => import("@/views/sources/AddSource.vue")),
        meta: { title: "routes.editSource", requiresAdmin: true },
      },
      {
        path: "sources/stats",
        name: "SourceInspection",
        component: lazy("SourceInspection", () => import("@/views/sources/SourceStats.vue")),
        meta: { title: "routes.sourceInspection", requiresAdmin: true, palette: { icon: "inspectSource" } },
      },
      {
        path: "settings",
        name: "AdminSettings",
        component: lazy("AdminSettings", () => import("@/views/admin/AdminSettings.vue")),
        meta: { title: "routes.systemSettings", requiresAdmin: true, palette: { icon: "systemSettings" } },
      },
      {
        path: "query-activity",
        name: "AdminQueryActivity",
        component: lazy("AdminQueryActivity", () => import("@/views/admin/QueryActivity.vue")),
        meta: { title: "routes.queryActivity", requiresAdmin: true, palette: { icon: "queryActivity" } },
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
        meta: { title: "routes.profile", palette: { icon: "profile" } },
      },
      {
        path: "preferences",
        name: "Preferences",
        component: lazy("UserPreferences", () => import("@/views/settings/UserPreferences.vue")),
        meta: { title: "routes.preferences", palette: { icon: "preferences" } },
      },
      {
        path: "connected-apps",
        name: "ConnectedApps",
        component: lazy("ConnectedApps", () => import("@/views/settings/ConnectedApps.vue")),
        meta: { title: "routes.connectedApps", requiresOAuth: true, palette: { icon: "connectedApps" } },
      },
    ],
  },

  // OAuth consent. Not public: without a session the guard sends the user to
  // login with this URL as the redirect, and login returns here.
  {
    path: "/oauth/consent",
    name: "OAuthConsent",
    component: lazy("OAuthConsent", () => import("@/views/oauth/OAuthConsent.vue")),
    meta: {
      title: "routes.oauthConsent",
      layout: "outer",
      requiresOAuth: true,
    },
  },

  // Error Pages
  {
    path: "/forbidden",
    name: "Forbidden",
    component: lazy("Forbidden", () => import("@/views/error/Forbidden.vue")),
    meta: {
      title: "routes.accessDenied",
      public: true,
    },
  },
  {
    path: "/:pathMatch(.*)*",
    name: "NotFound",
    component: lazy("NotFound", () => import("@/views/error/NotFound.vue")),
    meta: {
      title: "routes.notFound",
      public: true,
    },
  },
];

const router = createRouter({
  history: createWebHistory(basePath),
  routes,
  scrollBehavior(_to, _from, savedPosition) {
    return savedPosition ?? { top: 0 };
  },
});

usePendingNavigation(router);

async function guardViewer(requirements: readonly RouteRequirement[]): Promise<Viewer> {
  const metaStore = useMetaStore();
  // OAuth pages exist only when the server runs Logchef OAuth. Meta may not
  // be loaded yet on a cold visit, so load it before deciding.
  if (requirements.includes("oauth")) await metaStore.loadMeta();
  const authStore = useAuthStore();
  const isAdmin = authStore.user?.role === "admin";
  let isAnyTeamAdmin = false;
  if (requirements.includes("anyTeamAdmin") && authStore.isAuthenticated && !isAdmin) {
    // Lazy-import to avoid a circular store dependency at module init.
    const { useTeamsStore } = await import("@/stores/teams");
    isAnyTeamAdmin = useTeamsStore().isAnyTeamAdmin;
  }
  return { isAdmin, isAnyTeamAdmin, alertsEnabled: metaStore.alertsEnabled, oauthEnabled: metaStore.oauthIssuer !== null };
}

router.beforeEach(async (to) => {
  const requirements = routeRequirements(to.matched);
  const viewer = await guardViewer(requirements);
  const unmet = (requirement: RouteRequirement) =>
    requirements.includes(requirement) && !meetsRequirements([requirement], viewer);

  if (unmet("oauth")) {
    return { name: "NotFound", params: { pathMatch: to.path.slice(1).split("/") } };
  }

  const authStore = useAuthStore();
  const isAuthenticated = authStore.isAuthenticated;
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

  if (unmet("alerts")) {
    return { path: "/logs/explore" };
  }

  if (unmet("admin") || unmet("anyTeamAdmin")) {
    return { name: "Forbidden" };
  }

  if (isAuthenticated && !isPublic) {
    await contextRouterGuard(to);
  }
});

export default router;
