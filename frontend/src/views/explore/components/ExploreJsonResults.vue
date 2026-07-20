<script setup lang="ts">
import { computed, ref, watch } from "vue";
import {
  useVueTable,
  getCoreRowModel,
  getPaginationRowModel,
  type ColumnDef,
  type PaginationState,
} from "@tanstack/vue-table";
import JsonViewer from "@/components/json-viewer/JsonViewer.vue";
import DataTablePagination from "@/views/explore/table/data-table-pagination.vue";
import { valueUpdater } from "@/lib/utils";

const props = defineProps<{
  data: Record<string, any>[];
  isLoading?: boolean;
}>();

// This view used to hand the FULL result array (up to the 100k max_limit) to
// a single JsonViewer with expanded=true - one JSON.stringify + highlight.js
// pass over the whole thing, rendered as one v-html. That freezes/crashes the
// tab at high row counts. Paginate like the main DataTable (data-table.vue,
// pageSize 50) so only one page's worth of rows is ever serialized and
// highlighted at a time.
//
// We reuse TanStack's pagination row model purely for its row-windowing +
// the existing DataTablePagination control, rather than inventing a new
// windowing scheme - no real columns are needed since this isn't a grid.
const pagination = ref<PaginationState>({
  pageIndex: 0,
  pageSize: 50,
});

const columns: ColumnDef<Record<string, any>>[] = [];

const table = useVueTable({
  get data() {
    return props.data;
  },
  columns,
  state: {
    get pagination() {
      return pagination.value;
    },
  },
  onPaginationChange: (updaterOrValue) => valueUpdater(updaterOrValue, pagination),
  getCoreRowModel: getCoreRowModel(),
  getPaginationRowModel: getPaginationRowModel(),
});

// A new query result replaces `data` with a fresh array reference - jump
// back to page 1 rather than leaving the user stranded on a page that may
// no longer exist for the new result set.
watch(
  () => props.data,
  () => {
    pagination.value = { ...pagination.value, pageIndex: 0 };
  }
);

const pageData = computed(() => table.getRowModel().rows.map((row) => row.original));

const rangeStart = computed(() => {
  if (props.data.length === 0) return 0;
  return pagination.value.pageIndex * pagination.value.pageSize + 1;
});

const rangeEnd = computed(() =>
  Math.min(props.data.length, rangeStart.value + pagination.value.pageSize - 1)
);
</script>

<template>
  <div class="h-full min-h-0 flex flex-col">
    <div
      v-if="isLoading"
      class="flex h-full min-h-[240px] items-center justify-center rounded-md border border-dashed text-sm text-muted-foreground"
    >
      Loading results...
    </div>
    <template v-else>
      <div
        v-if="data.length > 0"
        class="flex items-center justify-between gap-2 px-4 py-2 border-b flex-shrink-0"
      >
        <span class="text-xs text-muted-foreground">
          Showing {{ rangeStart.toLocaleString() }}-{{ rangeEnd.toLocaleString() }} of
          {{ data.length.toLocaleString() }} rows
        </span>
        <DataTablePagination :table="table" />
      </div>
      <div class="flex-1 min-h-0 overflow-auto p-4">
        <JsonViewer v-if="data.length > 0" :value="pageData" :expanded="true" />
        <div v-else class="p-4 text-center text-sm text-muted-foreground">
          No logs to display
        </div>
      </div>
    </template>
  </div>
</template>
