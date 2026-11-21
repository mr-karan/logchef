<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { ListFilter, X } from 'lucide-vue-next'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { NUMBER_OPERATORS, TEXT_OPERATORS, type FilterOperator } from '@/utils/logchefql/filters'
import { pageFilterOperatorLabel, type PageFilter } from './pageFilters'

// A header filter narrows the rows loaded on this page. It never reruns
// the query; the column's filters are listed so each can be removed.
const props = withDefaults(defineProps<{
  column: string
  kind: 'text' | 'number'
  filters?: PageFilter[]
}>(), { filters: () => [] })

const emit = defineEmits<{
  (e: 'apply', value: PageFilter): void
  (e: 'remove', filter: PageFilter): void
  (e: 'clear', column: string): void
}>()

const { t } = useI18n()

const open = ref(false)
const operator = ref<FilterOperator>('=')
// v-model on a number input yields a number, so read the draft as text.
const draft = ref<string | number>('')

const active = computed(() => props.filters.length > 0)
const operators = computed(() => props.kind === 'number' ? NUMBER_OPERATORS : TEXT_OPERATORS)

function operatorLabel(op: FilterOperator): string {
  return pageFilterOperatorLabel(t, op, props.kind === 'number')
}

const value = computed<string | number | null>(() => {
  const text = String(draft.value).trim()
  if (!text) return null
  if (props.kind === 'text') return text
  return /^-?\d+(?:\.\d+)?$/.test(text) ? Number(text) : null
})

function apply() {
  if (value.value === null) return
  emit('apply', { column: props.column, operator: operator.value, value: value.value })
  draft.value = ''
  open.value = false
}

function clear() {
  emit('clear', props.column)
  open.value = false
}

const title = computed(() => active.value ? t('explore.pageFiltered', { column: props.column }) : t('explore.filterColumn', { column: props.column }))
</script>

<template>
  <Popover v-model:open="open">
    <PopoverTrigger as-child>
      <button
        type="button"
        draggable="false"
        data-column-filter
        class="relative z-20 flex items-center justify-center flex-shrink-0 h-5 w-5 rounded transition-colors"
        :class="active ? 'text-primary bg-primary/10' : 'text-muted-foreground/50 opacity-0 group-hover:opacity-100 group-hover:bg-muted hover:text-foreground'"
        :title="title"
        :aria-label="title"
        @mousedown.stop
        @dragstart.stop.prevent
        @click.stop
      >
        <ListFilter class="h-3 w-3" />
      </button>
    </PopoverTrigger>
    <PopoverContent class="w-64 p-2" align="start" @mousedown.stop @click.stop>
      <form class="space-y-2" @submit.prevent="apply">
        <p class="text-xs text-muted-foreground">{{ t('explore.pageFilterHint') }}</p>
        <ul v-if="active" class="space-y-1" data-testid="column-page-filters">
          <li v-for="(filter, index) in filters" :key="index" class="flex items-center justify-between gap-1 rounded bg-muted px-2 py-0.5 text-xs">
            <span class="truncate">{{ operatorLabel(filter.operator) }} {{ filter.value }}</span>
            <button type="button" class="text-muted-foreground hover:text-foreground" :aria-label="t('ui.delete')" @click="emit('remove', filter)">
              <X class="h-3 w-3" />
            </button>
          </li>
        </ul>
        <div class="flex gap-1.5">
          <Select v-model="operator">
            <SelectTrigger size="sm" class="h-7 px-2 text-xs" :aria-label="t('explore.columnFilterOperator')" data-testid="column-filter-operator">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem v-for="op in operators" :key="op" :value="op" class="text-xs">{{ operatorLabel(op) }}</SelectItem>
            </SelectContent>
          </Select>
          <Input
            v-model="draft"
            :type="kind === 'number' ? 'number' : 'text'"
            :placeholder="t('explore.columnFilterValue')"
            :aria-label="t('explore.columnFilterValue')"
            class="h-7 min-w-0 flex-1 text-xs"
            data-testid="column-filter-value"
            autofocus
          />
        </div>
        <div class="flex items-center justify-between gap-2">
          <Button v-if="active" type="button" variant="ghost" size="sm" class="h-7 px-2 text-xs" @click="clear">
            {{ t('explore.pageFilterClear') }}
          </Button>
          <Button type="submit" size="sm" class="ml-auto h-7 px-2 text-xs" :disabled="value === null">
            {{ t('explore.pageFilterApply') }}
          </Button>
        </div>
      </form>
    </PopoverContent>
  </Popover>
</template>
