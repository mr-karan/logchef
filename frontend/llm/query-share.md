Overview
Enable users to share their exact log exploration view with others by encoding the search parameters in the URL. This allows for better collaboration and easier bookmarking of specific log queries.

Requirements
The URL should reflect the current state of the log explorer
Users should be able to share URLs that preserve:
Selected source
Time range
Query string
Results limit
Loading a URL should restore the exact same view
URL parameters should be optional - if not provided, use defaults
Implementation Runbook

1. Setup Vue Router Integration
   CopyInsert
   Task: Add route query parameter handling

- Import `useRoute` and `useRouter` from vue-router
- Initialize route and router in component

2. URL Parameter Management
   CopyInsert
   Task: Define URL parameters structure
   Parameters:

- source: string (UUID of source)
- start_time: number (UNIX timestamp in ms)
- end_time: number (UNIX timestamp in ms)
- limit: number (default: 100)
- query: string (optional)

3. State Initialization from URL
   CopyInsert
   Task: Initialize component state from URL parameters

- Add URL parameter parsing in onMounted hook
- Parse source ID and validate against available sources
- Parse timestamps and create valid date objects
- Parse limit and query parameters
- Apply defaults if parameters are missing

4. URL Updates on State Changes
   CopyInsert
   Task: Update URL when state changes

- Add watcher for relevant state:
  - selectedSource
  - dateRange
  - queryLimit
  - queryInput
- Update URL parameters using router.replace
- Ensure URL updates don't trigger unnecessary API calls

5. Time Range Handling
   CopyInsert
   Task: Implement proper time range handling

- Convert between timestamps and date objects
- Handle invalid timestamps gracefully
- Use default time range (last 3 hours) when timestamps not provided
- Ensure timezone consistency

6. Error Handling
   CopyInsert
   Task: Add robust error handling

- Handle invalid source IDs
- Handle invalid timestamp formats
- Handle invalid limit values
- Show appropriate error messages to users

7. Testing
   CopyInsert
   Task: Test all scenarios

- Test loading with all parameters
- Test loading with partial parameters
- Test loading with invalid parameters
- Test URL updates on state changes
- Test timezone handling
- Test sharing URLs between different users
  Example URLs
  CopyInsert
  Complete URL:
  /explore?source=66859644-ff30-42b9-adb0-4feb5ea26f41&start_time=1738820380154&end_time=1738820380156&limit=100&query=error

Minimal URL:
/explore?source=66859644-ff30-42b9-adb0-4feb5ea26f41
Default Values
limit: 100
start_time: current time - 3 hours
end_time: current time
query: empty string
Implementation Notes
Use UNIX timestamps in milliseconds for consistent time handling
Don't update URL if parameters were provided in initial load
Update URL only when state changes through UI interactions
Preserve query parameters even when empty to maintain URL structure
Use Vue Router's query parameter handling to avoid manual URL manipulation
Success Metrics
Users can successfully share URLs that load the exact same view
Time ranges are preserved accurately across different timezones
URL parameters are human-readable and meaningful
Default values work correctly when parameters are missing
No unnecessary API calls or state updates when loading from URL
Would you like me to elaborate on any specific part of this PRD or implementation plan?
