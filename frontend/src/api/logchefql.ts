/**
 * LogchefQL API client
 * 
 * This module provides the frontend API for interacting with the LogchefQL
 * backend endpoints. The backend handles all query parsing, validation, and
 * SQL generation - the frontend just sends LogchefQL strings and receives
 * results.
 */

import { apiClient } from './apiUtils';

// Types matching backend response structures

export interface ParseError {
  code: string;
  message: string;
  position?: {
    line: number;
    column: number;
  };
}

export interface FilterCondition {
  field: string;
  operator: string;
  value: string;
  is_regex: boolean;
}

export interface TranslateRequest {
  query: string;
  start_time?: string;   // Optional. Format: "YYYY-MM-DD HH:mm:ss" - required for full_sql
  end_time?: string;     // Optional. Format: "YYYY-MM-DD HH:mm:ss" - required for full_sql
  timezone?: string;     // Optional. e.g., "UTC", "Asia/Kolkata" - required for full_sql
  limit?: number;        // Optional. e.g., 100 - defaults to 100
}

export interface TranslateResponse {
  sql: string;           // WHERE clause conditions only
  full_sql?: string;     // Complete executable SQL (when time params provided)
  select_clause?: string;  // Custom SELECT clause if pipe operator used
  valid: boolean;
  error?: ParseError;
  conditions: FilterCondition[];
  fields_used: string[];
}

export interface ValidateResponse {
  valid: boolean;
  error?: ParseError;
}

export interface QueryRequest {
  query: string;
  start_time: string;  // ISO8601 format
  end_time: string;    // ISO8601 format
  timezone?: string;
  limit?: number;
  query_timeout?: number;
}

export interface QueryResponse {
  logs: Record<string, any>[];
  columns: { name: string; type: string }[];
  stats: {
    execution_time_ms: number;
    rows_read: number;
    bytes_read: number;
  };
  query_id?: string;
  generated_sql?: string;  // The SQL that was executed (for "Show SQL" feature)
}

/**
 * LogchefQL API functions
 */
export const logchefqlApi = {
  /**
   * Translate a LogchefQL query to SQL
   * Returns the SQL string, validity status, and extracted metadata
   * 
   * When start_time, end_time, timezone, and limit are provided,
   * also returns full_sql with the complete executable query
   */
  translate: (teamId: number, sourceId: number, request: TranslateRequest) =>
    apiClient.post<TranslateResponse>(
      `/teams/${teamId}/sources/${sourceId}/logchefql/translate`,
      request
    ),

  /**
   * Validate a LogchefQL query
   * Lightweight endpoint for real-time validation in the editor
   */
  validate: (teamId: number, sourceId: number, query: string) =>
    apiClient.post<ValidateResponse>(
      `/teams/${teamId}/sources/${sourceId}/logchefql/validate`,
      { query }
    ),

  /**
   * Execute a LogchefQL query
   * The backend handles translation and execution in one step
   */
  query: (teamId: number, sourceId: number, params: QueryRequest) =>
    apiClient.post<QueryResponse>(
      `/teams/${teamId}/sources/${sourceId}/logchefql/query`,
      params
    ),
};

/**
 * Helper function to translate a LogchefQL query with caching
 * Uses a simple in-memory cache to avoid repeated API calls for the same query
 */
const translationCache = new Map<string, { result: TranslateResponse; timestamp: number }>();
const CACHE_TTL_MS = 30000; // 30 seconds

export async function translateWithCache(
  teamId: number,
  sourceId: number,
  request: TranslateRequest
): Promise<TranslateResponse> {
  // Only cache simple translations (no time params)
  const cacheKey = `${teamId}:${sourceId}:${request.query}`;
  const isSimpleTranslation = !request.start_time && !request.end_time;
  
  if (isSimpleTranslation) {
    const cached = translationCache.get(cacheKey);
    if (cached && Date.now() - cached.timestamp < CACHE_TTL_MS) {
      return cached.result;
    }
  }

  const response = await logchefqlApi.translate(teamId, sourceId, request);
  
  if (response.data) {
    // Only cache simple translations
    if (isSimpleTranslation) {
      translationCache.set(cacheKey, {
        result: response.data,
        timestamp: Date.now(),
      });
    }
    
    // Cleanup old cache entries
    if (translationCache.size > 100) {
      const now = Date.now();
      for (const [key, value] of translationCache.entries()) {
        if (now - value.timestamp > CACHE_TTL_MS) {
          translationCache.delete(key);
        }
      }
    }
    
    return response.data;
  }

  // Return an error result if the API call failed
  return {
    sql: '',
    valid: false,
    error: {
      code: 'API_ERROR',
      message: response.error?.message || 'Failed to translate query',
    },
    conditions: [],
    fields_used: [],
  };
}

/**
 * Helper function to validate a LogchefQL query with debouncing
 * Returns a function that can be cancelled
 */
export function createDebouncedValidator(
  teamId: number,
  sourceId: number,
  delayMs: number = 300
) {
  let timeoutId: ReturnType<typeof setTimeout> | null = null;
  let abortController: AbortController | null = null;

  return {
    validate: (query: string): Promise<ValidateResponse> => {
      // Cancel any pending validation
      if (timeoutId) {
        clearTimeout(timeoutId);
      }
      if (abortController) {
        abortController.abort();
      }

      return new Promise((resolve) => {
        timeoutId = setTimeout(async () => {
          abortController = new AbortController();
          
          try {
            const response = await logchefqlApi.validate(teamId, sourceId, query);
            if (response.data) {
              resolve(response.data);
            } else {
              resolve({
                valid: false,
                error: {
                  code: 'API_ERROR',
                  message: response.error?.message || 'Validation failed',
                },
              });
            }
          } catch (error) {
            // Ignore abort errors
            if ((error as Error).name !== 'AbortError') {
              resolve({
                valid: false,
                error: {
                  code: 'API_ERROR',
                  message: 'Validation request failed',
                },
              });
            }
          }
        }, delayMs);
      });
    },
    
    cancel: () => {
      if (timeoutId) {
        clearTimeout(timeoutId);
        timeoutId = null;
      }
      if (abortController) {
        abortController.abort();
        abortController = null;
      }
    },
  };
}

/**
 * Clear the translation cache
 * Call this when switching sources or when schema changes
 */
export function clearTranslationCache(): void {
  translationCache.clear();
}

