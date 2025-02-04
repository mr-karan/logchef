# LogChef Query/Explore Page Design Plan

## 1. Layout Structure

- **Main Container**: `Card` component
  - Split vertically into Query Input (top) and Results Table (bottom)
  - Use `Separator` for visual division
  - Responsive layout with proper spacing

## 2. Query Input Section

### Components

- **Query Input**: `Textarea`

  - Monospace font for better code readability
  - Multi-line support
  - Keyboard shortcuts (Ctrl+Enter to execute)

- **Source Selection**: `Select`

  - List of available log sources/tables
  - Remember last selected source

- **Recent Queries**: `Combobox`

  - Dropdown with recently executed queries
  - Quick selection and auto-fill

- **Execute Button**: `Button`
  - Primary variant with loading state
  - Icon + "Run Query" text

### Additional Features

- **Notifications**: `Toast`

  - Success/Error messages
  - Query execution status

- **Query Status**: `Badge`

  - Show execution status (Running/Success/Error)
  - Color-coded for different states

- **Keyboard Shortcuts**: `Tooltip`
  - Show available shortcuts on hover
  - Help text for common actions

## 3. Results Table Section

### Main Components

- **Data Table**: `Table`

  - Fixed header while scrolling
  - Virtualized rows for performance
  - Hover states for rows

- **Pagination**: `DataTablePagination`
  - Server-side pagination controls
  - Page size selector
  - Current page indicator

### Columns

1. **Timestamp**

   - Sortable
   - Formatted display
   - UTC with local time tooltip

2. **Log Level**

   - Colored badges (INFO/WARN/ERROR)
   - Filterable

3. **Message**

   - Expandable for long content
   - Code formatting when applicable
   - Click to expand full details

4. **Source**

   - Link to source details
   - Quick filter capability

5. **Metadata**
   - Collapsible additional columns
   - JSON viewer for structured data

## 4. Settings & Controls

### Table Settings

- **Column Manager**: `DropdownMenu`

  - Toggle column visibility
  - Reorder columns
  - Reset to default

- **View Options**: `Button` group
  - Table/JSON view toggle
  - Density settings (Compact/Normal/Relaxed)

### Export Options

- **Export Menu**: `DropdownMenu`
  - Export as CSV
  - Copy as JSON
  - Share query link

## 5. Loading States

- **Initial Load**: `Skeleton`

  - Placeholder table rows
  - Animated loading effect

- **Query Execution**: `Progress`

  - Linear progress indicator
  - Cancel button when applicable

- **Pagination**: `LoadingSpinner`
  - Inline spinner for page changes
  - Maintain table structure while loading

## 6. Responsive Design

### Mobile Adaptations

- Stack layout (Query input above results)
- Collapsible query section
- Horizontal scroll for table
- Compact controls menu

### Tablet/Desktop

- Side-by-side layout option
- Full keyboard navigation
- Hover previews
- Advanced filtering panel

## Next Steps

1. Create basic layout structure
2. Implement query input with basic execution
3. Build table with core features
4. Add pagination and loading states
5. Implement responsive design
6. Add advanced features (export, column management)
7. Polish UI/UX and performance optimization

---

Add a time range selector?
Add query type/mode selector?
Add a schema/column helper dropdown?
Implement the results table?
