<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { Eye, Send } from "lucide-vue-next";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { NativeSelect, NativeSelectOption } from "@/components/ui/native-select";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import ReportScheduleFields from "./ReportScheduleFields.vue";
import { useReportsStore } from "@/stores/reports";
import type { Report, ReportInput, ReportRangeAnchor, ReportSchedule, ReportSourceRef } from "@/api/reports";
import { REPORT_TIME_RANGES, isCalendarRange, savedQueryVariables, splitValues } from "@/utils/reportSchedule";
import { usePreferencesStore } from "@/stores/preferences";

const props = defineProps<{
  /** The report to edit; null creates one. */
  report: Report | null;
  /** Preselects this saved query for a new report. */
  savedQueryId?: number | null;
  /** Preselects this dashboard for a new report. */
  dashboardId?: number | null;
  /** Starts a new report as a copy of this one. */
  copyOf?: Report | null;
}>();
const open = defineModel<boolean>("open", { required: true });
const emit = defineEmits<{ history: [report: Report] }>();

const { t } = useI18n();
const store = useReportsStore();

// A new report starts in the zone the viewer reads times in.
const defaultZone: string = usePreferencesStore().zone;
const zones = typeof Intl.supportedValuesOf === "function" ? Intl.supportedValuesOf("timeZone") : [defaultZone];

// The input as last loaded or saved, to tell unsaved changes.
const saved = ref("");

// The report this dialog edits. A new report becomes the edited one once it
// is saved, so Preview and Send test work right after the first save.
const current = ref<Report | null>(null);
const name = ref("");
const sourceKind = ref<ReportSourceRef["kind"]>("saved_query");
const savedQueryId = ref<number | null>(null);
const dashboardId = ref<number | null>(null);
// "" sends every tab.
const tabId = ref("");
const schedule = ref<ReportSchedule>({ frequency: "daily", hour: 9, minute: 0 });
const timezone = ref(defaultZone);
const timeRange = ref("24h");
const anchor = ref<ReportRangeAnchor>("send_time");
// Variable values as typed. A list variable is comma-separated text.
const variables = ref<Record<string, string>>({});
// Variables the report stored as a list, so they go back as a list.
const storedLists = ref<Set<string>>(new Set());
const maxRows = ref(25);
const skipIfEmpty = ref(false);
const attachCSV = ref(false);

// Each load starts a new generation: a save from an earlier one finishes
// without touching what the dialog shows now.
let generation = 0;

function load(report: Report | null) {
  generation++;
  const base = report ?? props.copyOf ?? null;
  current.value = report;
  name.value = report?.name ?? (base ? t("common.copyName", { name: base.name }).slice(0, 200) : "");
  savedQueryId.value = base?.source.saved_query_id ?? props.savedQueryId ?? null;
  dashboardId.value = base?.source.dashboard_id ?? (base ? null : props.dashboardId ?? null);
  sourceKind.value = dashboardId.value !== null ? "dashboard" : "saved_query";
  tabId.value = base?.source.tab_id ?? "";
  schedule.value = base ? { ...base.schedule } : { frequency: "daily", hour: 9, minute: 0 };
  timezone.value = base?.timezone ?? defaultZone;
  // A dashboard report without a range takes the dashboard's default.
  timeRange.value = base?.time_range ?? (sourceKind.value === "dashboard" ? "" : "24h");
  anchor.value = base?.range_anchor ?? "send_time";
  const stored = Object.entries(base?.variables ?? {});
  storedLists.value = new Set(stored.filter(([, v]) => Array.isArray(v)).map(([k]) => k));
  variables.value = Object.fromEntries(stored.map(([k, v]) => [k, Array.isArray(v) ? v.join(", ") : String(v ?? "")]));
  maxRows.value = base?.max_rows ?? 25;
  skipIfEmpty.value = base?.skip_if_empty ?? false;
  attachCSV.value = base?.attach_csv ?? false;
  saved.value = report ? JSON.stringify(input.value) : "";
}

// Values of the previous saved query's variables do not apply to another.
const selectedQuery = computed({
  get: () => (savedQueryId.value === null ? "" : String(savedQueryId.value)),
  set: (value: string) => {
    const next = value === "" ? null : Number(value);
    if (next !== savedQueryId.value) {
      variables.value = {};
      storedLists.value = new Set();
    }
    savedQueryId.value = next;
  },
});


