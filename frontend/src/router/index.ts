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

  // Check if route requires auth
  if (to.matched.some((record) => record.meta.requiresAuth)) {
    // Check if user is authenticated
    if (!authStore.isAuthenticated) {
      // If not authenticated, redirect to login
      return next({
        path: "/",
        query: { redirect: to.fullPath },
      });
    }
  }

  // If route is login and user is already authenticated, redirect to dashboard
  if (to.name === "Login" && authStore.isAuthenticated) {
    return next({ name: "Dashboard" });
  }

  next();
});

export default router;
