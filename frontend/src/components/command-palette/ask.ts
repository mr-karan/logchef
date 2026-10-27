import type { AlertHistoryEntry } from '@/api/alerts';
import type { ExplorerAskContext } from '@/stores/askChef';
import { resolveWindow, type TimeWindow } from '@/utils/timeWindow';
import type { SourceEntry, TeamEntry } from './catalog';
import type { PaletteContext, PaletteEffect, PaletteItem } from './palette';
import type { ParsedInput } from './parse';

export function latestTriggered(entries: readonly AlertHistoryEntry[]): AlertHistoryEntry | null {
  return entries.filter(entry => entry.status === 'triggered' && Number.isFinite(Date.parse(entry.triggered_at)))
    .reduce<AlertHistoryEntry | null>((latest, entry) => !latest || Date.parse(entry.triggered_at) > Date.parse(latest.triggered_at) ? entry : latest, null);
}

export function strongNavigationMatch(input: string, labels: readonly string[]): boolean {
  const normalize = (text: string) => text.trim().replace(/\s+/g, ' ').toLocaleLowerCase();
  const query = normalize(input);
  return query.length > 0 && labels.some(label => normalize(label).startsWith(query));
}

export function askItem(parsed: ParsedInput, ctx: PaletteContext, target: { team: TeamEntry; source: SourceEntry } | null, problem: string | null = null): PaletteItem | null {
  if (!ctx.askChefEnabled) return null;
  const plain = (text: string) => ({ text, marks: [], dim: [] });
  const base = { shape: 'row', id: 'ask', icon: 'askChef', tab: null, mono: false, meta: null, badge: null, shortcut: 'modShiftEnter' } satisfies Partial<PaletteItem>;
  const reason = problem ?? (target && !target.source.connected ? ctx.t('palette.problem.disconnected', { source: target.source.name }) : null);
  if (reason) return { shape: 'blocked', id: 'ask', icon: 'askChef', label: ctx.t('palette.ask.title', { question: parsed.query }), reason, enter: null, tab: null };
  let captured: ExplorerAskContext | null = null;
  if (target) {
    const window: TimeWindow = parsed.range?.kind === 'valid' ? parsed.range.window : { kind: 'relative', spec: '15m' };
    captured = { subject: { kind: 'explorer' }, teamId: target.team.id, sourceId: target.source.id, ...resolveWindow(window, ctx.zone, ctx.now), mode: target.source.logchefql ? 'logchefql' : 'native' };
  } else if (!parsed.tag && ctx.routeName === 'LogExplorer' && ctx.explorer) {
    captured = parsed.range?.kind === 'valid'
      ? { ...ctx.explorer.context, ...resolveWindow(parsed.range.window, ctx.zone, ctx.now) }
      : ctx.explorer.context;
  }
  if (!captured) {
    if (parsed.tag) return null;
    const edit = { text: `@ ${parsed.ask ? parsed.text.trimStart() : `? ${parsed.text.trimStart()}`}`, caret: 1 };
    return { ...base, label: plain(ctx.t('palette.ask.pickSource')), detail: ctx.t('palette.ask.keepQuestion', { syntax: '@team/source' }), enter: { kind: 'edit', edit }, tab: edit };
  }
  const effect: PaletteEffect | null = parsed.query ? { kind: 'askChef', context: captured, question: parsed.query, navigation: parsed.tag || parsed.range ? 'scoped' : 'current' } : null;
  return { ...base, label: plain(parsed.query ? ctx.t('palette.ask.title', { question: parsed.query }) : ctx.t('askChef.title')), detail: target ? `${target.team.name}/${target.source.name}` : ctx.t('palette.ask.currentContext'), enter: effect };
}
