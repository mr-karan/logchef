<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { useRouter } from "vue-router";
import { BarChart3, LayoutDashboard, Table2 } from "lucide-vue-next";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { SearchableSelect } from "@/components/ui/searchable-select";
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip";
import PanelChart from "@/views/dashboards/components/PanelChart.vue";
import PanelStat from "@/views/dashboards/components/PanelStat.vue";
import PanelTable from "@/views/dashboards/components/PanelTable.vue";
import PanelTopList from "@/views/dashboards/components/PanelTopList.vue";
import type { AggregationInfo, ColumnInfo, QueryStats } from "@/api/explore";
import type { DashboardQueryPanel, DashboardViz } from "@/api/dashboards";
import { useDashboardsStore } from "@/stores/dashboards";
import { aggregationOutline, pipeTail } from "@/utils/logchefql/aggregation";
import { fillAggregationGaps, resultFrame } from "@/utils/panelFrame";
import { panelSeedState } from "@/utils/dashboardPanels";
import type { BucketAlignment, ResolvedWindow } from "@/utils/timeWindow";

const props = defineProps<{
  aggregation: AggregationInfo;
  columns: ColumnInfo[];
  rows: Record<string, unknown>[];
  stats: QueryStats;
  // The LogchefQL text that produced the result.
  query: string;
  teamId: number;
  sourceId: number;
  range: ResolvedWindow;
  alignment: BucketAlignment;
}>();
const emit = defineEmits<{ (e: "add-condition", field: string, value: unknown): void }>();

const { t } = useI18n();
const router = useRouter();
const dashboards = useDashboardsStore();

const byRole = (role: AggregationInfo["columns"][number]["role"]) => props.aggregation.columns.filter((c) => c.role === role);
const timeColumn = computed(() => byRole("time")[0]);
const groupColumns = computed(() => byRole("group"));
const valueColumns = computed(() => byRole("value"));

// The response declares every column type, VictoriaLogs included, so the frame trusts them.
const frame = computed(() => ({ ...resultFrame({ columns: props.columns, rows: props.rows, stats: props.stats }), interval: props.aggregation.interval || null }));

// A chart identifies a row by x and one series column, so two group fields have no chart.
const chartViz = computed<DashboardViz | null>(() => {
  const groups = groupColumns.value, values = valueColumns.value.map((c) => c.name);
  if (timeColumn.value && groups.length <= 1) {
    return { type: "chart", x: timeColumn.value.name, y: values, ...(groups[0] ? { series: groups[0].name } : {}), mark: "line", stack: "none" };
  }
  if (props.aggregation.shape === "grouped" && groups.length === 1) return { type: "toplist", label: groups[0].name, value: values[0] };
  if (props.aggregation.shape === "scalar" && values.length === 1) return { type: "stat", value: values[0], reduce: "last" };
  return null;
});
const chartUnavailable = computed(() => {
  if (chartViz.value) return null;
  return groupColumns.value.length > 1 ? t("explore.chartNeedsOneGroupField") : t("explore.chartNeedsOneValue");
});

const view = ref<"chart" | "table">("table");
const defaultView = () => (chartViz.value?.type === "chart" ? "chart" : "table");
// A rerun of the same aggregation keeps the view. A new set of columns starts from its default.
watch(() => JSON.stringify(props.aggregation.columns), () => { view.value = defaultView(); }, { immediate: true });
const showsChart = computed(() => view.value === "chart" && chartViz.value !== null);

const chartFrame = computed(() => (chartViz.value?.type === "chart" ? fillAggregationGaps(frame.value, props.aggregation, props.range, props.alignment) : frame.value));

const panel = computed<DashboardQueryPanel>(() => ({
  id: "explore-aggregation",
  kind: "query",
  title: (pipeTail(props.query) ?? props.query).trim().slice(0, 80),
  team_id: props.teamId,
  source_id: props.sourceId,
  query: { mode: "result", language: "logchefql", text: props.query },
  viz: showsChart.value && chartViz.value ? chartViz.value : { type: "table" },
}));

