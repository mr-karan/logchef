<script setup lang="ts">
import { computed } from "vue";
import { VisDonut, VisSingleContainer, VisTooltip } from "@unovis/vue";
import { Donut } from "@unovis/ts";
import type { DonutArcDatum } from "@unovis/ts/components/donut/types";
import { usePreferencesStore } from "@/stores/preferences";
import type { PanelFrame } from "@/utils/panelFrame";
import { buildPanelList, type ListItem, type ListViz } from "@/utils/panelCharts";

const props = withDefaults(defineProps<{ frame: PanelFrame; viz: Extract<ListViz, { type: "pie" }>; count: boolean; height?: number }>(), { height: 200 });
const preferences = usePreferencesStore();
const result = computed(() => {
  try { return { ...buildPanelList(props.frame, props.viz, props.count, preferences.zone), error: null }; }
  catch (error) { return { items: [], notices: [], total: null, error: error instanceof Error ? error.message : String(error) }; }
});
const tooltipTriggers = {
  [Donut.selectors.segment]: (arc: DonutArcDatum<ListItem>) => {
    const content = document.createElement("span");
    content.textContent = `${arc.data.label}: ${arc.data.value.exact}`;
    return content;
  },
};
</script>

<template>
  <div class="panel-pie">
    <p v-if="result.error" role="alert" class="text-xs text-destructive">{{ result.error }}</p>
    <p v-else-if="!result.items.length" class="text-xs text-muted-foreground">No data</p>
    <template v-else>
      <div class="panel-pie__body">
        <VisSingleContainer :data="result.items" :height="Math.max(90, height - 2 - result.notices.length * 18)" class="panel-pie__chart">
          <VisDonut :value="(item: ListItem) => item.percentage" :color="(item: ListItem) => item.color" :arc-width="viz.donut ? 20 : 0" :central-label="viz.donut ? result.total?.text : undefined" :central-sub-label="viz.donut ? 'Total' : undefined" :duration="0" />
          <VisTooltip :triggers="tooltipTriggers" />
        </VisSingleContainer>
        <div class="panel-pie__legend" aria-label="Slice legend">
          <div v-for="item in result.items" :key="item.key" class="panel-pie__item" :title="`${item.label}: ${item.value.exact}`" tabindex="0">
            <i :style="{ background: item.color }" /><span class="panel-pie__label">{{ item.label }}</span><span>{{ item.value.text }} · {{ item.percentage > 0 && item.percentage < 0.1 ? '<0.1' : item.displayPercentage.toFixed(1) }}%</span>
          </div>
        </div>
      </div>
      <p v-if="result.items.length && result.items.every((i) => i.percentage === 0)" role="status" class="text-xs text-muted-foreground">All values are zero.</p>
      <p v-for="notice in result.notices" :key="notice" role="status" class="text-xs text-muted-foreground">{{ notice }}</p>
    </template>
  </div>
</template>

<style scoped>
.panel-pie { display: flex; flex-direction: column; height: 100%; overflow: auto; }
.panel-pie__body { display: flex; min-height: 0; flex: 1; align-items: center; gap: 12px; }
.panel-pie__chart { width: 45%; flex-shrink: 0; }
.panel-pie__legend { flex: 1; min-width: 0; overflow: auto; max-height: 100%; }
.panel-pie__item { display: grid; grid-template-columns: 8px minmax(0, 1fr) auto; gap: 6px; align-items: center; font-size: 11px; padding: 3px 0; }
.panel-pie__item i { width: 8px; height: 8px; border-radius: 50%; }
.panel-pie__label { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
</style>
