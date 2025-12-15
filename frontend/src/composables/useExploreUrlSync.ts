import { ref, watch, nextTick } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { useExploreStore } from '@/stores/explore';
import { useTeamsStore } from '@/stores/teams';
import { useSourcesStore } from '@/stores/sources';
// Removed complex coordination imports - using clean router-first approach now

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

  // Add a debounce timer to avoid syncing during typing
  let syncDebounceTimer: number | null = null;

  // --- Initialization Logic ---

  async function initializeFromUrl() {
    // Prevent multiple initializations within 500ms of each other
    const now = Date.now();
    if (now - lastInitTimestamp < 500) {
      console.log("useExploreUrlSync: Skipping initialization - too soon after previous init");
      return;
    }
    lastInitTimestamp = now;

    isInitializing.value = true;
    initializationError.value = null;

    // Check immediately if we have a relative time parameter in the URL
    const hasRelativeTimeParam = !!route.query.relativeTime;
    if (hasRelativeTimeParam) {
      // Set a flag to make all URL sync operations more cautious
      preservingRelativeTime = true;
    }

    try {
      // 1. Ensure Teams are loaded (wait if necessary)
      if (!teamsStore.teams || teamsStore.teams.length === 0) {
        await teamsStore.loadTeams(false, false); // Explicitly use user teams endpoint
      }

      // Handle the case where no teams are available more gracefully
      if (teamsStore.teams.length === 0) {
        // Set initialization error without throwing
        initializationError.value = "No teams available or accessible.";

        // Clear any existing data for consistency
        exploreStore.setSource(0);
        sourcesStore.clearCurrentSourceDetails();

        // Mark initialization as complete even with error
        isInitializing.value = false;
        
        // Exit early but don't throw - allow the component to handle this state
        return;
      }

      // For explore routes without team/source params, ensure proper initialization
      if (route.path.startsWith('/logs/') && Object.keys(route.query).length === 0) {
        console.log('useExploreUrlSync: Initializing explore route with no URL params - teams are available');
        // Teams are loaded, router guard should have set a default team
        // Just ensure we complete initialization quickly
      }

      // Team/source initialization is now handled by router guard
      // Just initialize query params from URL
      exploreStore.initializeFromUrl(route.query as Record<string, string | undefined>);

    } catch (error: any) {
      console.error("useExploreUrlSync: Error during initialization:", error);
      initializationError.value = error.message || "Failed to initialize from URL.";
    } finally {
      // Use nextTick to ensure all initial store updates have propagated
      // before allowing watchers to update the URL.
      await nextTick();

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
          setTimeout(() => {
            preservingRelativeTime = false;
            console.log('Relative time preservation mode deactivated');
          }, 2000); // Ensure initial query completes first
      }
    }
  }

  // --- URL Update Logic ---

  const syncUrlFromState = () => {
    // Don't sync if we are still initializing from the URL
    if (isInitializing.value) {
       return;
    }

    // Note: Team/source coordination is now handled by router guard, not here

    // Skip this URL sync if the flag is set
    if (skipNextUrlSync.value) {
      console.log("Skipping URL sync as requested - waiting for pushQueryHistoryEntry");
      skipNextUrlSync.value = false; // Reset the flag
      return;
    }

    // If we're in preservation mode and URL already has relativeTime, don't change it
    if (preservingRelativeTime && route.query.relativeTime) {
      console.log(`Protecting relativeTime=${route.query.relativeTime} from URL sync`);
      return;
    }

    // Validate that current source belongs to current team before syncing
    if (exploreStore.sourceId && teamsStore.currentTeamId) {
      const currentTeamSources = sourcesStore.teamSources || [];
      const sourceExists = currentTeamSources.some(s => s.id === exploreStore.sourceId);
      if (!sourceExists) {
        console.log(`Skipping URL sync - source ${exploreStore.sourceId} doesn't belong to team ${teamsStore.currentTeamId}`);
        return;
      }
    }

    // Use the store's urlQueryParameters computed property
    const query = exploreStore.urlQueryParameters;

    // DO NOT try to handle encoding here - let Vue Router handle it
    // The URL framework will automatically encode values as needed

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

  // Push a history entry when a query is executed
  const pushQueryHistoryEntry = () => {
    // Don't push if we are still initializing from the URL
    if (isInitializing.value) {
      return;
    }

    // Set the flag to skip the next automatic URL sync
    skipNextUrlSync.value = true;

    // Use the store's urlQueryParameters computed property
    const query = exploreStore.urlQueryParameters;

    // DO NOT try to handle encoding manually - let Vue Router handle it
    // The URL framework will automatically encode values as needed

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

  // Modify the watch function to prevent immediate syncing of query content
  watch(
    [
      // Watch relevant state properties through the store
      () => teamsStore.currentTeamId,
      () => exploreStore.sourceId,
      () => exploreStore.limit,
      () => exploreStore.timeRange,
      () => exploreStore.selectedRelativeTime,
      () => exploreStore.activeMode,
      // Don't trigger URL updates during typing for these values
      // We'll handle them separately with manual sync
      // () => exploreStore.logchefqlCode,
      // () => exploreStore.rawSql,
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
      syncUrlFromState();
    },
    { deep: true } // Use deep watch for objects like timeRange
  );

  // Watch route changes to re-initialize if necessary (e.g., browser back/forward)
  // Note: This might be too aggressive if other query params change often.
  // Consider making this more specific if needed.
  watch(() => route?.fullPath, (newPath, oldPath) => {
      // Skip if route is undefined
      if (!route) return;

      // Only re-initialize if the path itself or the core query params changed significantly
      // Avoid re-init on minor changes if updateUrlFromState handles them.
      // A simple check for now: re-init if path changes.
      if (newPath !== oldPath && !isInitializing.value) {
          // Re-run initialization logic when route changes
          // initializeFromUrl(); // Potentially re-enable if back/forward needs full re-init
      }
  });

  // Add a new function to manually sync URL after typing completes
  function debouncedSyncUrlFromState() {
    // Cancel any pending timers
    if (syncDebounceTimer !== null) {
      clearTimeout(syncDebounceTimer);
    }

    // Set a new timer to sync after a delay
    syncDebounceTimer = window.setTimeout(() => {
      if (!isInitializing.value && !skipNextUrlSync.value) {
        syncUrlFromState();
      }
      syncDebounceTimer = null;
    }, 750); // Delay of 750ms after typing stops
  }

  return {
    isInitializing,
    initializationError,
    initializeFromUrl,
    syncUrlFromState,
    debouncedSyncUrlFromState,
    pushQueryHistoryEntry,
  };
}
