<template>
  <n-config-provider :theme-overrides="themeOverrides">
    <n-dialog-provider>
    <n-notification-provider>
      <n-global-style />
    <n-layout has-sider class="layout-container">
      <!-- Sidebar -->
      <n-layout-sider bordered collapse-mode="width" :collapsed-width="64" :width="240" class="sidebar">
        <div class="logo">
          <n-h3>LogAnalytics</n-h3>
        </div>
        <n-menu :options="menuOptions" :value="activeKey" @update:value="handleMenuUpdate" />
      </n-layout-sider>

      <!-- Main Content -->
      <n-layout>
        <n-layout-header bordered class="header">
          <n-space align="center">
            <n-h4>Log Analytics Dashboard</n-h4>
          </n-space>
        </n-layout-header>
        <n-layout-content class="content">
          <router-view v-slot="{ Component }">
            <transition name="fade" mode="out-in">
              <component :is="Component" />
            </transition>
          </router-view>
        </n-layout-content>
      </n-layout>
    </n-layout>
    </n-notification-provider>
    </n-dialog-provider>
  </n-config-provider>
</template>

<script setup>
import { h, ref } from 'vue'
import { NIcon, NGlobalStyle, NTag } from 'naive-ui'
import { useRouter, useRoute } from 'vue-router'
import {
  DesktopOutline as DashboardIcon,
  SearchOutline as ExploreIcon,
  ServerOutline as SourcesIcon,
  NotificationsOutline as AlertsIcon
} from '@vicons/ionicons5'

const router = useRouter()
const route = useRoute()
const activeKey = ref(route.name)

const handleMenuUpdate = (key) => {
  router.push({ name: key })
}

const themeOverrides = {
  common: {
    fontFamily: 'Inter, sans-serif',
    fontWeightStrong: '600'
  }
}

const renderIcon = (icon) => {
  return () => h(NIcon, null, { default: () => h(icon) })
}

const menuOptions = [
  {
    label: 'Dashboard',
    key: 'home',
    icon: renderIcon(DashboardIcon)
  },
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

const columns = [
  {
    title: 'Timestamp',
    key: 'timestamp',
    width: 200
  },
  {
    title: 'Level',
    key: 'level',
    width: 100,
    render(row) {
      const type = {
        INFO: 'info',
        ERROR: 'error',
        WARNING: 'warning'
      }[row.level]
      return h(
        'div',
        {
          style: {
            display: 'flex',
            alignItems: 'center'
          }
        },
        [
          h(
            NTag,
            {
              type,
              round: true,
              size: 'small'
            },
            { default: () => row.level }
          )
        ]
      )
    }
  },
  {
    title: 'Message',
    key: 'message',
    ellipsis: true
  }
]

const logs = [
  { timestamp: '2023-10-01 12:00', level: 'INFO', message: 'Server started successfully' },
  { timestamp: '2023-10-01 12:05', level: 'ERROR', message: 'Failed to connect to database' },
  { timestamp: '2023-10-01 12:07', level: 'WARNING', message: 'High memory usage detected' },
]

const pagination = {
  pageSize: 10
}
</script>

<style scoped>
.layout-container {
  height: 100vh;
}

.sidebar {
  background: #fff;
  box-shadow: 0 1px 2px rgba(0, 0, 0, 0.1);
}

.logo {
  padding: 16px 24px;
  border-bottom: 1px solid #eee;
}

.header {
  padding: 16px 24px;
  background: #fff;
}

.content {
  padding: 24px;
  background: #f5f5f5;
}

.fade-enter-active,
.fade-leave-active {
  transition: opacity 0.3s ease;
}

.fade-enter-from,
.fade-leave-to {
  opacity: 0;
}
</style>
