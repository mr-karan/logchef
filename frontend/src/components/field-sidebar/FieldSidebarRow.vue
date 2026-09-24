<script setup lang="ts">
// One filterable field in the Fields sidebar. It is its own component so a
// value load re-renders only this row: the sidebar passes the field's state
// object, which keeps its identity while other fields change.
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Skeleton } from '@/components/ui/skeleton'
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@/components/ui/collapsible'
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '@/components/ui/tooltip'
import { AlertTriangle, ChevronRight, Loader2, Minus, RefreshCw } from 'lucide-vue-next'
import { cn } from '@/lib/utils'
import type { FieldLoadingState } from '@/composables/useFieldValuesLoader'
import { formatCount, getCleanType, getTypeColorClass, getTypeIcon, type FieldInfo } from './fieldDisplay'

const props = defineProps<{
  field: FieldInfo
  // Undefined until the loader has seen the field; treated as idle.
  state: FieldLoadingState | undefined
  expanded: boolean
}>()

const emit = defineEmits<{
  (e: 'toggle', field: FieldInfo): void
  (e: 'load', field: FieldInfo): void
  (e: 'add-filter', field: string, value: string, operator: '=' | '!='): void
}>()

const { t } = useI18n()

const status = computed(() => props.state?.status ?? 'idle')
const values = computed(() => props.state?.values)
</script>

<template>
  <Collapsible :open="expanded" @update:open="emit('toggle', field)">
    <div class="rounded-md hover:bg-muted/50 transition-colors">
      <CollapsibleTrigger class="w-full">
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
            :title="`${values.total_distinct} unique values`"
          >
            {{ values.total_distinct }}
          </Badge>
          <Badge
            v-else-if="status === 'click-to-load'"
            variant="outline"
            class="text-[9px] h-4 px-1 font-normal flex-shrink-0 text-muted-foreground"
          >
            {{ t('ui.click') }}
          </Badge>
          <Badge
            variant="outline"
            class="text-[9px] h-4 px-1 font-normal flex-shrink-0 opacity-0 group-hover:opacity-100 transition-opacity"
          >
            {{ getCleanType(field.type) }}
          </Badge>
        </div>
      </CollapsibleTrigger>

      <CollapsibleContent v-if="expanded">
        <div class="pl-8 pr-2 pb-2">
          <template v-if="status === 'loading'">
            <div class="space-y-1">
              <Skeleton v-for="i in 3" :key="i" class="h-6 w-full" />
            </div>
          </template>

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
                  <span class="text-[10px] text-muted-foreground flex-shrink-0">
                    {{ formatCount(valueInfo.count) }}
                  </span>
                </button>

                <TooltipProvider>
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <button
                        class="h-5 w-5 flex items-center justify-center rounded opacity-0 group-hover/value:opacity-100 hover:bg-destructive/20 text-muted-foreground hover:text-destructive transition-all"
                        @click="emit('add-filter', field.name, valueInfo.value, '!=')"
                      >
                        <Minus class="h-3 w-3" />
                      </button>
                    </TooltipTrigger>
                    <TooltipContent side="right" class="text-xs">
                      <p>{{ t('ui.exclude') }} {{ field.name }}!="{{ valueInfo.value }}"</p>
                    </TooltipContent>
                  </Tooltip>
                </TooltipProvider>
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
