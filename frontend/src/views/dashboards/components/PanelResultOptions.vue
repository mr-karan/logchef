<script setup lang="ts">
import { computed } from "vue";
import type { ColumnKind, DashboardPanelQuery, DashboardViz, NumberFormat, StatReduce } from "@/api/dashboards";
import { suggestKind, type PanelFrame } from "@/utils/panelFrame";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";

const props = defineProps<{ query: DashboardPanelQuery; viz: DashboardViz; frame?: PanelFrame }>();
const emit = defineEmits<{ "update:viz": [viz: DashboardViz] }>();
const columns = computed(() => props.frame?.columns ?? []);
const kinds: ColumnKind[] = ["time", "date", "number", "category", "bool", "other"];
const reducers: StatReduce[] = ["last", "first", "sum", "avg", "min", "max"];
const notations: NonNullable<NumberFormat["notation"]>[] = ["auto", "standard", "compact"];
const units: NonNullable<NumberFormat["unit"]>[] = ["none", "percent", "bytes", "duration_ms", "duration_s"];
const format = computed(() => props.viz.type === "stat" ? props.viz.format ?? {} : {});
function kindFor(name: string): ColumnKind {
  const viz = props.viz;
  const confirmed = viz.type === "stat" || viz.type === "table" ? viz.columns?.[name] : undefined;
  const column = columns.value.find((c) => c.name === name);
  return confirmed?.kind ?? (column?.backendType ? column.kind : suggestKind(props.frame?.rawRows.slice(0, 100).map((r) => r[name]) ?? []));
}
function setKind(name: string, kind: ColumnKind) {
  if (props.viz.type !== "stat" && props.viz.type !== "table") return;
  emit("update:viz", { ...props.viz, columns: { ...props.viz.columns, [name]: { kind, confidence: "confirmed" } } });
}
function setRole(role: "value" | "order_by", name: string) {
  if (props.viz.type !== "stat") return;
  const viz = { ...props.viz };
  if (name) {
    viz[role] = name;
    if (columns.value.find((c) => c.name === name)?.backendType === null) viz.columns = { ...viz.columns, [name]: { kind: kindFor(name), confidence: "confirmed" } };
  } else delete viz[role];
  emit("update:viz", viz);
}
function setReduce(reduce: StatReduce) {
  if (props.viz.type === "stat") emit("update:viz", { ...props.viz, reduce });
}
function setFormat(patch: Partial<NumberFormat>) {
  if (props.viz.type !== "stat") return;
  const next = { ...format.value, ...patch };
  for (const key of Object.keys(next)) if (Reflect.get(next, key) === undefined) Reflect.deleteProperty(next, key);
  emit("update:viz", { ...props.viz, format: next });
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
    <template v-if="viz.type === 'stat'">
      <template v-if="query.mode === 'result'">
        <div class="space-y-1.5"><Label for="stat-value">Value column</Label>
          <Input id="stat-value" :model-value="viz.value ?? ''" list="result-columns" @update:model-value="(v) => setRole('value', String(v))" />
        </div>
        <div class="space-y-1.5"><Label for="stat-order">Order by</Label>
          <Input id="stat-order" :model-value="viz.order_by ?? ''" list="result-columns" @update:model-value="(v) => setRole('order_by', String(v))" />
        </div>
      </template>
      <div class="space-y-1.5"><Label>Reduce</Label>
        <div class="flex flex-wrap gap-1"><button v-for="reduce in reducers" :key="reduce" type="button" class="rounded border px-2 py-1 text-xs aria-pressed:bg-primary aria-pressed:text-primary-foreground" :aria-pressed="(viz.reduce ?? (query.mode === 'count' ? 'sum' : 'last')) === reduce" @click="setReduce(reduce)">{{ reduce }}</button></div>
      </div>
      <p v-if="query.mode === 'result' && !viz.order_by && (viz.reduce ?? 'last') !== 'sum' && ['first', 'last'].includes(viz.reduce ?? 'last') && (frame?.rows.length ?? 0) > 1" class="text-xs text-amber-600">Set Order by to select the first or last row chronologically.</p>
      <fieldset class="space-y-3 rounded border p-3"><legend class="px-1 text-xs">Number format</legend>
        <div class="flex flex-wrap gap-1"><button v-for="notation in notations" :key="notation" type="button" class="rounded border px-2 py-1 text-xs aria-pressed:bg-primary aria-pressed:text-primary-foreground" :aria-pressed="(format.notation ?? 'auto') === notation" @click="setFormat({ notation })">{{ notation }}</button></div>
        <div class="grid grid-cols-2 gap-2">
          <div><Label for="stat-decimals">Decimals</Label><Input id="stat-decimals" type="number" min="0" max="10" :model-value="format.decimals ?? ''" placeholder="Up to 2" @update:model-value="setDecimals" /></div>
          <div><Label for="stat-scale">Scale</Label><Input id="stat-scale" type="number" step="any" :model-value="format.scale ?? 1" @update:model-value="setScale" /></div>
          <div><Label for="stat-prefix">Prefix</Label><Input id="stat-prefix" :model-value="format.prefix ?? ''" :maxlength="16" @update:model-value="(v) => setFormat({ prefix: String(v) })" /></div>
          <div><Label for="stat-suffix">Suffix</Label><Input id="stat-suffix" :model-value="format.suffix ?? ''" :maxlength="16" @update:model-value="(v) => setFormat({ suffix: String(v) })" /></div>
        </div>
        <label class="flex items-center gap-2 text-xs"><input type="checkbox" :checked="format.grouping ?? true" @change="setFormat({ grouping: !(format.grouping ?? true) })" />Group thousands</label>
        <div class="flex flex-wrap gap-1"><button v-for="unit in units" :key="unit" type="button" class="rounded border px-2 py-1 text-xs aria-pressed:bg-primary aria-pressed:text-primary-foreground" :aria-pressed="(format.unit ?? 'none') === unit" @click="setFormat({ unit })">{{ unit }}</button></div>
      </fieldset>
    </template>
    <datalist id="result-columns"><option v-for="column in columns" :key="column.name" :value="column.name" /></datalist>
    <fieldset v-if="query.mode === 'result' && columns.length" class="space-y-2"><legend class="text-xs font-medium">Column kinds</legend>
      <div v-for="column in columns" :key="column.name" class="space-y-1">
        <Label :for="`kind-${column.name}`" class="text-xs">{{ column.name }} <span class="text-muted-foreground">{{ (viz.type === 'stat' || viz.type === 'table') && viz.columns?.[column.name] ? 'confirmed' : column.backendType ?? 'guessed' }}</span></Label>
        <div :id="`kind-${column.name}`" class="flex flex-wrap gap-1"><button v-for="kind in kinds" :key="kind" type="button" class="rounded border px-1.5 py-0.5 text-xs aria-pressed:bg-primary aria-pressed:text-primary-foreground" :aria-pressed="kindFor(column.name) === kind" @click="setKind(column.name, kind)">{{ kind }}</button></div>
      </div>
    </fieldset>
  </div>
</template>
