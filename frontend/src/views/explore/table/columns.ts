import type { ColumnDef, Column, Row } from "@tanstack/vue-table";
import { formatTimestamp, formatLogContent } from "@/lib/utils"; // Import formatLogContent
import { h, type VNode } from "vue"; // Import VNode
import { Button } from "@/components/ui/button";
import { ArrowUpDown, ArrowUp, ArrowDown } from "lucide-vue-next"; // Import new icons
import type { ColumnInfo } from "@/api/explore";
import { getSeverityClasses } from "@/lib/utils"; // Import getSeverityClasses

/**
 * Column type definitions with consistent size handling
 */
// Common column types for width settings
type ColumnType = 'timestamp' | 'severity' | 'message' | 'default' | 'status';

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
  default: { defaultWidth: 180, minWidth: 100, maxWidth: 600 },
  status: { defaultWidth: 120, minWidth: 80, maxWidth: 200 }
};

// Determine column type based on field name
function getColumnType(columnName: string, timestampField: string, severityField: string): ColumnType {
  const lowerColumnName = columnName.toLowerCase();

  // First check for exact match with the metadata timestamp field
  if (columnName === timestampField) {
    return 'timestamp';
  }

  // Then check if name contains timestamp or ends with _ts, _time, _at
  if (lowerColumnName.includes('timestamp') ||
      lowerColumnName.endsWith('_ts') ||
      lowerColumnName.endsWith('_time') ||
      lowerColumnName.endsWith('_at') ||  // common for created_at, updated_at
      lowerColumnName.match(/\d{4}[-_]\d{2}[-_]\d{2}/)) { // Contains date pattern
    return 'timestamp';
  }

  if (columnName === severityField) {
    return 'severity';
  }
  if (['message', 'log', 'msg', 'body', 'content'].includes(lowerColumnName)) {
    return 'message';
  }
  // More specific status code column detection
  if ([
    'status',
    'status_code',
    'http_status',
    'response_status',
    'statuscode',
    'response_code'
  ].some(statusName => lowerColumnName === statusName || lowerColumnName.endsWith('_' + statusName))) {
    return 'status';
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
  let sortedColumns = [...columns];

  // Create a regex for highlighting, case-insensitive, joining all non-empty terms
  const highlightRegex = searchTerms && searchTerms.filter(term => term.trim() !== '').length > 0
    ? new RegExp(`(${searchTerms.filter(term => term.trim() !== '').map(term => term.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')).join('|')})`, 'gi')
    : null;

  // First, prioritize the metadata timestamp field (from _meta_ts_field)
  const metaTsColumnIndex = sortedColumns.findIndex(
    (col) => col.name === timestampField
  );

  // Move the metadata timestamp field to the beginning if it exists
  if (metaTsColumnIndex >= 0) {
    const metaTsColumn = sortedColumns.splice(metaTsColumnIndex, 1)[0];
    sortedColumns.unshift(metaTsColumn);
  }

  // Next, find all other timestamp-like columns and sort them after the primary one
  const timestampColumns: ColumnInfo[] = [];
  sortedColumns = sortedColumns.filter(col => {
    // Skip the primary timestamp field which we already handled
    if (col.name === timestampField) return true;

    // Check if column should be treated as a timestamp
    const columnType = getColumnType(col.name, timestampField, severityField);
    if (columnType === 'timestamp') {
      timestampColumns.push(col);
      return false; // Remove from original array
    }
    return true; // Keep in original array
  });

  // Insert timestamp columns after the primary timestamp
  sortedColumns.splice(1, 0, ...timestampColumns);

  // Move severity field to be after the timestamp fields if it exists
  const severityColumnIndex = sortedColumns.findIndex(
    (col) => col.name === severityField
  );
  if (severityColumnIndex > 0) {
    const severityColumn = sortedColumns.splice(severityColumnIndex, 1)[0];
    sortedColumns.splice(1 + timestampColumns.length, 0, severityColumn);
  }

  return sortedColumns.map((col) => {
    const columnType = getColumnType(col.name, timestampField, severityField);
    const widthConfig = COLUMN_WIDTH_CONFIG[columnType];

    // Make sure we have a valid id, but avoid adding accessorKey
    const id = col.name || `col_${Math.random().toString(36).substr(2, 9)}`;

    return {
      id: id, // Set the id explicitly
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
      accessorFn: (row: Record<string, any>) => {
        // Try accessing by ID first, fall back to original column name if different
        return row[id] !== undefined ? row[id] : (col.name && id !== col.name ? row[col.name] : undefined);
      },

      // Header configuration with sorting
      header: ({ column }: { column: Column<Record<string, any>, unknown> }) => {
        return h(
          Button,
          {
            variant: "ghost",
            class: "h-8 px-2 hover:bg-muted/20 flex items-center", // Ensure flex alignment
            onClick: () => column.toggleSorting(column.getIsSorted() === "asc"),
          },
          () => {
            const children = [col.name || id]; // Start with column name
            const sortState = column.getIsSorted();
            const iconClass = "ml-2 h-3 w-3 text-muted-foreground flex-shrink-0"; // Keep icon style

            if (sortState === 'asc') {
              children.push(h(ArrowUp, { class: iconClass }));
            } else if (sortState === 'desc') {
              children.push(h(ArrowDown, { class: iconClass }));
            }
            // No icon if sortState is false

            return children;
          }
        );
      },

      // Cell configuration
      cell: ({ row, column }: { row: Row<Record<string, any>>, column: Column<Record<string, any>, unknown> }) => {
        // Use getValue to get the processed value via the accessorFn
        const value = row.getValue<unknown>(column.id);

        // Handle null/undefined values
        if (value === null || value === undefined) {
          return h(
            "div",
            { class: "text-muted-foreground flex-render-content" },
            "-"
          );
        }

        // Special handling for timestamp column
        if (id === timestampField || col.name === timestampField || columnType === 'timestamp') {
          const formattedTime = formatTimestamp(value as string, timezone);
          return h(
            "span",
            {
              class: "flex-render-content font-mono text-[13px]", // Base class
              title: value as string // Keep the original value as title
            },
            formatLogContent(formattedTime, false)
          );
        }

        // Check if the value looks like a timestamp regardless of column type
        // This is to catch timestamp values in columns that aren't explicitly identified as timestamps
        if (typeof value === 'string' && value.match(/^\d{4}-\d{2}-\d{2}[T ]\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})?$/)) {
          const formattedTime = formatTimestamp(value, timezone);
          return h(
            "span",
            {
              class: "flex-render-content font-mono text-[13px]", // Base class
              title: value // Keep the original value as title
            },
            formatLogContent(formattedTime, false)
          );
        }

        // Special handling for severity column
        if (id === severityField || col.name === severityField) {
          const severityValue = String(value);
          return h(
            "span",
            {
              class: `flex-render-content font-mono text-[13px] px-1.5 py-0.5 rounded-sm ${getSeverityClasses(severityValue, id || col.name)}`
            },
            severityValue
          );
        }

        // Define base classes once for consistency
        const baseClasses = "flex-render-content font-mono text-[13px]";

        // Check if this is a status column for better status code detection
        const isStatusColumn = columnType === 'status';

        // Convert value to string for processing
        let textValue: string;
        let isJsonObject = false;
        if (typeof value === "object") {
          try {
            textValue = JSON.stringify(value); // Compact JSON string
            isJsonObject = true;
          } catch (err) {
            textValue = String(value);
          }
        } else {
          textValue = String(value);
        }

        // Determine final classes (add JSON specific if needed)
        const finalClasses = `${baseClasses} ${isJsonObject ? 'json-content' : ''}`;

        // Check if the content contains HTTP methods or is a status column
        const hasHttpMethod = /\b(GET|POST|PUT|DELETE|HEAD|PATCH|OPTIONS)\b/i.test(textValue);
        const needsFormatting = isStatusColumn || hasHttpMethod;

        // Apply highlighting if regex exists and textValue is not empty
        if (highlightRegex && textValue.trim() !== '') {
          const parts = textValue.split(highlightRegex);
          // Filter out empty strings that can result from split if the match is at the start/end
          const filteredParts = parts.filter(part => part !== '');

          // Only proceed if splitting resulted in multiple parts or a single part that needs formatting
          if (filteredParts.length > 0) {
            const nodes: (string | VNode)[] = [];

            // Process each part individually
            filteredParts.forEach(part => {
              if (highlightRegex.test(part)) {
                // For highlighted parts, apply both highlight and any special formatting
                const formattedContent = needsFormatting ? formatLogContent(part, isStatusColumn) : [part];
                nodes.push(h('mark', { class: 'search-highlight' }, formattedContent));
              } else {
                // For non-highlighted parts, only apply special formatting if needed
                if (needsFormatting) {
                  const formattedContent = formatLogContent(part, isStatusColumn);
                  nodes.push(...formattedContent);
                } else {
                  nodes.push(part);
                }
              }
            });

            // Return the highlighted and formatted content
            return h("span", { class: finalClasses }, nodes);
          }
        }

        // If no highlighting needed but special formatting is required
        if (needsFormatting) {
          return h(
            "span",
            { class: finalClasses },
            formatLogContent(textValue, isStatusColumn)
          );
        }

        // For all other cases, render without special formatting
        return h(
          "span",
          { class: finalClasses },
          textValue
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
