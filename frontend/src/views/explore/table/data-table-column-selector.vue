<script setup lang="ts">
import { useI18n } from "vue-i18n";

import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Label } from '@/components/ui/label'
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from '@/components/ui/popover'
import { ScrollArea } from '@/components/ui/scroll-area'
import { SearchInput } from '@/components/layout'
import { Check, Columns3, Minus } from 'lucide-vue-next'
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
const normalizedSearch = computed(() => search.value.trim().toLowerCase())
const isFiltering = computed(() => normalizedSearch.value.length > 0)

// Columns shown in the list. Matching is a case-insensitive substring match
// on the column name.
const filteredColumns = computed(() => {
  const query = normalizedSearch.value
  if (!query) return columns.value
  return columns.value.filter(column => column.id.toLowerCase().includes(query))
})

// Select All acts on the listed columns only, so a search narrows what it
// toggles and leaves hidden-by-search columns as they are.
const selectAllState = computed<boolean | 'indeterminate'>(() => {
  const listed = filteredColumns.value
  if (listed.length === 0) return false
  const visible = listed.filter(column => column.getIsVisible()).length
  if (visible === 0) return false
  return visible === listed.length ? true : 'indeterminate'
})

const toggleAll = (checked: boolean | 'indeterminate') => {
  const visible = checked === true
  filteredColumns.value.forEach(column => {
    column.toggleVisibility(visible)
  })
}

const onOpenChange = (open: boolean) => {
  if (!open) search.value = ''
}
</script>

<template>
  <Popover @update:open="onOpenChange">
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
        <SearchInput
          v-model="search"
          :placeholder="t('ui.searchColumns')"
          class="max-w-none"
        />
        <ScrollArea class="h-[300px] pr-4">
          <div class="grid gap-2">
            <!-- Select All option (acts on the columns matching the search) -->
            <div
              v-if="filteredColumns.length > 0"
              class="flex items-center space-x-2 py-1 border-b border-border mb-2"
            >
              <Checkbox
                id="select-all"
                :model-value="selectAllState"
                class="data-[state=indeterminate]:bg-primary data-[state=indeterminate]:text-primary-foreground data-[state=indeterminate]:border-primary"
                @update:model-value="toggleAll"
              >
                <template #default="{ state }">
                  <Minus v-if="state === 'indeterminate'" class="size-3.5" />
                  <Check v-else class="size-3.5" />
                </template>
              </Checkbox>
              <Label for="select-all" class="flex-1 cursor-pointer font-medium">
                {{ isFiltering ? t('ui.selectAllMatching', { count: filteredColumns.length }) : t('ui.selectAll') }}
              </Label>
            </div>
            <p v-else class="py-6 text-center text-sm text-muted-foreground">
              {{ t('ui.noColumnsMatch', { search: search.trim() }) }}
            </p>

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
          </div>
        </ScrollArea>
      </div>
    </PopoverContent>
  </Popover>
</template>
