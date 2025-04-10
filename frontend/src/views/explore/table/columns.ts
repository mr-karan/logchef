import { h } from "vue";
import { ArrowDown, ArrowUp } from "lucide-vue-next";
import { Button } from "@/components/ui/button";
import type { Column, ColumnDef, Row } from "@tanstack/vue-table";
import { formatTimestamp, formatLogContent } from "@/lib/utils";
import { getSeverityClasses } from "@/lib/utils";
import type { ColumnInfo } from '@/api/explore';

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
const COLUMN_WIDTH_CONFIG: Record<string, { minWidth: number, defaultWidth: number, maxWidth: number }> = {
  timestamp: {
    minWidth: 160,
    defaultWidth: 180,
    maxWidth: 300,
  },
  severity: {
    minWidth: 80,
    defaultWidth: 100,
    maxWidth: 130,
  },
  status: {
    minWidth: 60,
    defaultWidth: 80,
    maxWidth: 120,
  },
  default: {
    minWidth: 100,
    defaultWidth: 150,
    maxWidth: 500,
  },
};

// Helper function to determine column type based on name or position
function getColumnType(
  columnName: string,
  timestampField: string,
  severityField: string
): string {
  // Exact match checks
  if (columnName === timestampField) {
    return "timestamp";
  }
  if (columnName === severityField) {
    return "severity";
  }

  // Pattern checks for status code columns
  if (
    /\b(status|code|statuscode|status_code|http_status)\b/i.test(columnName)
  ) {
    return "status";
  }

  // Pattern checks for timestamp-like columns
  if (
    /\b(time|date|timestamp|created|modified|updated|logged)\b/i.test(columnName)
  ) {
    return "timestamp";
  }

  // Pattern checks for severity-like columns
  if (
    /\b(severity|level|priority|type|log_level)\b/i.test(columnName)
  ) {
    return "severity";
  }

  // Default column type
  return "default";
}

