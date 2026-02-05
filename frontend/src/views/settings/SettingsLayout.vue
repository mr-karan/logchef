<script setup lang="ts">
import { computed } from "vue";
import { useRoute } from "vue-router";
import { Avatar, AvatarFallback } from "@/components/ui/avatar";
import { Badge } from "@/components/ui/badge";
import { cn } from "@/lib/utils";
import { useAuthStore } from "@/stores/auth";
import { SlidersHorizontal, UserCircle2 } from "lucide-vue-next";

const authStore = useAuthStore();
const route = useRoute();

const navItems = [
  {
    label: "Profile",
    description: "Account details and API tokens",
    to: "/profile",
    icon: UserCircle2,
  },
  {
    label: "Preferences",
    description: "Appearance and log explorer defaults",
    to: "/settings/preferences",
    icon: SlidersHorizontal,
  },
];

const activePath = computed(() => route.path);

const userInitials = computed(() => {
  const user = authStore.user;
  if (!user) return "?";
  const name = user.full_name?.trim();
  if (name) {
    const parts = name.split(" ").filter(Boolean);
    const initials = parts.slice(0, 2).map(part => part[0]?.toUpperCase()).join("");
    return initials || user.email?.[0]?.toUpperCase() || "?";
  }
  return user.email?.[0]?.toUpperCase() || "?";
});

const isActive = (to: string) => activePath.value === to || activePath.value.startsWith(to + "/");
</script>

<template>
  <div class="space-y-6">
    <div class="flex flex-col gap-4 md:flex-row md:items-center md:justify-between">
      <div>
        <h1 class="text-2xl font-bold tracking-tight">Settings</h1>
        <p class="text-muted-foreground mt-2">
          Manage your account, access, and experience preferences.
        </p>
      </div>

      <div class="flex items-center gap-3 rounded-lg border bg-card p-3">
        <Avatar class="h-9 w-9">
          <AvatarFallback class="text-xs font-semibold">
            {{ userInitials }}
          </AvatarFallback>
        </Avatar>
        <div class="min-w-0">
          <p class="text-sm font-medium leading-none truncate">{{ authStore.user?.full_name || authStore.user?.email }}</p>
          <p class="text-xs text-muted-foreground truncate">{{ authStore.user?.email }}</p>
        </div>
        <Badge v-if="authStore.user?.role" variant="secondary" class="capitalize">
          {{ authStore.user?.role }}
        </Badge>
      </div>
    </div>

    <div class="grid gap-6 lg:grid-cols-[240px_1fr]">
      <aside class="rounded-lg border bg-card p-2">
        <nav class="space-y-1">
          <router-link
            v-for="item in navItems"
            :key="item.to"
            :to="item.to"
            class="block"
          >
            <div
              :class="
                cn(
                  'flex items-start gap-3 rounded-md px-3 py-2 text-sm transition-colors',
                  isActive(item.to)
                    ? 'bg-accent text-accent-foreground'
                    : 'text-muted-foreground hover:bg-muted/60 hover:text-foreground'
                )
              "
            >
              <component :is="item.icon" class="mt-0.5 h-4 w-4" />
              <div class="space-y-1">
                <p class="font-medium leading-none">{{ item.label }}</p>
                <p class="text-xs text-muted-foreground">{{ item.description }}</p>
              </div>
            </div>
          </router-link>
        </nav>
      </aside>

      <main class="min-w-0">
        <router-view />
      </main>
    </div>
  </div>
</template>
