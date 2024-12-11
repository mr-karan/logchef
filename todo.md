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
   - [ ] In the SELECT * query, format the timestamp to be properly displayed in the UI, so no timezone conversion is needed in UI
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

