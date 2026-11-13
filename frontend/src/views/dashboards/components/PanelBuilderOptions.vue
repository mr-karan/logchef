<script setup lang="ts">
import PanelResultOptions from "./PanelResultOptions.vue";
import type { PanelFrame } from "@/utils/panelFrame";
import { useI18n } from "vue-i18n";

import { computed, ref } from "vue";
import { X } from "lucide-vue-next";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import type { DashboardChartMark, DashboardPanelQuery, DashboardViz } from "@/api/dashboards";
import { panelBuilderKind, trimDashboardGroupBy } from "@/utils/dashboardPanels";

const { t } = useI18n();

// Per-kind option fields for the panel builder drawer. Kept as a separate
// component so PanelBuilderDrawer.vue doesn't grow a third editing surface.
// A query patch merges into the panel's query; a viz is replaced whole.
interface Props {
  query: DashboardPanelQuery;
  viz: DashboardViz;
  fieldSuggestions: string[];
  frame?: PanelFrame;
}
const props = defineProps<Props>();
const emit = defineEmits<{
  (e: "update:query", patch: Partial<DashboardPanelQuery>): void;
  (e: "update:viz", viz: DashboardViz): void;
}>();

const kind = computed(() => panelBuilderKind(props.viz));

const CHART_STYLES: { value: DashboardChartMark; label: string }[] = [
  { value: "line", get label() { return t('ui.line'); } },
  { value: "area", get label() { return t('ui.area'); } },
  { value: "bar", get label() { return t('ui.bars'); } },
];

const BAR_MODES: { value: "stacked" | "none"; label: string }[] = [
  { value: "stacked", get label() { return t('ui.stacked'); } },
  { value: "none", get label() { return t('ui.grouped'); } },
];
const BREAKDOWN_VIEWS: { value: "toplist" | "pie"; label: string }[] = [
  { value: "toplist", get label() { return t('ui.horizontalBars'); } },
  { value: "pie", get label() { return t('ui.donut'); } },
];

// The effective chart style, matching what PanelChart renders.
const effectiveMark = computed<DashboardChartMark>(() => (props.viz.type === "chart" ? props.viz.mark ?? "line" : "line"));
const effectiveStack = computed(() => (props.viz.type === "chart" ? props.viz.stack ?? "stacked" : "stacked"));

// Bars and areas stack by default, as schema 1 drew them.
function setMark(mark: DashboardChartMark) {
  if (props.viz.type === "chart") emit("update:viz", { ...props.viz, mark, stack: mark === "line" ? "none" : "stacked" });
}

function setStack(stack: "stacked" | "none") {
  if (props.viz.type === "chart") emit("update:viz", { ...props.viz, stack });
}

function setBreakdownView(view: "toplist" | "pie") {
  if (props.viz.type !== "pie" && props.viz.type !== "toplist") return;
  const { label, value, columns, format } = props.viz;
  emit("update:viz", view === "pie" ? { type: "pie", donut: true, label, value, columns, format } : { type: "toplist", label, value, columns, format });
}

// A blank group leaves the key out, which the server requires.
function setXScale(event: Event) {
  if (props.viz.type !== "chart" || !(event.target instanceof HTMLSelectElement)) return;
  const value = event.target.value;
  if (value === "auto" || value === "time" || value === "category") emit("update:viz", { ...props.viz, x_scale: value });
}

function setGroupBy(value: unknown) {
  const text = String(value ?? "");
  emit("update:query", { group_by: trimDashboardGroupBy(text) === "" ? undefined : text });
}

const columnInput = ref("");

// The <input max> attribute alone doesn't stop the browser from emitting a
// larger typed value on every keystroke (native max-clamping only kicks in on
// step-button use / form validation, not free typing), so clamp explicitly so
// the panel can never end up with a limit above the enforced max.
const MAX_BUILDER_TABLE_LIMIT = 10000;
const LIMIT_MIN = 1;
function clampLimit(value: unknown): number | undefined {
  const n = Number(value);
  if (!Number.isFinite(n) || n <= 0) return undefined;
  return Math.min(MAX_BUILDER_TABLE_LIMIT, Math.max(LIMIT_MIN, Math.round(n)));
}

const visibleColumns = computed(() => (props.viz.type === "table" ? props.viz.visible_columns ?? [] : []));

function setColumns(columns: string[]) {
  emit("update:viz", columns.length ? { ...props.viz, type: "table", visible_columns: columns } : { type: "table", ...(props.viz.type === "table" ? { columns: props.viz.columns } : {}) });
}

function addColumn() {
  const raw = columnInput.value.trim().replace(/,$/, "").trim();
  if (raw && !visibleColumns.value.includes(raw)) {
    setColumns([...visibleColumns.value, raw]);
  }
  columnInput.value = "";
}

function removeColumn(name: string) {
  setColumns(visibleColumns.value.filter((c) => c !== name));
}
</script>

