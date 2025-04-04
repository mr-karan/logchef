import type { ColumnDef, Column, Row } from "@tanstack/vue-table";
import { formatTimestamp, formatLogContent } from "@/lib/utils"; // Import formatLogContent
import { h, type VNode } from "vue"; // Import VNode
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
  severityField: string = "severity_text",
  searchTerms: string[] = [] // Add searchTerms parameter
): ColumnDef<Record<string, any>>[] {
  // Create a new array with the columns in the desired order
  // First, let's sort out the timestamp field to be first if it exists
  let sortedColumns = [...columns];
  
  // Create a regex for highlighting, case-insensitive, joining all non-empty terms
  const highlightRegex = searchTerms && searchTerms.filter(term => term.trim() !== '').length > 0
    ? new RegExp(`(${searchTerms.filter(term => term.trim() !== '').map(term => term.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')).join('|')})`, 'gi')
    : null;
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

        // Default string representation with highlighting
        const textValue = String(value);
        const baseClasses = "flex-render-content font-mono text-[13px] whitespace-pre-wrap break-words";

        // Apply highlighting if regex exists and value is a string
        if (highlightRegex && typeof value === 'string' && value.trim() !== '') {
          const parts = textValue.split(highlightRegex);
          const nodes: VNode[] = parts.map(part => {
            // Check if the part matches any of the search terms (case-insensitive)
            if (highlightRegex.test(part)) {
              // Apply formatLogContent to the highlighted part as well
              const formattedPart = formatLogContent(part);
              // Return VNodes created by formatLogContent, wrapped in a mark tag
              if (Array.isArray(formattedPart)) {
                 // If formatLogContent returns an array of VNodes (e.g., for timestamps)
                 // wrap each node. This might need adjustment based on formatLogContent's output.
                 // For simplicity, let's assume formatLogContent returns VNode or string for highlighted parts
                 // If it returns complex VNodes, highlighting might need refinement.
                 // Let's assume it returns a single VNode or string for now.
                 const contentNode = typeof formattedPart === 'string' ? formattedPart : formattedPart;
                 return h('mark', { class: 'search-highlight' }, contentNode);
              } else {
                 // If formatLogContent returns a single VNode or string
                 return h('mark', { class: 'search-highlight' }, formattedPart);
              }
            } else {
              // Apply formatLogContent to non-highlighted parts
              return formatLogContent(part);
            }
          }).filter(node => node !== ''); // Filter out empty strings resulting from split

          // If nodes were created, return them wrapped in a span
          if (nodes.length > 0) {
            return h("span", { class: baseClasses }, nodes);
          }
        }

        // Fallback: Render without highlighting but with formatting
        return h(
          "span",
          { class: baseClasses },
          formatLogContent(textValue) // Use the formatter here
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
