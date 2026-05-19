<script setup lang="ts">
import { Avatar, AvatarFallback } from "@/components/ui/avatar";

import { Button } from "@/components/ui/button";

import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";

import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/tooltip";

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
  SidebarTrigger,
} from "@/components/ui/sidebar";

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
  Bell,
  Wrench,
  FolderOpen,
  KeyRound,
} from "lucide-vue-next";

import { useAuthStore } from "@/stores/auth";
import { useTeamPermissions } from "@/composables/useTeamPermissions";
import { useThemeStore, type ThemeMode } from "@/stores/theme";
import { usePreferencesStore } from "@/stores/preferences";
import { useMetaStore } from "@/stores/meta";
import { ref, watch, onMounted } from "vue";
import { useTeamsStore } from "@/stores/teams";
import { useExploreStore } from "@/stores/explore";

const authStore = useAuthStore();
const themeStore = useThemeStore();
const preferencesStore = usePreferencesStore();
const metaStore = useMetaStore();
const teamsStore = useTeamsStore();
const exploreStore = useExploreStore();
const { isGlobalAdmin, isAnyTeamAdmin } = useTeamPermissions();

const setThemePreference = (mode: ThemeMode) => {
  themeStore.setTheme(mode);
  preferencesStore.updatePreferences({ theme: mode }, { syncTheme: false });
};

const cycleTheme = () => {
  const next: Record<ThemeMode, ThemeMode> = { light: 'dark', dark: 'auto', auto: 'light' };
  setThemePreference(next[themeStore.preference]);
};

onMounted(() => {
  preferencesStore.loadPreferences();
});

// Carry team/source context into URLs that support it, so deep links keep
// the user's current scope.
function withContext(path: string) {
  const query: Record<string, string> = {};
  if (teamsStore.currentTeamId) query.team = String(teamsStore.currentTeamId);
  if (exploreStore.sourceId) query.source = String(exploreStore.sourceId);
  return { path, query };
}

const CONTEXTUAL_URLS = new Set(["/logs/explore", "/logs/alerts"]);
const resolveTo = (url: string) => CONTEXTUAL_URLS.has(url) ? withContext(url) : url;

// Get initial sidebar state from cookie or default to true
const getSavedState = () => {
  if (typeof document !== "undefined") {
    const savedState = document.cookie
      .split("; ")
      .find((row) => row.startsWith("sidebar_state="))
      ?.split("=")[1];

    return savedState === "true";
  }
  return false;
};

// Manage sidebar state locally with persistence
const sidebarOpen = ref(getSavedState());

// Save sidebar state to cookie when it changes
watch(sidebarOpen, (newValue) => {
  if (typeof document !== "undefined") {
    document.cookie = `sidebar_state=${newValue}; path=/; max-age=31536000; SameSite=Lax`;
  }
});


// Helper function to get user initials
function getUserInitials(name: string | undefined): string {
  if (!name) return "?";
  return name
    .split(" ")
    .map((part) => part[0])
    .join("")
    .toUpperCase()
    .slice(0, 2);
}

// Define navigation item type
interface NavItem {
  title: string;
  icon: any;
  url: string;
  adminOnly?: boolean;
}

// Group navigation items by category
const mainNavItems: NavItem[] = [
  {
    title: "Explorer",
    icon: Search,
    url: "/logs/explore",
  },
  {
    title: "Saved Queries",
    icon: ClipboardList,
    url: "/logs/saved",
  },
  {
    title: "Collections",
    icon: FolderOpen,
    url: "/logs/collections",
  },
  {
    title: "Alerts",
    icon: Bell,
    url: "/logs/alerts",
  },
];

const adminNavItems: NavItem[] = [
  {
    title: "Sources",
    icon: Database,
    url: "/admin/sources",
    adminOnly: true,
  },
  {
    title: "Users",
    icon: UsersRound,
    url: "/admin/users",
    adminOnly: true,
  },
  {
    title: "Service Tokens",
    icon: KeyRound,
    url: "/admin/service-tokens",
    adminOnly: true,
  },
  {
    title: "Teams",
    icon: Users,
    url: "/admin/teams",
    adminOnly: true,
  },
  {
    title: "System Settings",
    icon: Wrench,
    url: "/admin/settings",
    adminOnly: true,
  },
];

const navItems = [
  {
    title: "Profile",
    icon: UserCircle2,
    url: "/settings/profile",
  },
  {
    title: "Preferences",
    icon: Settings,
    url: "/settings/preferences",
  },
];
</script>

