<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { ListFilter } from 'lucide-vue-next'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import { NUMBER_OPERATORS, TEXT_OPERATORS, type FilterOperator } from '@/utils/logchefql/filters'

const props = defineProps<{
  column: string
  kind: 'text' | 'number'
  active: boolean
  disabled: boolean
}>()

const emit = defineEmits<{
  (e: 'apply', value: { column: string; operator: FilterOperator; value: string | number }): void
  (e: 'clear', column: string): void
}>()

const { t } = useI18n()

const open = ref(false)
const operator = ref<FilterOperator>('=')
// v-model on a number input yields a number, so read the draft as text.
const draft = ref<string | number>('')

const operators = computed(() => props.kind === 'number' ? NUMBER_OPERATORS : TEXT_OPERATORS)

function operatorLabel(op: FilterOperator): string {
  if (props.kind === 'number') return op
  switch (op) {
    case '=': return t('explore.columnFilterEquals')
    case '!=': return t('explore.columnFilterNotEquals')
    case '~': return t('explore.columnFilterContains')
    case '!~': return t('explore.columnFilterNotContains')
    default: return op
  }
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

const title = computed(() => {
  if (props.disabled) return t('explore.columnFilterLogchefqlOnly')
  return props.active ? t('explore.columnFiltered', { column: props.column }) : t('explore.filterColumn', { column: props.column })
})
</script>

<template>
  <button
    v-if="disabled"
    type="button"
    draggable="false"
    data-column-filter
    class="relative z-20 flex items-center justify-center flex-shrink-0 h-5 w-5 rounded cursor-not-allowed text-muted-foreground/30 opacity-0 group-hover:opacity-100"
    :title="title"
    :aria-label="title"
    aria-disabled="true"
    @mousedown.stop
    @dragstart.stop.prevent
    @click.stop
  >
    <ListFilter class="h-3 w-3" />
  </button>
  <Popover v-else v-model:open="open">
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
        <div class="flex gap-1.5">
          <NativeSelect v-model="operator" class="h-7 px-2 pr-7 text-xs" :aria-label="t('explore.columnFilterOperator')">
            <NativeSelectOption v-for="op in operators" :key="op" :value="op">{{ operatorLabel(op) }}</NativeSelectOption>
          </NativeSelect>
          <Input
            v-model="draft"
            :type="kind === 'number' ? 'number' : 'text'"
            :placeholder="t('explore.columnFilterValue')"
            :aria-label="t('explore.columnFilterValue')"
            class="h-7 min-w-0 flex-1 text-xs"
            autofocus
          />
        </div>
        <div class="flex items-center justify-between gap-2">
          <Button v-if="active" type="button" variant="ghost" size="sm" class="h-7 px-2 text-xs" @click="clear">
            {{ t('explore.columnFilterClear') }}
          </Button>
          <Button type="submit" size="sm" class="ml-auto h-7 px-2 text-xs" :disabled="value === null">
            {{ t('explore.columnFilterApply') }}
          </Button>
        </div>
      </form>
    </PopoverContent>
  </Popover>
</template>
