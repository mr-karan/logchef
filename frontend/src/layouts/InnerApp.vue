<script setup lang="ts">
import {
    Avatar,
    AvatarFallback,
} from '@/components/ui/avatar'

import {
    Breadcrumb,
    BreadcrumbItem,
    BreadcrumbList,
    BreadcrumbPage,
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
    Save,
    Database,
    ChevronRight,
    ChevronsUpDown,
    LogOut,
} from 'lucide-vue-next'

import { useRoute } from 'vue-router'
import { useAuthStore } from '@/stores/auth'

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
        title: 'Data',
        icon: Database,
        isActive: true,
        items: [
            {
                title: 'Explore',
                url: '/explore',
            },
            {
                title: 'Add Source',
                url: '/sources/new',
            },
            {
                title: 'Manage Sources',
                url: '/sources/manage',
            },
        ],
    },
    {
        title: 'Saved Queries',
        url: '/saved-queries',
        icon: Save,
        items: [
            {
                title: 'All Queries',
                url: '/saved-queries',
            },
            {
                title: 'Shared',
                url: '/saved-queries/shared',
            },
            {
                title: 'Favorites',
                url: '/saved-queries/favorites',
            },
        ],
    },
    {
        title: 'Settings',
        url: '/settings',
        icon: Settings2,
        items: [
            {
                title: 'General',
                url: '/settings/general',
            },
            {
                title: 'Team',
                url: '/settings/team',
            },
            {
                title: 'API Keys',
                url: '/settings/api-keys',
            },
        ],
    },
]
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
                            <Collapsible v-for="item in navMain" :key="item.title" as-child
                                :default-open="item.isActive" class="group/collapsible">
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
                            <BreadcrumbItem>
                                <BreadcrumbPage>{{ route.name }}</BreadcrumbPage>
                            </BreadcrumbItem>
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
