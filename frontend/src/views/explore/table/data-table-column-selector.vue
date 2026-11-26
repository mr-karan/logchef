<script setup lang="ts">
import { useI18n } from "vue-i18n";

import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Label } from '@/components/ui/label'
import SearchInput from '@/components/layout/SearchInput.vue'
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from '@/components/ui/popover'
import { ScrollArea } from '@/components/ui/scroll-area'
import { Columns3 } from 'lucide-vue-next'
import { computed, ref } from 'vue'
import type { Table } from './tableFeatures'

const { t } = useI18n();

interface Props {
  table: Table<Record<string, any>>
  showResetDefaults?: boolean
}

const props = withDefaults(defineProps<Props>(), {
  showResetDefaults: false,
})

const emit = defineEmits<{
  'reset-defaults': []
}>()

const columns = computed(() => props.table.getAllColumns().filter(column => column.getCanHide()))
const visibleColumns = computed(() => columns.value.filter(column => column.getIsVisible()))
const search = ref('')
const filteredColumns = computed(() => {
  const query = search.value.trim().toLowerCase()
  return columns.value.filter(column => column.id.toLowerCase().includes(query))
})

const isAllSelected = computed<boolean | 'indeterminate'>(() => {
  const hideable = filteredColumns.value.length
  const visible = filteredColumns.value.filter(column => column.getIsVisible()).length
  if (visible > 0 && visible < hideable) return 'indeterminate'
  return hideable > 0 && hideable === visible
})

const toggleAll = (checked: boolean | 'indeterminate') => {
  const visible = checked === true
  filteredColumns.value.forEach(column => {
    column.toggleVisibility(visible)
  })
}
</script>

<template>
  <Popover @update:open="search = ''">
    <PopoverTrigger as-child>
      <Button variant='outline' size="sm" class='h-7 px-2 lg:w-[130px]' :title="t('ui.selectColumns')">
        <Columns3 class="h-4 w-4" />
        <span class="hidden lg:inline ml-1.5">{{ t('ui.columns') }}</span>
        <span class="text-xs text-muted-foreground ml-1">({{ visibleColumns.length }})</span>
      </Button>
    </PopoverTrigger>
    <PopoverContent class="w-80">
      <div class="grid gap-4">
        <div class="space-y-2">
          <h4 class="font-medium leading-none">
            {{ t('ui.tableColumns') }}
          </h4>
          <div class="flex items-start justify-between gap-3">
            <p class="text-sm text-muted-foreground">
              {{ t('ui.selectColumnsToDisplayInTheTable') }}
            </p>
            <Button
              v-if="props.showResetDefaults"
              variant="ghost"
              size="sm"
              class="h-7 px-2 text-xs"
              @click="emit('reset-defaults')"
            >
              {{ t('ui.resetDefaults') }}
            </Button>
          </div>
        </div>
        <SearchInput v-model="search" :placeholder="t('ui.searchFields')" />
        <ScrollArea class="h-[300px] pr-4">
          <div class="grid gap-2">
            <!-- Select All option -->
            <div v-if="filteredColumns.length" class="flex items-center space-x-2 py-1 border-b border-border mb-2">
              <Checkbox
                id="select-all"
                :model-value="isAllSelected"
                @update:model-value="toggleAll"
              />
              <Label for="select-all" class="flex-1 cursor-pointer font-medium">
                {{ t('ui.selectAll') }}
              </Label>
            </div>

            <div
              v-for="column in filteredColumns"
              :key="column.id"
              class="flex items-center space-x-2 py-1"
            >
              <Checkbox
                :id="column.id"
                :model-value="column.getIsVisible()"
                @update:model-value="(checked) => column.toggleVisibility(!!checked)"
              />
              <Label :for="column.id" class="flex-1 cursor-pointer">
                {{ column.id }}
              </Label>
            </div>
            <p v-if="!filteredColumns.length" class="py-4 text-center text-sm text-muted-foreground">
              {{ t('access.noResults') }}
            </p>
          </div>
        </ScrollArea>
      </div>
    </PopoverContent>
  </Popover>
</template>
