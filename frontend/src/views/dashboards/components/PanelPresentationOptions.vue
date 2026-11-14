<script setup lang="ts">
import { computed } from "vue";
import { useI18n } from "vue-i18n";
import type { ColumnFormat, DashboardViz, NumberFormat, PaletteColor, TableRule, TimeFormat, ThresholdStep } from "@/api/dashboards";
import { paletteColors } from "@/utils/panelPresentation";
import type { PanelFrame } from "@/utils/panelFrame";
import { Input } from "@/components/ui/input";
import { Button } from "@/components/ui/button";
const props = defineProps<{ viz: DashboardViz; frame?: PanelFrame }>();
const emit = defineEmits<{ "update:viz": [viz: DashboardViz] }>();
const { t } = useI18n();
const thresholds = computed(() => props.viz.type === "stat" || props.viz.type === "chart" ? props.viz.thresholds ?? [] : []);
const rules = computed(() => props.viz.type === "table" ? props.viz.rules ?? [] : []);
const ops: TableRule["op"][] = [">", ">=", "<", "<=", "=", "!=", "contains", "not_contains", "empty", "not_empty"];
const notations: NonNullable<NumberFormat["notation"]>[] = ["auto", "standard", "compact"];
const units: NonNullable<NumberFormat["unit"]>[] = ["none", "percent", "bytes", "duration_ms", "duration_s"];
const unitKeys = { none: "ui.none", percent: "dashboardBuilder.Percent", bytes: "dashboardBuilder.Bytes", duration_ms: "dashboardBuilder.DurationMs", duration_s: "dashboardBuilder.DurationS" };
const notationKeys = { auto: "dashboardBuilder.Auto", standard: "dashboardBuilder.Standard", compact: "ui.compact" };
const dates = ["iso", "medium", "none"] as const;
const times = ["none", "minutes", "seconds", "millis"] as const;
function selected(event: Event) { return event.target instanceof HTMLSelectElement ? event.target.value : ""; }
function setThresholds(steps: ThresholdStep[]) {
 if (props.viz.type !== "stat" && props.viz.type !== "chart") return;
 const viz = { ...props.viz };
 if (steps.length) viz.thresholds = steps; else { delete viz.thresholds; if (viz.type === "stat") delete viz.color_mode; }
 emit("update:viz", viz);
}
function threshold(index: number, patch: Partial<ThresholdStep>) { setThresholds(thresholds.value.map((s,i) => i === index ? { ...s, ...patch } : s)); }
function colorMode(mode: "value" | "background") {
 if (props.viz.type !== "stat") return;
 const viz = { ...props.viz };
 if (mode === "background") viz.color_mode = mode; else delete viz.color_mode;
 emit("update:viz", viz);
}
function setRules(next: TableRule[]) {
 if (props.viz.type !== "table") return;
 const viz = { ...props.viz };
 if (next.length) viz.rules = next; else delete viz.rules;
 emit("update:viz", viz);
}
function rule(index: number, patch: Partial<TableRule>) {
 setRules(rules.value.map((r,i) => {
  if (i !== index) return r;
  const next = { ...r, ...patch };
  if (next.op === "empty" || next.op === "not_empty") delete next.value;
  return next;
 }));
}
function color(event: Event, update: (color: PaletteColor) => void) { const value = paletteColors.find(c => c === selected(event)); if (value) update(value); }
function op(event: Event, index: number) { const value = ops.find(o => o === selected(event)); if (value) rule(index, { op: value, value: "" }); }
function numberFor(name: string): NumberFormat { const f = formatFor(name); return f?.kind === "time" ? {} : f ?? {}; }
function timeFor(name: string): Partial<TimeFormat> { const f = formatFor(name); return f?.kind === "time" ? f : {}; }
function formatFor(name: string) { return props.viz.type === "table" ? props.viz.column_formats?.[name] : undefined; }
function setFormat(name: string, format?: ColumnFormat) {
 if (props.viz.type !== "table") return;
 const formats = { ...props.viz.column_formats };
 if (format) formats[name] = format; else delete formats[name];
 const viz = { ...props.viz };
 if (Object.keys(formats).length) viz.column_formats = formats; else delete viz.column_formats;
 emit("update:viz", viz);
}
function setFormatKind(name: string, event: Event) {
 const kind = selected(event);
 setFormat(name, kind === "time" ? { kind: "time" } : kind === "number" ? { kind: "number" } : undefined);
}
function setDate(name: string, event: Event) { const date = dates.find(v => v === selected(event)), f = formatFor(name); if (date && f?.kind === "time") setFormat(name, { ...f, date }); }
function setTime(name: string, event: Event) { const time = times.find(v => v === selected(event)), f = formatFor(name); if (time && f?.kind === "time") setFormat(name, { ...f, time }); }
function numberFormat(name: string, patch: Partial<NumberFormat>) {
 const f = formatFor(name);
 if (f?.kind !== "time") setFormat(name, { ...f, ...patch });
}
function numberField(name: string, field: "decimals" | "scale" | "prefix" | "suffix", value: unknown) {
 const f = formatFor(name);
 if (f?.kind === "time") return;
 setFormat(name, { ...f, [field]: value === "" ? undefined : field === "decimals" || field === "scale" ? Number(value) : String(value) });
}
function visible(name: string) {
 if (props.viz.type !== "table") return;
 const current = props.viz.visible_columns ?? props.frame?.columns.map(c => c.name) ?? [];
 emit("update:viz", { ...props.viz, visible_columns: current.includes(name) ? current.filter(n => n !== name) : [...current, name] });
}
</script>
<template>
 <fieldset v-if="viz.type === 'stat' || viz.type === 'chart'" class="space-y-2">
  <legend class="text-xs font-medium">{{ t('panelStyle.thresholds') }}</legend>
  <div v-for="(step,i) in thresholds" :key="i" class="flex gap-1 items-center">
   <span v-if="i === 0" class="text-xs">{{ t('panelStyle.base') }}</span>
   <Input v-else :aria-label="t('panelStyle.thresholdValue')" :model-value="step.value ?? ''" @update:model-value="v => threshold(i, { value: String(v) })" />
   <select :aria-label="t('panelStyle.color')" :value="step.color" @change="color($event, c => threshold(i, { color: c }))"><option v-for="c in paletteColors" :key="c" :value="c">{{ t(`panelStyle.${c}`) }}</option></select>
   <Button variant="ghost" size="sm" :aria-label="t('ui.delete')" @click="setThresholds(i === 0 ? [] : thresholds.filter((_,j) => j !== i))">×</Button>
  </div>
  <Button v-if="thresholds.length < 10" variant="outline" size="sm" @click="setThresholds([...thresholds, { value: thresholds.length ? '' : null, color: thresholds.length ? 'red' : 'green' }])">{{ t('panelStyle.addThreshold') }}</Button>
  <div v-if="viz.type === 'stat'" class="flex gap-2 text-xs"><button v-for="mode in (['value','background'] as const)" :key="mode" type="button" :aria-pressed="(viz.color_mode ?? 'value') === mode" @click="colorMode(mode)">{{ t(`panelStyle.${mode}`) }}</button></div>
 </fieldset>
 <template v-if="viz.type === 'table'">
  <fieldset class="space-y-2"><legend class="text-xs font-medium">{{ t('panelStyle.rules') }}</legend>
   <div v-for="(r,i) in rules" :key="i" class="space-y-1 rounded border p-2">
    <Input :aria-label="t('panelStyle.columns')" :model-value="r.columns.join(', ')" list="result-columns" @update:model-value="v => rule(i, { columns: String(v).split(',').map(s => s.trim()) })" />
    <select :aria-label="t('panelStyle.operator')" :value="r.op" @change="op($event,i)"><option v-for="o in ops" :key="o" :value="o">{{ ['contains','not_contains','empty','not_empty'].includes(o) ? t(`panelStyle.${o}`) : o }}</option></select>
    <Input v-if="r.op !== 'empty' && r.op !== 'not_empty'" :aria-label="t('panelStyle.matchValue')" :model-value="r.value ?? ''" @update:model-value="v => rule(i, { value: String(v) })" />
    <div class="flex gap-2"><button v-for="scope in (['cell','row'] as const)" :key="scope" type="button" :aria-pressed="r.scope === scope" @click="rule(i,{ scope })">{{ t(`panelStyle.${scope}`) }}</button>
    <select :aria-label="t('panelStyle.color')" :value="r.color" @change="color($event,c => rule(i, { color:c }))"><option v-for="c in paletteColors" :key="c" :value="c">{{ t(`panelStyle.${c}`) }}</option></select>
    <Button variant="ghost" size="sm" :aria-label="t('ui.delete')" @click="setRules(rules.filter((_,j) => j !== i))">×</Button></div>
   </div>
   <Button v-if="rules.length < 20" variant="outline" size="sm" @click="setRules([...rules, { columns: [frame?.columns[0]?.name ?? ''], op:'contains', value:'', color:'red', scope:'cell' }])">{{ t('panelStyle.addRule') }}</Button>
  </fieldset>
  <fieldset class="space-y-2"><legend class="text-xs font-medium">{{ t('panelStyle.columnFormats') }}</legend>
   <div v-for="c in frame?.columns ?? []" :key="c.name" class="space-y-1 rounded border p-2">
    <label class="flex gap-2 text-xs"><input type="checkbox" :checked="!viz.visible_columns || viz.visible_columns.includes(c.name)" @change="visible(c.name)" />{{ c.name }}</label>
    <select :aria-label="t('panelStyle.format')" :value="formatFor(c.name)?.kind ?? (formatFor(c.name) ? 'number' : '')" @change="setFormatKind(c.name,$event)"><option value="">{{ t('ui.none') }}</option><option value="number">{{ t('ui.number') }}</option><option value="time">{{ t('pages.time') }}</option></select>
    <template v-if="formatFor(c.name)?.kind === 'time'">
     <select :aria-label="t('ui.date')" :value="timeFor(c.name).date ?? 'iso'" @change="setDate(c.name,$event)"><option v-for="d in dates" :key="d" :value="d">{{ t(`panelStyle.${d}`) }}</option></select>
     <select :aria-label="t('pages.time')" :value="timeFor(c.name).time ?? (c.kind === 'date' ? 'none' : 'seconds')" @change="setTime(c.name,$event)"><option v-for="v in times" :key="v" :value="v">{{ t(`panelStyle.${v}`) }}</option></select>
    </template>
    <template v-else-if="formatFor(c.name)">
     <div class="flex gap-2 text-xs"><button v-for="notation in notations" :key="notation" type="button" :aria-pressed="(numberFor(c.name).notation ?? 'auto') === notation" @click="numberFormat(c.name,{notation})">{{ t(notationKeys[notation]) }}</button></div>
     <label class="flex gap-2 text-xs"><input type="checkbox" :checked="numberFor(c.name).grouping ?? true" @change="numberFormat(c.name,{grouping: !(numberFor(c.name).grouping ?? true)})" />{{ t('dashboardBuilder.GroupThousands') }}</label>
     <div class="flex flex-wrap gap-2 text-xs"><button v-for="unit in units" :key="unit" type="button" :aria-pressed="(numberFor(c.name).unit ?? 'none') === unit" @click="numberFormat(c.name,{unit})">{{ t(unitKeys[unit]) }}</button></div>
     <label v-for="field in (['decimals','scale','prefix','suffix'] as const)" :key="field" class="block text-xs">{{ t(`dashboardBuilder.${field[0].toUpperCase() + field.slice(1)}`) }}<Input :model-value="numberFor(c.name)[field] ?? ''" @update:model-value="v => numberField(c.name,field,v)" /></label></template>
   </div>
  </fieldset>
 </template>
</template>
<style scoped>
select { max-width: 100%; border: 1px solid var(--border); border-radius: 4px; background: var(--background); color: var(--foreground); padding: 4px; font-size: 12px; }
button[aria-pressed="true"] { font-weight: 700; text-decoration: underline; }
</style>
