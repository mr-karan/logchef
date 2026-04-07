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
} from "lucide-vue-next";

import { useAuthStore } from "@/stores/auth";
import { useThemeStore, type ThemeMode } from "@/stores/theme";
import { usePreferencesStore } from "@/stores/preferences";
import { useMetaStore } from "@/stores/meta";
import { ref, watch, computed, onMounted } from "vue";
import { useTeamsStore } from "@/stores/teams";
import { useExploreStore } from "@/stores/explore";
import { useRouter } from "vue-router";

const authStore = useAuthStore();
const themeStore = useThemeStore();
const preferencesStore = usePreferencesStore();
const metaStore = useMetaStore();
const teamsStore = useTeamsStore();
const exploreStore = useExploreStore();
const router = useRouter();

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

// Function to navigate to collections with clean URL  
const navigateToCollections = () => {
  const team = teamsStore.currentTeamId ? teamsStore.currentTeamId.toString() : undefined;
  const source = exploreStore.sourceId ? exploreStore.sourceId.toString() : undefined;
  
  // Explicitly define only the query params we want
  const query: Record<string, string> = {};
  if (team) query.team = team;
  if (source) query.source = source;
  
  // Use router.push to completely replace the URL with only our desired params
  router.push({
    path: "/logs/saved", 
    query
  });
};

const explorerTo = computed(() => {
  const team = teamsStore.currentTeamId ? teamsStore.currentTeamId.toString() : undefined;
  const source = exploreStore.sourceId ? exploreStore.sourceId.toString() : undefined;
  const query: Record<string, string> = {};
  if (team) query.team = team;
  if (source) query.source = source;
  return {
    path: "/logs/explore",
    query,
  };
});

const alertsTo = computed(() => {
  const team = teamsStore.currentTeamId ? teamsStore.currentTeamId.toString() : undefined;
  const source = exploreStore.sourceId ? exploreStore.sourceId.toString() : undefined;
  const query: Record<string, string> = {};
  if (team) query.team = team;
  if (source) query.source = source;
  return {
    path: "/logs/alerts",
    query,
  };
});

const collectionsTo = computed(() => {
  const team = teamsStore.currentTeamId ? teamsStore.currentTeamId.toString() : undefined;
  const source = exploreStore.sourceId ? exploreStore.sourceId.toString() : undefined;
  const query: Record<string, string> = {};
  if (team) query.team = team;
  if (source) query.source = source;
  return {
    path: "/logs/saved",
    query,
  };
});

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
    title: "Alerts",
    icon: Bell,
    url: "/logs/alerts",
  },
  {
    title: "Collections",
    icon: ClipboardList,
    url: "/logs/saved",
  },
];

const adminNavItems: NavItem[] = [
  {
    title: "Sources",
    icon: Database,
    url: "/management/sources/list",
    adminOnly: true,
  },
  {
    title: "Users",
    icon: UsersRound,
    url: "/management/users",
    adminOnly: true,
  },
  {
    title: "Teams",
    icon: Users,
    url: "/management/teams",
    adminOnly: true,
  },
  {
    title: "System Settings",
    icon: Wrench,
    url: "/management/settings",
    adminOnly: true,
  },
];

const navItems = [
  {
    title: "Profile",
    icon: UserCircle2,
    url: "/profile",
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
                      <!-- Collections uses custom navigation -->
                      <template v-if="item.url === '/logs/saved'">
                        <button @click="navigateToCollections" class="flex items-center w-full text-left">
                          <component :is="item.icon" class="size-5" :class="sidebarOpen ? 'mr-3 ml-1' : 'mx-auto'" />
                          <span v-if="sidebarOpen">{{ item.title }}</span>
                        </button>
                      </template>
                      <!-- Regular router links for other items -->
                      <template v-else>
                        <router-link :to="item.url === '/logs/explore' ? explorerTo : item.url === '/logs/alerts' ? alertsTo : item.url" class="flex items-center" active-class="font-medium">
                          <component :is="item.icon" class="size-5" :class="sidebarOpen ? 'mr-3 ml-1' : 'mx-auto'" />
                          <span v-if="sidebarOpen">{{ item.title }}</span>
                        </router-link>
                      </template>
                    </SidebarMenuButton>
                  </SidebarMenuItem>
                </template>
              </SidebarMenu>
            </SidebarGroupContent>
          </SidebarGroup>

          <!-- Admin Navigation -->
          <!-- Show for global admins OR team admins -->
          <SidebarGroup v-if="authStore.user?.role === 'admin' || teamsStore.isAnyTeamAdmin" class="mt-4">
            <SidebarGroupLabel v-if="sidebarOpen">Administration</SidebarGroupLabel>
            <SidebarGroupContent>
              <SidebarMenu>
                <template v-for="item in adminNavItems" :key="item.title">
                  <!-- Global admins see all items, team admins only see "Teams" -->
                  <SidebarMenuItem v-if="authStore.user?.role === 'admin' || item.title === 'Teams'">
                    <SidebarMenuButton asChild :tooltip="item.title"
                      class="hover:bg-primary hover:text-primary-foreground py-2 data-[active=true]:bg-sidebar-accent data-[active=true]:text-sidebar-accent-foreground rounded-md transition-colors duration-150">
                      <router-link :to="item.url === '/logs/saved' ? collectionsTo : item.url" class="flex items-center" active-class="font-medium">
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
                      <router-link :to="item.url === '/logs/saved' ? collectionsTo : item.url" class="flex items-center" active-class="font-medium">
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
