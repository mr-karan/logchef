import {
  createRouter,
  createWebHistory,
  type RouteRecordRaw,
} from "vue-router";
import { useAuthStore } from "@/stores/auth";
import { error } from "@/utils/debug";
import { contextRouterGuard } from "./contextGuard";

// Import the error component for reuse
import ComponentLoadError from "@/views/error/ComponentLoadError.vue";

/**
 * Route definitions
 */
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
        component: () => import("@/views/auth/Login.vue").catch(err => {
          error("Router", "Failed to load Login component", err);
          return { default: ComponentLoadError };
        }),
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
        component: () => import("@/views/auth/Logout.vue").catch(err => {
          error("Router", "Failed to load Logout component", err);
          return { default: ComponentLoadError };
        }),
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
    component: () => import("@/views/explore/LogsLayout.vue").catch(err => {
      error("Router", "Failed to load LogsLayout component", err);
      return { default: ComponentLoadError };
    }),
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
        component: () => import("@/views/explore/LogExplorer.vue").catch(err => {
          error("Router", "Failed to load LogExplorer component", err);
          return { default: ComponentLoadError };
        }),
        meta: { title: "Log Explorer" },
      },
      {
        path: "saved",
        name: "SavedQueries",
        component: () => import("@/views/collections/SavedQueriesView.vue").catch(err => {
          error("Router", "Failed to load SavedQueriesView component", err);
          return { default: ComponentLoadError };
        }),
        meta: { title: "Collections" },
      },
      {
        path: "alerts",
        name: "AlertsOverview",
        component: () => import("@/views/alerts/AlertsOverview.vue").catch(err => {
          error("Router", "Failed to load AlertsOverview component", err);
          return { default: ComponentLoadError };
        }),
        meta: { title: "Alerts" },
      },
      {
        path: "alerts/new",
        name: "AlertCreate",
        component: () => import("@/views/alerts/AlertCreate.vue").catch(err => {
          error("Router", "Failed to load AlertCreate component", err);
          return { default: ComponentLoadError };
        }),
        meta: { title: "Create Alert" },
      },
      {
        path: "alerts/:alertID",
        name: "AlertDetail",
        component: () => import("@/views/alerts/AlertDetail.vue").catch(err => {
          error("Router", "Failed to load AlertDetail component", err);
          return { default: ComponentLoadError };
        }),
        meta: { title: "Alert Detail" },
      },
      {
        path: "saved/:queryId",
        name: "SavedQueryRedirect",
        component: () => import("@/views/collections/CollectionRedirect.vue").catch(err => {
          error("Router", "Failed to load SavedQueryRedirect component", err);
          return { default: ComponentLoadError };
        }),
        meta: { title: "Loading Saved Query..." },
      },
      {
        path: "collections",
        name: "CollectionsList",
        component: () => import("@/views/collections/CollectionsListView.vue").catch(err => {
          error("Router", "Failed to load CollectionsListView component", err);
          return { default: ComponentLoadError };
        }),
        meta: { title: "Collections" },
      },
      {
        path: "collections/:collectionID",
        name: "CollectionDetail",
        component: () => import("@/views/collections/CollectionDetailView.vue").catch(err => {
          error("Router", "Failed to load CollectionDetailView component", err);
          return { default: ComponentLoadError };
        }),
        meta: { title: "Collection" },
      },

    ],
  },
  // Admin Section (Admin only)
  {
    path: "/admin",
    component: () => import("@/views/access/AccessLayout.vue").catch(err => {
      error("Router", "Failed to load AccessLayout component", err);
      return { default: ComponentLoadError };
    }),
    meta: {
      requiresAuth: true,
      requiresAdmin: true,
    },
    children: [
      {
        path: "",
        redirect: "users",
      },
      {
        path: "users",
        name: "ManageUsers",
        component: () => import("@/views/access/users/UsersList.vue").catch(err => {
          error("Router", "Failed to load UsersList component", err);
          return { default: ComponentLoadError };
        }),
        meta: { title: "Users" },
      },
      {
        path: "users/new",
        name: "NewUser",
        component: () => import("@/views/access/users/AddUser.vue").catch(err => {
          error("Router", "Failed to load AddUser component", err);
          return { default: ComponentLoadError };
        }),
        meta: { title: "New User" },
      },
      {
        path: "teams",
        name: "Teams",
        component: () => import("@/views/access/teams/TeamsList.vue").catch(err => {
          error("Router", "Failed to load TeamsList component", err);
          return { default: ComponentLoadError };
        }),
        meta: { title: "Teams" },
      },
      {
        path: "teams/:id",
        name: "TeamSettings",
        component: () => import("@/views/access/teams/TeamSettings.vue").catch(err => {
          error("Router", "Failed to load TeamSettings component", err);
          return { default: ComponentLoadError };
        }),
        meta: { title: "Team Settings" },
      },
      {
        path: "sources",
        name: "Sources",
        component: () => import("@/views/sources/ManageSources.vue").catch(err => {
          error("Router", "Failed to load ManageSources component", err);
          return { default: ComponentLoadError };
        }),
        meta: { title: "Sources", requiresAdmin: true },
      },
      {
        path: "sources/new",
        name: "NewSource",
        component: () => import("@/views/sources/AddSource.vue").catch(err => {
          error("Router", "Failed to load AddSource component", err);
          return { default: ComponentLoadError };
        }),
        meta: { title: "New Source", requiresAdmin: true },
      },
      {
        path: "sources/:sourceId/edit",
        name: "EditSource",
        component: () => import("@/views/sources/AddSource.vue").catch(err => {
          error("Router", "Failed to load AddSource component", err);
          return { default: ComponentLoadError };
        }),
        meta: { title: "Edit Source", requiresAdmin: true },
      },
      {
        path: "sources/stats",
        name: "SourceStats",
        component: () => import("@/views/sources/SourceStats.vue").catch(err => {
          error("Router", "Failed to load Source Stats component", err);
          return { default: ComponentLoadError };
        }),
        meta: { title: "Source Stats", requiresAdmin: true },
      },
      {
        path: "settings",
        name: "AdminSettings",
        component: () => import("@/views/admin/AdminSettings.vue").catch(err => {
          error("Router", "Failed to load AdminSettings component", err);
          return { default: ComponentLoadError };
        }),
        meta: { title: "System Settings", requiresAdmin: true },
      },
    ],
  },

  // Settings Section (user-scoped)
  {
    path: "/settings",
    component: () => import("@/views/settings/SettingsLayout.vue").catch(err => {
      error("Router", "Failed to load SettingsLayout component", err);
      return { default: ComponentLoadError };
    }),
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
        component: () => import("@/views/settings/UserProfile.vue").catch(err => {
          error("Router", "Failed to load UserProfile component", err);
          return { default: ComponentLoadError };
        }),
        meta: { title: "Profile" },
      },
      {
        path: "preferences",
        name: "Preferences",
        component: () => import("@/views/settings/UserPreferences.vue").catch(err => {
          error("Router", "Failed to load UserPreferences component", err);
          return { default: ComponentLoadError };
        }),
        meta: { title: "User Preferences" },
      },
    ],
  },

  // Error Pages
  {
    path: "/forbidden",
    name: "Forbidden",
    component: () => import("@/views/error/Forbidden.vue").catch(err => {
      error("Router", "Failed to load Forbidden component", err);
      return { default: ComponentLoadError };
    }),
    meta: {
      title: "Access Denied",
      public: true,
    },
  },
  {
    path: "/:pathMatch(.*)*",
    name: "NotFound",
    component: () => import("@/views/error/NotFound.vue").catch(err => {
      error("Router", "Failed to load NotFound component", err);
      return { default: ComponentLoadError };
    }),
    meta: {
      title: "Not Found",
      public: true,
    },
  },
];

