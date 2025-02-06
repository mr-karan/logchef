# Data Table Implementation Plan for Results.vue

## Overview

This document outlines the plan to enhance the existing table in `Results.vue` into a full-featured data table using Shadcn Vue components and TanStack Table.

## Features to Implement

### 1. Basic Table Setup

- [ ] Install required dependencies:
  ```bash
  npm install @tanstack/vue-table
  ```
- [ ] Create base table components using Shadcn Vue
  - DataTable.vue
  - DataTableHeader.vue
  - DataTableBody.vue
  - DataTablePagination.vue

### 2. Column Definition

- [ ] Define column configuration:
  - Timestamp (sortable)
  - Log Level (filterable, sortable)
  - Message (searchable)
  - Source (filterable)
  - Additional metadata columns (configurable)

### 3. Core Features

#### Sorting

- [ ] Implement client-side sorting
- [ ] Add sort indicators in column headers
- [ ] Support multi-column sorting
- [ ] Persist sort state in URL

#### Filtering

- [ ] Add column-specific filters
  - Dropdown for Log Level
  - Text search for Message
  - Source selection
- [ ] Implement debounced search
- [ ] Add clear filter options
- [ ] Persist filter state in URL

#### Pagination

- [ ] Add pagination controls
- [ ] Configure items per page options
- [ ] Show total records count
- [ ] Persist page state in URL

#### Row Selection

- [ ] Add checkbox column
- [ ] Implement select all functionality
- [ ] Add bulk actions for selected rows
- [ ] Show selection count

### 4. Advanced Features

- [ ] Column resizing
- [ ] Column reordering
- [ ] Column visibility toggle
- [ ] Row expansion for detailed view
- [ ] Export selected rows
- [ ] Keyboard navigation

### 5. Performance Optimizations

- [ ] Implement virtual scrolling for large datasets
- [ ] Optimize re-renders
- [ ] Add loading states
- [ ] Cache table state

## Implementation Steps

### Phase 1: Basic Setup

1. Create base table structure
2. Define column configurations
3. Implement basic data rendering

### Phase 2: Core Features

1. Add sorting functionality
2. Implement filtering system
3. Add pagination
4. Implement row selection

### Phase 3: Advanced Features

1. Add column management
2. Implement row expansion
3. Add export functionality
4. Implement keyboard navigation

### Phase 4: Polish & Optimization

1. Add loading states
2. Implement virtual scrolling
3. Optimize performance
4. Add final styling touches

## Component Structure

```vue
<!-- Results.vue -->
<script setup lang="ts">
import {
  DataTable,
  DataTableHeader,
  DataTableBody,
  DataTablePagination,
} from "@/components/ui/data-table";

// Column definitions
const columns = [
  {
    accessorKey: "timestamp",
    header: "Timestamp",
    sortable: true,
  },
  {
    accessorKey: "level",
    header: "Log Level",
    sortable: true,
    filterable: true,
  },
  {
    accessorKey: "message",
    header: "Message",
    searchable: true,
  },
  {
    accessorKey: "source",
    header: "Source",
    filterable: true,
  },
];

// Table state
const tableState = reactive({
  sorting: [],
  filtering: {},
  pagination: {
    pageIndex: 0,
    pageSize: 10,
  },
  selectedRows: new Set(),
});
</script>

<template>
  <DataTable
    :columns="columns"
    :data="logs"
    v-model:sorting="tableState.sorting"
    v-model:filtering="tableState.filtering"
    v-model:pagination="tableState.pagination"
    v-model:selected="tableState.selectedRows"
  >
    <DataTableHeader />
    <DataTableBody />
    <DataTablePagination />
  </DataTable>
</template>
```

## URL State Management

The following URL parameters will be managed:

- `sort`: Sorting configuration
- `filters`: Active filters
- `page`: Current page number
- `size`: Items per page
- `selected`: Selected row IDs

## Testing Checklist

- [ ] Test sorting functionality
- [ ] Verify filter operations
- [ ] Check pagination behavior
- [ ] Validate row selection
- [ ] Test URL state persistence
- [ ] Verify performance with large datasets
- [ ] Test keyboard navigation
- [ ] Validate export functionality
