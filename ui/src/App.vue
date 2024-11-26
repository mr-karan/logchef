<script setup lang="ts">
import { RouterView } from 'vue-router'
import { useRoute } from 'vue-router'

const route = useRoute()
const navItems = [
  {
    label: 'Sources',
    icon: 'pi pi-database',
    route: '/sources'
  },
  {
    label: 'Query Explorer',
    icon: 'pi pi-code',
    route: '/query'
  },
  {
    label: 'Saved Queries',
    icon: 'pi pi-bookmark',
    route: '/saved-queries'
  },
  {
    label: 'Settings',
    icon: 'pi pi-cog',
    route: '/settings'
  }
]
</script>

<template>
  <div class="min-h-screen bg-gray-50 flex">
    <!-- Fixed Sidebar -->
    <aside class="w-64 fixed left-0 top-0 h-full bg-white border-r border-gray-100">
      <div class="h-14 px-6 flex items-center border-b border-gray-100">
        <span class="text-xl font-semibold text-gray-900">LogChef</span>
      </div>
      
      <!-- Navigation -->
      <nav class="py-4">
        <router-link
          v-for="item in navItems"
          :key="item.route"
          :to="item.route"
          class="flex items-center px-6 py-3 text-gray-600 hover:text-gray-900 hover:bg-gray-50 transition-colors"
          :class="{ 'text-blue-600 bg-blue-50': route.path === item.route }"
        >
          <i :class="item.icon" class="mr-3 text-lg"></i>
          <span class="text-sm font-medium">{{ item.label }}</span>
        </router-link>
      </nav>

      <!-- User Profile -->
      <div class="absolute bottom-0 w-full p-4 border-t border-gray-100">
        <div class="flex items-center">
          <div class="w-8 h-8 rounded-full bg-gray-200 flex items-center justify-center">
            <i class="pi pi-user text-gray-600"></i>
          </div>
          <div class="ml-3">
            <p class="text-sm font-medium text-gray-900">Admin User</p>
            <p class="text-xs text-gray-500">admin@logchef.io</p>
          </div>
        </div>
      </div>
    </aside>

    <!-- Main Content -->
    <div class="ml-64 flex-1">
      <div class="h-14 px-6 bg-white border-b border-gray-100 flex items-center justify-end">
        <button class="p-2 hover:bg-gray-50 rounded-lg">
          <i class="pi pi-user text-gray-600"></i>
        </button>
      </div>
      <div class="p-6">
        <RouterView />
      </div>
    </div>
  </div>
</template>

<style>
.router-link-active {
  font-weight: 600;
}
</style>
