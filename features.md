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
