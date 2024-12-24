### High Priority

- [ ] Vector Log Ingestion
  - [ ] Implement vector ingest logs
  - [ ] Build proper log viewer UI with:
    - Typed data structures
    - LogTable component (timestamps, severity badges, message display)
    - Loading/error states
    - Basic fetch functionality
  - [ ] Add logs panel
- [ ] Timezone aware logs?
  - [ ] Give the user an option to specify timezone for the logs
  - [ ] Display the logs in the specified timezone
  - [ ] Load browser default timezone in the UI
  - [ ] In the SELECT \* query, format the timestamp to be properly displayed in the UI, so no timezone conversion is needed in UI
- [ ] Core Infrastructure
  - [ ] Upgrade echo to v5
  - [ ] Update critical dependencies
  - [ ] Implement golang migrate

### UI/UX Improvements

- [ ] Source Management

  - [ ] Create UI for source management
  - [ ] Evaluate datatable components (with virtual scroll)
  - [ ] Implement date range component with time selector

- [ ] Log Viewer Enhancements
  - [ ] Add infinite scrolling
  - [ ] Advanced filtering
  - [ ] Full text search
  - [ ] Time range selection
  - [ ] Live tail functionality

### Technical Debt

- [ ] Build Optimization
  - [ ] Optimize dist folder in go binary path
  - [ ] Clean up .gitignore
  - [x] Remove unused shadcn components

### Completed

- [x] Make log URI shareable with query params

---

- [ ] Focus on schemaless approach

We should have another sidebar for the log viewer in left side. That should have a list of fields that we can display in the datatable. For this to effectively work, we need to make an API call to get the list of fields. In backend, we should have a handler which returns the Clickhouse schema for the given source. Now, the interesting stuff here is that, the schema can be dynamic and can change based on the query params. Or even logs, different logs can have different fields. So, we need to have a "guesstimate" of the fields that we can display in the datatable, maybe based on pattern of last N logs in the given time range. In clickhouse, we can have fileds which are internally map (like JSON fields), so we need to have a way to display those fields as well using dot separator.

"save" the current view. This should be a named view and should be saved in the database. We should also have a "load" button to load a saved view.

"export" the current view to a file. This file should be in CSV format.

"share" the current view. This should create a shareable link which can be sent to someone else. When clicked, it should open in a new tab.

"download" the current view. This should create a downloadable file in CSV format.

- Charts/Histogram
- Alerting Feature
- Saved Log Queries
- Users and Permissions
- API Access
- CLI
- Query Language
- AI in Query Builder
- Create SQL query

---

## Datatable

- [ ] Add a "export" button to the datatable
- [ ] https://primevue.org/datatable/#row_expansion
- [ ] https://primevue.org/datatable/#frozen_rows Header should be fixed/sticky
- [ ] https://primevue.org/datatable/#virtualscroll for large datasets
- [ ] https://primevue.org/datatable/#scroll Adding scrollable property along with a scrollHeight for the data viewport enables vertical scrolling with fixed headers.
- [ ] https://primevue.org/datatable/#resize_expandmode
- [ ] https://primevue.org/datatable/#column_toggle
- [ ] https://primevue.org/datatable/#export
- [ ] https://primevue.org/datatable/#stateful

Here's a detailed improvement plan for your log analytics application's DataTable:

# DataTable Enhancements for Log Analytics

## 1. Virtual Scrolling for Large Datasets

**Why:** Essential for handling large log datasets (>1000 rows) without performance degradation
**Docs:** [PrimeVue Virtual Scroll](https://primevue.org/datatable/#virtualscroll)

```vue
<DataTable
  :value="logs"
  scrollable
  scrollHeight="400px"
  :virtualScrollerOptions="{
    itemSize: 46,
    delay: 150,
    showLoader: true,
    loading: lazyLoading,
    lazy: true,
    onLazyLoad: loadLogsLazy
  }"
>
```

## 2. Fixed Header with Vertical Scroll

**Why:** Maintains context while scrolling through logs
**Docs:** [PrimeVue Scroll](https://primevue.org/datatable/#scroll)

```vue
<DataTable
  :value="logs"
  scrollable
  scrollHeight="70vh"
  showGridlines
>
```

## 3. Column Management

**Why:** Allows users to customize visible log fields
**Docs:** [PrimeVue Column Toggle](https://primevue.org/datatable/#column_toggle)

```vue
<template #header>
  <MultiSelect
    v-model="selectedColumns"
    :options="availableColumns"
    @change="onColumnToggle"
    placeholder="Select Log Fields"
  />
</template>
```

## 4. Export Functionality

**Why:** Enables offline analysis and sharing of logs
**Docs:** [PrimeVue Export](https://primevue.org/datatable/#export)

```vue
<template #header>
  <Button
    icon="pi pi-external-link"
    label="Export Logs"
    @click="exportLogs($event)"
  />
</template>

// In component methods: { exportLogs() { this.$refs.dataTable.exportCSV({
selectionOnly: false, customFilename: `logs-${new Date().toISOString()}` }); } }
```

## 5. Stateful Table

**Why:** Persists user preferences and filters between sessions
**Docs:** [PrimeVue Stateful](https://primevue.org/datatable/#stateful)

```vue
<DataTable
  stateStorage="local"
  stateKey="logs-table-state"
  :value="logs"
>
```

## 6. Row Expansion for Details

**Why:** Shows detailed log information without cluttering main view
**Docs:** [PrimeVue Row Expansion](https://primevue.org/datatable/#row_expansion)

```vue
<DataTable :value="logs" v-model:expandedRows="expandedLogs" dataKey="id">
  <Column :expander="true" />
  <template #expansion="slotProps">
    <LogDetailView :log="slotProps.data" />
  </template>
</DataTable>
```

## 7. Resizable Columns

**Why:** Allows users to adjust column widths for better readability
**Docs:** [PrimeVue Resize](https://primevue.org/datatable/#resize_expandmode)

```vue
<DataTable
  :value="logs"
  resizableColumns
  columnResizeMode="expand"
>
```

## Implementation Priority:

1. Virtual Scrolling (Critical for performance)
2. Fixed Header (Basic UX requirement)
3. Export Functionality (Common user request)
4. Row Expansion (Improves information density)
5. Column Management (User customization)
6. Stateful Table (Quality of life)
7. Resizable Columns (Nice to have)

## Additional Considerations:

- Consider adding loading states for async operations
- Implement error boundaries for failed data loads
- Add keyboard navigation support
- Consider adding quick filters for common log queries
- Implement date range presets for log filtering

This implementation assumes you're using Vue 3 with PrimeVue's latest version and have proper TypeScript/JavaScript data structures for your logs.

---

## Source Management

- Database is not created automatically
- SQLite sync is problematic
  - Keep source of truth as same maybe
  - Downside - everything will be in same DB unless used a config file
- SQLite and Clickhouse can go out of sync?
  - When someone loses clickhouse volume, but has sqlite DB - Not a major issue
- Source creation is succesful even if Clickhouse table is not created
  - If DB doesn't exist this happens
  - Check for other cases and make it robust
-