const selectedDashboard = computed({
  get: () => (dashboardId.value === null ? "" : String(dashboardId.value)),
  set: (value: string) => {
    const next = value === "" ? null : Number(value);
    if (next !== dashboardId.value) tabId.value = "";
    dashboardId.value = next;
  },
});
const isDashboard = computed(() => sourceKind.value === "dashboard");
// A saved query needs a range; a dashboard starts on its own default.
const selectedKind = computed({
  get: () => sourceKind.value,
  set: (kind: ReportSourceRef["kind"]) => {
    if (kind === sourceKind.value) return;
    sourceKind.value = kind;
    timeRange.value = kind === "dashboard" ? "" : "24h";
  },
});
watch(dashboardId, (id) => {
  if (id !== null) void store.fetchDashboardTabs(id);
}, { immediate: true });
const tabs = computed(() => store.tabsOf(dashboardId.value));

const savedQuery = computed(() => (isDashboard.value ? null : store.savedQueries.find((q) => q.id === savedQueryId.value) ?? null));
const declared = computed(() => (savedQuery.value ? savedQueryVariables(savedQuery.value) : []));
const ranges = computed(() => (REPORT_TIME_RANGES as readonly string[]).includes(timeRange.value) ? REPORT_TIME_RANGES : [timeRange.value, ...REPORT_TIME_RANGES]);
const calendar = computed(() => isCalendarRange(timeRange.value));
watch(calendar, (isCalendar) => {
  if (isCalendar) anchor.value = "send_time";
});

function isList(name: string): boolean {
  return storedLists.value.has(name) || declared.value.some((v) => v.name === name && v.multiple);
}

const input = computed<ReportInput>(() => ({
  name: name.value.trim(),
  source: isDashboard.value
    ? { kind: "dashboard", dashboard_id: dashboardId.value ?? undefined, ...(tabId.value ? { tab_id: tabId.value } : {}) }
    : { kind: "saved_query", saved_query_id: savedQueryId.value ?? undefined },
  schedule: schedule.value,
  timezone: timezone.value,
  time_range: timeRange.value,
  range_anchor: anchor.value,
  // Only set values: an empty one falls back to the saved query's default.
  variables: isDashboard.value ? {} : Object.fromEntries(Object.entries(variables.value).filter(([, v]) => v.trim()).map(([k, v]) => [k, isList(k) ? splitValues(v) : v.trim()])),
  max_rows: maxRows.value,
  skip_if_empty: skipIfEmpty.value,
  // Only a saved query attaches its rows; a dashboard refuses the flag.
  attach_csv: !isDashboard.value && attachCSV.value,
}));
const dirty = computed(() => !current.value || JSON.stringify(input.value) !== saved.value);

watch(open, (isOpen) => {
  if (!isOpen) return;
  load(props.report);
  void store.fetchSavedQueries();
  void store.fetchDashboards();
}, { immediate: true });

const missingVariable = computed(() => declared.value.some((v) => !v.optional && !v.fallback && !variables.value[v.name]?.trim()));
const hasSource = computed(() => (isDashboard.value ? dashboardId.value !== null : savedQueryId.value !== null));
const canSave = computed(() => !!input.value.name && hasSource.value && maxRows.value >= 5 && maxRows.value <= 100 && !missingVariable.value);
const saving = computed(() => store.isLoadingOperation("saveReport"));
const readOnly = computed(() => !!current.value && !current.value.can_edit);

async function save(): Promise<Report | null> {
  if (!canSave.value) return null;
  const sent = input.value;
  const started = generation;
  const result = current.value ? await store.updateReport(current.value.id, sent) : await store.createReport(sent);
  if (!result.success || !result.data || started !== generation) return null;
  current.value = result.data;
  saved.value = JSON.stringify(sent);
  return result.data;
}

// Preview and Send test act on the stored report, so unsaved changes are
// saved first.
async function stored(): Promise<Report | null> {
  return dirty.value ? await save() : current.value;
}

