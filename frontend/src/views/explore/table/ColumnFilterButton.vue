<script setup lang="ts">
import { computed } from 'vue'
import type { Column } from '@tanstack/vue-table'
import { Input } from '@/components/ui/input'
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from '@/components/ui/popover'
import { ListFilter } from 'lucide-vue-next'

interface Props {
  column: Column<Record<string, any>, unknown>
}

const props = defineProps<Props>()

const filterValue = computed<string>({
  get: () => (props.column.getFilterValue() as string | undefined) ?? '',
  set: (value: string) => {
    props.column.setFilterValue(value === '' ? undefined : value)
  },
})

const isActive = computed(() => filterValue.value.trim().length > 0)

function clearFilter() {
  filterValue.value = ''
}
</script>

<template>
  <Popover>
    <PopoverTrigger as-child>
      <button
        type="button"
        draggable="false"
        class="relative z-20 flex items-center justify-center flex-shrink-0 h-5 w-5 rounded transition-colors"
        :class="isActive
          ? 'text-primary bg-primary/10'
          : 'text-muted-foreground/50 opacity-0 group-hover:opacity-100 hover:text-foreground hover:bg-muted'"
        :title="isActive ? `Filtered: ${filterValue}` : 'Filter this column'"
        @mousedown.stop
        @dragstart.stop.prevent
        @click.stop
      >
        <ListFilter class="h-3 w-3" />
      </button>
    </PopoverTrigger>
    <PopoverContent
      class="w-56 p-2"
      align="start"
      @mousedown.stop
      @click.stop
    >
      <div class="space-y-1.5">
        <Input
          v-model="filterValue"
          placeholder="Contains… or >, >=, <, <=, ="
          class="h-7 text-xs"
          autofocus
        />
        <div class="flex items-center justify-between">
          <p class="text-[11px] text-muted-foreground">
            Filters this page only
          </p>
          <button
            v-if="isActive"
            type="button"
            class="text-[11px] text-muted-foreground hover:text-foreground underline-offset-2 hover:underline"
            @click="clearFilter"
          >
            Clear
          </button>
        </div>
      </div>
    </PopoverContent>
  </Popover>
</template>
