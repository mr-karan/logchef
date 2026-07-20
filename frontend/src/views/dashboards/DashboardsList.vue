<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import { useRouter } from "vue-router";
import {
  LayoutDashboard,
  Plus,
  MoreVertical,
  Trash2,
  LayoutGrid,
  Search,
  BarChart3,
  Hash,
  Table2,
  User,
} from "lucide-vue-next";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { Label } from "@/components/ui/label";
import { Skeleton } from "@/components/ui/skeleton";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from "@/components/ui/dialog";
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuItem,
} from "@/components/ui/dropdown-menu";
import ConfirmDialog from "@/components/ui/ConfirmDialog.vue";
import { useDashboardsStore } from "@/stores/dashboards";
import type { Dashboard } from "@/api/dashboards";

const router = useRouter();
const store = useDashboardsStore();

const isLoading = computed(() => store.isLoadingOperation("fetchDashboards"));
const dashboards = computed(() => store.dashboards);

const search = ref("");
const filteredDashboards = computed(() => {
  const q = search.value.trim().toLowerCase();
  if (!q) return dashboards.value;
  return dashboards.value.filter(
    (d) => d.name.toLowerCase().includes(q) || d.description?.toLowerCase().includes(q)
  );
});

onMounted(() => {
  void store.fetchDashboards();
});

function openDashboard(id: number) {
  router.push(`/dashboards/${id}`);
}

function panelCount(d: Dashboard): number {
  return d.panels?.panels?.length ?? 0;
}

// Distinct panel types on a dashboard, for the small type-icon row on each
// card — a cheap stand-in for a real thumbnail that still hints at content.
const TYPE_ICONS = { timeseries: BarChart3, stat: Hash, table: Table2 } as const;
function panelTypeIcons(d: Dashboard) {
  const types = new Set((d.panels?.panels ?? []).map((p) => p.type));
  return Array.from(types)
    .filter((t): t is keyof typeof TYPE_ICONS => t in TYPE_ICONS)
    .map((t) => TYPE_ICONS[t]);
}

function updatedLabel(d: Dashboard): string {
  const date = new Date(d.updated_at);
  return Number.isNaN(date.getTime()) ? "" : date.toLocaleDateString();
}

// --- Create dialog ----------------------------------------------------------
const createOpen = ref(false);
const newName = ref("");
const newDescription = ref("");
const isCreating = computed(() => store.isLoadingOperation("createDashboard"));

function openCreate() {
  newName.value = "";
  newDescription.value = "";
  createOpen.value = true;
}

async function submitCreate() {
  const name = newName.value.trim();
  if (!name) return;
  const result = await store.createDashboard({
    name,
    description: newDescription.value.trim(),
    // A dashboard starts empty; panels are added in edit mode.
    panels: { version: 1, layout: [], panels: [] },
  });
  if (result.success && result.data) {
    createOpen.value = false;
    router.push(`/dashboards/${result.data.id}`);
  }
}

// --- Delete -----------------------------------------------------------------
const deleteTarget = ref<Dashboard | null>(null);
const deleteOpen = computed({
  get: () => deleteTarget.value !== null,
  set: (v: boolean) => {
    if (!v) deleteTarget.value = null;
  },
});

function confirmDelete(d: Dashboard) {
  deleteTarget.value = d;
}

async function doDelete() {
  if (!deleteTarget.value) return;
  await store.deleteDashboard(deleteTarget.value.id);
  deleteTarget.value = null;
}
</script>

