<script setup lang="ts">
import type { Table } from '@tanstack/vue-table'
import { Button } from '@/components/ui/button'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import {
  ChevronsLeft,
  ChevronLeft,
  ChevronRight,
  ChevronsRight,
} from 'lucide-vue-next'
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from '@/components/ui/tooltip'

interface Props {
  table: Table<Record<string, any>>
}

defineProps<Props>()

const pageSizes = [10, 25, 50, 100, 250, 500, 1000]
</script>

<template>
  <div class="flex items-center gap-6">
    <!-- Rows per page -->
    <TooltipProvider>
      <Tooltip :delayDuration="200">
        <TooltipTrigger>
          <Select
            :model-value="`${table.getState().pagination.pageSize}`"
            @update:model-value="(value) => table.setPageSize(Number(value))"
          >
            <SelectTrigger class="h-8 w-[85px]">
              <SelectValue :placeholder="`${table.getState().pagination.pageSize}`" />
            </SelectTrigger>
            <SelectContent side="top">
              <SelectItem v-for="size in pageSizes" :key="size" :value="`${size}`">
                {{ size }}
              </SelectItem>
            </SelectContent>
          </Select>
        </TooltipTrigger>
        <TooltipContent :sideOffset="4" side="top" align="center" class="text-xs">
          <p>Rows per page</p>
        </TooltipContent>
      </Tooltip>
    </TooltipProvider>

    <!-- Page info and navigation -->
    <div class="flex items-center gap-2">
      <!-- First Page -->
      <Button
        variant="ghost"
        size="icon"
        class="hidden h-8 w-8 p-0 lg:flex"
        :disabled="!table.getCanPreviousPage()"
        @click="table.setPageIndex(0)"
      >
        <span class="sr-only">Go to first page</span>
        <ChevronsLeft class="h-4 w-4" />
      </Button>

      <!-- Previous Page -->
      <Button
        variant="ghost"
        size="icon"
        class="h-8 w-8 p-0"
        :disabled="!table.getCanPreviousPage()"
        @click="table.previousPage()"
      >
        <span class="sr-only">Go to previous page</span>
        <ChevronLeft class="h-4 w-4" />
      </Button>

      <span class="text-sm">
        Page {{ table.getState().pagination.pageIndex + 1 }} of {{ table.getPageCount() }}
      </span>

      <!-- Next Page -->
      <Button
        variant="ghost"
        size="icon"
        class="h-8 w-8 p-0"
        :disabled="!table.getCanNextPage()"
        @click="table.nextPage()"
      >
        <span class="sr-only">Go to next page</span>
        <ChevronRight class="h-4 w-4" />
      </Button>

      <!-- Last Page -->
      <Button
        variant="ghost"
        size="icon"
        class="hidden h-8 w-8 p-0 lg:flex"
        :disabled="!table.getCanNextPage()"
        @click="table.setPageIndex(table.getPageCount() - 1)"
      >
        <span class="sr-only">Go to last page</span>
        <ChevronsRight class="h-4 w-4" />
      </Button>
    </div>
  </div>
</template>
