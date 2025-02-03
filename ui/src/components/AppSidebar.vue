<script setup lang="ts">
import Drawer from 'primevue/drawer'
import { ref } from 'vue'
import { useRouter, useRoute } from 'vue-router'

const router = useRouter()
const route = useRoute()
const visible = ref(true)

const menuItems = [
  { label: 'Dashboard', icon: 'pi pi-chart-line', route: '/' },
  { label: 'Live Logs', icon: 'pi pi-list', route: '/logs' },
  { label: 'Sources', icon: 'pi pi-database', route: '/sources' },
  { label: 'Alerts', icon: 'pi pi-bell', route: '/alerts' },
  { label: 'Settings', icon: 'pi pi-cog', route: '/settings' }
]
</script>

<template>
  <Drawer v-model:visible="visible" class="w-[250px]" position="left" :modal="false" :showCloseIcon="false">
    <template #header>
      <div class="flex items-center justify-between">
        <span class="text-xl font-semibold">LogChef</span>
      </div>
    </template>
    <div class="flex flex-col gap-2">
      <Button v-for="item in menuItems" 
              :key="item.label"
              :class="{ 'bg-primary/10': route.path === item.route }"
              :icon="item.icon"
              :label="item.label"
              text
              @click="() => router.push(item.route)" />
    </div>
  </Drawer>
</template>