// Group columns in written order, matched to their fields as written, quotes included.
const groupFields = computed(() => {
  const fields = (aggregationOutline(props.query)?.keys ?? []).flatMap((k) => (!k.time && k.field ? [props.query.slice(k.field.start, k.field.end)] : []));
  return new Map(groupColumns.value.flatMap((c, i) => (fields[i] ? [[c.name, fields[i]] as const] : [])));
});
function onCellClick(column: string, value: unknown) {
  const field = groupFields.value.get(column);
  if (field) emit("add-condition", field, value);
}

const pickerOpen = ref(false);
const dashboardId = ref("");
const editable = computed(() => dashboards.dashboards.filter((d) => d.can_edit && !d.corrupt).map((d) => ({ value: String(d.id), label: d.name })));
async function openPicker() {
  dashboardId.value = "";
  pickerOpen.value = true;
  await dashboards.fetchDashboards();
}
function addToDashboard() {
  if (!dashboardId.value) return;
  const { title, team_id, source_id, query, viz } = panel.value;
  pickerOpen.value = false;
  void router.push({ path: `/dashboards/${dashboardId.value}`, state: panelSeedState({ title, team_id, source_id, query, viz }) });
}
</script>

<template>
  <div class="flex h-full flex-col" data-aggregation-result>
    <div class="flex h-9 shrink-0 items-center justify-between gap-2 border-b px-3">
      <div class="flex items-center rounded-md bg-muted p-0.5" role="group">
        <TooltipProvider>
          <Tooltip>
            <TooltipTrigger as-child>
              <span>
                <button
                  type="button"
                  data-view="chart"
                  class="flex h-6 items-center gap-1 rounded px-2 text-xs transition-colors disabled:cursor-not-allowed disabled:opacity-50"
                  :class="showsChart ? 'bg-background shadow-sm' : 'hover:bg-background/50'"
                  :aria-pressed="showsChart"
                  :disabled="!chartViz"
                  @click="view = 'chart'"
                >
                  <BarChart3 class="h-3.5 w-3.5" />{{ t("explore.chartView") }}
                </button>
              </span>
            </TooltipTrigger>
            <TooltipContent v-if="chartUnavailable" side="bottom">
              <p class="text-xs" data-chart-hint>{{ chartUnavailable }}</p>
            </TooltipContent>
          </Tooltip>
        </TooltipProvider>
        <button
          type="button"
          data-view="table"
          class="flex h-6 items-center gap-1 rounded px-2 text-xs transition-colors"
          :class="!showsChart ? 'bg-background shadow-sm' : 'hover:bg-background/50'"
          :aria-pressed="!showsChart"
          @click="view = 'table'"
        >
          <Table2 class="h-3.5 w-3.5" />{{ t("ui.table") }}
        </button>
      </div>
      <Button variant="ghost" size="sm" class="h-7 gap-1.5 text-xs" data-add-to-dashboard @click="openPicker">
        <LayoutDashboard class="h-3.5 w-3.5" />{{ t("explore.addToDashboard") }}
      </Button>
    </div>

    <div class="min-h-0 flex-1 overflow-auto p-3">
      <template v-if="showsChart && chartViz">
        <PanelChart v-if="chartViz.type === 'chart'" :frame="chartFrame" :viz="chartViz" :count="false" :range="range" :height="360" class="h-[360px]" />
        <PanelTopList v-else-if="chartViz.type === 'toplist'" :frame="frame" :viz="chartViz" :count="false" />
        <PanelStat v-else-if="chartViz.type === 'stat'" :frame="frame" :panel="panel" class="h-40" />
      </template>
      <PanelTable v-else :frame="frame" :clickable-columns="[...groupFields.keys()]" @cell-click="onCellClick" />
    </div>

    <Dialog v-model:open="pickerOpen">
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{{ t("explore.addToDashboard") }}</DialogTitle>
          <DialogDescription>{{ t("explore.addToDashboardDescription") }}</DialogDescription>
        </DialogHeader>
        <p v-if="!dashboards.isLoadingOperation('fetchDashboards') && !editable.length" class="text-sm text-muted-foreground">
          {{ t("explore.noEditableDashboards") }}
        </p>
        <SearchableSelect v-else v-model="dashboardId" :items="editable" :placeholder="t('explore.chooseDashboard')" />
        <DialogFooter>
          <Button variant="outline" @click="pickerOpen = false">{{ t("ui.cancel") }}</Button>
          <Button :disabled="!dashboardId" data-confirm-add @click="addToDashboard">{{ t("ui.addPanel") }}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  </div>
</template>