<template>
  <div class="h-screen w-screen flex overflow-hidden">
    <SidebarProvider v-model:open="sidebarOpen" :defaultOpen="sidebarOpen">
      <Sidebar collapsible="icon"
        class="flex-none z-50 h-screen"
        :class="{ 'w-64': sidebarOpen, 'w-[72px]': !sidebarOpen }">
        <SidebarHeader>
          <SidebarMenu>
            <SidebarMenuItem>
              <SidebarMenuButton size="lg" :tooltip="metaStore.version ? `LogChef v${metaStore.version}` : 'LogChef'">
                <div class="flex aspect-square size-8 items-center justify-center rounded-lg bg-sidebar-primary text-sidebar-primary-foreground font-semibold text-sm">
                  LC
                </div>
                <div class="grid flex-1 text-left text-sm leading-tight">
                  <span class="truncate font-semibold">LogChef</span>
                  <span v-if="metaStore.version" class="truncate text-xs opacity-60">{{ metaStore.version }}</span>
                </div>
              </SidebarMenuButton>
            </SidebarMenuItem>
          </SidebarMenu>
        </SidebarHeader>

        <SidebarContent>
          <!-- Main Navigation -->
          <SidebarGroup>
            <SidebarGroupLabel v-if="sidebarOpen">Main</SidebarGroupLabel>
            <SidebarGroupContent>
              <SidebarMenu>
                <template v-for="item in mainNavItems" :key="item.title">
                  <SidebarMenuItem v-if="
                    !item.adminOnly ||
                    (item.adminOnly && authStore.user?.role === 'admin')
                  ">
                    <SidebarMenuButton asChild :tooltip="item.title"
                      class="hover:bg-primary hover:text-primary-foreground py-2 data-[active=true]:bg-sidebar-accent data-[active=true]:text-sidebar-accent-foreground rounded-md transition-colors duration-150">
                      <router-link :to="resolveTo(item.url)" class="flex items-center" active-class="font-medium">
                        <component :is="item.icon" class="size-5" :class="sidebarOpen ? 'mr-3 ml-1' : 'mx-auto'" />
                        <span v-if="sidebarOpen">{{ item.title }}</span>
                      </router-link>
                    </SidebarMenuButton>
                  </SidebarMenuItem>
                </template>
              </SidebarMenu>
            </SidebarGroupContent>
          </SidebarGroup>

          <!-- Administration section: visible to global admins and any-team admins.
               Global admins see every item; team admins see only Teams. -->
          <SidebarGroup v-if="isGlobalAdmin || isAnyTeamAdmin" class="mt-4">
            <SidebarGroupLabel v-if="sidebarOpen">Administration</SidebarGroupLabel>
            <SidebarGroupContent>
              <SidebarMenu>
                <template v-for="item in adminNavItems" :key="item.title">
                  <SidebarMenuItem v-if="isGlobalAdmin || item.title === 'Teams'">
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
              <SidebarMenuButton size="sm" @click="cycleTheme" :tooltip="themeStore.preference === 'light' ? 'Light mode' : themeStore.preference === 'dark' ? 'Dark mode' : 'System'">
                <Sun v-if="themeStore.preference === 'light'" class="size-4" />
                <Moon v-else-if="themeStore.preference === 'dark'" class="size-4" />
                <Monitor v-else class="size-4" />
                <span>{{ themeStore.preference === 'light' ? 'Light' : themeStore.preference === 'dark' ? 'Dark' : 'System' }}</span>
              </SidebarMenuButton>
            </SidebarMenuItem>
            <SidebarMenuItem>
              <DropdownMenu>
                <DropdownMenuTrigger asChild>
                  <SidebarMenuButton size="lg"
                    class="data-[state=open]:bg-sidebar-accent data-[state=open]:text-sidebar-accent-foreground hover:bg-primary hover:text-primary-foreground">
                    <Avatar class="h-8 w-8 rounded-lg">
                      <AvatarFallback class="rounded-lg bg-sidebar-primary text-sidebar-primary-foreground text-xs">
                        {{ getUserInitials(authStore.user?.full_name) }}
                      </AvatarFallback>
                    </Avatar>
                    <div v-if="sidebarOpen" class="grid flex-1 text-left text-sm leading-tight">
                      <span class="truncate font-semibold">{{
                        authStore.user?.full_name
                        }}</span>
                      <span class="truncate text-xs opacity-70">{{
                        authStore.user?.email
                        }}</span>
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
                        <span class="truncate font-semibold">{{
                          authStore.user?.full_name
                          }}</span>
                        <span class="truncate text-xs">{{
                          authStore.user?.email
                          }}</span>
                      </div>
                    </div>
                  </DropdownMenuLabel>
                  <DropdownMenuSeparator />
                  <DropdownMenuItem asChild>
                    <router-link to="/settings/profile" class="cursor-pointer">
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
