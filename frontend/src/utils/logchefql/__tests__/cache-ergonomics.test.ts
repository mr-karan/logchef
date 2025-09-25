import { describe, it, expect, beforeEach, vi } from 'vitest';
import { QueryCache } from '../cache';
import type { ASTNode } from '../types';

describe('Cache Ergonomics', () => {
  let cache: QueryCache;

  const mockAst: ASTNode = {
    type: 'expression',
    key: 'field',
    operator: '=' as any,
    value: 'test'
  };

  const mockSql = 'SELECT * FROM table WHERE field = ?';
  const mockParams = ['test'];

  beforeEach(() => {
    cache = new QueryCache(5, 1000); // Small cache, 1 second TTL for testing
    vi.clearAllTimers();
  });

  describe('TTL refresh on cache hits', () => {
    it('should refresh TTL when cache is hit', async () => {
      const query = 'field = "test"';

      // Store in cache
      cache.set(query, mockAst, mockSql, mockParams);

      // Wait 500ms (half of TTL)
      vi.useFakeTimers();
      vi.advanceTimersByTime(500);

      // Cache hit should refresh the TTL
      const result1 = cache.get(query);
      expect(result1).not.toBeNull();

      // Wait another 700ms (total 1200ms, but TTL was refreshed at 500ms)
      vi.advanceTimersByTime(700);

      // Should still be cached because TTL was refreshed
      const result2 = cache.get(query);
      expect(result2).not.toBeNull();

      vi.useRealTimers();
    });

    it('should expire entries that are not accessed', async () => {
      const query = 'field = "test"';

      // Store in cache
      cache.set(query, mockAst, mockSql, mockParams);

      vi.useFakeTimers();

      // Wait longer than TTL without accessing
      vi.advanceTimersByTime(1500);

      // Should be expired
      const result = cache.get(query);
      expect(result).toBeNull();

      vi.useRealTimers();
    });
  });

  describe('Schema version support', () => {
    it('should store and retrieve entries with schema version', () => {
      const query = 'field = "test"';
      const schemaVersion = 'v2.0';

      cache.set(query, mockAst, mockSql, mockParams, { schemaVersion });

      const result = cache.get(query, { schemaVersion });
      expect(result).not.toBeNull();
      expect(result?.sql).toBe(mockSql);
    });

    it('should invalidate entries with different schema versions', () => {
      const query = 'field = "test"';
      const oldVersion = 'v1.0';
      const newVersion = 'v2.0';

      // Store with old version
      cache.set(query, mockAst, mockSql, mockParams, { schemaVersion: oldVersion });

      // Try to get with new version - should return null
      const result = cache.get(query, { schemaVersion: newVersion });
      expect(result).toBeNull();
    });

    it('should allow invalidation by schema version', () => {
      const query1 = 'field1 = "test1"';
      const query2 = 'field2 = "test2"';
      const query3 = 'field3 = "test3"';

      // Store with different schema versions
      cache.set(query1, mockAst, mockSql, mockParams, { schemaVersion: 'v1.0' });
      cache.set(query2, mockAst, mockSql, mockParams, { schemaVersion: 'v2.0' });
      cache.set(query3, mockAst, mockSql, mockParams, { schemaVersion: 'v1.0' });

      expect(cache.getStats().size).toBe(3);

      // Invalidate all v1.0 entries
      const invalidatedCount = cache.invalidateBySchemaVersion('v2.0');
      expect(invalidatedCount).toBe(2); // query1 and query3
      expect(cache.getStats().size).toBe(1); // Only query2 remains

      // Verify the remaining entry is the v2.0 one
      const result = cache.get(query2, { schemaVersion: 'v2.0' });
      expect(result).not.toBeNull();
    });

    it('should provide schema version statistics', () => {
      const query1 = 'field1 = "test1"';
      const query2 = 'field2 = "test2"';
      const query3 = 'field3 = "test3"';
      const query4 = 'field4 = "test4"';

      // Store with different schema versions
      cache.set(query1, mockAst, mockSql, mockParams, { schemaVersion: 'v1.0' });
      cache.set(query2, mockAst, mockSql, mockParams, { schemaVersion: 'v2.0' });
      cache.set(query3, mockAst, mockSql, mockParams, { schemaVersion: 'v1.0' });
      cache.set(query4, mockAst, mockSql, mockParams); // No version - defaults to 'v1'

      const versionStats = cache.getEntriesBySchemaVersion();
      expect(versionStats.get('v1.0')).toBe(2);
      expect(versionStats.get('v2.0')).toBe(1);
      expect(versionStats.get('v1')).toBe(1); // Default version
    });
  });

  describe('Hit rate tracking', () => {
    it('should track hits and requests correctly', () => {
      const query1 = 'field1 = "test1"';
      const query2 = 'field2 = "test2"';

      // Initial stats
      expect(cache.getStats().totalRequests).toBe(0);
      expect(cache.getStats().totalHits).toBe(0);
      expect(cache.getStats().hitRate).toBe(0);

      // Store entries
      cache.set(query1, mockAst, mockSql, mockParams);
      cache.set(query2, mockAst, mockSql, mockParams);

      // Miss (query not in cache)
      cache.get('nonexistent');
      expect(cache.getStats().totalRequests).toBe(1);
      expect(cache.getStats().totalHits).toBe(0);
      expect(cache.getStats().hitRate).toBe(0);

      // Hit
      cache.get(query1);
      expect(cache.getStats().totalRequests).toBe(2);
      expect(cache.getStats().totalHits).toBe(1);
      expect(cache.getStats().hitRate).toBe(50);

      // Another hit
      cache.get(query2);
      expect(cache.getStats().totalRequests).toBe(3);
      expect(cache.getStats().totalHits).toBe(2);
      expect(cache.getStats().hitRate).toBe(66.66666666666666);

      // Hit the same query again
      cache.get(query1);
      expect(cache.getStats().totalRequests).toBe(4);
      expect(cache.getStats().totalHits).toBe(3);
      expect(cache.getStats().hitRate).toBe(75);
    });

    it('should reset stats when cache is cleared', () => {
      const query = 'field = "test"';

      cache.set(query, mockAst, mockSql, mockParams);
      cache.get(query);
      cache.get('nonexistent');

      expect(cache.getStats().totalRequests).toBeGreaterThan(0);
      expect(cache.getStats().totalHits).toBeGreaterThan(0);

      cache.clear();

      expect(cache.getStats().totalRequests).toBe(0);
      expect(cache.getStats().totalHits).toBe(0);
      expect(cache.getStats().hitRate).toBe(0);
      expect(cache.getStats().size).toBe(0);
    });
  });

  describe('LRU behavior with TTL refresh', () => {
    it('should prioritize recently accessed entries for eviction', async () => {
      const cache = new QueryCache(3, 10000); // 3 entries max, long TTL

      // Fill cache to capacity
      cache.set('query1', mockAst, 'sql1', []);
      // Wait to ensure different timestamps
      await new Promise(resolve => setTimeout(resolve, 10));
      cache.set('query2', mockAst, 'sql2', []);
      await new Promise(resolve => setTimeout(resolve, 10));
      cache.set('query3', mockAst, 'sql3', []);

      expect(cache.getStats().size).toBe(3);

      // Access query1 to refresh its timestamp - it should become the most recent
      cache.get('query1');

      // Add a new entry - should evict query2 (oldest unaccessed)
      cache.set('query4', mockAst, 'sql4', []);

      expect(cache.getStats().size).toBe(3);

      // query1 should still exist (was accessed recently)
      const result1 = cache.get('query1');
      expect(result1).not.toBeNull();
      expect(result1?.sql).toBe('sql1');

      // query4 should exist (just added)
      const result4 = cache.get('query4');
      expect(result4).not.toBeNull();
      expect(result4?.sql).toBe('sql4');

      // query2 should be evicted (was the oldest without recent access)
      expect(cache.get('query2')).toBeNull();

      // query3 should still exist
      expect(cache.get('query3')).not.toBeNull();
    });
  });

  describe('Cache key generation with schema version', () => {
    it('should generate different keys for different schema versions', () => {
      const query = 'field = "test"';

      // Same query with different schema versions should be treated as different cache entries
      cache.set(query, mockAst, 'sql_v1', [], { schemaVersion: 'v1' });
      cache.set(query, mockAst, 'sql_v2', [], { schemaVersion: 'v2' });

      expect(cache.getStats().size).toBe(2);

      const result_v1 = cache.get(query, { schemaVersion: 'v1' });
      const result_v2 = cache.get(query, { schemaVersion: 'v2' });

      expect(result_v1?.sql).toBe('sql_v1');
      expect(result_v2?.sql).toBe('sql_v2');
    });

    it('should handle missing schema version gracefully', () => {
      const query = 'field = "test"';

      // Store without schema version
      cache.set(query, mockAst, mockSql, mockParams);

      // Should be retrievable without schema version
      expect(cache.get(query)).not.toBeNull();

      // Should be retrievable with default version
      expect(cache.get(query, { schemaVersion: 'v1' })).not.toBeNull();
    });
  });

  describe('Integration scenarios', () => {
    it('should handle complex cache operations during log exploration', () => {
      const queries = [
        'level = "error"',
        'service = "api"',
        'level = "error" and service = "api"',
        'timestamp > "2023-01-01"'
      ];

      const schemaV1 = 'v1.0';
      const schemaV2 = 'v2.0';

      // Simulate user exploration with v1.0 schema
      queries.forEach((query, i) => {
        cache.set(query, mockAst, `sql_v1_${i}`, [], { schemaVersion: schemaV1 });
      });

      // User accesses some queries multiple times
      cache.get(queries[0], { schemaVersion: schemaV1 });
      cache.get(queries[2], { schemaVersion: schemaV1 });

      expect(cache.getStats().size).toBe(4);
      expect(cache.getStats().totalHits).toBe(2);

      // Schema changes to v2.0 - invalidate old entries
      const invalidated = cache.invalidateBySchemaVersion(schemaV2);
      expect(invalidated).toBe(4);
      expect(cache.getStats().size).toBe(0);

      // User continues exploration with v2.0 schema
      queries.slice(0, 2).forEach((query, i) => {
        cache.set(query, mockAst, `sql_v2_${i}`, [], { schemaVersion: schemaV2 });
      });

      // Verify new entries are cached correctly
      expect(cache.get(queries[0], { schemaVersion: schemaV2 })?.sql).toBe('sql_v2_0');
      expect(cache.get(queries[1], { schemaVersion: schemaV2 })?.sql).toBe('sql_v2_1');

      // Old entries shouldn't be accessible
      expect(cache.get(queries[2], { schemaVersion: schemaV2 })).toBeNull();
    });
  });
});