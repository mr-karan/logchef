<script setup lang="ts">
import { useI18n } from "vue-i18n";

import type { Table } from './tableFeatures'
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

const { t } = useI18n();

interface Props {
  table: Table<Record<string, any>>
}

defineProps<Props>()

const pageSizes = [10, 25, 50, 100, 250, 500, 1000]
</script>

<template>
  <div class="flex items-center gap-2">
    <!-- Rows per page - more compact -->
    <TooltipProvider>
      <Tooltip :delayDuration="200">
        <TooltipTrigger>
          <Select :model-value="`${table.atoms.pagination.get().pageSize}`"
            @update:model-value="(value) => table.setPageSize(Number(value))">
            <SelectTrigger class="h-7 w-[70px] text-xs">
              <SelectValue :placeholder="`${table.atoms.pagination.get().pageSize}`" />
            </SelectTrigger>
            <SelectContent side="top">
              <SelectItem v-for="size in pageSizes" :key="size" :value="`${size}`" class="text-xs">
                {{ size }}
              </SelectItem>
            </SelectContent>
          </Select>
        </TooltipTrigger>
        <TooltipContent :sideOffset="4" side="top" align="center" class="text-xs">
          <p>{{ t('ui.rowsPerPage') }}</p>
        </TooltipContent>
      </Tooltip>
    </TooltipProvider>

    <!-- Page navigation - more compact -->
    <div class="flex items-center gap-1">
      <!-- First Page -->
      <Button variant="ghost" size="icon" class="h-6 w-6 p-0" :disabled="!table.getCanPreviousPage()"
        @click="table.setPageIndex(0)">
        <span class="sr-only">{{ t('ui.goToFirstPage') }}</span>
        <ChevronsLeft class="h-3 w-3" />
      </Button>

      <!-- Previous Page -->
      <Button variant="ghost" size="icon" class="h-6 w-6 p-0" :disabled="!table.getCanPreviousPage()"
        @click="table.previousPage()">
        <span class="sr-only">{{ t('ui.goToPreviousPage') }}</span>
        <ChevronLeft class="h-3 w-3" />
      </Button>

      <span class="text-xs mx-1">
        {{ table.atoms.pagination.get().pageIndex + 1 }}/{{ table.getPageCount() }}
      </span>

      <!-- Next Page -->
      <Button variant="ghost" size="icon" class="h-6 w-6 p-0" :disabled="!table.getCanNextPage()"
        @click="table.nextPage()">
        <span class="sr-only">{{ t('ui.goToNextPage') }}</span>
        <ChevronRight class="h-3 w-3" />
      </Button>

      <!-- Last Page -->
      <Button variant="ghost" size="icon" class="h-6 w-6 p-0" :disabled="!table.getCanNextPage()"
        @click="table.setPageIndex(table.getPageCount() - 1)">
        <span class="sr-only">{{ t('ui.goToLastPage') }}</span>
        <ChevronsRight class="h-3 w-3" />
      </Button>
    </div>
  </div>
</template>
