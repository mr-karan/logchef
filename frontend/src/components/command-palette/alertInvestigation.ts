import { alertsApi, type Alert, type AlertHistoryEntry } from '@/api/alerts';
import { teamsApi } from '@/api/teams';
import { latestTriggered } from './ask';
import { LIMITS } from './catalog';

export type AlertInvestigation =
  | { kind: 'ready'; alert: Alert; entry: AlertHistoryEntry; teamId: number }
  | { kind: 'failed'; reason: 'noHistory' | 'noScope' | 'failed' };

export async function loadAlertInvestigation(alertId: number, preferredTeam: number | null, signal: AbortSignal): Promise<AlertInvestigation> {
  const options = { signal: AbortSignal.any([signal, AbortSignal.timeout(8_000)]), timeout: 8, suppressErrorToast: true, suppressForbiddenRedirect: true };
  const [alert, history, teams] = await Promise.all([
    alertsApi.get(alertId, options), alertsApi.history(alertId, 20, options), teamsApi.listUserTeams(options),
  ]);
  if (alert.status !== 'success' || !alert.data || history.status !== 'success' || !history.data) return { kind: 'failed', reason: 'failed' };
  const entry = latestTriggered(history.data.slice(0, 20));
  if (!entry) return { kind: 'failed', reason: 'noHistory' };
  if (teams.status !== 'success' || !teams.data) return { kind: 'failed', reason: 'noScope' };
  const memberships = teams.data.slice(0, LIMITS.maxItems).sort((a, b) => Number(b.id === preferredTeam) - Number(a.id === preferredTeam));
  for (const team of memberships) {
    options.signal.throwIfAborted();
    const sources = await teamsApi.listTeamSources(team.id, options);
    if (sources.status === 'success' && sources.data?.some(source => source.id === alert.data?.source_id)) return { kind: 'ready', alert: alert.data, entry, teamId: team.id };
  }
  return { kind: 'failed', reason: 'noScope' };
}
