<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { ChevronLeft, ChevronRight } from "lucide-vue-next";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { Button } from "@/components/ui/button";
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

// Rendering every row unbounded means a saved row limit of e.g. 100k turns
// into 100k DOM rows. Paginate client-side instead - the panel's own row
// limit (PanelBuilderOptions) already bounds how much data is fetched; this
// just bounds how much of it is in the DOM at once.
const PAGE_SIZE = 100;
const page = ref(0);

// A new query result replaces props.rows with a fresh array - always land
// back on page 1 rather than stranding the viewer on a page past the end of
// a smaller new result set.
watch(
  () => props.rows,
  () => {
    page.value = 0;
  }
);

const pageCount = computed(() => Math.max(1, Math.ceil(props.rows.length / PAGE_SIZE)));
const pagedRows = computed(() => {
  const start = page.value * PAGE_SIZE;
  return props.rows.slice(start, start + PAGE_SIZE);
});
const rangeLabel = computed(() => {
  if (props.rows.length === 0) return "0 rows";
  const start = page.value * PAGE_SIZE + 1;
  const end = Math.min(props.rows.length, start + PAGE_SIZE - 1);
  return `${start.toLocaleString()}–${end.toLocaleString()} of ${props.rows.length.toLocaleString()}`;
});

function prevPage() {
  if (page.value > 0) page.value -= 1;
}
function nextPage() {
  if (page.value < pageCount.value - 1) page.value += 1;
}

// How many rows to scan for the fallback column-union derivation below. Rows
// can have different shapes (e.g. sparse JSON-derived fields), so union
// across a bounded sample rather than trusting rows[0] alone - but a full
// scan over a 100k-row result would itself be wasteful, so cap it.
const COLUMN_UNION_SAMPLE_SIZE = 500;

const displayColumns = computed<string[]>(() => {
  const subset = (props.columnSubset ?? []).filter(Boolean);
  if (subset.length > 0) {
    return subset;
  }
  if (props.columns.length > 0) {
    return props.columns.map((c) => c.name);
  }
  // Fall back to the union of keys across a bounded sample of rows.
  const keys = new Set<string>();
  const sampleSize = Math.min(props.rows.length, COLUMN_UNION_SAMPLE_SIZE);
  for (let i = 0; i < sampleSize; i += 1) {
    for (const key of Object.keys(props.rows[i])) keys.add(key);
  }
  return [...keys];
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
    <div class="panel-table__scroll">
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
          <TableRow v-for="(row, idx) in pagedRows" :key="idx">
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
    <div v-if="rows.length > PAGE_SIZE" class="panel-table__footer">
      <span class="panel-table__range">{{ rangeLabel }}</span>
      <div class="panel-table__pager">
        <Button
          type="button"
          variant="outline"
          size="sm"
          class="h-6 w-6 p-0"
          :disabled="page === 0"
          aria-label="Previous page"
          @click="prevPage"
        >
          <ChevronLeft class="h-3.5 w-3.5" />
        </Button>
        <span class="panel-table__page">{{ page + 1 }} / {{ pageCount }}</span>
        <Button
          type="button"
          variant="outline"
          size="sm"
          class="h-6 w-6 p-0"
          :disabled="page >= pageCount - 1"
          aria-label="Next page"
          @click="nextPage"
        >
          <ChevronRight class="h-3.5 w-3.5" />
        </Button>
      </div>
    </div>
  </div>
</template>

<style scoped>
.panel-table {
  width: 100%;
  height: 100%;
  display: flex;
  flex-direction: column;
  font-size: 0.75rem;
}
.panel-table__scroll {
  flex: 1 1 auto;
  min-height: 0;
  overflow: auto;
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
.panel-table__footer {
  flex-shrink: 0;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 0.5rem;
  padding: 0.25rem 0.35rem 0;
  border-top: 1px solid var(--border);
  font-size: 0.7rem;
  color: var(--muted-foreground);
}
.panel-table__pager {
  display: flex;
  align-items: center;
  gap: 0.35rem;
}
.panel-table__page {
  min-width: 2.5rem;
  text-align: center;
  font-variant-numeric: tabular-nums;
}
</style>
