<script setup lang="ts">
import { computed, ref } from "vue";
import { X } from "lucide-vue-next";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import type { DashboardPanelOptions, DashboardPanelType } from "@/api/dashboards";

// Per-type option fields for the panel builder drawer. Kept as a separate
// component so PanelBuilderDrawer.vue doesn't grow a third editing surface —
// this one only ever patches `options`, never the panel's other fields.
interface Props {
  type: DashboardPanelType;
  options: DashboardPanelOptions;
  fieldSuggestions: string[];
}
const props = defineProps<Props>();
const emit = defineEmits<{
  (e: "update:options", patch: Partial<DashboardPanelOptions>): void;
}>();

const CHART_STYLES: { value: NonNullable<DashboardPanelOptions["chart"]>; label: string }[] = [
  { value: "line", label: "Line" },
  { value: "area", label: "Area" },
  { value: "bars", label: "Bars" },
];

const BAR_MODES: { value: NonNullable<DashboardPanelOptions["bar_mode"]>; label: string }[] = [
  { value: "stacked", label: "Stacked" },
  { value: "grouped", label: "Grouped" },
];
const BREAKDOWN_VIEWS: { value: NonNullable<DashboardPanelOptions["breakdown_view"]>; label: string }[] = [
  { value: "horizontal-bars", label: "Horizontal bars" },
  { value: "donut", label: "Donut" },
];

// The effective chart style, matching what PanelTimeseries renders.
const effectiveChart = computed<NonNullable<DashboardPanelOptions["chart"]>>(
  () => props.options.chart ?? "line"
);

const columnInput = ref("");

// The <input max="1000"> attribute alone doesn't stop the browser from
// emitting a larger typed value on every keystroke (native max-clamping only
// kicks in on step-button use / form validation, not free typing) - clamp
// explicitly so the panel can never end up with a limit above the enforced
// max.
const LIMIT_MIN = 1;
const LIMIT_MAX = 1000;
function clampLimit(value: unknown): number | undefined {
  const n = Number(value);
  if (!Number.isFinite(n) || n <= 0) return undefined;
  return Math.min(LIMIT_MAX, Math.max(LIMIT_MIN, Math.round(n)));
}

function addColumn() {
  const raw = columnInput.value.trim().replace(/,$/, "").trim();
  if (raw) {
    const cols = props.options.columns ?? [];
    if (!cols.includes(raw)) {
      emit("update:options", { columns: [...cols, raw] });
    }
  }
  columnInput.value = "";
}

function removeColumn(name: string) {
  emit("update:options", { columns: (props.options.columns ?? []).filter((c) => c !== name) });
}
</script>

<template>
  <!-- Timeseries: group-by field + chart render style. -->
  <div v-if="type === 'timeseries'" class="space-y-4">
    <div class="space-y-1.5">
      <Label for="panel-groupby">Group by <span class="text-muted-foreground">(optional)</span></Label>
      <Input
        id="panel-groupby"
        :model-value="options.group_by ?? ''"
        list="panel-field-suggestions"
        placeholder="e.g. service"
        @update:model-value="(v) => emit('update:options', { group_by: String(v ?? '') })"
      />
      <datalist id="panel-field-suggestions">
        <option v-for="name in fieldSuggestions" :key="name" :value="name" />
      </datalist>
    </div>
    <div class="space-y-1.5">
      <Label>Chart style</Label>
      <div class="grid grid-cols-3 gap-1.5">
        <Button
          v-for="style in CHART_STYLES"
          :key="style.value"
          type="button"
          size="sm"
          :variant="effectiveChart === style.value ? 'default' : 'outline'"
          class="h-8 text-xs"
          @click="emit('update:options', { chart: style.value })"
        >
          {{ style.label }}
        </Button>
      </div>
    </div>
    <div v-if="effectiveChart === 'bars'" class="space-y-1.5">
      <Label>Bar mode</Label>
      <div class="grid grid-cols-2 gap-1.5">
        <Button
          v-for="mode in BAR_MODES"
          :key="mode.value"
          type="button"
          size="sm"
          :variant="(options.bar_mode ?? 'stacked') === mode.value ? 'default' : 'outline'"
          class="h-8 text-xs"
          @click="emit('update:options', { bar_mode: mode.value })"
        >
          {{ mode.label }}
        </Button>
      </div>
    </div>
  </div>

  <!-- Breakdown: a required grouping field and a view-only selector. -->
  <div v-else-if="type === 'breakdown'" class="space-y-4">
    <div class="space-y-1.5">
      <Label for="panel-breakdown-groupby">Group by <span class="text-destructive">(required)</span></Label>
      <Input
        id="panel-breakdown-groupby"
        :model-value="options.group_by ?? ''"
        list="panel-field-suggestions"
        placeholder="e.g. service"
        @update:model-value="(v) => emit('update:options', { group_by: String(v ?? '') })"
      />
      <datalist id="panel-field-suggestions">
        <option v-for="name in fieldSuggestions" :key="name" :value="name" />
      </datalist>
    </div>
    <div class="space-y-1.5">
      <Label>View</Label>
      <div class="grid grid-cols-2 gap-1.5">
        <Button v-for="view in BREAKDOWN_VIEWS" :key="view.value" type="button" size="sm"
          :variant="(options.breakdown_view ?? 'horizontal-bars') === view.value ? 'default' : 'outline'"
          class="h-8 text-xs" @click="emit('update:options', { breakdown_view: view.value })">
          {{ view.label }}
        </Button>
      </div>
    </div>
  </div>

  <!-- Stat: no options yet. -->
  <p v-else-if="type === 'stat'" class="text-xs text-muted-foreground">
    Stat panels show the total match count over the selected time range. No options yet.
  </p>

  <!-- Table: row limit + column subset. -->
  <div v-else-if="type === 'table'" class="space-y-4">
    <div class="space-y-1.5">
      <Label for="panel-limit">Row limit</Label>
      <Input
        id="panel-limit"
        type="number"
        min="1"
        max="1000"
        class="w-32"
        :model-value="options.limit ?? 50"
        @update:model-value="(v) => emit('update:options', { limit: clampLimit(v) })"
      />
    </div>
    <div class="space-y-1.5">
      <Label for="panel-columns">Columns <span class="text-muted-foreground">(optional; all if empty)</span></Label>
      <div class="flex flex-wrap gap-1.5">
        <span
          v-for="col in options.columns ?? []"
          :key="col"
          class="inline-flex items-center gap-1 rounded bg-muted px-2 py-0.5 text-xs"
        >
          {{ col }}
          <button type="button" class="text-muted-foreground hover:text-foreground" @click="removeColumn(col)">
            <X class="h-3 w-3" />
          </button>
        </span>
      </div>
      <Input
        id="panel-columns"
        v-model="columnInput"
        list="panel-field-suggestions"
        placeholder="Type a column and press Enter"
        @keydown.enter.prevent="addColumn"
        @keydown="(e: KeyboardEvent) => e.key === ',' && (e.preventDefault(), addColumn())"
      />
      <datalist id="panel-field-suggestions">
        <option v-for="name in fieldSuggestions" :key="name" :value="name" />
      </datalist>
    </div>
  </div>
</template>
