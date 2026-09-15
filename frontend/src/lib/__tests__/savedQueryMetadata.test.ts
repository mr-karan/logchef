import { describe, expect, it } from 'vitest';
import { resolveSavedQueryMetadata } from '../queryMetadata';

describe('saved query editor metadata', () => {
  it('saves a new Search query as LogchefQL', () => {
    expect(resolveSavedQueryMetadata({ active_mode: 'logchefql', source_type: 'clickhouse' }))
      .toEqual({ queryLanguage: 'logchefql', editorMode: 'builder' });
  });

  it('uses the active Search editor when a saved SQL query was loaded', () => {
    expect(resolveSavedQueryMetadata({ active_mode: 'logchefql', query_language: 'clickhouse-sql', editor_mode: 'native' }))
      .toEqual({ queryLanguage: 'logchefql', editorMode: 'builder' });
  });

  it.each([
    ['clickhouse', 'clickhouse-sql'],
    ['victorialogs', 'logsql'],
  ])('uses the native language for %s after switching tabs', (source_type, queryLanguage) => {
    expect(resolveSavedQueryMetadata({ active_mode: 'native', source_type, query_language: 'logchefql', editor_mode: 'builder' }))
      .toEqual({ queryLanguage, editorMode: 'native' });
  });

  it('preserves stored metadata when editing outside Explorer', () => {
    expect(resolveSavedQueryMetadata({ query_language: 'logchefql', editor_mode: 'builder' }))
      .toEqual({ queryLanguage: 'logchefql', editorMode: 'builder' });
  });
});
