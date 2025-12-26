import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';

import { QueryService } from '../QueryService';
import { now, getLocalTimeZone } from '@internationalized/date';

vi.mock('@/stores/teams', () => ({
  useTeamsStore: vi.fn(() => ({
    currentTeamId: 1
  }))
}));

vi.mock('@/stores/sources', () => ({
  useSourcesStore: vi.fn(() => ({
    currentSourceDetails: { id: 1 }
  }))
}));

vi.mock('@/api/logchefql', () => ({
  logchefqlApi: {
    translate: vi.fn()
  },
  FilterCondition: {}
}));

import { logchefqlApi } from '@/api/logchefql';

describe('QueryService', () => {
  const testOptions = {
    tableName: 'logs.vector_logs',
    tsField: 'timestamp',
    timeRange: {
      start: now(getLocalTimeZone()).subtract({ hours: 24 }),
      end: now(getLocalTimeZone())
    },
    limit: 100
  };

  beforeEach(() => {
    vi.clearAllMocks();
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  describe('generateDefaultSQL', () => {
    it('should generate a default SQL query with proper structure', () => {
      const result = QueryService.generateDefaultSQL(testOptions);

      expect(result.success).toBe(true);
      expect(result.sql).toContain('SELECT *');
      expect(result.sql).toContain('FROM logs.vector_logs');
      expect(result.sql).toContain('WHERE');
      expect(result.sql).toContain('BETWEEN toDateTime');
      expect(result.sql).toContain('ORDER BY `timestamp` DESC');
      expect(result.sql).toContain('LIMIT 100');
    });

    it('should handle validation errors gracefully', () => {
      const invalidOptions = {
        ...testOptions,
        tableName: '',
      };

      const result = QueryService.generateDefaultSQL(invalidOptions);

      expect(result.success).toBe(true);
      expect(result.sql).toBeTruthy();
    });
  });

  describe('translateLogchefQLToSQLAsync', () => {
    it('should translate a simple LogchefQL query to SQL', async () => {
      vi.mocked(logchefqlApi.translate).mockResolvedValue({
        data: {
          sql: "`level` = 'error'",
          valid: true,
          conditions: [{ field: 'level', operator: '=', value: 'error', is_regex: false }],
          fields_used: ['level']
        }
      } as any);

      const result = await QueryService.translateLogchefQLToSQLAsync({
        ...testOptions,
        logchefqlQuery: 'level="error"'
      });

      expect(result.success).toBe(true);
      expect(result.sql).toContain('WHERE');
      expect(result.sql).toContain("`level` = 'error'");
      expect(logchefqlApi.translate).toHaveBeenCalledWith(1, 1, { query: 'level="error"' });
    });

    it('should handle complex LogchefQL queries with AND/OR', async () => {
      vi.mocked(logchefqlApi.translate).mockResolvedValue({
        data: {
          sql: "(`level` = 'error' AND `status` > 500) OR `message` LIKE '%timeout%'",
          valid: true,
          conditions: [
            { field: 'level', operator: '=', value: 'error', is_regex: false },
            { field: 'status', operator: '>', value: '500', is_regex: false },
            { field: 'message', operator: '~', value: 'timeout', is_regex: true }
          ],
          fields_used: ['level', 'status', 'message']
        }
      } as any);

      const result = await QueryService.translateLogchefQLToSQLAsync({
        ...testOptions,
        logchefqlQuery: 'level="error" and status>500 or message~"timeout"'
      });

      expect(result.success).toBe(true);
      expect(result.sql).toContain('level');
      expect(result.sql).toContain('status');
      expect(result.sql).toContain('message');
    });

    it('should return warnings for invalid LogchefQL but still produce SQL', async () => {
      vi.mocked(logchefqlApi.translate).mockResolvedValue({
        data: {
          sql: '',
          valid: false,
          error: { code: 'PARSE_ERROR', message: 'Invalid syntax' },
          conditions: [],
          fields_used: []
        }
      } as any);

      const result = await QueryService.translateLogchefQLToSQLAsync({
        ...testOptions,
        logchefqlQuery: 'invalid query syntax'
      });

      expect(result.success).toBe(true);
      expect(result.warnings).toBeTruthy();
      expect(result.warnings?.some(w => w.includes('Invalid syntax'))).toBe(true);
      expect(result.sql).toContain('SELECT *');
      expect(result.sql).toContain('FROM');
    });

    it('should handle missing team/source gracefully', async () => {
      const { useTeamsStore } = await import('@/stores/teams');
      const { useSourcesStore } = await import('@/stores/sources');
      
      vi.mocked(useTeamsStore).mockReturnValue({ currentTeamId: null } as any);
      vi.mocked(useSourcesStore).mockReturnValue({ currentSourceDetails: null } as any);

      const result = await QueryService.translateLogchefQLToSQLAsync({
        ...testOptions,
        logchefqlQuery: 'level="error"'
      });

      expect(result.success).toBe(true);
      expect(result.warnings).toBeTruthy();
      expect(result.warnings?.some(w => w.includes('Team or source not available'))).toBe(true);
      expect(logchefqlApi.translate).not.toHaveBeenCalled();
    });
  });

  describe('updateTimeRange', () => {
    it('should update the time range in an existing SQL query', () => {
      const sql = 'SELECT * FROM logs.vector_logs WHERE `timestamp` BETWEEN toDateTime(\'2023-01-01 00:00:00\') AND toDateTime(\'2023-01-02 00:00:00\') ORDER BY `timestamp` DESC LIMIT 100';

      const newTimeRange = {
        start: now(getLocalTimeZone()).subtract({ days: 7 }),
        end: now(getLocalTimeZone())
      };

      const result = QueryService.updateTimeRange(sql, 'timestamp', newTimeRange);

      expect(result.success).toBe(true);
      expect(result.sql).not.toContain('2023-01-01');
      expect(result.sql).toContain('BETWEEN toDateTime');
      expect(result.sql).toContain('ORDER BY `timestamp` DESC');
    });
  });

  describe('updateLimit', () => {
    it('should update the limit in an existing SQL query', () => {
      const sql = 'SELECT * FROM logs.vector_logs WHERE `timestamp` BETWEEN toDateTime(\'2023-01-01 00:00:00\') AND toDateTime(\'2023-01-02 00:00:00\') ORDER BY `timestamp` DESC LIMIT 100';

      const result = QueryService.updateLimit(sql, 500);

      expect(result.success).toBe(true);
      expect(result.sql).not.toContain('LIMIT 100');
      expect(result.sql).toContain('LIMIT 500');
    });
  });

  describe('prepareQueryForExecutionAsync', () => {
    it('should prepare a LogchefQL query for execution', async () => {
      vi.mocked(logchefqlApi.translate).mockResolvedValue({
        data: {
          sql: "`level` = 'error'",
          valid: true,
          conditions: [{ field: 'level', operator: '=', value: 'error', is_regex: false }],
          fields_used: ['level']
        }
      } as any);

      const result = await QueryService.prepareQueryForExecutionAsync({
        mode: 'logchefql',
        query: 'level="error"',
        ...testOptions
      });

      expect(result.success).toBe(true);
      expect(result.sql).toContain("`level` = 'error'");
    });

    it('should prepare a SQL query for execution', async () => {
      const sql = 'SELECT * FROM logs.vector_logs WHERE `timestamp` BETWEEN toDateTime(\'2023-01-01 00:00:00\') AND toDateTime(\'2023-01-02 00:00:00\') ORDER BY `timestamp` DESC LIMIT 100';

      const result = await QueryService.prepareQueryForExecutionAsync({
        mode: 'clickhouse-sql',
        query: sql,
        ...testOptions
      });

      expect(result.success).toBe(true);
      expect(result.sql).toContain('SELECT * FROM logs.vector_logs');
      expect(result.sql).toContain('WHERE `timestamp` BETWEEN toDateTime');
      expect(result.sql).toContain('ORDER BY `timestamp` DESC LIMIT 100');
      expect(result.sql).not.toContain('2023-01-01');
    });

    it('should handle empty queries correctly', async () => {
      const result = await QueryService.prepareQueryForExecutionAsync({
        mode: 'clickhouse-sql',
        query: '',
        ...testOptions
      });

      expect(result.success).toBe(false);
      expect(result.error).toBe('Query is empty');
    });
  });

});
