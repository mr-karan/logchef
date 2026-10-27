import { afterEach, describe, expect, it, vi } from 'vitest';
import { alertsApi, type Alert, type AlertHistoryEntry } from '@/api/alerts';
import { teamsApi } from '@/api/teams';
import { loadAlertInvestigation } from '../alertInvestigation';
import { latestTriggered } from '../ask';

const alert: Alert = { id: 5, source_id: 12, name: 'Error rate', query_language: 'logchefql', editor_mode: 'condition', query: '', lookback_seconds: 300, threshold_operator: 'gt', threshold_value: 5, frequency_seconds: 60, severity: 'warning', recipient_user_ids: [], webhook_urls: [], is_active: true, last_state: 'resolved', created_at: '', updated_at: '' };
const entry = (id: number, status: AlertHistoryEntry['status'], triggered_at: string): AlertHistoryEntry => ({ id, alert_id: 5, status, triggered_at, created_at: triggered_at });
const history = [entry(1, 'triggered', '2026-10-01T00:00:00Z'), entry(2, 'resolved', '2026-10-03T00:00:00Z'), entry(3, 'triggered', '2026-10-02T00:00:00Z')];
afterEach(() => vi.restoreAllMocks());

describe('alert investigation', () => {
  it('chooses the latest triggered entry regardless of response order or last alert state', () => {
    expect(latestTriggered(history)?.id).toBe(3);
    expect(latestTriggered([entry(4, 'triggered', 'invalid')])).toBeNull();
  });

  it('requests at most 20 recent events and returns a visible no-history reason', async () => {
    vi.spyOn(alertsApi, 'get').mockResolvedValue({ status: 'success', data: alert });
    const loaded = vi.spyOn(alertsApi, 'history').mockResolvedValue({ status: 'success', data: [entry(2, 'resolved', '2026-10-03T00:00:00Z')] });
    vi.spyOn(teamsApi, 'listUserTeams').mockResolvedValue({ status: 'success', data: [] });
    const controller = new AbortController();
    expect(await loadAlertInvestigation(5, null, controller.signal)).toEqual({ kind: 'failed', reason: 'noHistory' });
    expect(loaded).toHaveBeenCalledWith(5, 20, expect.objectContaining({ signal: expect.any(AbortSignal), timeout: 8 }));
  });

  it('does not infer an accessible scope from an admin catalog or a recent source', async () => {
    vi.spyOn(alertsApi, 'get').mockResolvedValue({ status: 'success', data: alert });
    vi.spyOn(alertsApi, 'history').mockResolvedValue({ status: 'success', data: history });
    vi.spyOn(teamsApi, 'listUserTeams').mockResolvedValue({ status: 'success', data: [] });
    expect(await loadAlertInvestigation(5, 3, new AbortController().signal)).toEqual({ kind: 'failed', reason: 'noScope' });
  });
});
