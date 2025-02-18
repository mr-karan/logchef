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
  // Logs Section
  {
    path: "/logs",
    component: () => import("@/views/logs/LogsLayout.vue"),
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
        component: () => import("@/views/logs/LogExplorer.vue"),
        meta: { title: "Log Explorer" },
      },
      // {
      //   path: "history",
      //   name: "QueryHistory",
      //   component: () => import("@/views/logs/QueryHistory.vue"),
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
        name: "Users",
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

router.beforeEach((to, from, next) => {
  const authStore = useAuthStore();
  const isAuthenticated = authStore.isAuthenticated;
  const requiresAuth = to.matched.some((record) => record.meta.requiresAuth);
  const requiresAdmin = to.matched.some((record) => record.meta.requiresAdmin);
  const isPublic = to.matched.some((record) => record.meta.public);

  // Update document title
  document.title = `${to.meta.title ? to.meta.title + " - " : ""}LogChef`;

  if (requiresAuth && !isAuthenticated) {
    next("/login");
  } else if (requiresAdmin && !authStore.isAdmin) {
    next("/forbidden");
  } else if (to.path === "/login" && isAuthenticated) {
    next("/");
  } else {
    next();
  }
});

export default router;
