<script setup lang="ts">
import PanelPresentationOptions from "./PanelPresentationOptions.vue";
import { useI18n } from "vue-i18n";
import { computed } from "vue";
import type { ColumnKind, DashboardPanelQuery, DashboardViz, NumberFormat, StatCompare, StatReduce } from "@/api/dashboards";
import { suggestKind, type PanelFrame } from "@/utils/panelFrame";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";

const { t } = useI18n();
const props = defineProps<{ query: DashboardPanelQuery; viz: DashboardViz; frame?: PanelFrame }>();
const emit = defineEmits<{ "update:viz": [viz: DashboardViz] }>();
const columns = computed(() => props.frame?.columns ?? []);
const missingSeries = computed(() => { const viz = props.viz; return viz.type === "chart" && viz.series && !columns.value.some(c => c.name === viz.series); });
const missingX = computed(() => {
  const viz = props.viz;
  return viz.type === "chart" && viz.x && !columns.value.some((c) => c.name === viz.x);
});
const enumLabels: Record<string, string> = {"time": "pages.time", "date": "ui.date", "number": "ui.number", "category": "dashboardBuilder.Category", "bool": "dashboardBuilder.Bool", "other": "dashboardBuilder.Other", "first": "dashboardBuilder.First", "last": "dashboardBuilder.Last", "sum": "dashboardBuilder.Sum", "avg": "dashboardBuilder.Avg", "min": "dashboardBuilder.Min", "max": "dashboardBuilder.Max", "auto": "dashboardBuilder.Auto", "standard": "dashboardBuilder.Standard", "compact": "ui.compact", "none": "ui.none", "percent": "dashboardBuilder.Percent", "bytes": "dashboardBuilder.Bytes", "duration_ms": "dashboardBuilder.DurationMs", "duration_s": "dashboardBuilder.DurationS"};
const kinds: ColumnKind[] = ["time", "date", "number", "category", "bool", "other"];
const compareModes: StatCompare["mode"][] = ["none", "previous_row"];
const compareGoods: NonNullable<StatCompare["good"]>[] = ["up", "down", "neutral"];
const reducers: StatReduce[] = ["last", "first", "sum", "avg", "min", "max"];
const notations: NonNullable<NumberFormat["notation"]>[] = ["auto", "standard", "compact"];
const units: NonNullable<NumberFormat["unit"]>[] = ["none", "percent", "bytes", "duration_ms", "duration_s"];
const format = computed(() => props.viz.type !== "table" ? props.viz.format ?? {} : {});
function kindFor(name: string): ColumnKind {
  const viz = props.viz;
  const confirmed = viz.columns && Object.hasOwn(viz.columns, name) ? viz.columns[name] : undefined;
  const column = columns.value.find((c) => c.name === name);
  return confirmed?.kind ?? (column?.backendType ? column.kind : suggestKind(props.frame?.rawRows.slice(0, 100).map((r) => r[name]) ?? []));
}
function setKind(name: string, kind: ColumnKind) {
  emit("update:viz", { ...props.viz, columns: { ...props.viz.columns, [name]: { kind, confidence: "confirmed" } } });
}
function setRole(role: "value" | "order_by", name: string) {
  if (props.viz.type !== "stat") return;
  const viz = { ...props.viz };
  const previous = viz[role];
  const otherRole = role === "value" ? "order_by" : "value";
  if (previous && previous !== name && previous !== viz[otherRole] && viz.columns) {
    viz.columns = { ...viz.columns };
    delete viz.columns[previous];
    if (!Object.keys(viz.columns).length) delete viz.columns;
  }
  if (name) {
    viz[role] = name;
    if (columns.value.find((c) => c.name === name)?.backendType === null) viz.columns = { ...viz.columns, [name]: { kind: kindFor(name), confidence: "confirmed" } };
  } else delete viz[role];
  emit("update:viz", viz);
}
function setChartRole(role: "x" | "series" | "label" | "value", name: string) {
  const viz = props.viz;
  if (viz.type !== "chart" && viz.type !== "pie" && viz.type !== "toplist") return;
  const next = { ...viz, [role]: name || undefined };
  if (name && columns.value.find((c) => c.name === name)?.backendType === null) next.columns = { ...next.columns, [name]: { kind: kindFor(name), confidence: "confirmed" } };
  pruneColumns(next);
  emit("update:viz", next);
}
function toggleY(name: string) {
  const viz = props.viz;
  if (viz.type !== "chart") return;
  const y = viz.y?.includes(name) ? viz.y.filter((v) => v !== name) : [...(viz.y ?? []), name];
  const columns = props.frame?.columns.find((c) => c.name === name)?.backendType === null ? { ...viz.columns, [name]: { kind: kindFor(name), confidence: "confirmed" as const } } : viz.columns;
  const next = { ...viz, y, columns };
  pruneColumns(next);
  emit("update:viz", next);
}
function pruneColumns(viz: DashboardViz) {
  const names = viz.type === "chart" ? [viz.x, ...(viz.y ?? []), viz.series] : viz.type === "pie" || viz.type === "toplist" ? [viz.label, viz.value] : [];
  viz.columns = Object.fromEntries(Object.entries(viz.columns ?? {}).filter(([name]) => names.includes(name)));
  if (!Object.keys(viz.columns).length) delete viz.columns;
}
function moveY(index: number, direction: number) {
  if (props.viz.type !== "chart") return;
  const y = [...(props.viz.y ?? [])];
  const target = index + direction;
  if (target < 0 || target >= y.length) return;
  [y[index], y[target]] = [y[target], y[index]];
  emit("update:viz", { ...props.viz, y });
}
function selectValue(event: Event): string {
  return event.target instanceof HTMLSelectElement ? event.target.value : "";
}
function setCompareMode(mode: string) {
  if (props.viz.type !== "stat" || (mode !== "none" && mode !== "previous_row")) return;
  const viz = { ...props.viz };
  if (mode === "none") delete viz.compare;
  else viz.compare = { ...viz.compare, mode };
  emit("update:viz", viz);
}
function toggleSparkline() {
  if (props.viz.type !== "stat") return;
  const viz = { ...props.viz };
  if (viz.sparkline) delete viz.sparkline;
  else viz.sparkline = true;
  emit("update:viz", viz);
}
function setGood(good: string) {
  if (props.viz.type === "stat" && (good === "up" || good === "down" || good === "neutral")) emit("update:viz", { ...props.viz, compare: { mode: props.viz.compare?.mode ?? "none", good } });
}
function setReduce(reduce: StatReduce) {
  if (props.viz.type === "stat") emit("update:viz", { ...props.viz, reduce });
}
function setFormat(patch: Partial<NumberFormat>) {
  if (props.viz.type === "table") return;
  const next = { ...format.value, ...patch };
  for (const key of Object.keys(next)) if (Reflect.get(next, key) === undefined) Reflect.deleteProperty(next, key);
  const viz = { ...props.viz };
  if (Object.keys(next).length) viz.format = next;
  else delete viz.format;
  emit("update:viz", viz);
}
function setDecimals(value: unknown) {
  setFormat({ decimals: value === "" ? undefined : Math.min(10, Math.max(0, Math.round(Number(value)))) });
}
function setScale(value: unknown) {
  const scale = Number(value);
  if (value === "") setFormat({ scale: undefined });
  else if (Number.isFinite(scale) && scale !== 0) setFormat({ scale });
}
</script>

