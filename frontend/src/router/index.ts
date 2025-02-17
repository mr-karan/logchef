import { createRouter, createWebHistory } from "vue-router";
import type { RouteRecordRaw } from "vue-router";
import { useAuthStore } from "@/stores/auth";

const routes: RouteRecordRaw[] = [
  {
    path: "/",
    name: "Login",
    component: () => import("@/views/auth/Login.vue"),
    meta: {
      title: "Login",
      public: true,
    },
  },
  {
    path: "/logout",
    name: "Logout",
    component: () => import("@/views/auth/Logout.vue"),
    meta: {
      title: "Logging out...",
      public: true,
    },
  },
  {
    path: "/dashboard",
    name: "Dashboard",
    component: () => import("@/views/Dashboard.vue"),
    meta: {
      title: "Dashboard",
      requiresAuth: true,
    },
  },
  {
    path: "/explore",
    name: "Explore",
    component: () => import("@/views/explore/Explore.vue"),
    meta: {
      title: "Explore",
      requiresAuth: true,
    },
  },
  // {
  //   path: '/query-explorer',
  //   component: () => import('@/views/QueryExplorer.vue'),
  //   meta: {
  //     title: 'Query Explorer'
  //   },
  //   children: [
  //     {
  //       path: '',
  //       name: 'QueryExplorer',
  //       redirect: { name: 'NewQuery' }
  //     },
  //     {
  //       path: 'new',
  //       name: 'NewQuery',
  //       component: () => import('@/views/query-explorer/NewQuery.vue')
  //     }
  //   ]
  // },
  // {
  //   path: '/saved-queries',
  //   name: 'SavedQueries',
  //   component: () => import('@/views/SavedQueries.vue'),
  //   meta: {
  //     title: 'Saved Queries'
  //   }
  // },
  {
    path: "/sources",
    component: () => import("@/views/Sources.vue"),
    meta: {
      title: "Sources",
      requiresAuth: true,
    },
    children: [
      {
        path: "",
        name: "Sources",
        redirect: { name: "NewSource" },
      },
      {
        path: "new",
        name: "NewSource",
        component: () => import("@/views/sources/AddSource.vue"),
      },
      {
        path: "manage",
        name: "ManageSources",
        component: () => import("@/views/sources/ManageSources.vue"),
      },
    ],
  },
  {
    path: "/users",
    component: () => import("@/views/Users.vue"),
    meta: {
      title: "Users",
      requiresAuth: true,
      requiresAdmin: true,
    },
    children: [
      {
        path: "",
        name: "Users",
        redirect: { name: "ManageUsers" },
      },
      {
        path: "new",
        name: "NewUser",
        component: () => import("@/views/users/AddUser.vue"),
      },
      {
        path: "manage",
        name: "ManageUsers",
        component: () => import("@/views/users/ManageUsers.vue"),
      },
    ],
  },
  // {
  //   path: '/settings',
  //   component: () => import('@/views/Settings.vue'),
  //   meta: {
  //     title: 'Settings'
  //   },
  //   children: [
  //     {
  //       path: '',
  //       name: 'Settings',
  //       redirect: { name: 'GeneralSettings' }
  //     },
  //     {
  //       path: 'general',
  //       name: 'GeneralSettings',
  //       component: () => import('@/views/settings/GeneralSettings.vue')
  //     },
  //     {
  //       path: 'team',
  //       name: 'TeamSettings',
  //       component: () => import('@/views/settings/TeamSettings.vue')
  //     },
  //     {
  //       path: 'api-keys',
  //       name: 'ApiKeys',
  //       component: () => import('@/views/settings/ApiKeys.vue')
  //     }
  //   ]
  // },
  {
    path: "/forbidden",
    name: "Forbidden",
    component: () => import("@/views/Forbidden.vue"),
    meta: {
      title: "Access Denied",
      public: true,
    },
  },
  {
    path: "/:pathMatch(.*)*",
    name: "NotFound",
    component: () => import("@/views/NotFound.vue"),
    meta: {
      title: "Not Found",
      public: true,
    },
  },
];

const router = createRouter({
  // Apply the base URL from Vite's environment variables
  history: createWebHistory(import.meta.env.BASE_URL),
  routes,
});

// Global navigation guard to handle auth and update document title
router.beforeEach(async (to, _from, next) => {
  const authStore = useAuthStore();

  // Update document title
  document.title = `${to.meta.title ? to.meta.title + " - " : ""}LogChef`;

  // Wait for auth to be initialized
  if (!authStore.isInitialized) {
    await authStore.initialize();
  }

  // If route is public, allow access
  if (to.meta.public) {
    // If route is login and user is already authenticated, redirect to explore
    if (to.name === "Login" && authStore.isAuthenticated) {
      // If there's a redirect query param, use that, otherwise go to explore
      const redirectPath = to.query.redirect as string;
      return next(redirectPath || { name: "Explore" });
    }
    return next();
  }

  // Check if route requires auth
  if (to.matched.some((record) => record.meta.requiresAuth)) {
    // Check if user is authenticated
    if (!authStore.isAuthenticated) {
      // If not authenticated, redirect to login with the current path as redirect
      return next({
        path: "/",
        query: { redirect: to.fullPath },
      });
    }

    // Check if route requires admin role
    if (to.matched.some((record) => record.meta.requiresAdmin)) {
      if (authStore.user?.role !== "admin") {
        // If not admin, redirect to forbidden page
        return next({ name: "Forbidden" });
      }
    }
  }

  next();
});

export default router;
