import { createRouter, createWebHistory } from 'vue-router'

const router = createRouter({
  history: createWebHistory(),
  routes: [
    {
      path: '/',
      component: () => import('../views/Dashboard.vue')
    },
    {
      path: '/logs',
      component: () => import('../views/LiveLogs.vue')
    },
    {
      path: '/sources',
      component: () => import('../views/Sources.vue')
    },
    {
      path: '/alerts',
      component: () => import('../views/Alerts.vue')
    },
    {
      path: '/settings',
      component: () => import('../views/Settings.vue')
    }
  ]
})

export default router
