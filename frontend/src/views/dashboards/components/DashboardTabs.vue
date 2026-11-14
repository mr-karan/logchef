<script setup lang="ts">
import { computed, ref } from "vue";
import { useI18n } from "vue-i18n";
import { ArrowLeft, ArrowRight, Pencil, Plus } from "lucide-vue-next";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { DropdownMenuItem } from "@/components/ui/dropdown-menu";
import ConfirmDialog from "@/components/ui/ConfirmDialog.vue";
import { RowActionsMenu } from "@/components/layout";
import type { DashboardTab } from "@/api/dashboards";
import { MAX_DASHBOARD_TABS, MAX_TAB_TITLE_CODE_POINTS, validTabTitle } from "@/utils/dashboardPanels";

const props = defineProps<{
  tabs: DashboardTab[];
  activeId: string;
  /** Edit mode adds the tab actions: add, rename, move and delete. */
  editing: boolean;
}>();

const emit = defineEmits<{
  select: [id: string];
  add: [title: string];
  rename: [id: string, title: string];
  move: [id: string, toIndex: number];
  remove: [id: string];
}>();

const { t } = useI18n();

// A dashboard with one tab shows no tab bar. Edit mode shows it anyway, as
// the place to add a second tab.
const showBar = computed(() => props.tabs.length > 1 || props.editing);
const activeIndex = computed(() => props.tabs.findIndex((tab) => tab.id === props.activeId));
const active = computed(() => props.tabs[activeIndex.value]);
const atTabLimit = computed(() => props.tabs.length >= MAX_DASHBOARD_TABS);

// One dialog names a new tab and renames the tab on screen.
const naming = ref<{ renameId: string | null } | null>(null);
const title = ref("");
const tooLong = computed(() => Array.from(title.value.trim()).length > MAX_TAB_TITLE_CODE_POINTS);
const storedTitle = computed(() => validTabTitle(title.value));

function openAdd() {
  title.value = "";
  naming.value = { renameId: null };
}

function openRename() {
  if (!active.value) return;
  title.value = active.value.title;
  naming.value = { renameId: active.value.id };
}

function submitName() {
  const target = naming.value;
  const stored = storedTitle.value;
  if (!target || stored === null) return;
  if (target.renameId === null) emit("add", stored);
  else emit("rename", target.renameId, stored);
  naming.value = null;
}

// Deleting a tab deletes its panels, so it asks first. An empty tab holds
// nothing to lose and goes at once.
const deleting = ref<DashboardTab | null>(null);

function requestDelete() {
  const tab = active.value;
  if (!tab) return;
  if (tab.layout.length === 0) emit("remove", tab.id);
  else deleting.value = tab;
}

function confirmDelete() {
  if (deleting.value) emit("remove", deleting.value.id);
  deleting.value = null;
}
</script>

<template>
  <!-- Manual activation: a tab runs its queries when it is shown, so arrow keys
       move focus and Enter or Space shows the tab. -->
  <Tabs
    v-if="showBar"
    :model-value="activeId"
    activation-mode="manual"
    class="gap-3"
    @update:model-value="emit('select', String($event))"
  >
    <div class="flex items-center gap-2">
      <TabsList class="h-auto min-h-9 max-w-full justify-start overflow-x-auto" :aria-label="t('dashboards.tabs')">
        <TabsTrigger
          v-for="tab in tabs"
          :key="tab.id"
          :value="tab.id"
          :title="tab.title"
          class="max-w-[16rem] flex-none"
        >
          <span class="truncate">{{ tab.title }}</span>
        </TabsTrigger>
      </TabsList>
      <template v-if="editing">
        <RowActionsMenu
          :delete-label="t('dashboards.deleteTab')"
          :on-delete="tabs.length > 1 ? requestDelete : undefined"
        >
          <DropdownMenuItem @click="openRename">
            <Pencil class="mr-2 h-4 w-4" />
            {{ t('dashboards.renameTab') }}
          </DropdownMenuItem>
          <DropdownMenuItem :disabled="activeIndex <= 0" @click="emit('move', activeId, activeIndex - 1)">
            <ArrowLeft class="mr-2 h-4 w-4" />
            {{ t('dashboards.moveTabLeft') }}
          </DropdownMenuItem>
          <DropdownMenuItem
            :disabled="activeIndex >= tabs.length - 1"
            @click="emit('move', activeId, activeIndex + 1)"
          >
            <ArrowRight class="mr-2 h-4 w-4" />
            {{ t('dashboards.moveTabRight') }}
          </DropdownMenuItem>
        </RowActionsMenu>
        <Button
          variant="outline"
          size="sm"
          class="h-8 shrink-0 gap-1.5 text-xs"
          :disabled="atTabLimit"
          :title="atTabLimit ? t('dashboards.tabLimitReached', { max: MAX_DASHBOARD_TABS }) : undefined"
          @click="openAdd"
        >
          <Plus class="h-3.5 w-3.5" />
          {{ t('dashboards.addTab') }}
        </Button>
      </template>
    </div>
    <TabsContent :value="activeId">
      <slot />
    </TabsContent>
  </Tabs>
  <slot v-else />

  <Dialog :open="naming !== null" @update:open="(open) => { if (!open) naming = null; }">
    <DialogContent :aria-describedby="undefined">
      <DialogHeader>
        <DialogTitle>{{ naming?.renameId ? t('dashboards.renameTab') : t('dashboards.addTab') }}</DialogTitle>
      </DialogHeader>
      <div class="space-y-1.5 py-1">
        <Label for="dash-tab-name">{{ t('dashboards.tabName') }}</Label>
        <Input
          id="dash-tab-name"
          v-model="title"
          autofocus
          :aria-invalid="tooLong"
          :aria-describedby="tooLong ? 'dash-tab-name-error' : undefined"
          @keydown.enter.prevent="submitName"
        />
        <p v-if="tooLong" id="dash-tab-name-error" class="text-xs text-destructive">
          {{ t('dashboards.tabNameTooLong', { max: MAX_TAB_TITLE_CODE_POINTS }) }}
        </p>
      </div>
      <DialogFooter>
        <Button variant="outline" @click="naming = null">{{ t('ui.cancel') }}</Button>
        <Button :disabled="storedTitle === null" @click="submitName">
          {{ naming?.renameId ? t('ui.save') : t('ui.add') }}
        </Button>
      </DialogFooter>
    </DialogContent>
  </Dialog>

  <ConfirmDialog
    :open="deleting !== null"
    :title="t('dashboards.deleteTabTitle', { name: deleting?.title ?? '' })"
    :description="t('dashboards.deleteTabConfirmation', { count: deleting?.layout.length ?? 0 }, deleting?.layout.length ?? 0)"
    :confirm-text="t('dashboards.deleteTab')"
    destructive
    @update:open="(open) => { if (!open) deleting = null; }"
    @confirm="confirmDelete"
  />
</template>
