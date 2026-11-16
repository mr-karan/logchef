<script setup lang="ts">
import { computed, watch } from "vue";
import { useI18n } from "vue-i18n";
import { History, RefreshCw } from "lucide-vue-next";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from "@/components/ui/sheet";
import { EmptyState, LoadingState } from "@/components/layout";
import { RUNS_PAGE, useReportsStore } from "@/stores/reports";
import { usePreferencesStore } from "@/stores/preferences";
import type { Report, ReportRun } from "@/api/reports";
import { epochMs, formatInZone } from "@/utils/timeWindow";
import { runStatusVariant } from "@/utils/reportSchedule";

const props = defineProps<{ report: Report | null }>();
const open = defineModel<boolean>("open", { required: true });

const { t, locale } = useI18n();
const store = useReportsStore();
const preferences = usePreferencesStore();

const runs = computed(() => (props.report ? store.runsOf(props.report.id) : []));
const loading = computed(() => !!props.report && store.isLoadingOperation(`reportRuns-${props.report.id}`));
// A full page means older runs may exist.
const hasMore = computed(() => runs.value.length > 0 && runs.value.length % RUNS_PAGE === 0);

watch([open, () => props.report?.id], ([isOpen, id]) => {
  if (isOpen && id) void store.loadRuns(id);
}, { immediate: true });

function when(value?: string): string {
  if (!value) return "";
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? value : formatInZone(epochMs(date), preferences.zone, { dateStyle: "medium", timeStyle: "short" }, locale.value);
}

function sentCount(run: ReportRun): number {
  return run.deliveries.filter((d) => d.status === "sent").length;
}
</script>

<template>
  <Sheet v-model:open="open">
    <SheetContent side="right" class="w-[520px] max-w-[94vw] flex flex-col p-0">
      <SheetHeader class="p-4 pb-3 border-b">
        <SheetTitle class="text-sm font-medium">{{ t("reports.historyTitle", { name: report?.name ?? "" }) }}</SheetTitle>
        <SheetDescription class="sr-only">{{ t("reports.historyTitle", { name: report?.name ?? "" }) }}</SheetDescription>
        <div>
          <Button variant="outline" size="sm" class="h-7 gap-1.5" :disabled="loading || !report" @click="report && store.loadRuns(report.id)">
            <RefreshCw class="h-3.5 w-3.5" />{{ t("reports.refresh") }}
          </Button>
        </div>
      </SheetHeader>
      <div class="flex-1 overflow-y-auto p-4 space-y-3">
        <LoadingState v-if="loading && !runs.length" :label="t('reports.loadingRuns')" />
        <EmptyState v-else-if="!runs.length" :icon="History" :title="t('reports.noRuns')" :description="t('reports.noRunsDescription')" />
        <article v-for="run in runs" :key="run.id" class="rounded-md border p-3 space-y-2 text-sm" :data-run-status="run.status">
          <div class="flex flex-wrap items-center gap-2">
            <Badge :variant="runStatusVariant(run.status)">{{ t(`reports.status_${run.status}`) }}</Badge>
            <code v-if="run.reason" class="text-xs bg-muted px-1.5 py-0.5 rounded">{{ run.reason }}</code>
            <span class="text-xs text-muted-foreground">{{ t(run.trigger === "manual" ? "reports.triggerManual" : "reports.triggerSchedule") }}</span>
            <span class="ml-auto text-xs text-muted-foreground tabular-nums">{{ when(run.started_at) }}</span>
          </div>
          <p v-if="run.window_start && run.window_end" class="text-xs text-muted-foreground">
            {{ t("reports.window", { from: when(run.window_start), to: when(run.window_end) }) }}
          </p>
          <ul v-if="run.panels.length" class="space-y-1">
            <li v-for="p in run.panels" :key="p.panel_id" class="text-xs">
              <span class="font-medium">{{ p.title }}</span>
              <span class="text-muted-foreground"> · {{ t("reports.panelRows", { count: p.rows }) }}</span>
              <pre v-if="p.error" class="mt-1 whitespace-pre-wrap break-words rounded bg-destructive/10 p-2 text-destructive">{{ p.error }}</pre>
            </li>
          </ul>
          <p v-if="run.deliveries.length" class="text-xs text-muted-foreground">
            {{ t("reports.deliveriesSent", { sent: sentCount(run), total: run.deliveries.length }) }}
            <template v-for="d in run.deliveries.filter((d) => d.status !== 'sent')" :key="d.target">
              · <code class="bg-muted px-1 rounded">{{ d.status }}{{ d.reason ? `: ${d.reason}` : "" }}</code>
            </template>
          </p>
        </article>
        <Button v-if="hasMore" variant="outline" class="w-full" :disabled="loading" @click="report && store.loadRuns(report.id, true)">
          {{ t("reports.olderRuns") }}
        </Button>
      </div>
    </SheetContent>
  </Sheet>
</template>
