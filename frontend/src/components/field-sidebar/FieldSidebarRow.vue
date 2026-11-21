<script setup lang="ts">
// One filterable field in the Fields sidebar. It is its own component so a
// value load re-renders only this row: the sidebar passes the field's state
// object, which keeps its identity while other fields change.
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Skeleton } from '@/components/ui/skeleton'
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@/components/ui/collapsible'
import { AlertTriangle, Check, ChevronRight, Columns3, Copy, Loader2, Minus, Plus, RefreshCw } from 'lucide-vue-next'
import { copyText } from '@/utils/clipboard'
import { cn } from '@/lib/utils'
import type { FieldLoadingState } from '@/composables/useFieldValuesLoader'
import { formatCount, getCleanType, getTypeColorClass, getTypeIcon, hasTooManyValues, type FieldInfo } from './fieldDisplay'

const props = defineProps<{
  field: FieldInfo
  // Undefined until the loader has seen the field; treated as idle.
  state: FieldLoadingState | undefined
  expanded: boolean
  // The results table shows this field as a column.
  columnShown?: boolean
}>()

const emit = defineEmits<{
  (e: 'toggle', field: FieldInfo): void
  (e: 'load', field: FieldInfo): void
  (e: 'add-filter', field: string, value: string, operator: '=' | '!='): void
  (e: 'toggle-column', field: string): void
}>()

const { t } = useI18n()

const status = computed(() => props.state?.status ?? 'idle')
const values = computed(() => props.state?.values)
const tooManyValues = computed(() => hasTooManyValues(props.field, values.value))

// The value just copied shows a check mark for a moment.
const copiedValue = ref<string | null>(null)
let copiedTimer: ReturnType<typeof setTimeout> | undefined
const copy = async (value: string) => {
  try {
    await copyText(value)
    copiedValue.value = value
    clearTimeout(copiedTimer)
    copiedTimer = setTimeout(() => { copiedValue.value = null }, 1200)
  } catch (error) {
    console.warn('Copy failed', error)
  }
}

const actionClass = 'h-5 w-5 flex items-center justify-center rounded flex-shrink-0 text-muted-foreground hover:bg-background hover:text-foreground'
</script>

