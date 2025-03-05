<script setup lang="ts">
import {
    Avatar,
    AvatarFallback,
} from '@/components/ui/avatar'

import { Button } from '@/components/ui/button'

import {
    DropdownMenu,
    DropdownMenuContent,
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
} from 'lucide-vue-next'

import { useRoute } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import { computed, ref } from 'vue'
import type { FunctionalComponent } from 'vue'
import type { LucideProps } from 'lucide-vue-next'
import type { RouteLocationRaw } from 'vue-router'
import {
    Breadcrumb,
    BreadcrumbItem,
    BreadcrumbLink,
    BreadcrumbList,
    BreadcrumbPage,
    BreadcrumbSeparator,
} from '@/components/ui/breadcrumb'

// Use the theme store instead of initializing directly
import { useThemeStore } from '@/stores/theme'
const themeStore = useThemeStore()

const route = useRoute()
const authStore = useAuthStore()
const sidebarOpen = ref(true)

// Helper function to get user initials
function getUserInitials(name: string | undefined): string {
    if (!name) return '?'
    return name.split(' ')
        .map(part => part[0])
        .join('')
        .toUpperCase()
        .slice(0, 2)
}

const navItems = [
    {
        title: 'Explorer',
        icon: Search,
        url: '/logs/explore',
    },
    {
        title: 'History',
        icon: ClipboardList,
        url: '/logs/history',
    },
    {
        title: 'Sources',
        icon: Database,
        url: '/sources/list',
    },
    {
        title: 'Users',
        icon: UsersRound,
        url: '/access/users',
        adminOnly: true,
    },
    {
        title: 'Teams',
        icon: Users,
        url: '/access/teams',
        adminOnly: true,
    },
    {
        title: 'Settings',
        icon: Settings,
        url: '/settings/profile',
    },
]

interface BreadcrumbItem {
    label: string;
    to?: RouteLocationRaw;
    icon?: FunctionalComponent<LucideProps>;
}

// Compute breadcrumb items based on current route
const breadcrumbs = computed<BreadcrumbItem[]>(() => {
    const paths = route.path.split('/').filter(Boolean)
    const items: BreadcrumbItem[] = [
        {
            label: 'Home',
            to: '/logs/explore'
        }
    ]

    let currentPath = ''
    paths.forEach(path => {
        currentPath += `/${path}`
        const matchedRoute = route.matched.find(r => r.path === currentPath)
        if (matchedRoute?.meta?.title) {
            items.push({
                label: matchedRoute.meta.title as string,
                to: currentPath
            })
        }
    })

    return items
})
</script>

<template>
    <div class="h-screen w-screen flex overflow-hidden">
        <SidebarProvider v-model:open="sidebarOpen">
            <Sidebar collapsible="icon"
                class="flex-none w-64 z-50 bg-[hsl(var(--sidebar-background))] text-[hsl(var(--sidebar-foreground))]">
                <SidebarHeader>
                    <SidebarMenu>
                        <SidebarMenuItem>
                            <SidebarMenuButton size="lg">
                                <div
                                    class="flex aspect-square size-8 items-center justify-center rounded-lg bg-sidebar-primary text-sidebar-primary-foreground">
                                    LC
                                </div>
                                <div class="grid flex-1 text-left text-sm leading-tight">
                                    <span class="truncate font-semibold">LogChef</span>
                                    <span class="truncate text-xs">Log Analytics</span>
                                </div>
                            </SidebarMenuButton>
                        </SidebarMenuItem>
                    </SidebarMenu>
                </SidebarHeader>

                <SidebarContent>
                    <SidebarGroup>
                        <SidebarGroupContent>
                            <SidebarMenu>
                                <template v-for="item in navItems" :key="item.title">
                                    <SidebarMenuItem
                                        v-if="!item.adminOnly || (item.adminOnly && authStore.user?.role === 'admin')">
                                        <router-link :to="item.url">
                                            <SidebarMenuButton :tooltip="item.title">
                                                <component :is="item.icon" />
                                                <span>{{ item.title }}</span>
                                            </SidebarMenuButton>
                                        </router-link>
                                    </SidebarMenuItem>
                                </template>
                            </SidebarMenu>
                        </SidebarGroupContent>
                    </SidebarGroup>
                </SidebarContent>

                <SidebarFooter>
                    <SidebarMenu>
                        <SidebarMenuItem>
                            <DropdownMenu>
                                <DropdownMenuTrigger as-child>
                                    <SidebarMenuButton size="lg"
                                        class="data-[state=open]:bg-sidebar-accent data-[state=open]:text-sidebar-accent-foreground">
                                        <Avatar class="h-8 w-8 rounded-lg">
                                            <AvatarFallback class="rounded-lg">
                                                {{ getUserInitials(authStore.user?.full_name) }}
                                            </AvatarFallback>
                                        </Avatar>
                                        <div class="grid flex-1 text-left text-sm leading-tight">
                                            <span class="truncate font-semibold">{{ authStore.user?.full_name }}</span>
                                            <span class="truncate text-xs">{{ authStore.user?.email }}</span>
                                        </div>
                                        <ChevronsUpDown class="ml-auto size-4" />
                                    </SidebarMenuButton>
                                </DropdownMenuTrigger>
                                <DropdownMenuContent class="w-[--radix-dropdown-menu-trigger-width] min-w-56 rounded-lg"
                                    side="bottom" align="end" :side-offset="4">
                                    <DropdownMenuLabel class="p-0 font-normal">
                                        <div class="flex items-center gap-2 px-1 py-1.5 text-left text-sm">
                                            <Avatar class="h-8 w-8 rounded-lg">
                                                <AvatarFallback class="rounded-lg">
                                                    {{ getUserInitials(authStore.user?.full_name) }}
                                                </AvatarFallback>
                                            </Avatar>
                                            <div class="grid flex-1 text-left text-sm leading-tight">
                                                <span class="truncate font-semibold">{{ authStore.user?.full_name
                                                    }}</span>
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
                                    <div class="px-2 py-1.5">
                                        <Button variant="outline" class="w-full justify-start mb-1.5" as-child>
                                            <router-link to="/settings/profile" class="flex items-center">
                                                <UserCircle2 class="mr-2 h-4 w-4" />
                                                Profile Settings
                                            </router-link>
                                        </Button>

                                        <Button variant="destructive" class="w-full justify-start"
                                            @click="authStore.logout">
                                            <LogOut class="mr-2 h-4 w-4" />
                                            Log out
                                        </Button>
                                    </div>
                                </DropdownMenuContent>
                            </DropdownMenu>
                        </SidebarMenuItem>
                    </SidebarMenu>
                </SidebarFooter>
                <SidebarRail />
            </Sidebar>

            <SidebarInset class="flex flex-col flex-1 min-w-0">
                <main class="flex-1 overflow-y-auto overflow-x-auto min-w-0">
                    <div class="h-full px-3 py-3 min-w-0">
                        <router-view />
                    </div>
                </main>
            </SidebarInset>
        </SidebarProvider>
    </div>
</template>
