<script setup lang="ts">
import { Sun, Moon, Monitor } from 'lucide-vue-next'
import { useThemeStore } from '@/stores/theme'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'

const themeStore = useThemeStore()
</script>

<template>
  <div class="min-h-screen relative">
    <!-- Theme Switcher -->
    <div class="absolute top-4 right-4">
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button variant="ghost" size="icon" class="rounded-full w-8 h-8">
            <Sun v-if="themeStore.preference === 'light'" class="h-[1.2rem] w-[1.2rem]" />
            <Moon v-else-if="themeStore.preference === 'dark'" class="h-[1.2rem] w-[1.2rem]" />
            <Monitor v-else class="h-[1.2rem] w-[1.2rem]" />
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end">
          <DropdownMenuItem @click="themeStore.setTheme('light')" :disabled="themeStore.preference === 'light'">
            <Sun class="mr-2 h-4 w-4" />
            <span>Light</span>
          </DropdownMenuItem>
          <DropdownMenuItem @click="themeStore.setTheme('dark')" :disabled="themeStore.preference === 'dark'">
            <Moon class="mr-2 h-4 w-4" />
            <span>Dark</span>
          </DropdownMenuItem>
          <DropdownMenuItem @click="themeStore.setTheme('auto')" :disabled="themeStore.preference === 'auto'">
            <Monitor class="mr-2 h-4 w-4" />
            <span>System</span>
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
    </div>
    
    <!-- Main Content -->
    <slot />
  </div>
</template>
