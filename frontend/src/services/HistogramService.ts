import { exploreApi, type HistogramResponse, type QueryParams } from '@/api/explore';
import { isErrorResponse } from '@/api/types';
import { useExploreStore } from '@/stores/explore';

export interface HistogramData {
  bucket: string;
  log_count: number;
  group_value?: string; // Field needed for grouped histogram data
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
  groupBy?: string; // Optional field to group by
  timezone?: string; // User's timezone identifier (e.g., 'America/New_York', 'UTC')
}

export class HistogramService {
  // Default target number of buckets we want to display
  private static readonly TARGET_BUCKETS = 100;

  // Minimum number of buckets (kept for future use)
  // private static readonly MIN_BUCKETS = 10;

  // List of "nice" intervals in seconds, from smallest to largest
  private static readonly NICE_INTERVALS = [
    { seconds: 1, label: '1s' },       // 1 second
    { seconds: 5, label: '5s' },       // 5 seconds
    { seconds: 10, label: '10s' },     // 10 seconds
    { seconds: 15, label: '15s' },     // 15 seconds
    { seconds: 30, label: '30s' },     // 30 seconds
    { seconds: 60, label: '1m' },      // 1 minute
    { seconds: 300, label: '5m' },     // 5 minutes
    { seconds: 600, label: '10m' },    // 10 minutes
    { seconds: 900, label: '15m' },    // 15 minutes
    { seconds: 1800, label: '30m' },   // 30 minutes
    { seconds: 3600, label: '1h' },    // 1 hour
    { seconds: 7200, label: '2h' },    // 2 hours
    { seconds: 10800, label: '3h' },   // 3 hours
    { seconds: 21600, label: '6h' },   // 6 hours
    { seconds: 43200, label: '12h' },  // 12 hours
    { seconds: 86400, label: '24h' }   // 1 day
  ];

  /**
   * Calculate optimal granularity based on time range span and target bucket count
   * Uses an algorithm similar to Kibana's auto_date_histogram to maintain consistent
   * visual density regardless of time range.
   */
  static calculateOptimalGranularity(startTime: string, endTime: string): string {
    try {
      const start = new Date(startTime).getTime();
      const end = new Date(endTime).getTime();
      const diffMs = end - start;
      const diffSeconds = diffMs / 1000;
      const diffMinutes = diffSeconds / 60;
      const diffHours = diffMinutes / 60;
      const diffDays = diffHours / 24;

      // Log time range details for debugging
      console.log(`Time range span: ${diffDays.toFixed(2)} days, ${diffHours.toFixed(2)} hours, ${diffMinutes.toFixed(2)} minutes`);

      // Calculate ideal interval to achieve target bucket count
      const idealIntervalSeconds = diffSeconds / this.TARGET_BUCKETS;

      // Find the smallest "nice" interval that is >= our ideal interval
      let selectedInterval = this.NICE_INTERVALS[this.NICE_INTERVALS.length - 1]; // Default to largest

      for (const interval of this.NICE_INTERVALS) {
        if (interval.seconds >= idealIntervalSeconds) {
          selectedInterval = interval;
          break;
        }
      }

      // Calculate how many buckets this interval will produce
      const estimatedBuckets = diffSeconds / selectedInterval.seconds;

      console.log(`Selected interval: ${selectedInterval.label} (${selectedInterval.seconds}s), ` +
                 `which will produce ~${Math.ceil(estimatedBuckets)} buckets`);

      // Return the label format expected by the backend
      return selectedInterval.label;
    } catch (error) {
      console.error('Error calculating optimal granularity:', error);
      // Fall back to 1h if there's an error
      return '1h';
    }
  }

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
      console.log("HistogramService: Starting fetchHistogramData with params:", {
        sourceId: params.sourceId,
        teamId: params.teamId,
        timeRange: {
          start: params.timeRange.start,
          end: params.timeRange.end
        },
        queryType: params.queryType,
        groupBy: params.groupBy,
        timezone: params.timezone,
        queryLength: params.query?.length || 0
      });

      // Auto-calculate granularity if not explicitly provided
      const granularity = params.granularity ||
        this.calculateOptimalGranularity(params.timeRange.start, params.timeRange.end);

      console.log(`Using histogram granularity: ${granularity}`);

      // Get the explore store to access generatedDisplaySql
      const exploreStore = useExploreStore();

      // Always use the provided query parameter first, then try store properties as fallback
      // This ensures we use the exact same SQL for both query and histogram endpoints
      const sql = params.query && params.query.trim() ? params.query : exploreStore.sqlForExecution;

      // Prepare query parameters for the API
      const queryParams: QueryParams = {
        raw_sql: sql || "", // Ensure we don't send undefined
        limit: 100,
        window: granularity,
        timezone: params.timezone,
      };

      // Only include group_by if it has a non-empty value
      if (params.groupBy && params.groupBy.trim() !== '') {
        queryParams.group_by = params.groupBy;
      }

      console.log("HistogramService: Calling API with params:", {
        sourceId: params.sourceId,
        window: queryParams.window,
        groupBy: queryParams.group_by,
        timezone: queryParams.timezone
      });

      // Call the API with all parameters in the body
      const response = await exploreApi.getHistogramData(
        params.sourceId,
        queryParams,
        params.teamId
      );

      console.log("HistogramService: API response received",
                 isErrorResponse(response) ? "Error" : "Success");

      if (isErrorResponse(response)) {
        console.error("HistogramService: Error response", response);
        return {
          success: false,
          error: {
            message: response.message || 'Failed to fetch histogram data',
            details: response.error_type
          }
        };
      }

      console.log("HistogramService: Success response with data points:",
                 response.data?.data?.length || 0);

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