<template>
  <!-- Chart: group-by field + render style. -->
  <div v-if="kind === 'chart'" class="space-y-4">
    <div v-if="query.mode === 'count'" class="space-y-1.5">
      <Label for="panel-groupby">{{ t('ui.groupBy2') }} <span class='text-muted-foreground'>{{ t('ui.optional2') }}</span></Label>
      <Input
        id="panel-groupby"
        :model-value="query.group_by ?? ''"
        list="panel-field-suggestions"
        :placeholder="t('ui.eGService')"
        @update:model-value="setGroupBy"
      />
      <datalist id="panel-field-suggestions">
        <option v-for="name in fieldSuggestions" :key="name" :value="name" />
      </datalist>
    </div>
    <div class="space-y-1.5">
      <Label>{{ t('ui.chartStyle') }}</Label>
      <div class="grid grid-cols-3 gap-1.5">
        <Button
          v-for="style in CHART_STYLES"
          :key="style.value"
          type="button"
          size="sm"
          :variant="effectiveMark === style.value ? 'default' : 'outline'"
          class="h-8 text-xs"
          @click="setMark(style.value)"
        >
          {{ style.label }}
        </Button>
      </div>
    </div>
    <div v-if="effectiveMark !== 'line'" class="space-y-1.5">
      <Label>{{ t('ui.barMode') }}</Label>
      <div class="grid grid-cols-2 gap-1.5">
        <Button
          v-for="mode in BAR_MODES"
          :key="mode.value"
          type="button"
          size="sm"
          :variant="effectiveStack === mode.value ? 'default' : 'outline'"
          class="h-8 text-xs"
          @click="setStack(mode.value)"
        >
          {{ mode.label }}
        </Button>
      </div>
    </div>
  </div>

  <!-- Breakdown: a required grouping field and a view-only selector. -->
  <div v-else-if="kind === 'breakdown'" class="space-y-4">
    <div v-if="query.mode === 'count'" class="space-y-1.5">
      <Label for="panel-breakdown-groupby">{{ t('ui.groupBy2') }} <span class='text-destructive'>{{ t('ui.required') }}</span></Label>
      <Input
        id="panel-breakdown-groupby"
        :model-value="query.group_by ?? ''"
        list="panel-field-suggestions"
        :placeholder="t('ui.eGService')"
        @update:model-value="setGroupBy"
      />
      <datalist id="panel-field-suggestions">
        <option v-for="name in fieldSuggestions" :key="name" :value="name" />
      </datalist>
    </div>
    <div class="space-y-1.5">
      <Label>{{ t('ui.view') }}</Label>
      <div class="grid grid-cols-2 gap-1.5">
        <Button v-for="view in BREAKDOWN_VIEWS" :key="view.value" type="button" size="sm"
          :variant="viz.type === view.value ? 'default' : 'outline'"
          class="h-8 text-xs" @click="setBreakdownView(view.value)">
          {{ view.label }}
        </Button>
      </div>
    </div>
  </div>


  <!-- Table: row limit + column subset. -->
  <div v-else-if="kind === 'table'" class="space-y-4">
    <div class="space-y-1.5">
      <Label for="panel-limit">{{ t('ui.rowLimit') }}</Label>
      <Input
        id="panel-limit"
        type="number"
        min="1"
        :max="MAX_BUILDER_TABLE_LIMIT"
        class="w-32"
        :model-value="query.limit ?? 1000"
        @update:model-value="(v) => emit('update:query', { limit: clampLimit(v) })"
      />
    </div>
    <div class="space-y-1.5">
      <Label for="panel-columns">{{ t('ui.columns') }} <span class='text-muted-foreground'>{{ t('ui.optionalAllIfEmpty') }}</span></Label>
      <div class="flex flex-wrap gap-1.5">
        <span
          v-for="col in visibleColumns"
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
        :placeholder="t('ui.typeAColumnAndPressEnter')"
        @keydown.enter.prevent="addColumn"
        @keydown="(e: KeyboardEvent) => e.key === ',' && (e.preventDefault(), addColumn())"
      />
      <datalist id="panel-field-suggestions">
        <option v-for="name in fieldSuggestions" :key="name" :value="name" />
      </datalist>
    </div>
  </div>
  <div v-if="viz.type === 'chart'" class="mt-4 space-y-3">
    <div class="space-y-1"><Label for="chart-scale">X scale</Label><select id="chart-scale" class="w-full rounded border bg-background p-1.5 text-xs" :value="viz.x_scale ?? 'auto'" @change="setXScale"><option value="auto">Auto, time for dates</option><option value="time">Time, oldest first</option><option value="category">Category, query order</option></select></div>
    <label class="flex items-center gap-2 text-xs"><input type="checkbox" :checked="viz.show_values ?? false" @change="emit('update:viz', { ...viz, show_values: !viz.show_values })" />Show values</label>
    <label class="flex items-center gap-2 text-xs"><input type="checkbox" :checked="viz.legend ?? true" @change="emit('update:viz', { ...viz, legend: !(viz.legend ?? true) })" />Show legend</label>
    <label class="flex items-center gap-2 text-xs"><input type="checkbox" :checked="viz.orientation === 'horizontal'" @change="emit('update:viz', { ...viz, orientation: viz.orientation === 'horizontal' ? 'vertical' : 'horizontal' })" />Horizontal</label>
  </div>
  <div v-if="viz.type === 'pie'" class="mt-4 space-y-2"><label class="flex items-center gap-2 text-xs"><input type="checkbox" :checked="viz.donut ?? false" @change="emit('update:viz', { ...viz, donut: !viz.donut })" />Donut</label><Label for="pie-min">Fold slices below %</Label><Input id="pie-min" type="number" min="0.01" max="99.99" step="any" :model-value="viz.min_slice_percent ?? ''" @update:model-value="(v) => viz.type === 'pie' && emit('update:viz', { ...viz, min_slice_percent: v === '' ? undefined : Number(v) })" /></div>
  <div v-if="viz.type === 'toplist'" class="mt-4 space-y-1"><Label for="toplist-max">Maximum rows</Label><Input id="toplist-max" type="number" min="1" max="10000" :model-value="viz.max_rows ?? 20" @update:model-value="(v) => viz.type === 'toplist' && emit('update:viz', { ...viz, max_rows: clampLimit(v) })" /></div>
  <PanelResultOptions class="mt-4" :query="query" :viz="viz" :frame="frame" @update:viz="(v) => emit('update:viz', v)" />

</template>
