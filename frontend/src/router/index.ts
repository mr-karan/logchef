import { createRouter, createWebHistory } from 'vue-router'
import type { RouteRecordRaw } from 'vue-router'

const routes: RouteRecordRaw[] = [
  {
    path: '/',
    redirect: '/query-explorer'
  },
  {
    path: '/query-explorer',
    name: 'QueryExplorer',
    component: () => import('@/views/QueryExplorer.vue'),
    meta: {
      title: 'Query Explorer'
    }
  },
  {
    path: '/saved-queries',
    name: 'SavedQueries',
    component: () => import('@/views/SavedQueries.vue'),
    meta: {
      title: 'Saved Queries'
    }
  },
  {
    path: '/sources',
    name: 'Sources',
    component: () => import('@/views/Sources.vue'),
    meta: {
      title: 'Sources'
    },
    children: [
      {
        path: '',
        redirect: { name: 'NewSource' }
      },
      {
        path: 'new',
        name: 'NewSource',
        component: () => import('@/views/sources/NewSource.vue')
      },
      {
        path: 'manage',
        name: 'ManageSources',
        component: () => import('@/views/sources/ManageSources.vue')
      }
    ]
  },
  {
    path: '/settings',
    name: 'Settings',
    component: () => import('@/views/Settings.vue'),
    meta: {
      title: 'Settings'
    },
    children: [
      {
        path: '',
        redirect: { name: 'GeneralSettings' }
      },
      {
        path: 'general',
        name: 'GeneralSettings',
        component: () => import('@/views/settings/GeneralSettings.vue')
      },
      {
        path: 'team',
        name: 'TeamSettings',
        component: () => import('@/views/settings/TeamSettings.vue')
      },
      {
        path: 'api-keys',
        name: 'ApiKeys',
        component: () => import('@/views/settings/ApiKeys.vue')
      }
    ]
  },
  {
    path: '/:pathMatch(.*)*',
    name: 'NotFound',
    component: () => import('@/views/NotFound.vue')
  }
]

const router = createRouter({
  history: createWebHistory(),
  routes
})

// Navigation guards
router.beforeEach((to, from, next) => {
  // Update document title
  document.title = `${to.meta.title ? to.meta.title + ' - ' : ''}LogChef`
  next()
})

export default router
