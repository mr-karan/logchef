<script setup lang="ts">
import {
  Avatar,
  AvatarFallback,
} from '@/components/ui/avatar'

import { Button } from '@/components/ui/button'

import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'

import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarGroup,
  SidebarGroupContent,
  SidebarGroupLabel,
  SidebarHeader,
  SidebarInset,
  SidebarMenu,
  SidebarMenuItem,
  SidebarMenuButton,
  SidebarProvider,
  SidebarRail,
} from '@/components/ui/sidebar'

import {
  Settings,
  LogOut,
  Users,
  Search,
  Database,
  ClipboardList,
  UserCircle2,
  UsersRound,
  ChevronsUpDown,
  Sun,
  Moon,
  Monitor,
  PanelLeft,
  PanelRight,
} from 'lucide-vue-next'

import { useAuthStore } from '@/stores/auth'
import { useThemeStore } from '@/stores/theme'
import { ref, onMounted, watch } from 'vue'

const authStore = useAuthStore()
const themeStore = useThemeStore()

// Get initial sidebar state from cookie or default to true
const getSavedState = () => {
  if (typeof document !== 'undefined') {
    const savedState = document.cookie
      .split('; ')
      .find(row => row.startsWith('sidebar_state='))
      ?.split('=')[1]

    return savedState === 'false' ? false : true
  }
  return true
}

// Manage sidebar state locally with persistence
const sidebarOpen = ref(getSavedState())

// Save sidebar state to cookie when it changes
watch(sidebarOpen, (newValue) => {
  if (typeof document !== 'undefined') {
    document.cookie = `sidebar_state=${newValue}; path=/; max-age=31536000; SameSite=Lax`
  }
})

// Toggle sidebar function
const toggleSidebar = () => {
  sidebarOpen.value = !sidebarOpen.value
}

// Helper function to get user initials
function getUserInitials(name: string | undefined): string {
  if (!name) return '?'
  return name.split(' ')
    .map(part => part[0])
    .join('')
    .toUpperCase()
    .slice(0, 2)
}

// Group navigation items by category
const mainNavItems = [
  {
    title: 'Explorer',
    icon: Search,
    url: '/logs/explore',
  },
  {
    title: 'Saved Queries',
    icon: ClipboardList,
    url: '/logs/saved',
  },
]

const adminNavItems = [
  {
    title: 'Sources',
    icon: Database,
    url: '/management/sources/list',
    adminOnly: true,
  },
  {
    title: 'Users',
    icon: UsersRound,
    url: '/management/users',
    adminOnly: true,
  },
  {
    title: 'Teams',
    icon: Users,
    url: '/management/teams',
    adminOnly: true,
  },
]

const navItems = [
  {
    title: 'Profile',
    icon: UserCircle2,
    url: '/profile',
  },
  {
    title: 'Preferences',
    icon: Settings,
    url: '/settings/preferences',
  },
]
</script>

