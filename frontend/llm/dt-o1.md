Objective:

Integrate a fully-featured data table into the Results.vue and Explore.vue components using shadcn-vue components and @tanstack/vue-table. The new data table should support features
like sorting, filtering, pagination, column visibility toggling, and row selection.

─────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
Step 1: Review Existing Implementation

• Files to Review:
• frontend/src/views/explore/Results.vue
• frontend/src/views/explore/Explore.vue
• Goals:
• Understand how the current table is implemented.
• Identify existing data structures and state management.
• Note any custom logic or features that need to be preserved.

─────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
Step 2: Install Required Dependencies

• Install @tanstack/vue-table:

    npm install @tanstack/vue-table

• Ensure shadcn-vue Components are Available:
• Verify that the relevant UI components (e.g., Table, Button, Input) from shadcn-vue are installed and imported correctly.

─────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
Step 3: Define Table Columns

• Create columns.ts in frontend/src/views/explore/:
• Define the table columns with appropriate configurations.
• For each column:
• Set accessorKey to the data field name.
• Provide a header component that may include sorting controls.
• Define cell rendering logic, including formatting (e.g., date formatting for timestamps).
• Example Column Definition:

    import type { ColumnDef } from '@tanstack/vue-table';
    import { h } from 'vue';

    export const columns: ColumnDef<any>[] = [
      {
        accessorKey: 'timestamp',
        header: () => 'Timestamp',
        cell: ({ row }) => {
          const value = row.getValue('timestamp');
          // Format timestamp as needed
          return formatTimestamp(value);
        },
      },
      // Define other columns similarly
    ];

─────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
Step 4: Create a Reusable DataTable Component

• Create DataTable.vue in frontend/src/components/ui/:
• Use @tanstack/vue-table to manage table state and features.
• Utilize shadcn-vue components for the table UI.
• Implement features such as sorting, filtering, pagination, column visibility, and row selection.
• Accept columns and data as props.
• Key Implementation Points:
• Initialize the table using useVueTable.
• Manage table state (sorting, filtering, pagination, etc.) using ref and computed properties.
• Render the table headers and cells using FlexRender.
• Add UI controls for pagination and other features.

─────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
Step 5: Refactor Results.vue to Use DataTable Component

• Replace Manual Table with DataTable Component:
• Import the DataTable.vue component and columns.ts.
• Pass the columns and logs (data) as props to DataTable.
• Remove the old table rendering code and related state.
• Example Usage:

    <template>
      <div class="h-full flex flex-col min-w-0">
        <!-- Stats Bar -->
        <!-- ... -->

        <!-- DataTable Component -->
        <DataTable :columns="columns" :data="logs" />
      </div>
    </template>

    <script setup lang="ts">
    import DataTable from '@/components/ui/DataTable.vue';
    import { columns } from './columns';
    // ...
    </script>

─────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
Step 6: Update Explore.vue for New Data Handling

• Adjust Data Fetching Logic:
• Ensure that the data fetched aligns with the requirements of the DataTable component.
• Modify API calls if necessary to support server-side sorting, filtering, and pagination.
• Manage Table State:
• Pass any table state (e.g., current page, filters) from Explore.vue to DataTable if needed.
• Handle events emitted by DataTable for actions like sorting and pagination.

─────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
Step 7: Implement Advanced Table Features in DataTable Component

• Sorting:
• Enable sorting on necessary columns.
• Update column headers to include sorting controls (e.g., arrows indicating sort direction).
• Filtering:
• Add input fields or controls for filtering data.
• Implement column-specific filtering logic.
• Pagination:
• Add pagination controls (e.g., next/previous buttons, page numbers).
• Manage pagination state and update data accordingly.
• Column Visibility:
• Include a dropdown menu to toggle the visibility of columns.
• Update the table rendering to show/hide columns based on user selection.
• Row Selection:
• Add checkboxes for row selection.
• Implement select-all functionality.
• Expose selected rows for any further actions.

─────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
Step 8: Ensure Responsive Design and Usability

• UI Adjustments:
• Make sure the table layout is responsive and looks good on different screen sizes.
• Use ScrollArea for handling overflows if necessary.
• Accessibility:
• Add appropriate ARIA labels and roles.
• Ensure keyboard navigation works as expected.

─────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
Step 9: Update and Optimize Data Fetching Logic

• Server-Side Handling:
• If the dataset is large, implement server-side pagination, sorting, and filtering.
• Update API endpoints to accept parameters for these operations.
• Client-Side Handling:
• If data volume is manageable, implement client-side operations.
• Optimize performance by using computed properties and efficient state management.

─────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
Step 10: Test the New Implementation

• Functional Testing:
• Verify that all features work as intended.
• Sorting sorts the data correctly.
• Filtering narrows down the results appropriately.
• Pagination navigates through pages smoothly.
• Column visibility toggles correctly.
• Row selection reflects the correct state.
• Edge Cases:
• Test with empty data sets.
• Test with maximum data loads.
• Performance Testing:
• Ensure that the table performs well with large datasets.
• Optimize rendering if necessary.

─────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
Step 11: Code Cleanup and Documentation

• Remove Deprecated Code:
• Delete any unused components or code fragments related to the old table implementation.
• Comment and Document:
• Add comments where necessary to explain complex logic.
• Update any documentation to reflect the new table implementation.

─────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
Step 12: Final Review and Deployment

• Code Review:
• Review the code changes to ensure best practices are followed.
• Check for any potential bugs or issues.
• Deployment:
• Merge changes into the main branch.
• Deploy to the staging environment for further testing.
• Once approved, deploy to production.

─────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
Additional Notes:

• Refer to shadcn-vue Documentation:
• Use the provided documentation and examples as a guide for implementing specific features.
• Pay attention to the nuances of integrating @tanstack/vue-table with shadcn-vue components.
• Reusable Components:
• Consider generalizing the DataTable component for reuse in other parts of the application.
• Extract common UI elements into separate components if applicable.

─────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
End of Plan
