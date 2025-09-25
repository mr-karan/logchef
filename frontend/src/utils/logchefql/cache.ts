import type { ASTNode } from './types';
import type { SchemaInfo } from './sql-generator';

interface CacheEntry {
  ast: ASTNode;
  sql: string;
  params: unknown[];
  timestamp: number;
  accessCount: number;
  schemaVersion?: string; // Track schema version for invalidation
}

export class QueryCache {
  private cache = new Map<string, CacheEntry>();
  private maxSize: number;
  private ttl: number;
  private totalRequests: number = 0;
  private totalHits: number = 0;

  constructor(maxSize: number = 100, ttl: number = 5 * 60 * 1000) {
    this.maxSize = maxSize;
    this.ttl = ttl;
  }

  get(query: string, options?: { schema?: SchemaInfo; schemaVersion?: string }): {
    ast: ASTNode;
    sql: string;
    params: unknown[];
  } | null {
    this.totalRequests++;
    const key = this.generateCacheKey(query, options);
    const entry = this.cache.get(key);

    if (!entry) return null;

    // Check TTL
    if (Date.now() - entry.timestamp > this.ttl) {
      this.cache.delete(key);
      return null;
    }

    // Check schema version compatibility
    // Both sides should use the same default version for comparison
    const requestedVersion = options?.schemaVersion || 'v1';
    const entryVersion = entry.schemaVersion || 'v1';
    if (requestedVersion !== entryVersion) {
      this.cache.delete(key);
      return null;
    }

    // Cache hit - update statistics and refresh TTL
    this.totalHits++;
    entry.accessCount++;
    entry.timestamp = Date.now(); // Refresh TTL on cache hit (LRU-ish behavior)

    return {
      ast: entry.ast,
      sql: entry.sql,
      params: entry.params
    };
  }

  set(
    query: string,
    ast: ASTNode,
    sql: string,
    params: unknown[],
    options?: { schema?: SchemaInfo; schemaVersion?: string }
  ): void {
    const key = this.generateCacheKey(query, options);

    // Implement LRU eviction if cache is full
    if (this.cache.size >= this.maxSize) {
      this.evictLeastRecentlyUsed();
    }

    this.cache.set(key, {
      ast,
      sql,
      params,
      timestamp: Date.now(),
      accessCount: 1,
      schemaVersion: options?.schemaVersion || 'v1'
    });
  }

  private generateCacheKey(query: string, options?: { schema?: SchemaInfo; schemaVersion?: string }): string {
    const schemaHash = options?.schema
      ? this.hash(JSON.stringify(options.schema))
      : 'no-schema';
    const schemaVersion = options?.schemaVersion || 'v1';
    return `${query}:${schemaHash}:${schemaVersion}`;
  }

  private hash(str: string): string {
    // Simple hash function for cache keys
    let hash = 0;
    for (let i = 0; i < str.length; i++) {
      const char = str.charCodeAt(i);
      hash = ((hash << 5) - hash) + char;
      hash = hash & hash; // Convert to 32-bit integer
    }
    return hash.toString();
  }

  private evictLeastRecentlyUsed(): void {
    let oldestKey: string | null = null;
    let oldestTime = Date.now();

    for (const [key, entry] of this.cache.entries()) {
      if (entry.timestamp < oldestTime) {
        oldestTime = entry.timestamp;
        oldestKey = key;
      }
    }

    if (oldestKey) {
      this.cache.delete(oldestKey);
    }
  }

  clear(): void {
    this.cache.clear();
    this.totalRequests = 0;
    this.totalHits = 0;
  }

  /**
   * Invalidate cache entries that don't match the current schema version
   */
  invalidateBySchemaVersion(currentSchemaVersion: string): number {
    let invalidatedCount = 0;
    const keysToDelete: string[] = [];

    for (const [key, entry] of this.cache.entries()) {
      const entryVersion = entry.schemaVersion || 'v1';
      if (entryVersion !== currentSchemaVersion) {
        keysToDelete.push(key);
        invalidatedCount++;
      }
    }

    keysToDelete.forEach(key => this.cache.delete(key));
    return invalidatedCount;
  }

  /**
   * Get entries by schema version for debugging
   */
  getEntriesBySchemaVersion(): Map<string, number> {
    const versionCounts = new Map<string, number>();

    for (const entry of this.cache.values()) {
      const version = entry.schemaVersion || 'unknown';
      versionCounts.set(version, (versionCounts.get(version) || 0) + 1);
    }

    return versionCounts;
  }

  getStats(): { size: number; hitRate: number; totalRequests: number; totalHits: number; totalAccesses: number } {
    const entries = Array.from(this.cache.values());
    const totalAccesses = entries.reduce((sum, entry) => sum + entry.accessCount, 0);

    return {
      size: this.cache.size,
      hitRate: this.totalRequests > 0 ? (this.totalHits / this.totalRequests) * 100 : 0,
      totalRequests: this.totalRequests,
      totalHits: this.totalHits,
      totalAccesses
    };
  }
}