import { createRouter, createWebHistory, RouteRecordRaw } from 'vue-router'

const routes: RouteRecordRaw[] = [
    {
        path: '/',
        redirect: '/sources'
    },
    {
        path: '/sources',
        name: 'sources',
        component: () => import('../views/SourcesView.vue')
    },
    {
        path: '/query',
        name: 'query',
        component: () => import('../views/QueryView.vue'),
        props: (route) => ({
            sourceId: route.query.source,
            initialStartTime: route.query.start_time,
            initialEndTime: route.query.end_time,
            initialSearchQuery: route.query.q,
            initialSeverity: route.query.severity
        })
    },
    {
        path: '/saved-queries',
        name: 'saved-queries',
        component: () => import('../views/SavedQueriesView.vue')
    },
    {
        path: '/settings',
        name: 'settings',
        component: () => import('../views/SettingsView.vue')
    }
]

const router = createRouter({
    history: createWebHistory(import.meta.env.BASE_URL),
    routes
})

export default router