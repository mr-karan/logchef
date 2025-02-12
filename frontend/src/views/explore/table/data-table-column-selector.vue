<script setup lang="ts">
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Label } from '@/components/ui/label'
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from '@/components/ui/popover'
import { ScrollArea } from '@/components/ui/scroll-area'
import { computed } from 'vue'
import type { Table } from '@tanstack/vue-table'

interface Props {
  table: Table<Record<string, any>>
}

const props = defineProps<Props>()

const columns = computed(() => props.table.getAllColumns().filter(column => column.getCanHide()))
const visibleColumns = computed(() => columns.value.filter(column => column.getIsVisible()))

const isAllSelected = computed(() => {
  const hideable = columns.value.length
  const visible = visibleColumns.value.length
  return hideable > 0 && hideable === visible
})

const toggleAll = (checked: boolean) => {
  columns.value.forEach(column => {
    column.toggleVisibility(checked)
  })
}
</script>

<template>
  <Popover>
    <PopoverTrigger as-child>
      <Button variant="outline" size="sm" class="w-[150px]">
        <span class="mr-2">Columns</span>
        <span class="text-xs text-muted-foreground">({{ visibleColumns.length }})</span>
      </Button>
    </PopoverTrigger>
    <PopoverContent class="w-80">
      <div class="grid gap-4">
        <div class="space-y-2">
          <h4 class="font-medium leading-none">
            Table Columns
          </h4>
          <p class="text-sm text-muted-foreground">
            Select columns to display in the table
          </p>
        </div>
        <ScrollArea class="h-[300px] pr-4">
          <div class="grid gap-2">
            <!-- Select All option -->
            <div class="flex items-center space-x-2 py-1 border-b border-border mb-2">
              <Checkbox
                id="select-all"
                :checked="isAllSelected"
                @update:checked="toggleAll"
              />
              <Label for="select-all" class="flex-1 cursor-pointer font-medium">
                Select All
              </Label>
            </div>

            <div
              v-for="column in columns"
              :key="column.id"
              class="flex items-center space-x-2 py-1"
            >
              <Checkbox
                :id="column.id"
                :checked="column.getIsVisible()"
                @update:checked="(checked) => column.toggleVisibility(!!checked)"
              />
              <Label :for="column.id" class="flex-1 cursor-pointer">
                {{ column.id }}
              </Label>
            </div>
          </div>
        </ScrollArea>
      </div>
    </PopoverContent>
  </Popover>
</template>