<template>
  <Collapsible :open="expanded" @update:open="emit('toggle', field)">
    <div class="rounded-md hover:bg-muted/50 transition-colors">
      <div class="group/header flex items-center pr-1">
      <CollapsibleTrigger class="min-w-0 flex-1">
        <div class="flex items-center gap-2 px-2 py-1.5 cursor-pointer group">
          <ChevronRight
            :class="cn(
              'h-3.5 w-3.5 text-muted-foreground transition-transform flex-shrink-0',
              expanded && 'rotate-90'
            )"
          />
          <component
            :is="getTypeIcon(field.type)"
            :class="cn('h-3.5 w-3.5 flex-shrink-0', getTypeColorClass(field))"
          />
          <span
            class="text-sm font-medium text-foreground truncate flex-1 text-left"
            :title="field.name"
          >
            {{ field.name }}
          </span>
          <Loader2
            v-if="status === 'loading'"
            class="h-3 w-3 text-muted-foreground animate-spin flex-shrink-0"
          />
          <Badge
            v-else-if="!expanded && values?.total_distinct"
            variant="secondary"
            class="text-[9px] h-4 px-1.5 font-normal tabular-nums flex-shrink-0"
            :title="t('pages.uniqueValueCount', { count: values.total_distinct })"
          >
            {{ values.total_distinct }}
          </Badge>
          <Badge
            variant="outline"
            class="text-[9px] h-4 px-1 font-normal flex-shrink-0 text-muted-foreground"
          >
            {{ getCleanType(field.type) }}
          </Badge>
        </div>
      </CollapsibleTrigger>
      <button
        type="button"
        :class="cn(actionClass, 'focus-visible:opacity-100 transition-opacity', columnShown ? 'text-primary hover:text-primary' : 'opacity-0 group-hover/header:opacity-100')"
        :title="columnShown ? t('explore.removeColumn') : t('explore.addAsColumn')"
        :aria-label="`${columnShown ? t('explore.removeColumn') : t('explore.addAsColumn')}: ${field.name}`"
        :aria-pressed="!!columnShown"
        data-column-toggle
        @click.stop="emit('toggle-column', field.name)"
      >
        <Columns3 class="h-3 w-3" />
      </button>
      </div>

      <CollapsibleContent v-if="expanded">
        <div class="pl-8 pr-2 pb-2">
          <template v-if="status === 'loading'">
            <div class="space-y-1">
              <Skeleton v-for="i in 3" :key="i" class="h-6 w-full" />
            </div>
          </template>

          <p v-else-if="tooManyValues" data-too-many-values class="text-xs text-muted-foreground py-1 px-2">
            {{ values ? t('explore.tooManyValuesCount', { count: values.total_distinct.toLocaleString() }) : t('explore.tooManyValues') }}
          </p>

          <template v-else-if="status === 'error'">
            <div class="flex items-center gap-2 text-xs text-amber-600 dark:text-amber-500 py-1 px-2">
              <AlertTriangle class="h-3.5 w-3.5 flex-shrink-0" />
              <span class="flex-1">{{ t('ui.failedToLoad') }}</span>
              <Button
                variant="ghost"
                size="sm"
                class="h-5 px-2 text-xs"
                @click.stop="emit('load', field)"
              >
                {{ t('ui.retry') }}
              </Button>
            </div>
          </template>

          <template v-else-if="status === 'click-to-load' || status === 'idle'">
            <div class="py-2 px-2">
              <Button
                variant="outline"
                size="sm"
                class="w-full h-7 text-xs"
                @click.stop="emit('load', field)"
              >
                <RefreshCw class="h-3 w-3 mr-1.5" />
                {{ t('ui.loadValues') }}
              </Button>
              <p class="text-[10px] text-muted-foreground mt-1.5 text-center">
                {{ t('ui.mayBeSlowForHighCardinalityFields') }}
              </p>
            </div>
          </template>

          <template v-else-if="values?.values?.length">
            <div class="space-y-0.5">
              <div
                v-for="valueInfo in values.values"
                :key="valueInfo.value"
                class="flex items-center gap-1 group/value"
              >
                <button
                  class="flex-1 flex items-center gap-2 px-2 py-1 rounded text-left hover:bg-primary/10 transition-colors min-w-0"
                  @click="emit('add-filter', field.name, valueInfo.value, '=')"
                  :title="`${field.name}=&quot;${valueInfo.value}&quot;`"
                >
                  <span class="text-xs text-foreground truncate flex-1">
                    {{ valueInfo.value || '(empty)' }}
                  </span>
                  <!-- A Loki label preload reads values from the index, without counts. -->
                  <span v-if="valueInfo.count > 0" class="text-[10px] text-muted-foreground flex-shrink-0">
                    {{ formatCount(valueInfo.count) }}
                  </span>
                </button>

                <span class="flex items-center flex-shrink-0 opacity-60 group-hover/value:opacity-100 focus-within:opacity-100">
                  <button
                    type="button"
                    :class="actionClass"
                    :title="`${t('ui.filterThisValue')}: ${field.name}`"
                    :aria-label="`${t('ui.filterThisValue')}: ${field.name}=&quot;${valueInfo.value}&quot;`"
                    data-value-action="include"
                    @click="emit('add-filter', field.name, valueInfo.value, '=')"
                  >
                    <Plus class="h-3 w-3" />
                  </button>
                  <button
                    type="button"
                    :class="cn(actionClass, 'hover:bg-destructive/20 hover:text-destructive')"
                    :title="`${t('ui.filterThisValue2')}: ${field.name}`"
                    :aria-label="`${t('ui.filterThisValue2')}: ${field.name}!=&quot;${valueInfo.value}&quot;`"
                    data-value-action="exclude"
                    @click="emit('add-filter', field.name, valueInfo.value, '!=')"
                  >
                    <Minus class="h-3 w-3" />
                  </button>
                  <button
                    type="button"
                    :class="actionClass"
                    :title="t('ui.copyValue')"
                    :aria-label="`${t('ui.copyValue')}: ${valueInfo.value}`"
                    data-value-action="copy"
                    @click="copy(valueInfo.value)"
                  >
                    <Check v-if="copiedValue === valueInfo.value" class="h-3 w-3 text-emerald-500" />
                    <Copy v-else class="h-3 w-3" />
                  </button>
                </span>
              </div>

              <div
                v-if="values.total_distinct > values.values.length"
                class="text-[10px] text-muted-foreground px-2 pt-1"
              >
                +{{ values.total_distinct - values.values.length }} {{ t('ui.moreValues') }}
              </div>
            </div>
          </template>

          <template v-else-if="status === 'loaded'">
            <div class="text-xs text-muted-foreground italic py-1 px-2">
              {{ t('ui.noValuesFound') }}
            </div>
          </template>
        </div>
      </CollapsibleContent>
    </div>
  </Collapsible>
</template>
