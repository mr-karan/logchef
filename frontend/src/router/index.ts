import {
  createRouter,
  createWebHistory,
  type RouteRecordRaw,
} from "vue-router";
import { useAuthStore } from "@/stores/auth";
import { error } from "@/utils/debug";

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
        component: () => import("@/views/SavedQueriesView.vue").catch(err => {
          error("Router", "Failed to load SavedQueriesView component", err);
          return { default: ComponentLoadError };
        }),
        meta: { title: "Saved Queries" },
      },
      // {
      //   path: "history",
      //   name: "QueryHistory",
      //   component: () => import("@/views/explore/QueryHistory.vue"),
      //   meta: { title: "Query History" },
      // },
    ],
  },

  // Redirect old team queries URLs to the new path
  {
    path: "/teams/:teamId/queries",
    redirect: (to) => ({
      path: "/logs/saved",
      query: { team: to.params.teamId },
    }),
  },

  // Sources Section
  {
    path: "/sources",
    component: () => import("@/views/sources/SourcesLayout.vue").catch(err => {
      error("Router", "Failed to load SourcesLayout component", err);
      return { default: ComponentLoadError };
    }),
    meta: {
      requiresAuth: true,
    },
    children: [
      {
        path: "",
        redirect: "list",
      },
      {
        path: "list",
        name: "Sources",
        component: () => import("@/views/sources/ManageSources.vue").catch(err => {
          error("Router", "Failed to load ManageSources component", err);
          return { default: ComponentLoadError };
        }),
        meta: { title: "Sources" },
      },
      {
        path: "new",
        name: "NewSource",
        component: () => import("@/views/sources/AddSource.vue").catch(err => {
          error("Router", "Failed to load AddSource component", err);
          return { default: ComponentLoadError };
        }),
        meta: { title: "New Source" },
      },
      {
        path: "stats",
        name: "SourceStats",
        component: () => import("@/views/sources/SourceStats.vue").catch(err => {
          error("Router", "Failed to load SourceStats component", err);
          return { default: ComponentLoadError };
        }),
        meta: { title: "Source Stats" },
      },
    ],
  },

  // Access Section (Admin only)
  {
    path: "/access",
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
    ],
  },

  // Settings Section
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
        meta: { title: "Profile Settings" },
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
  scrollBehavior(to, from, savedPosition) {
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

  // Update document title
  document.title = `${to.meta.title ? to.meta.title + " - " : ""}LogChef`;

  // Handle authentication
  if (!isAuthenticated && !isPublic) {
    return {
      name: "Login",
      query: { redirect: to.fullPath },
    };
  }

  // Prevent authenticated users from accessing login page
  if (isAuthenticated && to.name === "Login") {
    return { path: "/" };
  }

  // Handle admin routes
  if (to.matched.some((record) => record.meta.requiresAdmin) && !isAdmin) {
    return { name: "Forbidden" };
  }
});

export default router;