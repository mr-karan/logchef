import type { ColumnDef, Column } from "@tanstack/vue-table";
import { formatTimestamp } from "@/lib/utils";
import { h } from "vue";
import { Button } from "@/components/ui/button";
import { ArrowUpDown } from "lucide-vue-next";
import type { ColumnInfo } from "@/api/explore";

/**
 * Column type definitions with consistent size handling
 */
// Common column types for width settings
type ColumnType = 'timestamp' | 'severity' | 'message' | 'default';

// Column width configuration
interface ColumnWidthConfig {
  defaultWidth: number;
  minWidth: number;
  maxWidth: number;
}

// Width configurations for each column type
const COLUMN_WIDTH_CONFIG: Record<ColumnType, ColumnWidthConfig> = {
  timestamp: { defaultWidth: 220, minWidth: 180, maxWidth: 300 },
  severity: { defaultWidth: 120, minWidth: 80, maxWidth: 150 },
  message: { defaultWidth: 500, minWidth: 200, maxWidth: 1200 },
  default: { defaultWidth: 180, minWidth: 100, maxWidth: 400 }
};

// Determine column type based on field name
function getColumnType(columnName: string, timestampField: string, severityField: string): ColumnType {
  if (columnName === timestampField) {
    return 'timestamp';
  }
  if (columnName === severityField) {
    return 'severity';
  }
  if (['message', 'log', 'msg', 'body', 'content'].includes(columnName)) {
    return 'message';
  }
  return 'default';
}

// Function to generate column definitions based on source schema
export function createColumns(
  columns: ColumnInfo[],
  timestampField: string = "timestamp",
  timezone: 'local' | 'utc' = 'local',
  severityField: string = "severity_text"
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

  // Move severity field to be second if it exists
  const severityColumnIndex = sortedColumns.findIndex(
    (col) => col.name === severityField
  );
  if (severityColumnIndex > 0) {
    const severityColumn = sortedColumns.splice(severityColumnIndex, 1)[0];
    sortedColumns.splice(1, 0, severityColumn);
  }

  return sortedColumns.map((col) => {
    const columnType = getColumnType(col.name, timestampField, severityField);
    const widthConfig = COLUMN_WIDTH_CONFIG[columnType];
    
    return {
      id: col.name,
      // Store column type in meta for easy access
      meta: { columnType },
      // Enable column resizing with consistent sizes from config
      enableResizing: true,
      size: widthConfig.defaultWidth,
      minSize: widthConfig.minWidth,
      maxSize: widthConfig.maxWidth,
      // Use accessor function instead of accessorKey for nested properties
      accessorFn: (row) => row[col.name],
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
        let formattedTimestamp;
        try {
          // Parse the timestamp
          const date = new Date(value as string);

          // Check if the date is valid
          if (isNaN(date.getTime())) {
            // If invalid, just use the original formatting
            formattedTimestamp = formatTimestamp(value as string);
          } else {
            // Format based on timezone preference
            if (timezone === 'utc') {
              // Use UTC formatting - keep the 'Z' to indicate UTC
              formattedTimestamp = date.toISOString();
            } else {
              // Use local timezone with ISO format and timezone offset
              const tzOffset = date.getTimezoneOffset();
              const absOffset = Math.abs(tzOffset);
              const offsetHours = Math.floor(absOffset / 60).toString().padStart(2, '0');
              const offsetMinutes = (absOffset % 60).toString().padStart(2, '0');
              const offsetSign = tzOffset <= 0 ? '+' : '-'; // Note: getTimezoneOffset returns negative for positive offsets

              // Format the date in ISO format with timezone offset
              const localISOTime = new Date(date.getTime() - (tzOffset * 60000))
                .toISOString()
                .slice(0, -1); // Remove the trailing Z

              formattedTimestamp = `${localISOTime}${offsetSign}${offsetHours}:${offsetMinutes}`;
            }
          }
        } catch (e) {
          // Fallback to original formatting if there's an error
          formattedTimestamp = formatTimestamp(value as string);
        }

        return h(
          "span",
          {
            class: "flex-render-content font-mono text-[13px]",
            title: value as string // Show original value on hover
          },
          formattedTimestamp
        );
      }

      // Special handling for severity column
      if (col.name === severityField) {
        return h(
          "span",
          {
            class: "flex-render-content font-mono text-[13px] px-1.5 py-0.5 rounded-sm",
            title: value as string // Show original value on hover
          },
          () => value
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
              class: "flex-render-content json-content font-mono text-[13px]"
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
          class: "flex-render-content font-mono text-[13px]"
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
  });
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
