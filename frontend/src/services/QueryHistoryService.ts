interface QueryHistoryEntry {
  id: string;
  teamId: number;
  sourceId: number;
  mode: 'logchefql' | 'native';
  query: string;
  timestamp: number;
  executionCount: number;
  lastExecuted: number;
  title?: string;
}

interface QueryHistoryOptions {
  teamId: number;
  sourceId: number;
  mode: 'logchefql' | 'native';
  query: string;
  title?: string;
}

const STORAGE_KEY = 'logchef_query_history';
const MAX_ENTRIES_PER_TEAM_SOURCE = 10;
const MAX_HISTORY_AGE_DAYS = 30;

class QueryHistoryService {
  // Generate a unique hash for de-duplication
  private generateQueryHash(teamId: number, sourceId: number, mode: string, query: string): string {
    const content = `${teamId}-${sourceId}-${mode}-${query}`;
    let hash = 0;
    for (let i = 0; i < content.length; i++) {
      const char = content.charCodeAt(i);
      hash = ((hash << 5) - hash) + char;
      hash = hash & hash; // Convert to 32-bit integer
    }
    return Math.abs(hash).toString(36);
  }

  // Get all query history from localStorage
  private getStoredHistory(): QueryHistoryEntry[] {
    try {
      const stored = localStorage.getItem(STORAGE_KEY);
      if (!stored) return [];

      const parsed = JSON.parse(stored);
      return Array.isArray(parsed) ? parsed : [];
    } catch (error) {
      console.warn('Failed to parse query history from localStorage:', error);
      return [];
    }
  }

  // Save history to localStorage
  private saveToLocalStorage(history: QueryHistoryEntry[]): void {
    try {
      localStorage.setItem(STORAGE_KEY, JSON.stringify(history));
    } catch (error) {
      console.warn('Failed to save query history to localStorage:', error);
      // If localStorage is full, try to clean up old entries
      this.cleanupOldEntries();
    }
  }

  // Clean up old entries (older than 30 days)
  private cleanupOldEntries(): void {
    const history = this.getStoredHistory();
    const cutoffTime = Date.now() - (MAX_HISTORY_AGE_DAYS * 24 * 60 * 60 * 1000);

    const filtered = history.filter(entry => entry.lastExecuted > cutoffTime);

    if (filtered.length !== history.length) {
      this.saveToLocalStorage(filtered);
    }
  }

  // Compact history to ensure max 10 entries per team-source
  private compactHistory(history: QueryHistoryEntry[]): QueryHistoryEntry[] {
    const teamSourceGroups = new Map<string, QueryHistoryEntry[]>();

    // Group by team-source combination
    history.forEach(entry => {
      const key = `${entry.teamId}-${entry.sourceId}`;
      if (!teamSourceGroups.has(key)) {
        teamSourceGroups.set(key, []);
      }
      teamSourceGroups.get(key)!.push(entry);
    });

    // Sort each group by lastExecuted and keep only the most recent 10
    const compacted: QueryHistoryEntry[] = [];
    teamSourceGroups.forEach(entries => {
      entries.sort((a, b) => b.lastExecuted - a.lastExecuted);
      compacted.push(...entries.slice(0, MAX_ENTRIES_PER_TEAM_SOURCE));
    });

    return compacted;
  }

  // Add or update query entry with de-duplication
  addQueryEntry(options: QueryHistoryOptions): void {
    const { teamId, sourceId, mode, query, title } = options;

    // Skip empty queries
    if (!query || !query.trim()) {
      return;
    }

    const history = this.getStoredHistory();
    const queryHash = this.generateQueryHash(teamId, sourceId, mode, query);
    const now = Date.now();

    // Check if this query already exists
    const existingIndex = history.findIndex(entry => {
      const existingHash = this.generateQueryHash(entry.teamId, entry.sourceId, entry.mode, entry.query);
      return existingHash === queryHash;
    });

    if (existingIndex >= 0) {
      // Update existing entry
      const existing = history[existingIndex];
      existing.executionCount++;
      existing.lastExecuted = now;
      if (title) {
        existing.title = title;
      }

      // Move to the end (most recent)
      history.splice(existingIndex, 1);
      history.push(existing);
    } else {
      // Add new entry
      const newEntry: QueryHistoryEntry = {
        id: `${now}-${Math.random().toString(36).substr(2, 9)}`,
        teamId,
        sourceId,
        mode,
        query: query.trim(),
        timestamp: now,
        executionCount: 1,
        lastExecuted: now,
        title
      };

      history.push(newEntry);
    }

    // Compact history to maintain size limits
    const compacted = this.compactHistory(history);
    this.saveToLocalStorage(compacted);
  }

  // Get query history for a specific team-source combination
  getQueryHistory(teamId: number, sourceId: number): QueryHistoryEntry[] {
    const history = this.getStoredHistory();

    // Filter for current team-source and sort by lastExecuted
    const filtered = history
      .filter(entry => entry.teamId === teamId && entry.sourceId === sourceId)
      .sort((a, b) => b.lastExecuted - a.lastExecuted);

    return filtered;
  }

  // Delete a specific query entry
  deleteQueryEntry(entryId: string): void {
    const history = this.getStoredHistory();
    const filtered = history.filter(entry => entry.id !== entryId);
    this.saveToLocalStorage(filtered);
  }

  // Clear history for a specific team-source combination
  clearTeamSourceHistory(teamId: number, sourceId: number): void {
    const history = this.getStoredHistory();
    const filtered = history.filter(entry => !(entry.teamId === teamId && entry.sourceId === sourceId));
    this.saveToLocalStorage(filtered);
  }

  // Clear all query history
  clearAllHistory(): void {
    try {
      localStorage.removeItem(STORAGE_KEY);
    } catch (error) {
      console.warn('Failed to clear query history from localStorage:', error);
    }
  }

  // Get storage statistics
  getStorageStats(): { totalEntries: number; teamSourceGroups: number; oldestEntry?: number } {
    const history = this.getStoredHistory();
    const teamSourceGroups = new Set(history.map(entry => `${entry.teamId}-${entry.sourceId}`));
    const oldestEntry = history.length > 0 ? Math.min(...history.map(entry => entry.timestamp)) : undefined;

    return {
      totalEntries: history.length,
      teamSourceGroups: teamSourceGroups.size,
      oldestEntry
    };
  }
}

// Export singleton instance
export const queryHistoryService = new QueryHistoryService();

// Export types for use in components
export type { QueryHistoryEntry, QueryHistoryOptions };