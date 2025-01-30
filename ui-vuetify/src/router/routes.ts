import type { RouteRecordRaw } from 'vue-router'

const routes: RouteRecordRaw[] = [
  {
    path: '/',
    redirect: '/explore'
  },
  {
    path: '/explore',
    name: 'Explore',
    component: () => import('@/pages/explore.vue')
  },
  {
    path: '/sources',
    name: 'Sources',
    component: () => import('@/pages/sources.vue')
  },
  {
    path: '/alerts',
    name: 'Alerts',
    component: () => import('@/pages/alerts.vue')
  }
]

export default routes
