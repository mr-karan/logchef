<script setup lang="ts">
import { Sun, Moon, Monitor } from 'lucide-vue-next'
import { useThemeStore, type ThemeMode } from '@/stores/theme'
import { usePreferencesStore } from '@/stores/preferences'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { computed } from 'vue'

const themeStore = useThemeStore()
const preferencesStore = usePreferencesStore()

const setThemePreference = (mode: ThemeMode) => {
  themeStore.setTheme(mode)
  preferencesStore.updatePreferences({ theme: mode }, { syncTheme: false })
}

const tooltipText = computed(() => {
  switch (themeStore.preference) {
    case 'light':
      return 'Theme: Light'
    case 'dark':
      return 'Theme: Dark'
    case 'auto':
      return 'Theme: System'
    default:
      return 'Theme'
  }
})
</script>

<template>
  <div class="min-h-screen relative">
    <!-- Theme Switcher -->
    <div class="absolute top-4 right-4">
      <TooltipProvider>
        <DropdownMenu>
          <Tooltip>
            <TooltipTrigger asChild>
              <DropdownMenuTrigger asChild>
                <Button variant="ghost" size="icon" class="rounded-full w-8 h-8">
                  <Sun v-if="themeStore.preference === 'light'" class="h-[1.2rem] w-[1.2rem]" />
                  <Moon v-else-if="themeStore.preference === 'dark'" class="h-[1.2rem] w-[1.2rem]" />
                  <Monitor v-else class="h-[1.2rem] w-[1.2rem]" />
                </Button>
              </DropdownMenuTrigger>
            </TooltipTrigger>
            <TooltipContent>
              <p>{{ tooltipText }}</p>
            </TooltipContent>
          </Tooltip>
          <DropdownMenuContent align="end">
            <DropdownMenuItem @click="setThemePreference('light')" :disabled="themeStore.preference === 'light'">
              <Sun class="mr-2 h-4 w-4" />
              <span>Light</span>
            </DropdownMenuItem>
            <DropdownMenuItem @click="setThemePreference('dark')" :disabled="themeStore.preference === 'dark'">
              <Moon class="mr-2 h-4 w-4" />
              <span>Dark</span>
            </DropdownMenuItem>
            <DropdownMenuItem @click="setThemePreference('auto')" :disabled="themeStore.preference === 'auto'">
              <Monitor class="mr-2 h-4 w-4" />
              <span>System</span>
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </TooltipProvider>
    </div>

    <!-- Main Content -->
    <slot />
  </div>
</template>
