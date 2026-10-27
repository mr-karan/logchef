import { h } from "vue";
import "./badges.css";
import { ArrowDown, ArrowUp } from "lucide-vue-next";
import type { Column, ColumnDef, Row } from "./tableFeatures";
import { formatTimestamp } from "@/lib/utils";
import { getStandardSeverityClasses } from "@/lib/utils";
import type { ColumnInfo } from '@/api/explore';
import { columnFilterFn } from './columnFilter';
import { compareTimestamps } from './timestampSort';
import { getOutcomeTone, type OutcomeTone } from './outcomeTone';
import { classifyCellValue, type CellValueKind } from './cellTypography';

const CELL_VALUE_CLASSES: Record<CellValueKind, string> = {
  text: 'tabular-nums',
  identifier: 'font-mono tabular-nums text-muted-foreground',
  number: 'tabular-nums',
  structured: 'font-mono',
};

const OUTCOME_CLASSES: Record<OutcomeTone, string> = {
  failure: 'outcome-badge px-1 py-px rounded ring-1 ring-inset text-red-700 bg-red-100 ring-red-300 dark:text-red-200 dark:bg-red-950 dark:ring-red-800',
  pending: 'outcome-badge px-1 py-px rounded ring-1 ring-inset text-amber-800 bg-amber-100 ring-amber-300 dark:text-amber-200 dark:bg-amber-950 dark:ring-amber-800',
  success: '',
};

