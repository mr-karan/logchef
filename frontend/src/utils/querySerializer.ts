import type { SavedQueryContent } from "@/api/types";

/**
 * Serializes the current explore state to a saved query content object
 * 
 * @param state Current explore state
 * @returns Serialized saved query content
 */
export function serializeQueryState(state: any): SavedQueryContent {
  // Get timestamps for the time range
  const getTimestamps = () => {
    if (!state.timeRange?.start || !state.timeRange?.end) {
      // Default to last hour if time range not set
      const end = new Date();
      const start = new Date(end.getTime() - 60 * 60 * 1000);
      return {
        start: start.getTime(),
        end: end.getTime()
      };
    }

    // Try to extract timestamps from the time range objects
    try {
      const start = state.timeRange.start instanceof Date 
        ? state.timeRange.start.getTime() 
        : new Date(state.timeRange.start.toString()).getTime();
      
      const end = state.timeRange.end instanceof Date 
        ? state.timeRange.end.getTime() 
        : new Date(state.timeRange.end.toString()).getTime();
      
      return { start, end };
    } catch (error) {
      console.error("Error serializing time range:", error);
      const end = new Date();
      const start = new Date(end.getTime() - 60 * 60 * 1000);
      return {
        start: start.getTime(),
        end: end.getTime()
      };
    }
  };

  const timestamps = getTimestamps();

  return {
    version: 1,
    activeTab: state.rawSql ? "raw_sql" : "filters",
    sourceId: state.sourceId || "",
    timeRange: {
      absolute: {
        start: timestamps.start,
        end: timestamps.end,
      },
    },
    limit: state.limit || 100,
    rawSql: state.rawSql || "",
  };
}

/**
 * Deserializes a saved query content object into a partial explore state
 * 
 * @param content Saved query content
 * @returns Partial explore state
 */
export function deserializeQueryContent(content: SavedQueryContent): any {
  // Convert timestamps back to Date objects
  const startDate = new Date(content.timeRange.absolute.start);
  const endDate = new Date(content.timeRange.absolute.end);
  
  return {
    sourceId: content.sourceId,
    limit: content.limit,
    timeRange: {
      start: startDate,
      end: endDate
    },
    rawSql: content.rawSql,
  };
}

/**
 * Generates a URL for a saved query
 * 
 * @param queryId Saved query ID
 * @param sourceId Source ID
 * @param limit Result limit
 * @param start Start timestamp
 * @param end End timestamp
 * @returns URL string
 */
export function generateQueryURL(
  queryId: string,
  sourceId: string,
  limit: number,
  start: number,
  end: number
): string {
  const params = new URLSearchParams();
  params.set("query_id", queryId);
  params.set("source", sourceId);
  params.set("limit", limit.toString());
  params.set("start_time", start.toString());
  params.set("end_time", end.toString());

  return `/logs/explore?${params.toString()}`;
}