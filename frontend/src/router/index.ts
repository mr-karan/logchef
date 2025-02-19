import {
  createRouter,
  createWebHistory,
  type RouteRecordRaw,
} from "vue-router";
import { useAuthStore } from "@/stores/auth";
import { authApi } from "@/api/auth";

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
      // {
      //   path: "history",
      //   name: "QueryHistory",
      //   component: () => import("@/views/explore/QueryHistory.vue"),
      //   meta: { title: "Query History" },
      // },
    ],
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
        path: "teams/new",
        name: "NewTeam",
        component: () => import("@/views/access/teams/AddTeam.vue"),
        meta: { title: "New Team" },
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

router.beforeEach(async (to, from, next) => {
  const authStore = useAuthStore();
  const isAuthenticated = authStore.isAuthenticated;
  const isAdmin = authStore.user?.role === "admin";
  const requiresAuth = to.matched.some((record) => record.meta.requiresAuth);
  const requiresAdmin = to.matched.some((record) => record.meta.requiresAdmin);
  const isPublic = to.matched.some((record) => record.meta.public);
  const skipAuthCheck = to.matched.some((record) => record.meta.skipAuthCheck);

  // Update document title
  document.title = `${to.meta.title ? to.meta.title + " - " : ""}LogChef`;

  // Skip auth check for error pages
  if (skipAuthCheck) {
    return next();
  }

  // Handle authentication
  if (!isAuthenticated && !isPublic) {
    // Save the intended path for redirect after login
    return next({
      name: "Login",
      query: { redirect: to.fullPath },
    });
  }

  // Prevent authenticated users from accessing login page
  if (isAuthenticated && to.name === "Login") {
    return next({ path: "/" });
  }

  // Check admin access
  if (requiresAdmin && !isAdmin) {
    return next({ name: "Forbidden" });
  }

  // Proceed with navigation
  return next();
});

export default router;