// Helper function to determine column type based on name or position
function getColumnType(
  columnName: string,
  schemaType: string,
  timestampField: string,
  severityField: string
): string {
  if (/^(?:Nullable\()?Date(?:32)?\b/i.test(schemaType)) return 'date';

  // Keep exact match checks
  if (columnName === timestampField) {
    return "timestamp";
  }
  if (columnName === severityField) {
    return "severity";
  }
  if (/^(Nullable\()?Date(Time)?(32|64)?\b/i.test(schemaType)) {
    return "timestamp";
  }

  // Keep pattern checks for common log formats
  if (/\b(status|code|statuscode|status_code|http_status)\b/i.test(columnName)) {
    return "status";
  }

  if (/\b(time|date|timestamp|created|modified|updated|logged)\b/i.test(columnName)) {
    return "timestamp";
  }

  if (/\b(severity|level|priority|type|log_level)\b/i.test(columnName)) {
    return "severity";
  }

  return "default";
}

export function calendarDate(value: unknown): string {
  const text = String(value);
  return /^\d{4}-\d{2}-\d{2}(?=$|[T ])/.exec(text)?.[0] ?? text;
}

export function timestampPrecision(schemaType: string): number | null {
  const fractionalType = /\bDateTime64\(\s*([0-9])\s*[,)]/i.exec(schemaType);
  if (fractionalType) return Number(fractionalType[1]);
  return /\bDateTime\b/i.test(schemaType) ? 0 : null;
}

export function timestampCell(value: unknown, timezone: 'local' | 'utc', precision: number | null) {
  const rawValue = String(value);
  const input = typeof value === 'number' ? new Date(value).toJSON() ?? rawValue : rawValue;
  const formatted = /^\d{4}-\d{2}-\d{2}$/.test(input) ? input : formatTimestamp(input, timezone);
  const parts = /^(\d{4}-\d{2}-\d{2})T(\d{2}:\d{2}:\d{2})\.\d+(?:Z|[+-]\d{2}:\d{2})$/.exec(formatted);
  // Read fractions before Date conversion, which discards digits beyond milliseconds.
  const fraction = /[T ]\d{2}:\d{2}:\d{2}\.(\d+)/.exec(input)?.[1] ?? '';
  const digits = precision === null ? fraction : fraction.padEnd(precision, '0').slice(0, precision);
  const content = parts ? [
    h('span', { class: 'table-timestamp-date text-muted-foreground' }, parts[1]),
    ' ',
    h('span', { class: 'table-timestamp-time text-foreground font-medium' }, parts[2] + (digits ? `.${digits}` : '')),
  ] : rawValue;
  return h('span', {
    class: 'flex-render-content font-mono tabular-nums text-[13px]',
    title: rawValue,
  }, content);
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
  } else {
    console.warn(`Timestamp field '${timestampField}' not found in columns. Using default column order.`);
  }

  // Next, find all other timestamp-like columns and sort them after the primary one
  const timestampColumns: ColumnInfo[] = [];
  sortedColumns = sortedColumns.filter(col => {
    // Skip the primary timestamp field which we already handled
    if (col.name === timestampField) return true;

    // Check if column should be treated as a timestamp
    const columnType = getColumnType(col.name, col.type, timestampField, severityField);
    if (columnType === 'timestamp' || columnType === 'date') {
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
    const columnType = getColumnType(col.name, col.type, timestampField, severityField);
    const precision = timestampPrecision(col.type);
    const isFieldInQuery = queryFields.includes(col.name);
    const hasRegexHighlight = !!regexHighlights[col.name];
    const highlightInfo = regexHighlights[col.name];
    const highlightRegex = highlightInfo?.pattern && !highlightInfo.isNegated
      ? new RegExp(`(${highlightInfo.pattern.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')})`, 'gi')
      : null;

    // Make sure we have a valid id, but avoid adding accessorKey
    const id = col.name || `col_${Math.random().toString(36).substr(2, 9)}`;

    return {
      id: id, // Set the id explicitly
      // Store column type in meta for easy access
      meta: {
        columnType,
        // Flag if this column is used in the query
        isFieldInQuery,
        // Flag if this column has a regex highlight pattern
        hasRegexHighlight,
        // Store regex pattern if available
        regexHighlight: regexHighlights[col.name]
      },
      // Column sizing configuration
      enableResizing: true,
      size: Math.max(180, Math.ceil(col.name.length * 7.5) + 32),
      minSize: 50,
      maxSize: 2000,
      sortFn: columnType === 'timestamp'
        ? (left, right, columnId) => compareTimestamps(left.getValue(columnId), right.getValue(columnId))
        : 'auto',

      // Accessor function for data
      accessorFn: (row: Record<string, any>) => {
        // Try accessing by ID first, fall back to original column name if different
        return row[id] !== undefined ? row[id] : (col.name && id !== col.name ? row[col.name] : undefined);
      },

      // Header configuration with sorting
      header: ({ column }: { column: Column<Record<string, any>, unknown> }) => {
        return h(
          "div",
          {
            class: "w-full flex items-center space-x-1 px-1 cursor-pointer select-none",
            onClick: () => column.toggleSorting(column.getIsSorted() === "asc"),
            title: col.name || id,
          },
          [
            // Query indicator
            isFieldInQuery && h('span', {
              class: 'w-2 h-2 rounded-full bg-emerald-500 flex-shrink-0',
              title: 'This column is referenced in your query'
            }),

            // Text label with proper truncation
            h('span', {
              class: 'truncate flex-grow text-left font-medium',
              style: 'word-break: normal; white-space: nowrap;'
            }, col.name || id),

            // Regex indicator
            hasRegexHighlight && h('span', {
              class: 'text-xs text-amber-500 font-mono flex-shrink-0 opacity-80 ml-1',
              title: `Regex pattern: ${regexHighlights[col.name]?.pattern}${regexHighlights[col.name]?.isNegated ? ' (negated)' : ''}`
            }, '~'),

            // Sort indicator (with spacing)
            column.getIsSorted() && h(
              column.getIsSorted() === 'asc' ? ArrowUp : ArrowDown,
              { class: "h-3 w-3 text-muted-foreground flex-shrink-0 ml-1" }
            )
          ].filter(Boolean)
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

        if (columnType === 'date') {
          return h('span', { class: 'flex-render-content font-mono tabular-nums text-[13px]', title: String(value) }, calendarDate(value));
        }

        if (columnType === 'timestamp' || (typeof value === 'string' && /^\d{4}-\d{2}-\d{2}[T ]\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})?$/.test(value))) {
          return timestampCell(value, timezone, precision);
        }

        // Special handling for severity column
        const severityClasses = id === severityField && typeof value === 'string'
          ? getStandardSeverityClasses(value)
            ?? (getOutcomeTone(value, id, severityField) ? undefined : getStandardSeverityClasses('debug'))
          : undefined;
        if (severityClasses) {
          const severityValue = String(value);
          return h(
            "span",
            {
              class: `flex-render-content font-mono text-[13px] px-1.5 py-0.5 rounded-sm ${severityClasses}`
            },
            severityValue
          );
        }

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

        const valueKind = classifyCellValue(value);
        const typography = isJsonObject ? 'font-mono json-content' : CELL_VALUE_CLASSES[valueKind];
        const finalClasses = `flex-render-content text-[13px] ${typography}`;

        // Check if the content contains HTTP methods or is a status column
        const hasHttpMethod = /\b(GET|POST|PUT|DELETE|HEAD|PATCH|OPTIONS)\b/i.test(textValue);

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
        const highlightRange = (start = 0, end = textValue.length) => {
          if (!highlightRegex || isJsonObject) return textValue.slice(start, end);
          let offset = 0;
          return textValue.split(highlightRegex).map((part, index) => {
            const text = part.slice(Math.max(0, start - offset), Math.max(0, end - offset));
            offset += part.length;
            return text && index % 2 === 1 ? h('span', { class: 'search-highlight' }, text) : text;
          });
        };

        if (valueKind === 'identifier') {
          const tailStart = Math.max(0, textValue.length - 8);
          return h('span', { class: `${finalClasses} table-identifier`, title: textValue }, [
            h('span', { class: 'table-identifier-head' }, highlightRange(0, tailStart)),
            h('span', { class: 'table-identifier-tail' }, highlightRange(tailStart)),
          ]);
        }

        const outcomeTone = !isJsonObject ? getOutcomeTone(textValue, col.name, severityField) : undefined;
        const outcomeClasses = outcomeTone ? ` outcome-${outcomeTone} ${OUTCOME_CLASSES[outcomeTone]}` : '';

        // Return the cell with or without highlighting
        return h(
          "span",
          { class: finalClasses + outcomeClasses, title: textValue },
          highlightRange()
        );
      },

      enableHiding: true,
      enableColumnFilter: true,
      // Client-side filtering of the currently loaded result page (see columnFilter.ts).
      filterFn: columnFilterFn,
    };
  });
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
