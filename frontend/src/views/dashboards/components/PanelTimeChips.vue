<script setup lang="ts">
import { computed } from "vue";
import { useI18n } from "vue-i18n";
import type { DashboardQueryPanel } from "@/api/dashboards";
import type { PanelState } from "@/stores/dashboards";
import { panelTimeBound } from "@/utils/dashboardRuntime";
import { relativeRangeLabel } from "@/utils/rangeLabel";
import { formatInZone, type EpochMs, type ZoneId } from "@/utils/timeWindow";

// The chips tell a viewer what the time picker does to this panel: nothing
// (a fixed window), a range of the panel's own, and the bucket size in use.
const props = defineProps<{ panel: DashboardQueryPanel; state: PanelState }>();
const { t, locale } = useI18n();

const fixedWindow = computed(() => !panelTimeBound(props.panel.query));

const range = computed(() => {
  const spec = props.panel.query.range;
  if (!spec || fixedWindow.value) return null;
  const label = relativeRangeLabel(spec);
  const resolved = props.state.range;
  const format = (ms: EpochMs, zone: ZoneId) => formatInZone(ms, zone, { month: "short", day: "numeric", hour: "2-digit", minute: "2-digit" }, locale.value);
  return {
    text: label ? t(label.key, { count: label.count }, label.count) : spec,
    title: resolved
      ? t("dashboards.chipRangeResolvedHint", { from: format(resolved.start, resolved.zone), to: format(resolved.end, resolved.zone), zone: resolved.zone })
      : t("dashboards.chipRangeHint"),
  };
});

const interval = computed(() => {
  const chosen = props.panel.query.interval;
  const used = props.state.interval;
  if (props.state.intervalRaised && used && chosen) {
    return { text: used, raised: true, title: t("dashboards.chipIntervalRaisedHint", { chosen, used }) };
  }
  if (!chosen || chosen === "auto") return null;
  return { text: chosen, raised: false, title: t("dashboards.chipIntervalHint", { interval: chosen }) };
});
</script>

<template>
  <span
    v-if="fixedWindow"
    class="panel-time-chip border-border bg-muted text-muted-foreground"
    data-testid="chip-fixed-window"
    :title="t('dashboards.chipFixedWindowHint')"
  >{{ t('dashboards.chipFixedWindow') }}</span>
  <span
    v-if="range"
    class="panel-time-chip border-sky-500/40 bg-sky-500/10 text-sky-700 dark:text-sky-300"
    data-testid="chip-range"
    :title="range.title"
  >{{ range.text }}</span>
  <span
    v-if="interval"
    class="panel-time-chip"
    :class="interval.raised ? 'border-amber-500/40 bg-amber-500/10 text-amber-700 dark:text-amber-400' : 'border-border bg-muted text-muted-foreground'"
    data-testid="chip-interval"
    :title="interval.title"
  >{{ interval.text }}</span>
</template>

<style scoped>
.panel-time-chip {
  padding: 0.1rem 0.45rem;
  border-width: 1px;
  border-radius: 999px;
  font-size: 0.65rem;
  line-height: 1.2;
  white-space: nowrap;
  flex-shrink: 0;
}
</style>