/**
 * Router instance
 */
const router = createRouter({
  history: createWebHistory(),
  routes,
  // Add scroll behavior to restore position
  scrollBehavior(_to, _from, savedPosition) {
    if (savedPosition) {
      return savedPosition;
    } else {
      return { top: 0 };
    }
  },
});

router.beforeEach(async (to) => {
  const authStore = useAuthStore();
  const isAuthenticated = authStore.isAuthenticated;
  const isAdmin = authStore.user?.role === "admin";
  const isPublic = to.matched.some((record) => record.meta.public);

  // Title is now managed reactively in App.vue via useTitle

  if (!isAuthenticated && !isPublic) {
    return {
      name: "Login",
      query: { redirect: to.fullPath },
    };
  }

  if (isAuthenticated && to.name === "Login") {
    return { path: "/" };
  }

  const querylessRouteNames = new Set([
    "SavedQueries",
    "SavedQueryRedirect",
    "CollectionsList",
    "CollectionDetail",
  ]);
  if (
    querylessRouteNames.has(String(to.name)) &&
    Object.keys(to.query).length > 0
  ) {
    return {
      path: to.path,
      hash: to.hash,
      query: {},
      replace: true,
    };
  }

  if (to.matched.some((record) => record.meta.requiresAdmin) && !isAdmin) {
    return { name: "Forbidden" };
  }

  if (isAuthenticated && !isPublic) {
    contextRouterGuard(to);
  }
});

export default router;
