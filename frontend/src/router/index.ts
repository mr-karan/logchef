import {
  createRouter,
  createWebHistory,
  type RouteRecordRaw,
} from "vue-router";
import { useAuthStore } from "@/stores/auth";

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
        component: () => import("@/views/auth/Login.vue"),
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
    component: () => import("@/views/explore/LogsLayout.vue"),
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
        component: () => import("@/views/explore/LogExplorer.vue"),
        meta: { title: "Log Explorer" },
      },
      {
        path: "saved",
        name: "SavedQueries",
        component: () => import("@/views/SavedQueriesView.vue"),
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
    redirect: to => {
      return { 
        path: '/logs/saved',
        query: { team: to.params.teamId }
      }
    }
  },
  // Sources Section
  {
    path: "/sources",
    component: () => import("@/views/sources/SourcesLayout.vue"),
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
        component: () => import("@/views/sources/ManageSources.vue"),
        meta: { title: "Sources" },
      },
      {
        path: "new",
        name: "NewSource",
        component: () => import("@/views/sources/AddSource.vue"),
        meta: { title: "New Source" },
      },
      {
        path: "stats",
        name: "SourceStats",
        component: () => import("@/views/sources/SourceStats.vue"),
        meta: { title: "Source Stats" },
      },
    ],
  },
  // Access Section (Admin only)
  {
    path: "/access",
    component: () => import("@/views/access/AccessLayout.vue"),
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
        component: () => import("@/views/access/users/UsersList.vue"),
        meta: { title: "Users" },
      },
      {
        path: "users/new",
        name: "NewUser",
        component: () => import("@/views/access/users/AddUser.vue"),
        meta: { title: "New User" },
      },
      {
        path: "teams",
        name: "Teams",
        component: () => import("@/views/access/teams/TeamsList.vue"),
        meta: { title: "Teams" },
      },
      {
        path: "teams/:id",
        name: "TeamSettings",
        component: () => import("@/views/access/teams/TeamSettings.vue"),
        meta: { title: "Team Settings" },
      },
    ],
  },
  // Settings Section
  {
    path: "/settings",
    component: () => import("@/views/settings/SettingsLayout.vue"),
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
        component: () => import("@/views/settings/UserProfile.vue"),
        meta: { title: "Profile Settings" },
      },
    ],
  },
  // Error Pages
  {
    path: "/forbidden",
    name: "Forbidden",
    component: () => import("@/views/error/Forbidden.vue"),
    meta: {
      title: "Access Denied",
      public: true,
    },
  },
  {
    path: "/:pathMatch(.*)*",
    name: "NotFound",
    component: () => import("@/views/error/NotFound.vue"),
    meta: {
      title: "Not Found",
      public: true,
    },
  },
];

const router = createRouter({
  history: createWebHistory(),
  routes,
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
