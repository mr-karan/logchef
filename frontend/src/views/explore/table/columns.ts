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

        // Define base classes once for consistency
        const baseClasses = "flex-render-content font-mono text-[13px]";
        
        // --- Process Objects and Strings Consistently for Highlighting ---
        let textValue: string;
        let isJsonObject = false;

        // Convert objects to string first
        if (typeof value === "object") {
          try {
            textValue = JSON.stringify(value, null, 2); // Stringify the object
            isJsonObject = true;
          } catch (err) {
            textValue = String(value); // Fallback to basic string conversion
          }
        } else {
          textValue = String(value); // Use value directly if already string/number/etc.
        }

        // Determine final classes (add JSON specific if needed)
        const finalClasses = `${baseClasses} ${isJsonObject ? 'json-content' : ''}`;

        // Apply highlighting if regex exists and textValue is not empty
        if (highlightRegex && textValue.trim() !== '') {
          const parts = textValue.split(highlightRegex);
          // Filter out empty strings that can result from split if the match is at the start/end
          const filteredParts = parts.filter(part => part !== '');

          // Only proceed if splitting resulted in multiple parts or a single part that needs formatting
          if (filteredParts.length > 0) {
            const nodes: (VNode | string)[] = filteredParts.map(part => {
              // Check if the part matches any of the search terms (case-insensitive)
              if (highlightRegex.test(part)) {
                // Apply formatLogContent to the highlighted part
                const formattedPart = formatLogContent(part);
                // Wrap the formatted part (which could be VNode or string) in a mark tag
                return h('mark', { class: 'search-highlight' }, formattedPart);
              } else {
                // Apply formatLogContent to non-highlighted parts
                return formatLogContent(part);
              }
            });

            // If nodes were created, return them wrapped in a span
            // Filter out any potential null/empty nodes from formatLogContent
            const validNodes = nodes.filter(n => n !== null && n !== '');
            if (validNodes.length > 0) {
              return h("span", { class: finalClasses }, validNodes);
            }
          }
        }

        // Fallback: Render without highlighting but with formatting
        // This handles cases where highlightRegex is null, textValue is empty,
        // or splitting didn't produce highlightable parts.
        return h(
          "span",
          { class: finalClasses },
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
