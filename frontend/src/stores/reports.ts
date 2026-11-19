import { defineStore } from "pinia";
import { computed } from "vue";
import { useBaseStore } from "./base";
import { reportsApi, type Report, type ReportInput, type ReportPreview, type ReportRecipientOptions, type ReportRun, type ReportScope } from "@/api/reports";
import { savedQueriesApi, type SavedQuery } from "@/api/savedQueries";
import { dashboardsApi, type Dashboard, type DashboardSummary } from "@/api/dashboards";
import { i18n } from "@/i18n";

interface ReportsState {
  reports: Report[];
  scope: ReportScope;
  savedQueries: SavedQuery[];
  dashboards: DashboardSummary[];
  /** The tabs of each dashboard the editor picked, by dashboard id. */
  dashboardTabs: Record<number, { id: string; title: string }[]>;
  runs: Record<number, ReportRun[]>;
  recipientOptions: ReportRecipientOptions;
}

export const RUNS_PAGE = 20;

export const useReportsStore = defineStore("reports", () => {
  const state = useBaseStore<ReportsState>({ reports: [], scope: "mine", savedQueries: [], dashboards: [], dashboardTabs: {}, runs: {}, recipientOptions: { users: [], teams: [], external: { enabled: false, domains: [] } } });

  const reports = computed(() => state.data.value.reports);
  const scope = computed(() => state.data.value.scope);
  const savedQueries = computed(() => state.data.value.savedQueries);
  const dashboards = computed(() => state.data.value.dashboards);
  const recipientOptions = computed(() => state.data.value.recipientOptions);

  async function fetchReports(next: ReportScope = state.data.value.scope) {
    state.data.value.scope = next;
    return await state.callApi<Report[]>({
      apiCall: () => reportsApi.list(next),
      operationKey: "fetchReports",
      showToast: false,
      onSuccess: (data) => {
        if (state.data.value.scope === next) state.data.value.reports = data ?? [];
      },
    });
  }

  async function getReport(id: number) {
    return await state.callApi<Report>({
      apiCall: () => reportsApi.get(id),
      operationKey: `getReport-${id}`,
    });
  }

  // The source picker lists the saved queries the caller can see.
  async function fetchSavedQueries() {
    return await state.callApi<SavedQuery[]>({
      apiCall: () => savedQueriesApi.list(),
      operationKey: "fetchReportSavedQueries",
      showToast: false,
      onSuccess: (data) => {
        state.data.value.savedQueries = data ?? [];
      },
    });
  }

  // The source picker lists the dashboards the caller can see.
  async function fetchDashboards() {
    return await state.callApi<DashboardSummary[]>({
      apiCall: () => dashboardsApi.list(),
      operationKey: "fetchReportDashboards",
      showToast: false,
      onSuccess: (data) => {
        state.data.value.dashboards = data ?? [];
      },
    });
  }

  async function fetchDashboardTabs(id: number) {
    return await state.callApi<Dashboard>({
      apiCall: () => dashboardsApi.get(id),
      operationKey: `fetchReportDashboardTabs-${id}`,
      showToast: false,
      onSuccess: (data) => {
        if (data && !data.corrupt) state.data.value.dashboardTabs[id] = data.panels.tabs.map((tab) => ({ id: tab.id, title: tab.title }));
      },
    });
  }

  // The recipient picker lists the users and teams the caller may add.
  async function fetchRecipientOptions() {
    return await state.callApi<ReportRecipientOptions>({
      apiCall: () => reportsApi.recipientOptions(),
      operationKey: "fetchReportRecipientOptions",
      showToast: false,
      onSuccess: (data) => {
        if (data) state.data.value.recipientOptions = data;
      },
    });
  }

  function tabsOf(id: number | null): { id: string; title: string }[] {
    return id === null ? [] : state.data.value.dashboardTabs[id] ?? [];
  }

  function replace(report: Report | null) {
    if (!report) return;
    const list = state.data.value.reports;
    const i = list.findIndex((r) => r.id === report.id);
    if (i >= 0) list.splice(i, 1, report);
    else if (state.data.value.scope === "all" || (state.data.value.scope === "mine" && report.can_edit)) list.push(report);
  }

  async function createReport(input: ReportInput) {
    return await state.callApi<Report>({
      apiCall: () => reportsApi.create(input),
      operationKey: "saveReport",
      successMessage: i18n.global.t("reports.created"),
      onSuccess: replace,
    });
  }

  async function updateReport(id: number, input: ReportInput) {
    return await state.callApi<Report>({
      apiCall: () => reportsApi.update(id, input),
      operationKey: "saveReport",
      successMessage: i18n.global.t("reports.saved"),
      onSuccess: replace,
    });
  }

  async function deleteReport(id: number) {
    return await state.callApi<null>({
      apiCall: () => reportsApi.remove(id),
      operationKey: `deleteReport-${id}`,
      successMessage: i18n.global.t("reports.deleted"),
      onSuccess: () => {
        state.data.value.reports = state.data.value.reports.filter((r) => r.id !== id);
        delete state.data.value.runs[id];
      },
    });
  }

  async function leaveReport(id: number) {
    return await state.callApi<null>({
      apiCall: () => reportsApi.leave(id),
      operationKey: `leaveReport-${id}`,
      successMessage: i18n.global.t("reports.left"),
      // A team can still send the report, so the list is read again.
      onSuccess: () => void fetchReports(),
    });
  }

  async function setPaused(id: number, paused: boolean) {
    return await state.callApi<Report>({
      apiCall: () => (paused ? reportsApi.pause(id) : reportsApi.resume(id)),
      operationKey: `pauseReport-${id}`,
      successMessage: i18n.global.t(paused ? "reports.paused" : "reports.resumed"),
      onSuccess: replace,
    });
  }

  async function claimReport(id: number) {
    return await state.callApi<Report>({
      apiCall: () => reportsApi.claim(id),
      operationKey: `claimReport-${id}`,
      successMessage: i18n.global.t("reports.claimed"),
      onSuccess: replace,
    });
  }

  async function sendTest(id: number) {
    return await state.callApi<ReportRun>({
      apiCall: () => reportsApi.run(id, "me"),
      operationKey: `runReport-${id}`,
      successMessage: i18n.global.t("reports.testQueued"),
    });
  }

  async function previewReport(id: number) {
    return await state.callApi<ReportPreview>({
      apiCall: () => reportsApi.preview(id),
      operationKey: `previewReport-${id}`,
    });
  }

  // loadRuns reads the newest page, or the page before the oldest run held.
  async function loadRuns(id: number, more = false) {
    const held = state.data.value.runs[id] ?? [];
    const before = more && held.length ? held[held.length - 1].id : 0;
    return await state.callApi<ReportRun[]>({
      apiCall: () => reportsApi.runs(id, before, RUNS_PAGE),
      operationKey: `reportRuns-${id}`,
      showToast: false,
      onSuccess: (data) => {
        state.data.value.runs[id] = more ? [...held, ...(data ?? [])] : (data ?? []);
        // The newest run is the report's last run; the list shows it.
        const newest = data?.[0];
        const report = state.data.value.reports.find((r) => r.id === id);
        if (!more && newest && report && (!report.last_run || report.last_run.id <= newest.id)) {
          report.last_run = { id: newest.id, status: newest.status, reason: newest.reason, started_at: newest.started_at, finished_at: newest.finished_at };
        }
      },
    });
  }

  function runsOf(id: number): ReportRun[] {
    return state.data.value.runs[id] ?? [];
  }

  return {
    reports,
    scope,
    savedQueries,
    dashboards,
    recipientOptions,
    isLoadingOperation: state.isLoadingOperation,
    fetchReports,
    fetchSavedQueries,
    fetchDashboards,
    fetchDashboardTabs,
    fetchRecipientOptions,
    tabsOf,
    getReport,
    createReport,
    updateReport,
    deleteReport,
    leaveReport,
    setPaused,
    claimReport,
    sendTest,
    previewReport,
    loadRuns,
    runsOf,
  };
});