// Function to generate column definitions based on source schema
export function createColumns(
  columns: ColumnInfo[],
  timestampField: string = "timestamp",
  timezone: 'local' | 'utc' = 'local',
  severityField: string = "severity_text",
  queryFields: string[] = [], // Fields used in query
  regexHighlights: Record<string, { pattern: string, isNegated: boolean }> = {} // Column-specific regex patterns
): ColumnDef<Record<string, any>>[] {
  // Create a new array with the columns in the desired order
  let sortedColumns = [...columns];

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
    const isFieldInQuery = queryFields.includes(col.name);
    const hasRegexHighlight = !!regexHighlights[col.name];

    // Make sure we have a valid id, but avoid adding accessorKey
    const id = col.name || `col_${Math.random().toString(36).substr(2, 9)}`;

    return {
      id: id, // Set the id explicitly
      // Store column type in meta for easy access
      meta: {
        columnType,
        // Store width config in meta for reference
        widthConfig,
        // Flag if this column is used in the query
        isFieldInQuery,
        // Flag if this column has a regex highlight pattern
        hasRegexHighlight,
        // Store regex pattern if available
        regexHighlight: regexHighlights[col.name]
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
            const children = []; // Start with empty array

            // Add query indicator before the name if column is used in query
            if (isFieldInQuery) {
              children.push(h('span', {
                class: 'w-2 h-2 rounded-full bg-emerald-500 mr-1.5 flex-shrink-0',
                title: 'This column is referenced in your query'
              }));
            }

            // Add the column name
            children.push(col.name || id);

            // Add regex indicator if applicable
            if (hasRegexHighlight) {
              children.push(h('span', {
                class: 'ml-1.5 text-xs text-amber-500 font-mono flex-shrink-0 opacity-80',
                title: `Regex pattern: ${regexHighlights[col.name].pattern}${regexHighlights[col.name].isNegated ? ' (negated)' : ''}`
              }, '~'));
            }

            // Add sort indicator
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

        // Format HTTP status codes
        if (isStatusColumn && /^\d{3}$/.test(textValue)) {
          const code = parseInt(textValue, 10);
          const statusClass = getStatusCodeClass(code);
          return h(
            "span",
            {
              class: `flex-render-content status-code ${statusClass}`,
              title: getStatusCodeDescription(code),
            },
            textValue
          );
        }

        // Format HTTP methods
        if (hasHttpMethod && !isJsonObject) {
          return h(
            "span",
            { class: finalClasses },
            formatHttpMethod(textValue)
          );
        }

        // --- Column-specific Highlighting Logic ---
        let highlightedContent = null;

        // Check if we have a regex highlight specifically for this column
        if (col.name && regexHighlights[col.name] && !isJsonObject) {
          const highlightInfo = regexHighlights[col.name];

          if (!highlightInfo.isNegated) { // Only highlight for non-negated patterns
            try {
              // Create a regex that escapes special characters
              const escapedPattern = highlightInfo.pattern.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
              const regex = new RegExp(`(${escapedPattern})`, 'gi');

              // Split by the regex
              const parts = textValue.split(regex);

              // Only render highlighted content if we found matches
              if (parts.length > 1) {
                highlightedContent = [];

                // Build the highlighted content
                parts.forEach((part, i) => {
                  if (i % 2 === 1) { // Match parts (odd indices)
                    highlightedContent.push(h('span', { class: 'search-highlight' }, part));
                  } else if (part) { // Non-match parts
                    highlightedContent.push(part);
                  }
                });
              }
            } catch (err) {
              console.warn('Error highlighting regex pattern:', err);
            }
          }
        }

        // Return the cell with or without highlighting
        return h(
          "span",
          { class: finalClasses, title: textValue },
          highlightedContent || textValue
        );
      },

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

// Add helper functions for status codes and HTTP methods
function getStatusCodeClass(code: number): string {
  if (code >= 100 && code < 200) return 'status-info';
  if (code >= 200 && code < 300) return 'status-success';
  if (code >= 300 && code < 400) return 'status-redirect';
  if (code >= 400 && code < 500) return 'status-error';
  if (code >= 500) return 'status-server';
  return '';
}

function getStatusCodeDescription(code: number): string {
  const statusMap: Record<number, string> = {
    // 1xx Informational
    100: 'Continue',
    101: 'Switching Protocols',
    102: 'Processing',
    103: 'Early Hints',
    // 2xx Success
    200: 'OK',
    201: 'Created',
    202: 'Accepted',
    203: 'Non-Authoritative Information',
    204: 'No Content',
    205: 'Reset Content',
    206: 'Partial Content',
    207: 'Multi-Status',
    208: 'Already Reported',
    226: 'IM Used',
    // 3xx Redirection
    300: 'Multiple Choices',
    301: 'Moved Permanently',
    302: 'Found',
    303: 'See Other',
    304: 'Not Modified',
    305: 'Use Proxy',
    307: 'Temporary Redirect',
    308: 'Permanent Redirect',
    // 4xx Client Error
    400: 'Bad Request',
    401: 'Unauthorized',
    402: 'Payment Required',
    403: 'Forbidden',
    404: 'Not Found',
    405: 'Method Not Allowed',
    406: 'Not Acceptable',
    407: 'Proxy Authentication Required',
    408: 'Request Timeout',
    409: 'Conflict',
    410: 'Gone',
    411: 'Length Required',
    412: 'Precondition Failed',
    413: 'Payload Too Large',
    414: 'URI Too Long',
    415: 'Unsupported Media Type',
    416: 'Range Not Satisfiable',
    417: 'Expectation Failed',
    418: "I'm a Teapot",
    421: 'Misdirected Request',
    422: 'Unprocessable Entity',
    423: 'Locked',
    424: 'Failed Dependency',
    425: 'Too Early',
    426: 'Upgrade Required',
    428: 'Precondition Required',
    429: 'Too Many Requests',
    431: 'Request Header Fields Too Large',
    451: 'Unavailable For Legal Reasons',
    // 5xx Server Error
    500: 'Internal Server Error',
    501: 'Not Implemented',
    502: 'Bad Gateway',
    503: 'Service Unavailable',
    504: 'Gateway Timeout',
    505: 'HTTP Version Not Supported',
    506: 'Variant Also Negotiates',
    507: 'Insufficient Storage',
    508: 'Loop Detected',
    510: 'Not Extended',
    511: 'Network Authentication Required',
  };

  return statusMap[code] || `HTTP Status ${code}`;
}

function formatHttpMethod(text: string): any[] {
  // Define regex for HTTP methods
  const methodRegex = /\b(GET|POST|PUT|DELETE|HEAD|PATCH|OPTIONS)\b/gi;

  // Split the text by HTTP methods
  const parts = text.split(methodRegex);

  if (parts.length <= 1) {
    // No HTTP methods found, return the text as is
    return [text];
  }

  // Create result array with formatted HTTP methods
  const result: any[] = [];

  parts.forEach((part, i) => {
    // Skip empty parts
    if (!part) return;

    // Check if this part is an HTTP method (would be at odd indices after split)
    const isMethod = i % 2 === 1;

    if (isMethod) {
      // Determine the method type for styling
      const method = part.toUpperCase();
      let methodClass = 'http-method';

      switch (method) {
        case 'GET':
          methodClass += ' http-method-get';
          break;
        case 'POST':
          methodClass += ' http-method-post';
          break;
        case 'PUT':
          methodClass += ' http-method-put';
          break;
        case 'DELETE':
          methodClass += ' http-method-delete';
          break;
        case 'HEAD':
          methodClass += ' http-method-head';
          break;
        case 'OPTIONS':
        case 'PATCH':
        default:
          methodClass += ' http-method-utility';
          break;
      }

      // Add the formatted HTTP method
      result.push(h('span', { class: methodClass }, method));
    } else {
      // Add the regular text
      result.push(part);
    }
  });

  return result;
}
