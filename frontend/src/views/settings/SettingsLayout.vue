<script setup lang="ts">
import { computed } from "vue";
import { useRoute } from "vue-router";
import { cn } from "@/lib/utils";
import { SlidersHorizontal, UserCircle2 } from "lucide-vue-next";

const route = useRoute();

const navItems = [
  {
    label: "Profile",
    to: "/settings/profile",
    icon: UserCircle2,
  },
  {
    label: "Preferences",
    to: "/settings/preferences",
    icon: SlidersHorizontal,
  },
];

const activePath = computed(() => route.path);
const isActive = (to: string) => activePath.value === to || activePath.value.startsWith(to + "/");
</script>

<template>
  <div class="grid gap-6 lg:grid-cols-[200px_1fr]">
    <aside class="rounded-md border bg-card p-1.5 h-fit">
      <nav class="space-y-0.5">
        <router-link
          v-for="item in navItems"
          :key="item.to"
          :to="item.to"
          class="block"
        >
          <div
            :class="
              cn(
                'flex items-center gap-2 rounded-md px-3 py-2 text-sm transition-colors',
                isActive(item.to)
                  ? 'bg-accent text-accent-foreground font-medium'
                  : 'text-muted-foreground hover:bg-muted/60 hover:text-foreground'
              )
            "
          >
            <component :is="item.icon" class="h-4 w-4" />
            <span>{{ item.label }}</span>
          </div>
        </router-link>
      </nav>
    </aside>

    <main class="min-w-0">
      <router-view />
    </main>
  </div>
</template>