<template>
  <div class="h-screen w-screen flex overflow-hidden">
    <SidebarProvider v-model:open="sidebarOpen" :defaultOpen="sidebarOpen">
      <Sidebar collapsible="icon"
        class="flex-none z-50 bg-[hsl(var(--sidebar-background))] text-[hsl(var(--sidebar-foreground))] h-screen"
        :class="{ 'w-64': sidebarOpen, 'w-[72px]': !sidebarOpen }">
        <SidebarHeader class="pt-4 pb-2">
          <!-- Layout when expanded - horizontal -->
          <div v-if="sidebarOpen" class="flex items-center justify-between px-3">
            <div class="flex items-center">
              <div
                class="flex aspect-square size-10 items-center justify-center rounded-lg bg-sidebar-primary text-sidebar-primary-foreground font-semibold">
                LC
              </div>
              <div class="grid flex-1 text-left leading-tight ml-3">
                <span class="truncate text-lg font-semibold">LogChef</span>
              </div>
            </div>

            <Button variant="ghost" size="icon"
              class="text-sidebar-foreground hover:bg-sidebar-accent hover:text-sidebar-accent-foreground"
              @click="toggleSidebar" title="Collapse sidebar">
              <PanelLeft class="h-4 w-4" />
            </Button>
          </div>

          <!-- Layout when collapsed - vertical -->
          <div v-else class="flex flex-col items-center px-3 space-y-2">
            <div
              class="flex aspect-square size-10 items-center justify-center rounded-lg bg-sidebar-primary text-sidebar-primary-foreground font-semibold">
              LC
            </div>

            <Button variant="ghost" size="icon"
              class="text-sidebar-foreground hover:bg-sidebar-accent hover:text-sidebar-accent-foreground"
              @click="toggleSidebar" title="Expand sidebar">
              <PanelRight class="h-4 w-4" />
            </Button>
          </div>
        </SidebarHeader>

        <SidebarContent>
          <!-- Main Navigation -->
          <SidebarGroup>
            <SidebarGroupLabel v-if="sidebarOpen">Main</SidebarGroupLabel>
            <SidebarGroupContent>
              <SidebarMenu>
                <template v-for="item in mainNavItems" :key="item.title">
                  <SidebarMenuItem v-if="!item.adminOnly || (item.adminOnly && authStore.user?.role === 'admin')">
                    <SidebarMenuButton asChild :tooltip="item.title"
                      class="hover:bg-primary hover:text-primary-foreground py-2 data-[active=true]:bg-sidebar-accent data-[active=true]:text-sidebar-accent-foreground rounded-md transition-colors duration-150">
                      <router-link :to="item.url" class="flex items-center" active-class="font-medium">
                        <component :is="item.icon" class="size-5" :class="sidebarOpen ? 'mr-3 ml-1' : 'mx-auto'" />
                        <span v-if="sidebarOpen">{{ item.title }}</span>
                      </router-link>
                    </SidebarMenuButton>
                  </SidebarMenuItem>
                </template>
              </SidebarMenu>
            </SidebarGroupContent>
          </SidebarGroup>

          <!-- Admin Navigation -->
          <SidebarGroup v-if="authStore.user?.role === 'admin'" class="mt-4">
            <SidebarGroupLabel v-if="sidebarOpen">Administration</SidebarGroupLabel>
            <SidebarGroupContent>
              <SidebarMenu>
                <template v-for="item in adminNavItems" :key="item.title">
                  <SidebarMenuItem>
                    <SidebarMenuButton asChild :tooltip="item.title"
                      class="hover:bg-primary hover:text-primary-foreground py-2 data-[active=true]:bg-sidebar-accent data-[active=true]:text-sidebar-accent-foreground rounded-md transition-colors duration-150">
                      <router-link :to="item.url" class="flex items-center" active-class="font-medium">
                        <component :is="item.icon" class="size-5" :class="sidebarOpen ? 'mr-3 ml-1' : 'mx-auto'" />
                        <span v-if="sidebarOpen">{{ item.title }}</span>
                      </router-link>
                    </SidebarMenuButton>
                  </SidebarMenuItem>
                </template>
              </SidebarMenu>
            </SidebarGroupContent>
          </SidebarGroup>

          <!-- User Settings Navigation -->
          <SidebarGroup class="mt-4">
            <SidebarGroupLabel v-if="sidebarOpen">User</SidebarGroupLabel>
            <SidebarGroupContent>
              <SidebarMenu>
                <template v-for="item in navItems" :key="item.title">
                  <SidebarMenuItem>
                    <SidebarMenuButton asChild :tooltip="item.title"
                      class="hover:bg-primary hover:text-primary-foreground py-2 data-[active=true]:bg-sidebar-accent data-[active=true]:text-sidebar-accent-foreground rounded-md transition-colors duration-150">
                      <router-link :to="item.url" class="flex items-center" active-class="font-medium">
                        <component :is="item.icon" class="size-5" :class="sidebarOpen ? 'mr-3 ml-1' : 'mx-auto'" />
                        <span v-if="sidebarOpen">{{ item.title }}</span>
                      </router-link>
                    </SidebarMenuButton>
                  </SidebarMenuItem>
                </template>
              </SidebarMenu>
            </SidebarGroupContent>
          </SidebarGroup>
        </SidebarContent>

        <SidebarFooter class="border-t border-sidebar-border pt-2">
          <SidebarMenu>
            <SidebarMenuItem>
              <DropdownMenu>
                <DropdownMenuTrigger asChild>
                  <SidebarMenuButton size="lg"
                    class="data-[state=open]:bg-sidebar-accent data-[state=open]:text-sidebar-accent-foreground hover:bg-primary hover:text-primary-foreground">
                    <Avatar class="h-9 w-9 rounded-full border-2 border-sidebar-primary">
                      <AvatarFallback class="rounded-full bg-sidebar-primary text-sidebar-primary-foreground">
                        {{ getUserInitials(authStore.user?.full_name) }}
                      </AvatarFallback>
                    </Avatar>
                    <div v-if="sidebarOpen" class="grid flex-1 text-left text-sm leading-tight">
                      <span class="truncate font-semibold">{{ authStore.user?.full_name }}</span>
                      <span class="truncate text-xs opacity-70">{{ authStore.user?.email }}</span>
                    </div>
                    <ChevronsUpDown v-if="sidebarOpen" class="ml-auto size-4" />
                  </SidebarMenuButton>
                </DropdownMenuTrigger>
                <DropdownMenuContent class="w-[--radix-dropdown-menu-trigger-width] min-w-56 rounded-lg" side="top"
                  align="end" :side-offset="8">
                  <DropdownMenuLabel class="p-0 font-normal">
                    <div class="flex items-center gap-2 px-1 py-1.5 text-left text-sm">
                      <Avatar class="h-8 w-8 rounded-lg">
                        <AvatarFallback class="rounded-lg">
                          {{ getUserInitials(authStore.user?.full_name) }}
                        </AvatarFallback>
                      </Avatar>
                      <div class="grid flex-1 text-left text-sm leading-tight">
                        <span class="truncate font-semibold">{{ authStore.user?.full_name }}</span>
                        <span class="truncate text-xs">{{ authStore.user?.email }}</span>
                      </div>
                    </div>
                  </DropdownMenuLabel>
                  <DropdownMenuSeparator />
                  <DropdownMenuLabel>Theme</DropdownMenuLabel>
                  <div class="px-2 py-1.5">
                    <div class="flex items-center justify-between space-x-2">
                      <Button variant="outline" size="icon" class="w-9 px-0 flex-1 rounded-md"
                        :class="{ 'bg-primary text-primary-foreground': themeStore.preference === 'light' }"
                        @click="themeStore.setTheme('light')">
                        <Sun class="h-5 w-5" />
                      </Button>
                      <Button variant="outline" size="icon" class="w-9 px-0 flex-1 rounded-md"
                        :class="{ 'bg-primary text-primary-foreground': themeStore.preference === 'dark' }"
                        @click="themeStore.setTheme('dark')">
                        <Moon class="h-5 w-5" />
                      </Button>
                      <Button variant="outline" size="icon" class="w-9 px-0 flex-1 rounded-md"
                        :class="{ 'bg-primary text-primary-foreground': themeStore.preference === 'auto' }"
                        @click="themeStore.setTheme('auto')">
                        <Monitor class="h-5 w-5" />
                      </Button>
                    </div>
                  </div>
                  <DropdownMenuSeparator />
                  <DropdownMenuLabel>Account</DropdownMenuLabel>
                  <DropdownMenuItem asChild>
                    <router-link to="/profile" class="cursor-pointer">
                      <UserCircle2 class="mr-2 h-4 w-4" />
                      <span>Profile</span>
                    </router-link>
                  </DropdownMenuItem>
                  <DropdownMenuItem class="text-destructive focus:text-destructive cursor-pointer"
                    @click="authStore.logout">
                    <LogOut class="mr-2 h-4 w-4" />
                    <span>Log out</span>
                  </DropdownMenuItem>
                </DropdownMenuContent>
              </DropdownMenu>
            </SidebarMenuItem>
          </SidebarMenu>
        </SidebarFooter>
        <SidebarRail />
      </Sidebar>

      <SidebarInset class="flex flex-col flex-1 min-w-0 overflow-hidden h-screen">
        <main class="flex-1 min-w-0 h-full flex flex-col">
          <div class="flex-1 px-3 py-3 min-w-0 overflow-y-auto">
            <router-view />
          </div>
        </main>
      </SidebarInset>
    </SidebarProvider>
  </div>
</template>
