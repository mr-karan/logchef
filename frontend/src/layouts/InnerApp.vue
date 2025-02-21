<script setup lang="ts">
import {
    Avatar,
    AvatarFallback,
} from '@/components/ui/avatar'

import {
    Breadcrumb,
    BreadcrumbItem,
    BreadcrumbList,
} from '@/components/ui/breadcrumb'

import {
    Collapsible,
    CollapsibleContent,
    CollapsibleTrigger,
} from '@/components/ui/collapsible'

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
    SidebarGroupLabel,
    SidebarHeader,
    SidebarInset,
    SidebarMenu,
    SidebarMenuItem,
    SidebarMenuButton,
    SidebarMenuSub,
    SidebarMenuSubButton,
    SidebarMenuSubItem,
    SidebarProvider,
    SidebarRail,
    SidebarTrigger,
} from '@/components/ui/sidebar'

import {
    Settings2,
    Database,
    ChevronRight,
    ChevronsUpDown,
    LogOut,
    Users,
    ScrollText,
} from 'lucide-vue-next'

import { useRoute } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import { computed } from 'vue'
import type { FunctionalComponent } from 'vue'
import type { LucideProps } from 'lucide-vue-next'
import type { RouteLocationRaw } from 'vue-router'

const route = useRoute()
const authStore = useAuthStore()

// Helper function to get user initials
function getUserInitials(name: string | undefined): string {
    if (!name) return '?'
    return name.split(' ')
        .map(part => part[0])
        .join('')
        .toUpperCase()
        .slice(0, 2)
}

const navMain = [
    {
        title: 'Logs',
        icon: ScrollText,
        items: [
            {
                title: 'Explorer',
                url: '/logs/explore',
            },
            {
                title: 'History',
                url: '/logs/history',
            },
        ],
    },
    {
        title: 'Sources',
        icon: Database,
        items: [
            {
                title: 'All Sources',
                url: '/sources/list',
            },
        ],
    },
    {
        title: 'Access',
        icon: Users,
        adminOnly: true,
        items: [
            {
                title: 'Users',
                url: '/access/users',
            },
            {
                title: 'Teams',
                url: '/access/teams',
            },
        ],
    },
    {
        title: 'Profile',
        icon: Settings2,
        items: [
            {
                title: 'Settings',
                url: '/settings/profile',
            },
        ],
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
        <SidebarProvider>
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
                        <SidebarGroupLabel>Navigation</SidebarGroupLabel>
                        <SidebarMenu>
                            <template v-for="item in navMain" :key="item.title">
                                <Collapsible
                                    v-if="!item.adminOnly || (item.adminOnly && authStore.user?.role === 'admin')"
                                    as-child class="group/collapsible">
                                    <SidebarMenuItem>
                                        <CollapsibleTrigger as-child>
                                            <SidebarMenuButton :tooltip="item.title">
                                                <component :is="item.icon" />
                                                <span>{{ item.title }}</span>
                                                <ChevronRight
                                                    class="ml-auto transition-transform duration-200 group-data-[state=open]/collapsible:rotate-90" />
                                            </SidebarMenuButton>
                                        </CollapsibleTrigger>
                                        <CollapsibleContent>
                                            <SidebarMenuSub>
                                                <SidebarMenuSubItem v-for="subItem in item.items" :key="subItem.title">
                                                    <SidebarMenuSubButton as-child>
                                                        <router-link :to="subItem.url">
                                                            <span>{{ subItem.title }}</span>
                                                        </router-link>
                                                    </SidebarMenuSubButton>
                                                </SidebarMenuSubItem>
                                            </SidebarMenuSub>
                                        </CollapsibleContent>
                                    </SidebarMenuItem>
                                </Collapsible>
                            </template>
                        </SidebarMenu>
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
                                    <DropdownMenuItem @click="authStore.logout">
                                        <LogOut class="mr-2" />
                                        Log out
                                    </DropdownMenuItem>
                                </DropdownMenuContent>
                            </DropdownMenu>
                        </SidebarMenuItem>
                    </SidebarMenu>
                </SidebarFooter>
                <SidebarRail />
            </Sidebar>

            <SidebarInset class="flex flex-col flex-1 min-w-0">
                <header class="flex h-16 shrink-0 items-center gap-2 border-b px-4">
                    <SidebarTrigger class="-ml-1" />
                    <Breadcrumb>
                        <BreadcrumbList>
                            <template v-for="(item, index) in breadcrumbs" :key="index">
                                <BreadcrumbItem>
                                    <router-link v-if="index < breadcrumbs.length - 1" :to="item.to || ''"
                                        class="text-muted-foreground hover:text-foreground">
                                        {{ item.label }}
                                    </router-link>
                                    <span v-else>
                                        {{ item.label }}
                                    </span>
                                </BreadcrumbItem>
                                <span v-if="index < breadcrumbs.length - 1" class="mx-2 text-muted-foreground">/</span>
                            </template>
                        </BreadcrumbList>
                    </Breadcrumb>
                </header>
                <main class="flex-1 overflow-y-auto overflow-x-auto min-w-0">
                    <div class="h-full px-6 py-6 min-w-0">
                        <router-view />
                    </div>
                </main>
            </SidebarInset>
        </SidebarProvider>
    </div>
</template>
