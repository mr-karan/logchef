<script setup lang="ts">
import { computed, ref } from "vue";
import { useI18n } from "vue-i18n";
import { ArrowUpToLine, Ban, Eye, EyeOff, MinusCircle, Pencil, PlusCircle, Plus, SlidersHorizontal, Trash2, X } from "lucide-vue-next";
import { Button } from "@/components/ui/button";
import { Popover, PopoverAnchor, PopoverContent } from "@/components/ui/popover";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { filterFieldLabel, type FilterLanguage, type QueryFilter } from "@/utils/queryFilters";
import FilterEditor from "./FilterEditor.vue";
import { filterTitle, filterValueLabel } from "./filterLabels";

// Kibana-style filter bar: each pill is a field filter that joins the query
// with "and" when it runs. A pill opens a menu to edit, exclude, disable,
// copy into the query text or delete it.
const props = defineProps<{
  filters: QueryFilter[];
  fields: string[];
  language: FilterLanguage;
}>();

const emit = defineEmits<{
  (e: "update:filters", filters: QueryFilter[]): void;
  // The pills to move into the query text; the parent removes them from the bar.
  (e: "copy-to-query", filters: QueryFilter[]): void;
}>();

const { t } = useI18n();

// The pill whose popover is open, and whether it shows the editor.
const openIndex = ref<number | null>(null);
const editing = ref(false);
const adding = ref(false);

const hasFilters = computed(() => props.filters.length > 0);
const enabledFilters = computed(() => props.filters.filter(filter => !filter.disabled));

function setOpen(index: number, open: boolean) {
  if (open) {
    openIndex.value = index;
    editing.value = false;
  } else if (openIndex.value === index) {
    openIndex.value = null;
    editing.value = false;
  }
}

function close() {
  openIndex.value = null;
  editing.value = false;
}

function replace(index: number, filter: QueryFilter) {
  emit("update:filters", props.filters.map((item, i) => (i === index ? filter : item)));
}

function update(index: number, patch: Partial<QueryFilter>) {
  const next = { ...props.filters[index], ...patch };
  if (!next.negate) delete next.negate;
  if (!next.disabled) delete next.disabled;
  replace(index, next);
  close();
}

function remove(index: number) {
  emit("update:filters", props.filters.filter((_, i) => i !== index));
  close();
}

function saveEdit(index: number, filter: QueryFilter) {
  replace(index, filter);
  close();
}

function addFilter(filter: QueryFilter) {
  emit("update:filters", [...props.filters, filter]);
  adding.value = false;
}

function copyOne(index: number) {
  emit("copy-to-query", [props.filters[index]]);
  close();
}

function setAll(patch: (filter: QueryFilter) => Partial<QueryFilter>) {
  emit("update:filters", props.filters.map((filter) => {
    const next = { ...filter, ...patch(filter) };
    if (!next.negate) delete next.negate;
    if (!next.disabled) delete next.disabled;
    return next;
  }));
}
</script>

