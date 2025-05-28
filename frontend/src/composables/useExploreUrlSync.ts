import { ref, watch, nextTick, computed } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { useExploreStore } from '@/stores/explore';
import { useTeamsStore } from '@/stores/teams';
import { useSourcesStore } from '@/stores/sources';
import { useSavedQueriesStore } from '@/stores/savedQueries';
import {
  CalendarDateTime,
  now,
  getLocalTimeZone,
  type DateValue,
  toCalendarDateTime
} from '@internationalized/date';
import { parseRelativeTimeString } from '@/utils/time';

export function useExploreUrlSync() {
  const route = useRoute();
  const router = useRouter();
  const exploreStore = useExploreStore();
  const teamsStore = useTeamsStore();
  const sourcesStore = useSourcesStore();

  const isInitializing = ref(true);
  const initializationError = ref<string | null>(null);
  const skipNextUrlSync = ref(false);

  // A guard flag to prevent URL updates during the active loading of a page with relativeTime
  let preservingRelativeTime = false;

  // Add a last initialization timestamp to prevent rapid re-initialization
  let lastInitTimestamp = 0;

  // --- Internal Helper Functions ---

  function parseTimestamp(value: string | null | undefined): number | null {
    if (!value) return null;
    const num = parseInt(value);
    return !isNaN(num) ? num : null;
  }

  function timestampToCalendarDateTime(timestamp: number | null): CalendarDateTime | null {
    if (timestamp === null) return null;
    try {
      const date = new Date(timestamp);
      if (isNaN(date.getTime())) return null;
      // Construct CalendarDateTime directly from JS Date parts
      return new CalendarDateTime(
        date.getFullYear(),
        date.getMonth() + 1, // JS month is 0-indexed
        date.getDate(),
        date.getHours(),
        date.getMinutes(),
        date.getSeconds(),
        date.getMilliseconds()
      );
    } catch (e) {
      console.error("Error converting timestamp to CalendarDateTime:", e);
      return null;
    }
  }

  function calendarDateTimeToTimestamp(dateTime: DateValue | null | undefined): number | null {
    if (!dateTime) return null;
    try {
      // Convert any DateValue to JS Date object using the local timezone
      const date = dateTime.toDate(getLocalTimeZone());
      return date.getTime();
    } catch (e) {
      console.error("Error converting DateValue to timestamp:", e);
      return null;
    }
  }

  // Helper to safely check source details
  function isCorrectSourceDetail(details: any, expectedId: number): boolean {
    if (!details) return false;
    if (typeof details !== 'object') return false;
    // @ts-ignore - This is a runtime check
    if (!('id' in details)) return false;
    // @ts-ignore - We've already checked that id exists
    return details.id === expectedId;
  }

  // --- Initialization Logic ---

  async function initializeFromUrl() {
    // Prevent multiple initializations within 500ms of each other (reduced from 1000ms)
    const now = Date.now();
    if (now - lastInitTimestamp < 500) {
      console.log("useExploreUrlSync: Skipping initialization - too soon after previous init");
      return;
    }
    lastInitTimestamp = now;

    isInitializing.value = true;
    initializationError.value = null;

    // Check immediately if we have a relativeTime parameter in the URL
    const hasRelativeTimeParam = !!route.query.relativeTime;
    if (hasRelativeTimeParam) {
      // Set a flag to make all URL sync operations more cautious
      preservingRelativeTime = true;
    }

    // Check if we have a query_id in the URL, which indicates we're loading a saved query
    const queryId = route.query.query_id as string | undefined;
    const hasQueryId = !!queryId;

    try {
      // 1. Ensure Teams are loaded (wait if necessary)
      if (!teamsStore.teams || teamsStore.teams.length === 0) {
        await teamsStore.loadTeams(false, false); // Explicitly use user teams endpoint
      }

      // Handler the case where no teams are available more gracefully
      if (teamsStore.teams.length === 0) {
        // Set initialization error without throwing
        initializationError.value = "No teams available or accessible.";

        // Clear any existing data for consistency
        exploreStore.setSource(0);
        sourcesStore.clearCurrentSourceDetails();

        // Exit early but don't throw - allow the component to handle this state
        return;
      }

      // 2. Set Team from URL or default
      let teamId: number | null = null;
      const urlTeamIdStr = route.query.team as string | undefined;
      if (urlTeamIdStr) {
        const parsedTeamId = parseInt(urlTeamIdStr);
        if (!isNaN(parsedTeamId) && teamsStore.teams.some(t => t.id === parsedTeamId)) {
          teamId = parsedTeamId;
        } else {
          initializationError.value = `Invalid or inaccessible team ID: ${urlTeamIdStr}. Falling back to default.`;
        }
      }
      if (!teamId) {
        teamId = teamsStore.teams[0].id; // Default to first team
      }

      // Set team *before* loading sources
      if (teamsStore.currentTeamId !== teamId) {
        teamsStore.setCurrentTeam(teamId);
      }

      // 3. Load Sources for the selected team (wait if necessary)
      await sourcesStore.loadTeamSources(teamId);

      // 4. Set Source from URL or default (validate against loaded sources)
      let sourceId: number | null = null;
      const urlSourceIdStr = route.query.source as string | undefined;

      if (urlSourceIdStr) {
        const parsedSourceId = parseInt(urlSourceIdStr);

        if (!isNaN(parsedSourceId) && sourcesStore.teamSources.some(s => s.id === parsedSourceId)) {
          sourceId = parsedSourceId;
        } else {
          initializationError.value = `Invalid or inaccessible source ID: ${urlSourceIdStr} for team ${teamId}. Falling back to default.`;
        }
      }
      if (!sourceId && sourcesStore.teamSources.length > 0) {
        sourceId = sourcesStore.teamSources[0].id; // Default to first source
      }
      
      let didTriggerSourceDetailsLoad = false;
      
      // Set source ID and potentially load details
      if (sourceId) {
         // Check if this would be a source change
         const isSourceChange = exploreStore.sourceId !== sourceId;
         
         if (isSourceChange) {
            // Clear any previous source data since this is a new source
            console.log(`useExploreUrlSync: Changing source ID from ${exploreStore.sourceId} to ${sourceId}`);
            exploreStore.setSource(sourceId);
            
            // For initialization, we should load source details here to prevent race conditions
            try {
              console.log(`useExploreUrlSync: Pre-loading source details for ID ${sourceId}`);
              await sourcesStore.loadSourceDetails(sourceId);
              didTriggerSourceDetailsLoad = true;
              
              // Check if details loaded
              console.log(`useExploreUrlSync: Checking if source details are loaded for ID ${sourceId}`);
              if (!sourcesStore.currentSourceDetails) {
                console.warn(`useExploreUrlSync: Source details still not loaded after delay for ID ${sourceId}`);
              } else if (!isCorrectSourceDetail(sourcesStore.currentSourceDetails, sourceId)) {
                console.warn(`useExploreUrlSync: Source details don't match expected source ID ${sourceId}`);
              } else {
                console.log(`useExploreUrlSync: Successfully loaded details for source ID ${sourceId}`);
              }
            } catch (error) {
              console.error(`useExploreUrlSync: Error loading source details:`, error);
              // Don't fail the whole initialization for this
            }
         } else {
            console.log(`useExploreUrlSync: Source ID unchanged at ${sourceId}`);
         }
      } else {
         exploreStore.setSource(0); // Explicitly set to 0 if no valid source
         sourcesStore.clearCurrentSourceDetails();
         initializationError.value = `No sources available for team ${teamId}.`;
      }

      // If we have a query_id in the URL, we'll let the component handle loading the query
      // We'll still set other URL params but those will be overridden when the query is loaded
      if (hasQueryId) {
        console.log(`useExploreUrlSync: URL has query_id=${queryId}, component will load saved query content`);
        exploreStore.setSelectedQueryId(queryId); // Store the query_id for reference
      } else {
        // Only set these values from URL if we're not loading a saved query
        // 5. Set Limit from URL or default
        const urlLimitStr = route.query.limit as string | undefined;
        const limit = urlLimitStr ? parseInt(urlLimitStr) : 100;
        exploreStore.setLimit(!isNaN(limit) && limit > 0 && limit <= 10000 ? limit : 100);

        // 6. Time range - SIMPLIFIED PRIORITY:
        // First check for relative time (this takes precedence)
        const relativeTime = route.query.relativeTime as string | undefined;
        if (relativeTime) {
          // Set relative time - this will also calculate and set the absolute time range
          exploreStore.setRelativeTimeRange(relativeTime);
          // Make a note that we're preserving a relative time
          console.log(`Initializing with relative time: ${relativeTime}`);
          preservingRelativeTime = true;
        }
        // Only use absolute times if no relative time is specified
        else {
          const urlStartTime = parseTimestamp(route.query.start_time as string | undefined);
          const urlEndTime = parseTimestamp(route.query.end_time as string | undefined);

          // If we have valid timestamps, use them
          if (urlStartTime !== null && urlEndTime !== null) {
            const parsedStart = timestampToCalendarDateTime(urlStartTime);
            const parsedEnd = timestampToCalendarDateTime(urlEndTime);

            if (parsedStart && parsedEnd) {
              exploreStore.setTimeRange({ start: parsedStart, end: parsedEnd });
            } else {
              // Fall back to default range if timestamp parsing fails
              setDefaultTimeRange();
            }
          } else {
            // No times in URL, use default
            setDefaultTimeRange();
          }
        }

        // 7. Set Mode from URL or default
        const urlMode = route.query.mode as string | undefined;
        const mode = (urlMode === 'sql' ? 'sql' : 'logchefql') as 'logchefql' | 'sql';
        exploreStore.setActiveMode(mode);

        // 8. Set Query Content from URL
        const urlQuery = route.query.q as string | undefined;
        let queryContent = "";

        if (urlQuery) {
          try {
            // Safely decode the URL parameter, handling double-encoded characters
            queryContent = decodeURIComponent(urlQuery);
          } catch (decodeError) {
            // If decoding fails, use the raw value
            console.error("Error decoding URL query parameter:", decodeError);
            queryContent = urlQuery;
          }
        }

        if (mode === 'logchefql') {
          exploreStore.setLogchefqlCode(queryContent);
          exploreStore.setRawSql(""); // Clear other mode
        } else {
          exploreStore.setRawSql(queryContent);
          exploreStore.setLogchefqlCode(""); // Clear other mode
        }
      }

      // If we changed source but haven't loaded details yet, add a small delay to allow the details to load
      if (sourceId && !didTriggerSourceDetailsLoad && !sourcesStore.currentSourceDetails) {
        console.log(`useExploreUrlSync: Waiting for source details to load for ID ${sourceId}`);
        
        // Wait a bit for any in-flight source detail requests to complete
        await new Promise(resolve => setTimeout(resolve, 100));
        
        // Check if details loaded
        console.log(`useExploreUrlSync: Checking if source details are loaded for ID ${sourceId}`);
        if (!sourcesStore.currentSourceDetails) {
          console.warn(`useExploreUrlSync: Source details still not loaded after delay for ID ${sourceId}`);
        } else if (!isCorrectSourceDetail(sourcesStore.currentSourceDetails, sourceId)) {
          console.warn(`useExploreUrlSync: Source details don't match expected source ID ${sourceId}`);
        }
      }

    } catch (error: any) {
      console.error("useExploreUrlSync: Error during initialization:", error);
      initializationError.value = error.message || "Failed to initialize from URL.";
    } finally {
      // Use nextTick to ensure all initial store updates have propagated
      // before allowing watchers to update the URL.
      await nextTick();

      // CRITICAL: Save original URL parameters to avoid immediate overwrite
      const originalParams = { ...route.query };
      const hadRelativeTime = !!originalParams.relativeTime;

      // Mark initialization as complete *after* the next tick
      isInitializing.value = false;

      // Determine URL sync behavior *after* initialization is marked complete
      // Never auto-sync URL during load if we're preserving a relative time URL
      if (!preservingRelativeTime) {
          // Normal URL sync if no special handling is needed
          console.log('Safe to auto-sync URL now');
      } else {
          // We're preserving a relative time parameter - special URL sync behavior
          console.log('Preserving relative time parameter in URL - no auto sync');

          // Keep the preservation mode active for a bit longer
          // (This timeout is for the preservation flag, not for isInitializing)
          setTimeout(() => {
            preservingRelativeTime = false;
            console.log('Relative time preservation mode deactivated');
          }, 2000); // Increase timeout to ensure initial query completes first
      }
    }
  }

  // Helper function to set default time range (last 15 minutes)
  function setDefaultTimeRange() {
    // Use relative time format instead of absolute timestamps
    exploreStore.setRelativeTimeRange('15m');
  }

  // --- URL Update Logic ---

  const syncUrlFromState = () => {
    // Don't sync if we are still initializing from the URL
    if (isInitializing.value) {
       return;
    }

    // Skip this URL sync if the flag is set (in this composable or in the store)
    if (skipNextUrlSync.value || exploreStore.skipNextUrlSync) {
      console.log("Skipping URL sync as requested - waiting for pushQueryHistoryEntry");
      skipNextUrlSync.value = false; // Reset the flag
      
      // Also reset the store flag if it exists
      if (exploreStore.skipNextUrlSync !== undefined) {
        exploreStore.skipNextUrlSync = false;
      }
      return;
    }

    // If we're in preservation mode and URL already has relativeTime, don't change it
    if (preservingRelativeTime && route.query.relativeTime) {
      console.log(`Protecting relativeTime=${route.query.relativeTime} from URL sync`);
      return;
    }

    // Start with current query parameters to preserve non-tracking parameters
    const query: Record<string, string> = {};

    // Team
    if (teamsStore.currentTeamId) {
      query.team = teamsStore.currentTeamId.toString();
    }

    // Source (only if valid and belongs to current team)
    if (exploreStore.sourceId > 0 && sourcesStore.teamSources.some(s => s.id === exploreStore.sourceId)) {
      query.source = exploreStore.sourceId.toString();
    }

    // Get the query_id from the route or from the store's selected query ID
    // Ensure we preserve query_id if it exists in the URL
    if (route.query.query_id || exploreStore.selectedQueryId) {
      const queryId = (route.query.query_id as string) || exploreStore.selectedQueryId;
      if (queryId) {
        query.query_id = queryId;
      }
    }

    // Limit
    query.limit = exploreStore.limit.toString();

    // Time handling - prioritize relative time if available
    if (exploreStore.selectedRelativeTime) {
      query.relativeTime = exploreStore.selectedRelativeTime;

      // Remove any absolute time params to avoid confusion
      delete query.start_time;
      delete query.end_time;
    } else {
      // Only use absolute times if no relative time is available
      const startTime = calendarDateTimeToTimestamp(exploreStore.timeRange?.start);
      const endTime = calendarDateTimeToTimestamp(exploreStore.timeRange?.end);
      if (startTime !== null && endTime !== null) {
        query.start_time = startTime.toString();
        query.end_time = endTime.toString();
      }

      // Remove any relative time param to avoid confusion
      delete query.relativeTime;
    }

    // Mode
    query.mode = exploreStore.activeMode;

    // Query Content - prevent double-encoding of URL characters
    const queryContent = exploreStore.activeMode === 'logchefql'
      ? exploreStore.logchefqlCode?.trim()
      : exploreStore.rawSql?.trim();
    if (queryContent) {
      // Encode but avoid double-encoding special characters
      // First decode the content in case it contains encoded characters
      const decodedContent = decodeURIComponent(queryContent);
      // Then safely encode it
      query.q = encodeURIComponent(decodedContent);
    }

    // Compare with current URL and update only if changed
    if (JSON.stringify(query) !== JSON.stringify(route.query)) {
      console.log("URL Sync: Updating URL parameters:", JSON.stringify(query));
      router.replace({ query }).catch(err => {
          // Ignore navigation duplicated errors which can happen with rapid updates
          if (err.name !== 'NavigationDuplicated') {
              console.error("useExploreUrlSync: Error updating URL:", err);
          }
      });
    }
  };

  // New method to push a history entry when a query is executed
  const pushQueryHistoryEntry = () => {
    // Don't push if we are still initializing from the URL
    if (isInitializing.value) {
      return;
    }

    // Set the flag to skip the next automatic URL sync
    skipNextUrlSync.value = true;

    // Get the original query parameters
    const query: Record<string, string> = {};

    // Include the same parameters as syncUrlFromState
    if (teamsStore.currentTeamId) {
      query.team = teamsStore.currentTeamId.toString();
    }

    if (exploreStore.sourceId > 0 && sourcesStore.teamSources.some(s => s.id === exploreStore.sourceId)) {
      query.source = exploreStore.sourceId.toString();
    }

    if (exploreStore.selectedQueryId) {
      query.query_id = exploreStore.selectedQueryId;
    }

    query.limit = exploreStore.limit.toString();

    // Time handling - same logic as syncUrlFromState
    if (exploreStore.selectedRelativeTime) {
      query.relativeTime = exploreStore.selectedRelativeTime;
      // Remove any absolute time params
      delete query.start_time;
      delete query.end_time;
    } else {
      // Only use absolute times if no relative time is available
      const startTime = calendarDateTimeToTimestamp(exploreStore.timeRange?.start);
      const endTime = calendarDateTimeToTimestamp(exploreStore.timeRange?.end);
      if (startTime !== null && endTime !== null) {
        query.start_time = startTime.toString();
        query.end_time = endTime.toString();
      }
      // Remove any relative time param
      delete query.relativeTime;
    }

    query.mode = exploreStore.activeMode;

    const queryContent = exploreStore.activeMode === 'logchefql'
      ? exploreStore.logchefqlCode?.trim()
      : exploreStore.rawSql?.trim();
    if (queryContent) {
      // Ensure consistent encoding of query content to avoid double-encoding
      const decodedContent = decodeURIComponent(queryContent);
      query.q = encodeURIComponent(decodedContent);
    }

    console.log("Push History: Using parameters:", JSON.stringify(query));

    // Use router.push instead of router.replace to create a new history entry
    router.push({ query }).catch(err => {
      // Ignore navigation duplicated errors
      if (err.name !== 'NavigationDuplicated') {
        console.error("useExploreUrlSync: Error pushing query history:", err);
      }
    });
  };

  // --- Watchers ---

  // Watch relevant store state and trigger URL update
  watch(
    [
      // Watch relevant state properties
      () => teamsStore.currentTeamId,
      () => exploreStore.sourceId,
      () => exploreStore.limit,
      () => exploreStore.timeRange,
      () => exploreStore.selectedRelativeTime, // Add watcher for selectedRelativeTime
      () => exploreStore.activeMode,
      () => exploreStore.logchefqlCode,
      () => exploreStore.rawSql,
    ],
    () => {
      // Avoid syncing during the initial setup phase
      if (isInitializing.value) {
        return;
      }
      // Don't sync if we're planning to push a history entry instead
      if (skipNextUrlSync.value) {
        return;
      }
      // Debounce? Consider adding debounce later if updates are too frequent
      syncUrlFromState();
    },
    { deep: true } // Use deep watch for objects like timeRange
  );

  // Watch route changes to re-initialize if necessary (e.g., browser back/forward)
  watch(() => route.fullPath, (newPath, oldPath) => {
    // Check for significant route changes
    const isSignificantChange = (newPath: string, oldPath: string) => {
      // If the paths are different (not just query params), it's significant
      if (newPath.split('?')[0] !== oldPath.split('?')[0]) {
        return true;
      }
      
      // Otherwise, check if query_id parameter is added, removed, or changed
      const newQueryId = new URL(window.location.origin + newPath).searchParams.get('query_id');
      const oldQueryId = new URL(window.location.origin + oldPath).searchParams.get('query_id');
      
      return newQueryId !== oldQueryId;
    };
    
    // Only re-initialize on significant changes and when not already initializing
    if (isSignificantChange(newPath, oldPath) && !isInitializing.value) {
      console.log("useExploreUrlSync: Detected significant change in URL, reinitializing");
      // Re-run initialization logic when route changes significantly
      initializeFromUrl();
    }
  });


  return {
    isInitializing,
    initializationError,
    initializeFromUrl,
    syncUrlFromState,
    pushQueryHistoryEntry,
  };
}
