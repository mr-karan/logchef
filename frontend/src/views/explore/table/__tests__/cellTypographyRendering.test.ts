import { createApp, nextTick } from 'vue';
import { createPinia } from 'pinia';
import { describe, expect, it } from 'vitest';
import { i18n } from '@/i18n';
import { COLUMN_SIZING_VERSION } from '@/composables/useColumnState';
import DataTable from '../data-table.vue';

it('renders readable values, identifiers and numbers differently in the same column', async () => {
  const values = [
    'DELIVERED', 'notifications-otp', 'api-01.example.com',
    '550e8400-e29b-41d4-a716-446655440000', '+15551234567',
    '5d24f63865268abd3e904199ac107fdd', 123456789,
    { message: 'retained JSON' },
    '{"message":"retained JSON string"}',
  ];
  const host = document.createElement('div');
  document.body.appendChild(host);
  const app = createApp(DataTable, {
    columns: [{ name: 'timestamp', type: 'DateTime' }, { name: 'value', type: 'String' }],
    data: values.map(value => ({ timestamp: '2026-10-27T10:00:00Z', value })),
    regexHighlights: { value: { pattern: 'notifications', isNegated: false } },
    stats: { execution_time_ms: 1, rows_read: values.length, bytes_read: 64 },
    sourceId: 'cell-typography-test', teamId: null,
  });
  app.use(createPinia());
  app.use(i18n);
  try {
    app.mount(host);
    await nextTick();
    const cells = [...host.querySelectorAll('tbody tr')].map(row =>
      row.querySelectorAll('td[data-cell-id]')[1]?.querySelector('.flex-render-content'));
    expect(cells.map(cell => cell?.textContent)).toEqual(values.map(value =>
      typeof value === 'object' ? JSON.stringify(value) : String(value)));
    for (const index of [0, 1, 2, 6]) {
      expect(cells[index]?.classList.contains('font-mono')).toBe(false);
      expect(cells[index]?.classList.contains('text-muted-foreground')).toBe(false);
      expect(cells[index]?.classList.contains('tabular-nums')).toBe(true);
      expect(cells[index]?.closest('td')?.classList.contains('font-mono')).toBe(false);
    }
    for (const index of [3, 4, 5]) {
      expect(cells[index]?.classList.contains('font-mono')).toBe(true);
      expect(cells[index]?.classList.contains('text-muted-foreground')).toBe(true);
    }
    expect(cells[1]?.querySelector('.search-highlight')?.textContent).toBe('notifications');
    expect(cells[7]?.classList.contains('font-mono')).toBe(true);
    expect(cells[7]?.classList.contains('json-content')).toBe(true);
    expect(cells[8]?.classList.contains('font-mono')).toBe(true);
    expect(cells[8]?.classList.contains('text-muted-foreground')).toBe(false);
    expect(host.querySelector('.table-timestamp-date')?.closest('.flex-render-content')?.classList.contains('font-mono')).toBe(true);
  } finally {
    app.unmount();
    host.remove();
  }
});

describe('identifier cells', () => {
  it.each([640, 128])('retains the full identifier with a fixed tail at %ipx', async width => {
    const value = '0123456789abcdef0123456789abcdef';
    const sourceId = `identifier-render-${width}`;
    const storageKey = `logchef-tableState-1-${sourceId}`;
    localStorage.setItem(storageKey, JSON.stringify({
      columnOrder: ['timestamp', 'value', 'message'], columnSizing: { value: width },
      sizingVersion: COLUMN_SIZING_VERSION,
    }));
    const host = document.createElement('div');
    document.body.appendChild(host);
    const app = createApp(DataTable, {
      columns: [{ name: 'timestamp', type: 'DateTime' }, { name: 'value', type: 'String' }, { name: 'message', type: 'String' }],
      data: [{ timestamp: '2026-01-01T00:00:00Z', value, message: 'received' }],
      regexHighlights: { value: { pattern: '6789abcdef', isNegated: false } },
      stats: { execution_time_ms: 1, rows_read: 1, bytes_read: 32 },
      sourceId, teamId: 1,
    });
    app.use(createPinia());
    app.use(i18n);
    try {
      app.mount(host);
      await nextTick();
      const cell = host.querySelector('.table-identifier');
      expect(cell?.textContent).toBe(value);
      expect(cell?.getAttribute('title')).toBe(value);
      expect(cell?.querySelector('.table-identifier-head')?.textContent).toBe(value.slice(0, -8));
      expect(cell?.querySelector('.table-identifier-tail')?.textContent).toBe(value.slice(-8));
      expect(cell?.querySelector('.table-identifier-tail .search-highlight')?.textContent).toBe('89abcdef');
      expect([...cell?.querySelectorAll('.table-identifier-head .search-highlight') ?? []].map(part => part.textContent)).toContain('67');
      expect(cell?.textContent).not.toMatch(/[…\s]/);
      expect(cell?.children).toHaveLength(2);
      expect(cell?.closest('td')?.style.width).toBe(`${width}px`);
    } finally {
      app.unmount();
      host.remove();
      localStorage.removeItem(storageKey);
    }
  });
});
