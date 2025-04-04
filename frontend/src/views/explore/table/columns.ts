import type { ColumnDef, Column, Row } from "@tanstack/vue-table";
import { formatTimestamp, formatLogContent } from "@/lib/utils"; // Import formatLogContent
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
  timestamp: { defaultWidth: 260, minWidth: 160, maxWidth: 400 },
  severity: { defaultWidth: 120, minWidth: 80, maxWidth: 200 },
  message: { defaultWidth: 500, minWidth: 200, maxWidth: 1200 },
  default: { defaultWidth: 180, minWidth: 100, maxWidth: 600 }
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
      meta: {
        columnType,
        // Store width config in meta for reference
        widthConfig
      },
      // Column sizing configuration
      enableResizing: true,
      size: widthConfig.defaultWidth,
      minSize: widthConfig.minWidth,
      maxSize: widthConfig.maxWidth,

      // Accessor function for data
      accessorFn: (row: Record<string, any>) => row[col.name],

      // Header configuration with sorting
      header: ({ column }: { column: Column<Record<string, any>, unknown> }) => {
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

      // Cell configuration
      cell: ({ row, column }: { row: Row<Record<string, any>>, column: Column<Record<string, any>, unknown> }) => {
        const value = row.getValue<unknown>(column.id); // Use getValue<T> for type inference

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
          return h(
            "span",
            {
              class: "flex-render-content font-mono text-[13px]", // Base class
              title: value as string // Keep the original value as title
            },
            // First format for timezone, then format for coloring
            formatLogContent(formatTimestamp(value as string, timezone))
          );
        }

        // Special handling for severity column
        if (col.name === severityField) {
          return h(
            "span",
            {
              class: "flex-render-content font-mono text-[13px] px-1.5 py-0.5 rounded-sm",
              title: value as string
            },
            () => value
          );
        }

        // Handle objects
        if (typeof value === "object") {
          try {
            const jsonString = JSON.stringify(value, null, 2);
            return h(
              "span",
              {
                class: "flex-render-content json-content font-mono text-[13px]"
              },
              jsonString
            );
          } catch (err) {
            return h(
              "span",
              {
                class: "flex-render-content"
              },
              String(value)
            );
          }
        }

        // Default string representation
        return h(
          "span",
          {
            class: "flex-render-content font-mono text-[13px] whitespace-pre-wrap break-words" // Allow wrapping
          },
          formatLogContent(String(value)) // Use the formatter here
        );
      },

      // Enable sorting and filtering
      enableSorting: true,
      enableHiding: true,
      enableColumnFilter: true,
    };
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
