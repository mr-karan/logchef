import type { APIResponse } from '@/api/types';
import { exploreApi, type HistogramResponse, type QueryParams } from '@/api/explore';
import { isErrorResponse } from '@/api/types';

export interface HistogramData {
  bucket: string;
  log_count: number;
}

export interface HistogramParams {
  sourceId: number;
  teamId: number;
  timeRange: {
    start: string; // ISO string
    end: string; // ISO string
  };
  query: string;  // Already prepared SQL query
  queryType: 'logchefql' | 'sql';
  granularity?: string; // '1m', '10m', '1h', '1d', etc.
}

export class HistogramService {
  /**
   * Fetch histogram data for a given source, time range, and query
   * This expects the SQL query to already be properly prepared/transformed
   */
  static async fetchHistogramData(params: HistogramParams): Promise<{
    success: boolean;
    data?: HistogramResponse;
    error?: { message: string; details?: string };
  }> {
    try {
      // Prepare query parameters for the API
      const queryParams: QueryParams = {
        raw_sql: params.query,
        limit: 100,
        start_timestamp: new Date(params.timeRange.start).getTime(),
        end_timestamp: new Date(params.timeRange.end).getTime(),
        query_type: params.queryType
      };

      // Call the API
      const response = await exploreApi.getHistogramData(
        params.sourceId,
        queryParams,
        params.teamId,
        params.granularity
      );

      if (isErrorResponse(response)) {
        return {
          success: false,
          error: {
            message: response.message || 'Failed to fetch histogram data',
            details: response.error_type
          }
        };
      }

      return {
        success: true,
        data: response.data || undefined
      };
    } catch (error) {
      console.error('Error in HistogramService:', error);
      return {
        success: false,
        error: {
          message: 'Failed to fetch histogram data',
          details: error instanceof Error ? error.message : String(error)
        }
      };
    }
  }
}