<template>
  <div class="space-y-4">
    <PanelPresentationOptions :viz="viz" :frame="frame" @update:viz="emit('update:viz', $event)" />
    <template v-if="query.mode === 'result' && viz.type === 'chart'">
      <Label for="chart-x">{{ t('dashboardBuilder.XColumn') }}</Label>
      <select id="chart-x" class="w-full rounded border bg-background p-1.5 text-xs" :value="viz.x ?? ''" @change="setChartRole('x', selectValue($event))"><option value="">{{ t('dashboardBuilder.SelectColumn') }}</option><option v-for="c in columns" :key="c.name" :value="c.name">{{ c.name }}</option><option v-if="missingX" :value="viz.x">{{ viz.x }} {{ t('dashboardBuilder.Missing') }}</option></select>
      <fieldset class="space-y-1"><legend class="text-xs">{{ t('dashboardBuilder.YColumnsInLegendOrder') }}</legend>
        <div v-for="(name, i) in viz.y ?? []" :key="name" class="flex items-center gap-2 text-xs"><span class="flex-1">{{ name }}</span><button type="button" :aria-label="t('dashboardBuilder.MoveNameUp', { name })" :disabled="i === 0" @click="moveY(i, -1)">↑</button><button type="button" :aria-label="t('dashboardBuilder.MoveNameDown', { name })" :disabled="i === (viz.y?.length ?? 0) - 1" @click="moveY(i, 1)">↓</button><button type="button" :aria-label="t('dashboardBuilder.RemoveName', { name })" @click="toggleY(name)">×</button></div>
        <label v-for="c in columns" :key="c.name" class="flex items-center gap-2 text-xs"><input type="checkbox" :checked="viz.y?.includes(c.name)" @change="toggleY(c.name)" />{{ c.name }}</label>
      </fieldset>
      <Label for="chart-series">{{ t('dashboardBuilder.SeriesColumn') }}</Label>
      <select id="chart-series" class="w-full rounded border bg-background p-1.5 text-xs" :value="viz.series ?? ''" @change="setChartRole('series', selectValue($event))"><option value="">{{ t('dashboardBuilder.OneSeriesPerYColumn') }}</option><option v-for="c in columns" :key="c.name" :value="c.name">{{ c.name }}</option><option v-if="missingSeries" :value="viz.series">{{ viz.series }} {{ t('dashboardBuilder.Missing') }}</option></select>
    </template>
    <template v-if="query.mode === 'result' && (viz.type === 'pie' || viz.type === 'toplist')">
      <div v-for="role in (['label', 'value'] as const)" :key="role" class="space-y-1"><Label :for="`list-${role}`">{{ role === 'label' ? t('dashboardBuilder.LabelColumn') : t('dashboardBuilder.ValueColumn') }}</Label><select :id="`list-${role}`" class="w-full rounded border bg-background p-1.5 text-xs" :value="viz[role] ?? ''" @change="setChartRole(role, selectValue($event))"><option value="">{{ t('dashboardBuilder.SelectColumn') }}</option><option v-for="c in columns" :key="c.name" :value="c.name">{{ c.name }}</option></select></div>
    </template>
    <template v-if="viz.type === 'stat'">
      <template v-if="query.mode === 'result'">
        <div class="space-y-1.5"><Label for="stat-value">{{ t('dashboardBuilder.ValueColumn') }}</Label>
          <Input id="stat-value" :model-value="viz.value ?? ''" list="result-columns" @update:model-value="(v) => setRole('value', String(v))" />
        </div>
        <div class="space-y-1.5"><Label for="stat-order">{{ t('dashboardBuilder.OrderBy') }}</Label>
          <Input id="stat-order" :model-value="viz.order_by ?? ''" list="result-columns" @update:model-value="(v) => setRole('order_by', String(v))" />
        </div>
      </template>
      <div class="space-y-1.5"><Label>{{ t('dashboardBuilder.Reduce') }}</Label>
        <div class="flex flex-wrap gap-1"><button v-for="reduce in reducers" :key="reduce" type="button" class="rounded border px-2 py-1 text-xs aria-pressed:bg-primary aria-pressed:text-primary-foreground" :aria-pressed="(viz.reduce ?? (query.mode === 'count' ? 'sum' : 'last')) === reduce" @click="setReduce(reduce)">{{ t(enumLabels[reduce]) }}</button></div>
      </div>
      <p v-if="query.mode === 'result' && !viz.order_by && (viz.reduce ?? 'last') !== 'sum' && ['first', 'last'].includes(viz.reduce ?? 'last') && (frame?.rows.length ?? 0) > 1" class="text-xs text-amber-600">{{ t('dashboardBuilder.SetOrderByToSelectTheFirstOrLastRowChronologically') }}</p>
    </template>
    <template v-if="viz.type === 'stat'">
      <Label for="stat-compare">{{ t('dashboardBuilder.Comparison') }}</Label>
      <select id="stat-compare" class="w-full rounded border bg-background p-1.5 text-xs" :value="viz.compare?.mode ?? 'none'" @change="setCompareMode(selectValue($event))"><option v-for="mode in compareModes" :key="mode" :value="mode">{{ t(mode === 'none' ? 'ui.none' : 'dashboardBuilder.PreviousRow') }}</option></select>
      <template v-if="viz.compare?.mode === 'previous_row'">
        <Label for="stat-good">{{ t('dashboardBuilder.GoodDirection') }}</Label>
        <select id="stat-good" class="w-full rounded border bg-background p-1.5 text-xs" :value="viz.compare.good ?? 'neutral'" @change="setGood(selectValue($event))"><option v-for="good in compareGoods" :key="good" :value="good">{{ t(`dashboardBuilder.Good_${good}`) }}</option></select>
        <p class="text-xs text-muted-foreground">{{ t('dashboardBuilder.NeighborWarning') }}</p>
        <p v-if="!['first', 'last'].includes(viz.reduce ?? (query.mode === 'count' ? 'sum' : 'last'))" role="alert" class="text-xs text-amber-600">{{ t('dashboardBuilder.CompareReducerWarning') }}</p>
      </template>
      <label class="flex items-center gap-2 text-xs"><input type="checkbox" :checked="viz.sparkline ?? false" @change="toggleSparkline" />{{ t('dashboardBuilder.Sparkline') }}</label>
      <p v-if="query.mode === 'result' && !viz.order_by && (viz.sparkline || viz.compare?.mode === 'previous_row')" role="alert" class="text-xs text-amber-600">{{ t('dashboardBuilder.CompareOrderWarning') }}</p>
    </template>
    <template v-if="viz.type !== 'table'">
      <fieldset class="space-y-3 rounded border p-3"><legend class="px-1 text-xs">{{ t('dashboardBuilder.NumberFormat') }}</legend>
        <div class="flex flex-wrap gap-1"><button v-for="notation in notations" :key="notation" type="button" class="rounded border px-2 py-1 text-xs aria-pressed:bg-primary aria-pressed:text-primary-foreground" :aria-pressed="(format.notation ?? 'auto') === notation" @click="setFormat({ notation })">{{ t(enumLabels[notation]) }}</button></div>
        <div class="grid grid-cols-2 gap-2">
          <div><Label for="stat-decimals">{{ t('dashboardBuilder.Decimals') }}</Label><Input id="stat-decimals" type="number" min="0" max="10" :model-value="format.decimals ?? ''" :placeholder="t('dashboardBuilder.AutomaticPrecision')" @update:model-value="setDecimals" /></div>
          <div><Label for="stat-scale">{{ t('dashboardBuilder.Scale') }}</Label><Input id="stat-scale" type="number" step="any" :model-value="format.scale ?? 1" @update:model-value="setScale" /></div>
          <div><Label for="stat-prefix">{{ t('dashboardBuilder.Prefix') }}</Label><Input id="stat-prefix" :model-value="format.prefix ?? ''" :maxlength="16" @update:model-value="(v) => setFormat({ prefix: String(v) })" /></div>
          <div><Label for="stat-suffix">{{ t('dashboardBuilder.Suffix') }}</Label><Input id="stat-suffix" :model-value="format.suffix ?? ''" :maxlength="16" @update:model-value="(v) => setFormat({ suffix: String(v) })" /></div>
        </div>
        <label class="flex items-center gap-2 text-xs"><input type="checkbox" :checked="format.grouping ?? true" @change="setFormat({ grouping: !(format.grouping ?? true) })" />{{ t('dashboardBuilder.GroupThousands') }}</label>
        <div class="flex flex-wrap gap-1"><button v-for="unit in units" :key="unit" type="button" class="rounded border px-2 py-1 text-xs aria-pressed:bg-primary aria-pressed:text-primary-foreground" :aria-pressed="(format.unit ?? 'none') === unit" @click="setFormat({ unit })">{{ t(enumLabels[unit]) }}</button></div>
      </fieldset>
    </template>
    <datalist id="result-columns"><option v-for="column in columns" :key="column.name" :value="column.name" /></datalist>
    <fieldset v-if="query.mode === 'result' && columns.length" class="space-y-2"><legend class="text-xs font-medium">{{ t('dashboardBuilder.ColumnKinds') }}</legend>
      <div v-for="column in columns" :key="column.name" class="space-y-1">
        <Label :for="`kind-${column.name}`" class="text-xs">{{ column.name }} <span class="text-muted-foreground">{{ viz.columns && Object.hasOwn(viz.columns, column.name) ? t('dashboardBuilder.Confirmed') : column.backendType ?? t('dashboardBuilder.Guessed') }}</span></Label>
        <div :id="`kind-${column.name}`" class="flex flex-wrap gap-1"><button v-for="kind in kinds" :key="kind" type="button" class="rounded border px-1.5 py-0.5 text-xs aria-pressed:bg-primary aria-pressed:text-primary-foreground" :aria-pressed="kindFor(column.name) === kind" @click="setKind(column.name, kind)">{{ t(enumLabels[kind]) }}</button></div>
      </div>
    </fieldset>
  </div>
</template>
