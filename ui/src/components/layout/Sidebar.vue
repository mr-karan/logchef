<script setup>
import { ref } from 'vue'
import { RouterLink } from 'vue-router'
import { Database, Terminal, Bookmark, Settings, User } from 'lucide-vue-next'
import { Button } from '@/components/ui/button'

const isExpanded = ref(false)
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
    class="fixed left-0 top-0 h-full bg-slate-900 text-white transition-all duration-300"
    :class="{ 'w-64': isExpanded, 'w-16': !isExpanded }"
  >
    <div class="flex h-full flex-col">
      <!-- Logo -->
      <div class="flex h-16 items-center justify-center">
        <span v-if="isExpanded" class="text-xl font-bold">LogChef</span>
        <span v-else class="text-xl font-bold">LC</span>
      </div>

      <!-- Navigation -->
      <nav class="flex-1">
        <RouterLink
          v-for="item in navItems"
          :key="item.route"
          :to="item.route"
          class="flex h-12 items-center px-4 text-sm text-gray-300 hover:bg-slate-800 hover:text-white"
        >
          <component :is="item.icon" class="h-5 w-5" />
          <span v-if="isExpanded" class="ml-3">{{ item.label }}</span>
        </RouterLink>
      </nav>

      <!-- Profile -->
      <div class="mb-4 px-4">
        <Button
          variant="ghost"
          class="flex h-12 w-full items-center text-gray-300 hover:bg-slate-800 hover:text-white"
        >
          <User class="h-5 w-5" />
          <span v-if="isExpanded" class="ml-3">Profile</span>
        </Button>
      </div>

      <!-- Toggle -->
      <div class="mb-4 px-4">
        <Button
          variant="ghost"
          class="flex h-12 w-full items-center text-gray-300 hover:bg-slate-800 hover:text-white"
          @click="toggleSidebar"
        >
          <span v-if="isExpanded">Collapse</span>
          <span v-else>•••</span>
        </Button>
      </div>
    </div>
  </aside>
</template>
