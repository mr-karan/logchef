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

export function useExploreUrlSync() {
  const route = useRoute();
  const router = useRouter();
  const exploreStore = useExploreStore();
  const teamsStore = useTeamsStore();
  const sourcesStore = useSourcesStore();

  const isInitializing = ref(true);
  const initializationError = ref<string | null>(null);

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

  // --- Initialization Logic ---

  async function initializeFromUrl() {
    isInitializing.value = true;
    initializationError.value = null;
    console.log("useExploreUrlSync: Initializing state from URL", route.query);

    try {
      // 1. Ensure Teams are loaded (wait if necessary)
      if (!teamsStore.teams || teamsStore.teams.length === 0) {
        console.log("useExploreUrlSync: Waiting for teams to load...");
        await teamsStore.loadTeams();
        console.log("useExploreUrlSync: Teams loaded.");
      }
      if (teamsStore.teams.length === 0) {
        throw new Error("No teams available or accessible.");
      }

      // Check if we have a query_id in the URL, which indicates we're editing a saved query
      const queryId = route.query.query_id as string | undefined;
      if (queryId) {
        console.log(`useExploreUrlSync: Found query_id=${queryId} in URL, in edit mode`);
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
          console.warn(initializationError.value);
        }
      }
      if (!teamId) {
        teamId = teamsStore.teams[0].id; // Default to first team
      }
      // Set team *before* loading sources
      if (teamsStore.currentTeamId !== teamId) {
         teamsStore.setCurrentTeam(teamId);
      }
      console.log(`useExploreUrlSync: Current team set to ${teamId}`);


      // 3. Load Sources for the selected team (wait if necessary)
      console.log(`useExploreUrlSync: Loading sources for team ${teamId}...`);
      await sourcesStore.loadTeamSources(teamId);
      console.log("useExploreUrlSync: Sources loaded.");


      // 4. Set Source from URL or default (validate against loaded sources)
      let sourceId: number | null = null;
      const urlSourceIdStr = route.query.source as string | undefined;
      if (urlSourceIdStr) {
        const parsedSourceId = parseInt(urlSourceIdStr);
        if (!isNaN(parsedSourceId) && sourcesStore.teamSources.some(s => s.id === parsedSourceId)) {
          sourceId = parsedSourceId;
        } else {
          initializationError.value = `Invalid or inaccessible source ID: ${urlSourceIdStr} for team ${teamId}. Falling back to default.`;
          console.warn(initializationError.value);
        }
      }
      if (!sourceId && sourcesStore.teamSources.length > 0) {
        sourceId = sourcesStore.teamSources[0].id; // Default to first source
      }

      // Set source and load details *only if* sourceId is valid
      if (sourceId) {
         if (exploreStore.sourceId !== sourceId) {
            exploreStore.setSource(sourceId);
         }
         console.log(`useExploreUrlSync: Current source set to ${sourceId}. Loading details...`);
         await sourcesStore.loadSourceDetails(sourceId); // Wait for details
         console.log("useExploreUrlSync: Source details loaded.");
      } else {
         exploreStore.setSource(0); // Explicitly set to 0 if no valid source
         sourcesStore.clearCurrentSourceDetails();
         console.log("useExploreUrlSync: No valid source found for team.");
         initializationError.value = `No sources available for team ${teamId}.`;
      }


      // 5. Set Limit from URL or default
      const urlLimitStr = route.query.limit as string | undefined;
      const limit = urlLimitStr ? parseInt(urlLimitStr) : 100;
      exploreStore.setLimit(!isNaN(limit) && limit > 0 && limit <= 10000 ? limit : 100);
      console.log(`useExploreUrlSync: Limit set to ${exploreStore.limit}`);


      // 6. Set Time Range from URL or default
      const urlStartTime = parseTimestamp(route.query.start_time as string | undefined);
      const urlEndTime = parseTimestamp(route.query.end_time as string | undefined);
      let startDateTime: DateValue;
      let endDateTime: DateValue;

      const parsedStart = timestampToCalendarDateTime(urlStartTime);
      const parsedEnd = timestampToCalendarDateTime(urlEndTime);

      if (!parsedStart || !parsedEnd) {
        console.log("useExploreUrlSync: Using default time range.");
        const nowDt = now(getLocalTimeZone());
        startDateTime = new CalendarDateTime(
          nowDt.year, nowDt.month, nowDt.day, nowDt.hour, nowDt.minute, nowDt.second
        ).subtract({ hours: 1 });
        endDateTime = new CalendarDateTime(
          nowDt.year, nowDt.month, nowDt.day, nowDt.hour, nowDt.minute, nowDt.second
        );
      } else {
          startDateTime = parsedStart;
          endDateTime = parsedEnd;
      }
      exploreStore.setTimeRange({ start: startDateTime, end: endDateTime });
      console.log(`useExploreUrlSync: Time range set.`);


      // 7. Set Mode from URL or default
      const urlMode = route.query.mode as string | undefined;
      const mode = (urlMode === 'sql' ? 'sql' : 'logchefql') as 'logchefql' | 'sql';
      exploreStore.setActiveMode(mode);
      console.log(`useExploreUrlSync: Mode set to ${mode}`);


      // 8. Set Query Content from URL
      const urlQuery = route.query.q as string | undefined;
      const queryContent = urlQuery ? decodeURIComponent(urlQuery) : "";
      if (mode === 'logchefql') {
        exploreStore.setLogchefqlCode(queryContent);
        exploreStore.setRawSql(""); // Clear other mode
      } else {
        exploreStore.setRawSql(queryContent);
        exploreStore.setLogchefqlCode(""); // Clear other mode
      }
      console.log(`useExploreUrlSync: Query content set.`);

      // 9. Saved Query Check (LogExplorer still handles loading the data)
      // This composable only sets the initial state from basic params.
      // If query_id is present, LogExplorer's logic will take over after this init.

    } catch (error: any) {
      console.error("useExploreUrlSync: Error during initialization:", error);
      initializationError.value = error.message || "Failed to initialize from URL.";
      // Attempt to set defaults even on error? Maybe not, let the component show the error.
    } finally {
      // Use nextTick to ensure all initial store updates have propagated
      // before allowing watchers to update the URL.
      await nextTick();
      isInitializing.value = false;
      console.log("useExploreUrlSync: Initialization finished.");
      // Trigger initial URL sync *after* initialization is marked complete
      syncUrlFromState();
    }
  }

  // --- URL Update Logic ---

  const syncUrlFromState = () => {
    // Don't sync if we are still initializing from the URL
    if (isInitializing.value) {
       console.log("useExploreUrlSync: Skipping syncUrlFromState during initialization");
       return;
    }

    console.log("useExploreUrlSync: Syncing URL from state...");
    const query: Record<string, string> = {};

    // Team
    if (teamsStore.currentTeamId) {
      query.team = teamsStore.currentTeamId.toString();
    }

    // Source (only if valid and belongs to current team)
    if (exploreStore.sourceId > 0 && sourcesStore.teamSources.some(s => s.id === exploreStore.sourceId)) {
      query.source = exploreStore.sourceId.toString();
    }

    // Only preserve query_id if store has a selectedQueryId - this respects manual deletion
    if (exploreStore.selectedQueryId) {
      query.query_id = exploreStore.selectedQueryId;
    }

    // Limit
    query.limit = exploreStore.limit.toString();

    // Time Range - Always include if available
    const startTime = calendarDateTimeToTimestamp(exploreStore.timeRange?.start);
    const endTime = calendarDateTimeToTimestamp(exploreStore.timeRange?.end);
    if (startTime !== null && endTime !== null) {
      query.start_time = startTime.toString();
      query.end_time = endTime.toString();
    } else {
      console.warn("useExploreUrlSync: Missing valid time range for URL sync");
    }

    // Mode
    query.mode = exploreStore.activeMode;

    // Query Content
    const queryContent = exploreStore.activeMode === 'logchefql'
      ? exploreStore.logchefqlCode?.trim()
      : exploreStore.rawSql?.trim();
    if (queryContent) {
      query.q = encodeURIComponent(queryContent);
    }

    // Only update if the query params actually changed
    if (JSON.stringify(query) !== JSON.stringify(route.query)) {
        console.log("useExploreUrlSync: Updating URL with new state:", query);
        router.replace({ query }).catch(err => {
            // Ignore navigation duplicated errors which can happen with rapid updates
            if (err.name !== 'NavigationDuplicated') {
                console.error("useExploreUrlSync: Error updating URL:", err);
            }
        });
    } else {
        // console.log("useExploreUrlSync: URL state matches current state, no update needed.");
    }
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
      () => exploreStore.activeMode,
      () => exploreStore.logchefqlCode,
      () => exploreStore.rawSql,
    ],
    () => {
      // Avoid syncing during the initial setup phase
      if (isInitializing.value) {
        // console.log("useExploreUrlSync: Skipping URL sync during initialization.");
        return;
      }
      // Debounce? Consider adding debounce later if updates are too frequent
      syncUrlFromState();
    },
    { deep: true } // Use deep watch for objects like timeRange
  );

  // Watch route changes to re-initialize if necessary (e.g., browser back/forward)
  // Note: This might be too aggressive if other query params change often.
  // Consider making this more specific if needed.
  watch(() => route.fullPath, (newPath, oldPath) => {
      // Only re-initialize if the path itself or the core query params changed significantly
      // Avoid re-init on minor changes if updateUrlFromState handles them.
      // A simple check for now: re-init if path changes.
      if (newPath !== oldPath && !isInitializing.value) {
          console.log("useExploreUrlSync: Route changed, re-initializing...");
          // Re-run initialization logic when route changes
          // initializeFromUrl(); // Potentially re-enable if back/forward needs full re-init
      }
  });


  return {
    isInitializing,
    initializationError,
    initializeFromUrl,
    syncUrlFromState,
  };
}
