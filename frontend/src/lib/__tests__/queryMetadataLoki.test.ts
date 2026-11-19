import { describe, expect, it } from 'vitest';
import {
  getNativeQueryLanguageForSource,
  getQueryLanguageLabel,
  isSchemalessSource,
  resolveSavedQueryMetadata,
  supportsQueryLanguage,
} from '../queryMetadata';

describe('Loki query metadata', () => {
  it.each([
    [{ source_type: 'loki', query_languages: ['logql'] }, 'logql'],
    [{ source_type: 'loki', query_languages: ['logchefql', 'logql'] }, 'logql'],
    [{ source_type: 'loki' }, 'logql'],
    [{ source_type: 'victorialogs', query_languages: ['logchefql', 'logsql'] }, 'logsql'],
    [{ source_type: 'clickhouse', query_languages: ['logchefql', 'clickhouse-sql'] }, 'clickhouse-sql'],
  ])('takes the native language from the first non-LogchefQL entry of %j', (source, language) => {
    expect(getNativeQueryLanguageForSource(source)).toBe(language);
  });

  it('labels LogQL', () => {
    expect(getQueryLanguageLabel('logql')).toBe('LogQL');
  });

  it('saves a native query on a Loki source as LogQL', () => {
    expect(resolveSavedQueryMetadata({ active_mode: 'native', source_type: 'loki', query_languages: ['logql'] }))
      .toEqual({ queryLanguage: 'logql', editorMode: 'native' });
  });

  it('keeps a stored LogQL language', () => {
    expect(resolveSavedQueryMetadata({ query_language: 'logql', editor_mode: 'native', source_type: 'loki', query_languages: ['logql'] }))
      .toEqual({ queryLanguage: 'logql', editorMode: 'native' });
    expect(supportsQueryLanguage({ query_languages: ['logql'] }, 'logchefql')).toBe(false);
  });

  it.each([
    ['victorialogs', true],
    ['loki', true],
    ['clickhouse', false],
  ])('treats %s as schemaless: %s', (source_type, schemaless) => {
    expect(isSchemalessSource({ source_type })).toBe(schemaless);
  });
});
