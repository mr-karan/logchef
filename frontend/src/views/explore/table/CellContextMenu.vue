<script setup lang="ts">
// One context menu for the whole table body. The table records which cell was
// right-clicked (or had the context-menu key pressed) in `target`; the menu
// never sits over a value or under an ordinary click.
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuPortal,
  ContextMenuRoot,
  ContextMenuSeparator,
  ContextMenuTrigger,
} from 'reka-ui'
import { Copy, Minus, Plus } from 'lucide-vue-next'
import { filterChoices, type FilterChoice } from '@/utils/logchefql/filters'

export interface CellMenuTarget {
  field: string
  value: unknown
  selection: string
  canFilter: boolean
}

const props = defineProps<{ target: CellMenuTarget | null }>()

const emit = defineEmits<{
  (e: 'filter', value: { column: string; value: unknown; operator: FilterChoice['operator'] }): void
  (e: 'copy', value: unknown): void
}>()

const { t } = useI18n()

const choices = computed(() => props.target ? filterChoices(props.target.value, props.target.selection) : null)

function label(choice: FilterChoice, kind: 'include' | 'exclude'): string {
  if (choice.operator === '~' || choice.operator === '!~') {
    const term = String(choice.value)
    const shown = term.length > 40 ? `${term.slice(0, 40)}…` : term
    return t(kind === 'include' ? 'explore.filterContains' : 'explore.filterNotContains', { term: shown })
  }
  return t(kind === 'include' ? 'ui.filterThisValue' : 'ui.filterThisValue2')
}

function apply(choice: FilterChoice) {
  if (!props.target) return
  emit('filter', { column: props.target.field, value: choice.value, operator: choice.operator })
}

const itemClass = 'flex cursor-default select-none items-center gap-2 rounded-sm px-2 py-1.5 text-sm outline-hidden data-[highlighted]:bg-accent data-[highlighted]:text-accent-foreground [&_svg]:size-3.5 [&_svg]:text-muted-foreground'
</script>

<template>
  <ContextMenuRoot :modal="false">
    <ContextMenuTrigger as-child>
      <slot />
    </ContextMenuTrigger>
    <ContextMenuPortal>
      <ContextMenuContent
        v-if="target"
        data-cell-menu
        class="bg-popover text-popover-foreground z-50 min-w-[12rem] max-w-[24rem] overflow-hidden rounded-md border p-1 shadow-md"
      >
        <ContextMenuItem :class="itemClass" @select="emit('copy', target.value)">
          <Copy /> {{ t('ui.copyValue') }}
        </ContextMenuItem>
        <template v-if="target.canFilter && choices">
          <ContextMenuSeparator class="bg-border -mx-1 my-1 h-px" />
          <ContextMenuItem :class="itemClass" @select="apply(choices.include)">
            <Plus /> <span class="truncate">{{ label(choices.include, 'include') }}</span>
          </ContextMenuItem>
          <ContextMenuItem :class="itemClass" @select="apply(choices.exclude)">
            <Minus /> <span class="truncate">{{ label(choices.exclude, 'exclude') }}</span>
          </ContextMenuItem>
        </template>
      </ContextMenuContent>
    </ContextMenuPortal>
  </ContextMenuRoot>
</template>
