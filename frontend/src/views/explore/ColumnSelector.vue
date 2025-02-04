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

interface Column {
  name: string
  type: string
}

interface Props {
  columns: Column[]
  modelValue: string[]
}

const props = defineProps<Props>()
const emit = defineEmits(['update:modelValue'])

const toggleColumn = (columnName: string) => {
  const newSelectedColumns = props.modelValue.includes(columnName)
    ? props.modelValue.filter(col => col !== columnName)
    : [...props.modelValue, columnName]
  emit('update:modelValue', newSelectedColumns)
}
</script>

<template>
  <Popover>
    <PopoverTrigger as-child>
      <Button variant="outline" size="sm" class="w-[150px]">
        <span class="mr-2">Columns</span>
        <span class="text-xs text-muted-foreground">({{ modelValue.length }})</span>
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
            <div
              v-for="column in columns"
              :key="column.name"
              class="flex items-center space-x-2 py-1"
            >
              <Checkbox
                :id="column.name"
                :checked="modelValue.includes(column.name)"
                @update:checked="() => toggleColumn(column.name)"
              />
              <Label :for="column.name" class="flex-1 cursor-pointer">
                {{ column.name }}
                <span class="ml-1 text-xs text-muted-foreground">{{ column.type }}</span>
              </Label>
            </div>
          </div>
        </ScrollArea>
      </div>
    </PopoverContent>
  </Popover>
</template>
