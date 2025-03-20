import type { ColumnDef, Column } from "@tanstack/vue-table";
import { formatTimestamp } from "@/lib/utils";
import { h } from "vue";
import get from "lodash/get";
import { Button } from "@/components/ui/button";
import { ArrowUpDown } from "lucide-vue-next";
import type { ColumnInfo } from "@/api/explore";

// Function to generate column definitions based on source schema
export function createColumns(
  columns: ColumnInfo[],
  timestampField: string = "timestamp"
): ColumnDef<Record<string, any>>[] {
  // Create a new array with the columns in the desired order
  // First, let's sort out the timestamp field to be first if it exists
  let sortedColumns = [...columns];
  const tsColumnIndex = sortedColumns.findIndex(
    (col) => col.name === timestampField
  );

  // Move the timestamp field to the beginning if it exists
  if (tsColumnIndex > 0) {
    const tsColumn = sortedColumns.splice(tsColumnIndex, 1)[0];
    sortedColumns.unshift(tsColumn);
  }

  return sortedColumns.map((col) => ({
    id: col.name,
    // Set width classes for different column types
    meta: {
      className: col.name === timestampField ? 'timestamp-column' : 
              (col.name === "severity" || col.name === "severity_text") ? 'severity-column' : 
              (col.name === "message" || col.name === "log" || col.name === "msg" || col.name === "content") ? 'message-column' : 'default-column'
    },
    // Enable column resizing with sensible defaults based on column type
    enableResizing: true,
    size: col.name === timestampField ? 180 : 
          (col.name === "severity" || col.name === "severity_text") ? 110 : 
          (col.name === "message" || col.name === "log" || col.name === "msg" || col.name === "content") ? 500 : 200,
    minSize: col.name === timestampField ? 120 : 
             (col.name === "severity" || col.name === "severity_text") ? 80 : 
             (col.name === "message" || col.name === "log" || col.name === "msg" || col.name === "content") ? 200 : 80,
    maxSize: 1000,
    // Use accessor function instead of accessorKey for nested properties
    accessorFn: (row) => {
      // Ensure we get the correct value for this specific column
      return row[col.name];
    },
    header: ({ column }) => {
      return h(
        Button,
        {
          variant: "ghost",
          class: "h-8 px-2 hover:bg-muted/20",
          onClick: () => column.toggleSorting(column.getIsSorted() === "asc"),
        },
        () => [
          col.name,
          h(ArrowUpDown, { class: "ml-2 h-3 w-3 text-muted-foreground" }),
        ]
      );
    },
    cell: ({ row, column }) => {
      // Get the value using the column ID to ensure correct mapping
      const value = row.getValue(column.id);

      // Handle null/undefined values
      if (value === null || value === undefined) {
        return h(
          "div",
          { class: "text-muted-foreground flex-render-content" },
          "-"
        );
      }

      // Special handling for timestamp column
      if (col.name === timestampField) {
        // Format timestamp with better readability
        try {
          const timestamp = new Date(value as string);
          
          // Check if timestamp is valid before formatting
          if (!isNaN(timestamp.getTime())) {
            // Keep raw timestamp for tooltip
            const isoTimestamp = timestamp.toISOString();
            
            // Format relative time for recent timestamps (within last 24 hours)
            const now = new Date();
            const diffMs = now.getTime() - timestamp.getTime();
            const diffHours = diffMs / (1000 * 60 * 60);
            
            let displayTime;
            if (diffHours < 24) {
              // For timestamps less than 24 hours old, show relative time
              if (diffHours < 0.016) { // less than 1 minute
                displayTime = 'just now';
              } else if (diffHours < 1) {
                displayTime = `${Math.round(diffHours * 60)} min ago`;
              } else {
                displayTime = `${Math.round(diffHours)} hour${Math.round(diffHours) !== 1 ? 's' : ''} ago`;
              }
            } else {
              // For older timestamps, show date and time
              const options: Intl.DateTimeFormatOptions = { 
                month: 'short', 
                day: 'numeric',
                hour: '2-digit', 
                minute: '2-digit',
                second: '2-digit'
              };
              
              // Add year if not current year
              if (timestamp.getFullYear() !== now.getFullYear()) {
                options.year = 'numeric';
              }
              
              displayTime = timestamp.toLocaleString(undefined, options);
            }
            
            return h(
              "span",
              { 
                class: "flex-render-content font-mono text-[11px] relative group cursor-default",
                title: isoTimestamp
              },
              displayTime
            );
          }
        } catch (e) {
          console.error("Error formatting timestamp:", e);
        }
        
        // Fallback to basic formatting if there's an error
        const formattedTimestamp = formatTimestamp(value as string);
        return h(
          "span",
          { 
            class: "flex-render-content font-mono text-[11px]"
          },
          formattedTimestamp
        );
      }

      // Handle objects by converting to JSON string with proper formatting
      if (typeof value === "object") {
        try {
          // Try to format the JSON nicely with indentation
          const jsonString = typeof value === 'object' && value !== null
            ? JSON.stringify(value, null, 2)
            : JSON.stringify(value);
          
          return h(
            "span",
            { 
              class: "flex-render-content json-content font-mono text-[11px]"
            },
            jsonString
          );
        } catch (err) {
          // Fallback to simple string if there's an error
          return h(
            "span",
            { 
              class: "flex-render-content"
            },
            String(value)
          );
        }
      }

      // Default to string representation with ellipsis support
      const stringValue = String(value);
      return h(
        "span", 
        { 
          class: "flex-render-content font-mono text-[11px]"
        }, 
        stringValue
      );
    },
    // Enable sorting for all columns
    enableSorting: true,
    // Enable column visibility toggle
    enableHiding: true,
    // Enable filtering for all columns
    enableColumnFilter: true,
    filterFn: (row, id, filterValue) => {
      const value = row.getValue(id);
      if (!value) return false;

      const stringValue = String(value).toLowerCase();
      const { operator, value: searchValue } = filterValue;
      const searchString = String(searchValue).toLowerCase();

      switch (operator) {
        case "=":
          return stringValue === searchString;
        case "!=":
          return stringValue !== searchString;
        case "contains":
          return stringValue.includes(searchString);
        case "startsWith":
          return stringValue.startsWith(searchString);
        case "endsWith":
          return stringValue.endsWith(searchString);
        default:
          return false;
      }
    },
  }));
}

// Helper function to get initial visible columns (timestamp + first column if exists)
export function getInitialVisibleColumns(
  columns: ColumnInfo[],
  timestampField: string = "timestamp"
): string[] {
  const hasTimestamp = columns.some((col) => col.name === timestampField);
  if (hasTimestamp) {
    return columns.length > 1
      ? [
          timestampField,
          columns.find((col) => col.name !== timestampField)!.name,
        ]
      : [timestampField];
  }
  return columns.length > 0 ? [columns[0].name] : [];
}
