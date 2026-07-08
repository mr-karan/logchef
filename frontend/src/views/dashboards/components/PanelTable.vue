<script setup lang="ts">
import { computed } from "vue";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import type { PanelColumn } from "@/stores/dashboards";

// A "table" panel: rows from /logs/query or /logchefql/query. When the panel
// declares an explicit column subset (options.columns) we show only those, in
// that order; otherwise every returned column is shown.
interface Props {
  columns: PanelColumn[];
  rows: Record<string, any>[];
  columnSubset?: string[];
}
const props = defineProps<Props>();

const displayColumns = computed<string[]>(() => {
  const subset = (props.columnSubset ?? []).filter(Boolean);
  if (subset.length > 0) {
    return subset;
  }
  if (props.columns.length > 0) {
    return props.columns.map((c) => c.name);
  }
  // Fall back to the union of keys on the first row.
  return props.rows.length ? Object.keys(props.rows[0]) : [];
});

function cellValue(row: Record<string, any>, column: string): string {
  const value = row[column];
  if (value === null || value === undefined) return "";
  if (typeof value === "object") return JSON.stringify(value);
  return String(value);
}
</script>

<template>
  <div class="panel-table">
    <Table>
      <TableHeader class="panel-table__header">
        <TableRow>
          <TableHead
            v-for="col in displayColumns"
            :key="col"
            class="panel-table__head"
          >
            {{ col }}
          </TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        <TableRow v-for="(row, idx) in rows" :key="idx">
          <TableCell
            v-for="col in displayColumns"
            :key="col"
            class="panel-table__cell"
            :title="cellValue(row, col)"
          >
            {{ cellValue(row, col) }}
          </TableCell>
        </TableRow>
      </TableBody>
    </Table>
  </div>
</template>

<style scoped>
.panel-table {
  width: 100%;
  height: 100%;
  overflow: auto;
  font-size: 0.75rem;
}
.panel-table__header {
  position: sticky;
  top: 0;
  z-index: 1;
  background: var(--card);
}
.panel-table__head {
  height: 1.75rem;
  font-size: 0.7rem;
  white-space: nowrap;
}
.panel-table__cell {
  max-width: 22rem;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-variant-numeric: tabular-nums;
  padding-top: 0.3rem;
  padding-bottom: 0.3rem;
}
</style>
