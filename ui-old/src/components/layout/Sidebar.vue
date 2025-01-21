<script setup>
import { ref } from 'vue'
import { RouterLink, useRoute } from 'vue-router'
import { Database, Terminal, Bookmark, Settings, User, ChevronLeft, ChevronRight } from 'lucide-vue-next'
import { Button } from '@/components/ui/button'

const route = useRoute()
const isExpanded = ref(true)
const toggleSidebar = () => {
  isExpanded.value = !isExpanded.value
}

const navItems = [
  { icon: Database, label: 'Sources', route: '/sources' },
  { icon: Terminal, label: 'Query Explorer', route: '/query' },
  { icon: Bookmark, label: 'Saved Queries', route: '/saved-queries' },
  { icon: Settings, label: 'Settings', route: '/settings' }
]
</script>

<template>
  <aside
    class="fixed left-0 top-0 h-full bg-[#0F172A] border-r border-[#1E293B] shadow-lg transition-all duration-300 z-50"
    :class="{ 'w-64': isExpanded, 'w-20': !isExpanded }"
  >
    <div class="flex h-full flex-col">
      <!-- Logo -->
      <div class="flex h-16 items-center justify-between px-4 border-b border-[#1E293B]">
        <div class="flex items-center">
          <!-- <img src="/logo.svg" alt="LogChef" class="h-8 w-8" /> -->
          <span v-if="isExpanded" class="ml-3 text-xl font-semibold text-white">LogChef</span>
        </div>
      </div>

      <!-- Navigation -->
      <nav class="flex-1 px-2 py-4 space-y-1">
        <RouterLink
          v-for="item in navItems"
          :key="item.route"
          :to="item.route"
          class="flex items-center px-3 py-2 rounded-lg text-sm font-medium transition-colors"
          :class="[
            route.path === item.route
              ? 'bg-[#2563EB] text-white'
              : 'text-[#94A3B8] hover:bg-[#1E293B] hover:text-white'
          ]"
        >
          <component :is="item.icon"
            class="h-5 w-5"
            :class="[route.path === item.route ? 'text-white' : 'text-gray-400']"
          />
          <span v-if="isExpanded" class="ml-3">{{ item.label }}</span>
        </RouterLink>
      </nav>

      <!-- Profile -->
      <div class="border-t border-[#1E293B] p-4">
        <div class="flex items-center">
          <div class="flex-shrink-0">
            <div class="h-8 w-8 rounded-full bg-[#1E293B] flex items-center justify-center text-white">
              <User class="h-4 w-4" />
            </div>
          </div>
          <div v-if="isExpanded" class="ml-3">
            <p class="text-sm font-medium text-[#E2E8F0]">Admin User</p>
            <p class="text-xs text-[#94A3B8]">admin@logchef.io</p>
          </div>
        </div>
      </div>

      <!-- Toggle -->
      <button
        class="absolute -right-3 top-20 bg-[#1E293B] border border-[#334155] rounded-full p-1.5 shadow-lg hover:bg-[#334155] transition-colors"
        @click="toggleSidebar"
      >
        <component
          :is="isExpanded ? ChevronLeft : ChevronRight"
          class="h-4 w-4 text-gray-600"
        />
      </button>
    </div>
  </aside>
</template>
