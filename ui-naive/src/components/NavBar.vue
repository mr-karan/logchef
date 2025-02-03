<template>
    <n-layout-sider bordered collapse-mode="width" :collapsed-width="64" :width="240" show-trigger :inverted="inverted"
        class="sidebar">
        <div class="logo">
            <n-h3>LogAnalytics</n-h3>
        </div>
        <n-menu :inverted="inverted" :collapsed-width="64" :collapsed-icon-size="22" :options="menuOptions"
            :value="activeKey" @update:value="handleMenuUpdate" />
    </n-layout-sider>
</template>

<script setup lang="ts">
import { h, ref } from 'vue'
import type { Component } from 'vue'
import type { MenuOption } from 'naive-ui'
import { NIcon } from 'naive-ui'
import { useRouter, useRoute } from 'vue-router'
import {
    SearchOutline as ExploreIcon,
    ServerOutline as SourcesIcon,
    NotificationsOutline as AlertsIcon
} from '@vicons/ionicons5'

const router = useRouter()
const route = useRoute()
const activeKey = ref(route.name)
const inverted = ref(false)

function renderIcon(icon: Component) {
    return () => h(NIcon, null, { default: () => h(icon) })
}

const menuOptions: MenuOption[] = [
    {
        label: 'Explore',
        key: 'explore',
        icon: renderIcon(ExploreIcon)
    },
    {
        label: 'Sources',
        key: 'sources',
        icon: renderIcon(SourcesIcon)
    },
    {
        label: 'Alerts',
        key: 'alerts',
        icon: renderIcon(AlertsIcon)
    }
]

const handleMenuUpdate = (key: string) => {
    router.push({ name: key })
}
</script>

<style scoped>
.sidebar {
    background: #fff;
    box-shadow: 0 1px 2px rgba(0, 0, 0, 0.1);
}

.logo {
    padding: 16px 24px;
    border-bottom: 1px solid #eee;
}
</style>