const previewOpen = ref(false);
const previewHTML = ref("");
const previewSubject = ref("");
const previewing = computed(() => !!current.value && store.isLoadingOperation(`previewReport-${current.value.id}`));
async function preview() {
  const report = await stored();
  if (!report) return;
  const result = await store.previewReport(report.id);
  if (!result.success || !result.data) return;
  previewSubject.value = result.data.subject;
  previewHTML.value = result.data.html;
  previewOpen.value = true;
}

const sending = computed(() => !!current.value && store.isLoadingOperation(`runReport-${current.value.id}`));
async function sendTest() {
  const report = await stored();
  if (report) await store.sendTest(report.id);
}

function warningText(code: string): string {
  switch (code) {
    case "smtp_not_configured": return t("reports.warningSmtp");
    case "no_frontend_url": return t("reports.warningNoUrl");
    case "panel_locked_for_owner": return t("reports.warningOwnerLocked");
  }
  return code;
}
</script>

<template>
  <Dialog v-model:open="open">
    <DialogContent class="sm:max-w-2xl max-h-[90vh] overflow-y-auto">
      <DialogHeader>
        <DialogTitle>{{ current ? t("reports.editReport") : t("reports.newReport") }}</DialogTitle>
        <DialogDescription>{{ t("reports.editorDescription") }}</DialogDescription>
      </DialogHeader>

      <Alert v-for="w in current?.warnings ?? []" :key="w.code" variant="destructive">
        <AlertDescription>{{ warningText(w.code) }}</AlertDescription>
      </Alert>

      <form class="space-y-5" @submit.prevent="save">
        <fieldset :disabled="readOnly" class="space-y-5">
          <div class="space-y-1.5">
            <Label for="report-name">{{ t("ui.name") }}</Label>
            <Input id="report-name" v-model="name" maxlength="200" :placeholder="t('reports.namePlaceholder')" />
          </div>

          <section class="space-y-3">
            <h3 class="text-sm font-medium">{{ t("reports.sectionSource") }}</h3>
            <div class="space-y-1.5">
              <Label for="report-source-kind">{{ t("reports.sourceKind") }}</Label>
              <NativeSelect id="report-source-kind" v-model="selectedKind" class="w-full" :disabled="!!current">
                <NativeSelectOption value="saved_query">{{ t("reports.savedQuery") }}</NativeSelectOption>
                <NativeSelectOption value="dashboard">{{ t("reports.dashboard") }}</NativeSelectOption>
              </NativeSelect>
            </div>
            <div v-if="isDashboard" class="grid gap-3 sm:grid-cols-2">
              <div class="space-y-1.5">
                <Label for="report-dashboard">{{ t("reports.dashboard") }}</Label>
                <NativeSelect id="report-dashboard" class="w-full" v-model="selectedDashboard">
                  <NativeSelectOption value="" disabled>{{ t("reports.pickDashboard") }}</NativeSelectOption>
                  <NativeSelectOption v-for="d in store.dashboards" :key="d.id" :value="String(d.id)">{{ d.name }}</NativeSelectOption>
                </NativeSelect>
              </div>
              <div class="space-y-1.5">
                <Label for="report-tab">{{ t("reports.tab") }}</Label>
                <NativeSelect id="report-tab" class="w-full" v-model="tabId" :disabled="dashboardId === null">
                  <NativeSelectOption value="">{{ t("reports.allTabs") }}</NativeSelectOption>
                  <NativeSelectOption v-for="tab in tabs" :key="tab.id" :value="tab.id">{{ tab.title }}</NativeSelectOption>
                </NativeSelect>
              </div>
            </div>
            <div v-else class="space-y-1.5">
              <Label for="report-source">{{ t("reports.savedQuery") }}</Label>
              <NativeSelect id="report-source" class="w-full" v-model="selectedQuery">
                <NativeSelectOption value="" disabled>{{ t("reports.pickSavedQuery") }}</NativeSelectOption>
                <NativeSelectOption v-for="q in store.savedQueries" :key="q.id" :value="String(q.id)">
                  {{ q.source_name ? `${q.name} (${q.source_name})` : q.name }}
                </NativeSelectOption>
              </NativeSelect>
            </div>
          </section>

          <section class="space-y-3">
            <h3 class="text-sm font-medium">{{ t("reports.sectionSchedule") }}</h3>
            <ReportScheduleFields v-model="schedule" />
            <div class="space-y-1.5">
              <Label for="report-timezone">{{ t("reports.timezone") }}</Label>
              <NativeSelect id="report-timezone" v-model="timezone" class="w-full">
                <NativeSelectOption v-for="z in zones" :key="z" :value="z">{{ z }}</NativeSelectOption>
              </NativeSelect>
            </div>
          </section>

          <section class="space-y-3">
            <h3 class="text-sm font-medium">{{ t("reports.sectionRange") }}</h3>
            <div class="grid gap-3 sm:grid-cols-2">
              <div class="space-y-1.5">
                <Label for="report-range">{{ t("reports.timeRange") }}</Label>
                <NativeSelect id="report-range" v-model="timeRange" class="w-full">
                  <NativeSelectOption v-if="isDashboard" value="">{{ t("reports.dashboardDefaultRange") }}</NativeSelectOption>
                  <NativeSelectOption v-for="r in ranges.filter(Boolean)" :key="r" :value="r">{{ r }}</NativeSelectOption>
                </NativeSelect>
              </div>
              <div class="space-y-1.5">
                <Label for="report-anchor">{{ t("reports.rangeEnds") }}</Label>
                <NativeSelect id="report-anchor" v-model="anchor" class="w-full" :disabled="calendar">
                  <NativeSelectOption value="send_time">{{ t("reports.anchorSendTime") }}</NativeSelectOption>
                  <NativeSelectOption value="day_start">{{ t("reports.anchorDayStart") }}</NativeSelectOption>
                </NativeSelect>
              </div>
              <div v-for="v in declared" :key="v.name" class="space-y-1.5">
                <Label :for="`report-var-${v.name}`">{{ v.label }}</Label>
                <Input :id="`report-var-${v.name}`" v-model="variables[v.name]" :placeholder="v.fallback || (v.optional ? t('reports.optional') : '')" />
              </div>
              <div class="space-y-1.5">
                <Label for="report-max-rows">{{ t("reports.maxRows") }}</Label>
                <Input id="report-max-rows" v-model.number="maxRows" type="number" min="5" max="100" />
              </div>
              <div class="flex items-center gap-2 sm:col-span-2">
                <Switch id="report-skip-empty" v-model="skipIfEmpty" />
                <Label for="report-skip-empty">{{ t("reports.skipIfEmpty") }}</Label>
              </div>
              <div v-if="!isDashboard" class="flex items-center gap-2 sm:col-span-2">
                <Switch id="report-attach-csv" v-model="attachCSV" />
                <Label for="report-attach-csv">{{ t("reports.attachCSV") }}</Label>
              </div>
            </div>
          </section>
        </fieldset>

        <DialogFooter class="gap-2 sm:justify-between">
          <div class="flex flex-wrap gap-2">
            <template v-if="current">
              <Button type="button" variant="outline" :disabled="readOnly || previewing || (dirty && !canSave)" @click="preview">
                <Eye class="mr-2 h-4 w-4" />{{ t("reports.preview") }}
              </Button>
              <Button type="button" variant="outline" :disabled="readOnly || sending || (dirty && !canSave)" @click="sendTest">
                <Send class="mr-2 h-4 w-4" />{{ t("reports.sendTest") }}
              </Button>
              <Button type="button" variant="ghost" @click="emit('history', current)">{{ t("reports.history") }}</Button>
            </template>
          </div>
          <div class="flex gap-2">
            <Button type="button" variant="outline" @click="open = false">{{ t("ui.close") }}</Button>
            <Button type="submit" :disabled="readOnly || !canSave || !dirty || saving">{{ current ? t("ui.save") : t("reports.create") }}</Button>
          </div>
        </DialogFooter>
      </form>
    </DialogContent>
  </Dialog>

  <Dialog v-model:open="previewOpen">
    <DialogContent class="sm:max-w-3xl">
      <DialogHeader>
        <DialogTitle>{{ t("reports.previewTitle") }}</DialogTitle>
        <DialogDescription>{{ previewSubject }}</DialogDescription>
      </DialogHeader>
      <iframe
        :srcdoc="previewHTML"
        sandbox=""
        referrerpolicy="no-referrer"
        :title="t('reports.previewTitle')"
        class="h-[65vh] w-full rounded border bg-white"
      />
    </DialogContent>
  </Dialog>
</template>
