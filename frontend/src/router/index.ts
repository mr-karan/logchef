import { createRouter, createWebHistory } from 'vue-router'
import type { RouteRecordRaw } from 'vue-router'

const routes: RouteRecordRaw[] = [
  {
    path: '/',
    redirect: '/explore'
  },
  {
    path: '/explore',
    name: 'Explore',
    component: () => import('@/views/explore/Explore.vue'),
    meta: {
      title: 'Explore'
    }
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
    path: '/sources',
    component: () => import('@/views/Sources.vue'),
    meta: {
      title: 'Sources'
    },
    children: [
      {
        path: '',
        name: 'Sources',
        redirect: { name: 'NewSource' }
      },
      {
        path: 'new',
        name: 'NewSource',
        component: () => import('@/views/sources/AddSource.vue')
      },
      {
        path: 'manage',
        name: 'ManageSources',
        component: () => import('@/views/sources/ManageSources.vue')
      }
    ]
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
    path: '/:pathMatch(.*)*',
    name: 'NotFound',
    component: () => import('@/views/NotFound.vue')
  }
]

const router = createRouter({
  // Apply the base URL from Vite's environment variables
  history: createWebHistory(import.meta.env.BASE_URL),
  routes
})

// Global navigation guard to update the document title dynamically
router.beforeEach((to, from, next) => {
  document.title = `${to.meta.title ? to.meta.title + ' - ' : ''}LogChef`
  next()
})

export default router
