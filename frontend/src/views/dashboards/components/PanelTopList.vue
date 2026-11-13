<script setup lang="ts">
import { computed } from "vue";
import { usePreferencesStore } from "@/stores/preferences";
import type { PanelFrame } from "@/utils/panelFrame";
import { buildPanelList, type ListViz } from "@/utils/panelCharts";

const props = defineProps<{ frame: PanelFrame; viz: Extract<ListViz, { type: "toplist" }>; count: boolean }>();
const preferences = usePreferencesStore();
const result = computed(() => {
  try { return { ...buildPanelList(props.frame, props.viz, props.count, preferences.zone), error: null }; }
  catch (error) { return { items: [], notices: [], error: error instanceof Error ? error.message : String(error) }; }
});
const maximum = computed(() => Math.max(0, ...result.value.items.map((i) => Math.abs(i.value.coordinate))));
</script>

<template>
  <div class="panel-toplist" aria-label="Top list">
    <p v-if="result.error" role="alert" class="text-xs text-destructive">{{ result.error }}</p>
    <p v-if="!result.error && !result.items.length" class="text-xs text-muted-foreground">No data</p>
    <div v-for="item in result.items" :key="item.key" class="panel-toplist__row" :title="`${item.label}: ${item.value.exact}`" tabindex="0">
      <span class="panel-toplist__label">{{ item.label }}</span>
      <div class="panel-toplist__track" aria-hidden="true"><div :style="{ width: `${maximum ? Math.abs(item.value.coordinate) / maximum * 100 : 0}%`, background: item.color }" /></div>
      <span class="panel-toplist__value">{{ item.value.text }}</span>
    </div>
    <p v-for="notice in result.notices" :key="notice" role="status" class="text-xs text-muted-foreground">{{ notice }}</p>
  </div>
</template>

<style scoped>
.panel-toplist { display: flex; flex-direction: column; gap: 6px; height: 100%; overflow: auto; }
.panel-toplist__row { display: grid; grid-template-columns: minmax(3rem, 28%) minmax(1rem, 1fr) auto; align-items: center; gap: 8px; font-size: 11px; }
.panel-toplist__label { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.panel-toplist__track { height: 10px; background: var(--muted); border-radius: 3px; overflow: hidden; }
.panel-toplist__track div { height: 100%; border-radius: inherit; }
.panel-toplist__value { font-variant-numeric: tabular-nums; }
</style>