<template>
  <div class="flex flex-wrap items-center gap-1.5 px-3 py-1.5" data-testid="filter-pills-bar">
    <DropdownMenu v-if="hasFilters">
      <DropdownMenuTrigger as-child>
        <Button variant="ghost" size="icon" class="h-6 w-6" :title="t('filters.allFilters')" :aria-label="t('filters.allFilters')" data-testid="filter-bar-menu">
          <SlidersHorizontal class="h-3.5 w-3.5" />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="start" class="w-52">
        <DropdownMenuItem :disabled="enabledFilters.length === 0" data-testid="filter-copy-all" @click="emit('copy-to-query', enabledFilters)">
          <ArrowUpToLine class="mr-2 h-4 w-4" />
          {{ t('filters.copyAllToQuery') }}
        </DropdownMenuItem>
        <DropdownMenuSeparator />
        <DropdownMenuItem @click="setAll(() => ({ disabled: false }))">
          <Eye class="mr-2 h-4 w-4" />
          {{ t('filters.enableAll') }}
        </DropdownMenuItem>
        <DropdownMenuItem @click="setAll(() => ({ disabled: true }))">
          <EyeOff class="mr-2 h-4 w-4" />
          {{ t('filters.disableAll') }}
        </DropdownMenuItem>
        <DropdownMenuItem @click="setAll((filter) => ({ negate: !filter.negate }))">
          <Ban class="mr-2 h-4 w-4" />
          {{ t('filters.invertAll') }}
        </DropdownMenuItem>
        <DropdownMenuSeparator />
        <DropdownMenuItem class="text-destructive focus:text-destructive" data-testid="filter-remove-all" @click="emit('update:filters', [])">
          <Trash2 class="mr-2 h-4 w-4" />
          {{ t('filters.removeAll') }}
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>

    <Popover
      v-for="(filter, index) in filters"
      :key="`${index}:${filter.field}:${filter.operator}`"
      :open="openIndex === index"
      @update:open="(open: boolean) => setOpen(index, open)"
    >
      <PopoverAnchor as-child>
        <span
          class="inline-flex max-w-[22rem] items-center rounded-md border text-xs shadow-xs"
          :class="[
            filter.disabled ? 'border-dashed bg-muted/40 text-muted-foreground' : 'bg-background',
            filter.negate && !filter.disabled ? 'border-destructive/50' : '',
          ]"
          data-testid="filter-pill"
          :data-disabled="filter.disabled || undefined"
          :data-negate="filter.negate || undefined"
        >
          <button
            type="button"
            class="min-w-0 truncate px-2 py-0.5 text-left"
            :class="{ 'line-through': filter.disabled }"
            :title="filterTitle(t, filter)"
            data-testid="filter-pill-label"
            @click="setOpen(index, openIndex !== index)"
          >
            <template v-if="filter.label">{{ filter.label }}</template>
            <template v-else>
              <span v-if="filter.negate" class="font-semibold text-destructive">NOT </span>
              <span class="font-medium">{{ filterFieldLabel(filter.field) }}:</span>&#32;<span :class="filter.disabled ? '' : 'text-emerald-700 dark:text-emerald-400'">{{ filterValueLabel(t, filter) }}</span>
            </template>
          </button>
          <button
            type="button"
            class="border-l px-1 py-0.5 text-muted-foreground hover:text-foreground"
            :aria-label="t('ui.delete')"
            :title="t('ui.delete')"
            data-testid="filter-pill-remove"
            @click.stop="remove(index)"
          >
            <X class="h-3 w-3" />
          </button>
        </span>
      </PopoverAnchor>
      <PopoverContent align="start" :class="editing ? 'w-[min(34rem,90vw)] p-4' : 'w-56 p-1'">
        <FilterEditor
          v-if="editing"
          :filter="filter"
          :fields="fields"
          :language="language"
          @save="(next) => saveEdit(index, next)"
          @cancel="close"
        />
        <div v-else class="flex flex-col text-sm" role="menu">
          <button type="button" role="menuitem" class="flex items-center gap-2 rounded px-2 py-1.5 text-left hover:bg-accent focus-visible:bg-accent focus-visible:outline-none" data-testid="filter-edit" @click="editing = true">
            <Pencil class="h-4 w-4" /> {{ t('filters.editFilter') }}
          </button>
          <button type="button" role="menuitem" class="flex items-center gap-2 rounded px-2 py-1.5 text-left hover:bg-accent focus-visible:bg-accent focus-visible:outline-none" data-testid="filter-negate" @click="update(index, { negate: !filter.negate })">
            <component :is="filter.negate ? PlusCircle : MinusCircle" class="h-4 w-4" />
            {{ filter.negate ? t('filters.includeResults') : t('filters.excludeResults') }}
          </button>
          <button type="button" role="menuitem" class="flex items-center gap-2 rounded px-2 py-1.5 text-left hover:bg-accent focus-visible:bg-accent focus-visible:outline-none" data-testid="filter-disable" @click="update(index, { disabled: !filter.disabled })">
            <component :is="filter.disabled ? Eye : EyeOff" class="h-4 w-4" />
            {{ filter.disabled ? t('filters.enable') : t('filters.disable') }}
          </button>
          <button type="button" role="menuitem" class="flex items-center gap-2 rounded px-2 py-1.5 text-left hover:bg-accent focus-visible:bg-accent focus-visible:outline-none" data-testid="filter-copy" @click="copyOne(index)">
            <ArrowUpToLine class="h-4 w-4" /> {{ t('filters.copyToQuery') }}
          </button>
          <button type="button" role="menuitem" class="flex items-center gap-2 rounded px-2 py-1.5 text-left text-destructive hover:bg-accent focus-visible:bg-accent focus-visible:outline-none" data-testid="filter-delete" @click="remove(index)">
            <Trash2 class="h-4 w-4" /> {{ t('ui.delete') }}
          </button>
        </div>
      </PopoverContent>
    </Popover>

    <Popover :open="adding" @update:open="(open: boolean) => (adding = open)">
      <PopoverAnchor as-child>
        <Button
          variant="ghost"
          size="sm"
          class="h-6 gap-1 px-2 text-xs text-muted-foreground"
          data-testid="filter-add"
          @click="adding = !adding"
        >
          <Plus class="h-3.5 w-3.5" />
          {{ t('filters.addFilter') }}
        </Button>
      </PopoverAnchor>
      <PopoverContent align="start" class="w-[min(34rem,90vw)] p-4">
        <FilterEditor v-if="adding" :fields="fields" :language="language" @save="addFilter" @cancel="adding = false" />
      </PopoverContent>
    </Popover>
  </div>
</template>

