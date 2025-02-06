import type { ColumnDef } from "@tanstack/vue-table";
import { formatTimestamp } from "@/lib/utils";
import { h } from "vue";
import get from "lodash/get";
import { Button } from "@/components/ui/button";
import { ArrowUpDown } from "lucide-vue-next";

// This type represents a column in our source schema
interface ColumnSchema {
  name: string;
  type: string;
}

// Function to generate column definitions based on source schema
export function createColumns(
  columns: ColumnSchema[]
): ColumnDef<Record<string, any>>[] {
  return columns.map((col) => ({
    id: col.name,
    // Use accessor function instead of accessorKey for nested properties
    accessorFn: (row) => get(row, col.name),
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
    cell: ({ row }) => {
      const value = row.getValue(col.name);

      // Handle null/undefined values
      if (value === null || value === undefined) {
        return h("div", { class: "text-muted-foreground text-xs" }, "-");
      }

      // Special handling for timestamp column
      if (col.name === "timestamp") {
        return h(
          "div",
          { class: "font-mono text-xs truncate" },
          formatTimestamp(value as string)
        );
      }

      // Handle objects by converting to JSON string
      if (typeof value === "object") {
        return h(
          "div",
          { class: "font-mono text-xs truncate" },
          JSON.stringify(value)
        );
      }

      // Default to string representation
      return h("div", { class: "font-mono text-xs truncate" }, String(value));
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
export function getInitialVisibleColumns(columns: ColumnSchema[]): string[] {
  const hasTimestamp = columns.some((col) => col.name === "timestamp");
  if (hasTimestamp) {
    return columns.length > 1
      ? ["timestamp", columns.find((col) => col.name !== "timestamp")!.name]
      : ["timestamp"];
  }
  return columns.length > 0 ? [columns[0].name] : [];
}