<template>
  <div class="mx-auto w-full max-w-[1400px] px-4 py-4">
    <!-- Header -->
    <div class="mb-5 flex flex-wrap items-center justify-between gap-3">
      <div class="flex items-center gap-2">
        <LayoutDashboard class="h-5 w-5 text-muted-foreground" />
        <h1 class="text-lg font-semibold">Dashboards</h1>
      </div>
      <div class="flex items-center gap-2">
        <div v-if="dashboards.length > 0" class="relative">
          <Search class="absolute left-2.5 top-2.5 h-3.5 w-3.5 text-muted-foreground" />
          <Input v-model="search" placeholder="Search dashboards…" class="h-9 w-56 pl-8" />
        </div>
        <Button size="sm" class="gap-1.5" @click="openCreate">
          <Plus class="h-4 w-4" />
          New dashboard
        </Button>
      </div>
    </div>

    <!-- Loading: card-shaped skeletons previewing the real card layout -->
    <div v-if="isLoading && dashboards.length === 0" class="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
      <div v-for="n in 6" :key="n" class="flex flex-col gap-3 rounded-lg border bg-card p-4">
        <div class="flex items-start justify-between gap-2">
          <Skeleton class="h-4 w-2/3" />
          <Skeleton class="h-4 w-4 shrink-0 rounded" />
        </div>
        <Skeleton class="h-3 w-full" />
        <Skeleton class="h-3 w-4/5" />
        <div class="mt-1 flex items-center gap-2">
          <Skeleton class="h-4 w-16 rounded" />
          <Skeleton class="h-3 w-20" />
          <Skeleton class="ml-auto h-3 w-14" />
        </div>
      </div>
    </div>

    <!-- Empty: no dashboards at all -->
    <div v-else-if="dashboards.length === 0" class="dash-list-empty">
      <div class="dash-list-empty__icon">
        <LayoutGrid class="h-8 w-8" />
      </div>
      <div>
        <p class="text-base font-semibold">No dashboards yet</p>
        <p class="mx-auto mt-1 max-w-sm text-sm text-muted-foreground">
          Build a dashboard to group saved queries into a shared view — errors by service, latency
          trends, or anything you check often.
        </p>
      </div>
      <Button size="sm" class="mt-1 gap-1.5" @click="openCreate">
        <Plus class="h-4 w-4" />
        Create your first dashboard
      </Button>
    </div>

    <!-- Empty: search matched nothing -->
    <div
      v-else-if="filteredDashboards.length === 0"
      class="flex flex-col items-center justify-center gap-1.5 rounded-lg border border-dashed py-14 text-center"
    >
      <p class="text-sm font-medium">No dashboards match “{{ search }}”</p>
      <p class="text-sm text-muted-foreground">Try a different name or clear the search.</p>
    </div>

    <!-- Cards -->
    <div v-else class="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
      <div
        v-for="d in filteredDashboards"
        :key="d.id"
        class="group relative flex cursor-pointer flex-col rounded-lg border bg-card p-4 transition-all hover:-translate-y-0.5 hover:border-primary/50 hover:shadow-md"
        @click="openDashboard(d.id)"
      >
        <div class="flex items-start justify-between gap-2">
          <h2 class="font-medium leading-tight truncate">{{ d.name }}</h2>
          <DropdownMenu v-if="d.can_edit">
            <DropdownMenuTrigger as-child @click.stop>
              <Button
                variant="ghost"
                size="sm"
                class="h-6 w-6 p-0 opacity-0 transition-opacity group-hover:opacity-100"
              >
                <MoreVertical class="h-4 w-4" />
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end" class="w-32">
              <DropdownMenuItem class="text-destructive text-xs" @click.stop="confirmDelete(d)">
                <Trash2 class="mr-2 h-3.5 w-3.5" />
                Delete
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        </div>

        <p class="mt-1 line-clamp-2 min-h-[2.5rem] text-sm text-muted-foreground">
          {{ d.description || "No description" }}
        </p>

        <div class="mt-3 flex items-center gap-2 text-xs text-muted-foreground">
          <span class="rounded bg-muted px-1.5 py-0.5 font-medium">
            {{ panelCount(d) }} panel{{ panelCount(d) === 1 ? "" : "s" }}
          </span>
          <span v-if="panelTypeIcons(d).length" class="flex items-center gap-1 text-muted-foreground/70">
            <component :is="icon" v-for="(icon, i) in panelTypeIcons(d)" :key="i" class="h-3 w-3" />
          </span>
          <span class="ml-auto flex items-center gap-1 truncate">
            <User class="h-3 w-3 shrink-0" />
            <span class="truncate">{{ d.created_by_name || d.created_by_email || "Unknown" }}</span>
          </span>
        </div>
        <p v-if="updatedLabel(d)" class="mt-1 text-[11px] text-muted-foreground/70">
          Updated {{ updatedLabel(d) }}
        </p>
      </div>
    </div>

    <!-- Create dialog -->
    <Dialog v-model:open="createOpen">
      <DialogContent>
        <DialogHeader>
          <DialogTitle>New dashboard</DialogTitle>
          <DialogDescription>Give your dashboard a name. Add panels after creating it.</DialogDescription>
        </DialogHeader>
        <div class="space-y-3 py-1">
          <div class="space-y-1.5">
            <Label for="dash-name">Name</Label>
            <Input
              id="dash-name"
              v-model="newName"
              placeholder="e.g. HTTP error overview"
              autofocus
              @keydown.enter.prevent="submitCreate"
            />
          </div>
          <div class="space-y-1.5">
            <Label for="dash-desc">Description</Label>
            <Textarea
              id="dash-desc"
              v-model="newDescription"
              placeholder="Optional"
              rows="2"
            />
          </div>
        </div>
        <DialogFooter>
          <Button variant="outline" @click="createOpen = false">Cancel</Button>
          <Button :disabled="!newName.trim() || isCreating" @click="submitCreate">
            {{ isCreating ? "Creating…" : "Create" }}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>

    <!-- Delete confirm -->
    <ConfirmDialog
      v-model:open="deleteOpen"
      title="Delete dashboard?"
      :description="`This permanently deletes “${deleteTarget?.name}”. This can't be undone.`"
      confirm-text="Delete"
      destructive
      @confirm="doDelete"
    />
  </div>
</template>

<style scoped>
.dash-list-empty {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 0.6rem;
  border: 1px solid var(--border);
  border-radius: 0.75rem;
  background: color-mix(in srgb, var(--muted) 12%, transparent);
  padding: 3.5rem 1.5rem;
  text-align: center;
}
.dash-list-empty__icon {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 3.5rem;
  height: 3.5rem;
  border-radius: 9999px;
  background: color-mix(in srgb, var(--primary) 12%, transparent);
  color: var(--primary);
}
</style>
