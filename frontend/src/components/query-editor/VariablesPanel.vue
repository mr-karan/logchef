<script setup lang="ts">
import { storeToRefs } from 'pinia'
import { Settings, ChevronDown } from 'lucide-vue-next'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Checkbox } from '@/components/ui/checkbox'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from '@/components/ui/popover'
import { ScrollArea } from '@/components/ui/scroll-area'
import { SingleDatePicker } from '@/components/date-time-picker'
import { useVariableStore, type VariableState } from '@/stores/variables'
import { hasVariableValue, inputTypeFor, getPlaceholderForType } from './variableUtils'

const emit = defineEmits<{
  (e: 'open-config'): void
}>()

const variableStore = useVariableStore()
const { allVariables } = storeToRefs(variableStore)

// Multi-select helper functions
const getMultiSelectDisplay = (variable: VariableState): string => {
  const values = Array.isArray(variable.value) ? variable.value : []
  if (values.length === 0) return variable.isOptional ? 'Select (optional)' : 'Select...'
  if (values.length === 1) {
    const opt = variable.options?.find(o => o.value === values[0])
    return opt?.label || values[0]
  }
  return `${values.length} selected`
}

const getMultiSelectValues = (variable: VariableState): string[] => {
  return Array.isArray(variable.value) ? variable.value : []
}

const toggleMultiSelectValue = (variable: VariableState, value: string) => {
  const current = Array.isArray(variable.value) ? [...variable.value] : []
  const index = current.indexOf(value)
  if (index >= 0) {
    current.splice(index, 1)
  } else {
    current.push(value)
  }
  variable.value = current
  variableStore.upsertVariable(variable)
}

const isMultiSelectValueSelected = (variable: VariableState, value: string): boolean => {
  return Array.isArray(variable.value) && variable.value.includes(value)
}

const clearMultiSelectValues = (variable: VariableState) => {
  variable.value = []
  variableStore.upsertVariable(variable)
}
</script>

<template>
  <div v-if="allVariables && allVariables.length > 0" class="mb-3">
    <!-- Variables Header -->
    <div class="flex items-center justify-between mb-2 px-1">
      <div class="flex items-center gap-2">
        <div class="w-1 h-3 bg-primary rounded-full"></div>
        <span class="text-xs font-medium text-foreground">Variables</span>
        <span class="text-xs text-muted-foreground bg-muted px-1.5 py-0.5 rounded">
          {{ allVariables.length }}
        </span>
      </div>
      <Button variant="ghost" size="sm" class="h-6 px-2 text-xs" @click="emit('open-config')"
        title="Configure variables">
        <Settings class="h-3 w-3 mr-1" />
        Configure
      </Button>
    </div>

    <!-- Compact Variables List -->
    <div class="bg-muted/20 border border-border/30 rounded-md p-2">
      <div class="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 gap-2">
        <div v-for="variable in allVariables" :key="variable.name" class="flex flex-col gap-1.5 min-w-0">
          <!-- Variable indicator and label -->
          <div class="flex items-center gap-1.5 min-w-0">
            <div class="w-1.5 h-1.5 rounded-full flex-shrink-0" :class="variable.isOptional ? 'bg-muted-foreground/40' : 'bg-primary/60'"></div>
            <Label :for="`var-${variable.name}`"
              class="text-xs font-medium truncate cursor-pointer min-w-0"
              :class="variable.isOptional ? 'text-muted-foreground' : 'text-foreground'"
              :title="(variable.label || variable.name) + (variable.isOptional ? ' (optional)' : '')">
              {{ variable.label || variable.name }}
            </Label>
            <span class="text-xs px-1 py-0.5 bg-muted text-muted-foreground rounded font-mono flex-shrink-0">
              {{ variable.type[0] }}
            </span>
            <span v-if="variable.isOptional" class="text-[10px] px-1 py-0.5 bg-muted/50 text-muted-foreground/70 rounded flex-shrink-0 italic">
              optional
            </span>
          </div>

          <!-- Multi-select dropdown -->
          <Popover v-if="variable.inputType === 'multiselect' && variable.options?.length">
            <PopoverTrigger as-child>
              <Button variant="outline" :id="`var-${variable.name}`"
                class="h-7 text-xs w-full justify-between font-normal transition-colors"
                :class="{
                  'border-primary/30 bg-primary/5': hasVariableValue(variable),
                  'border-dashed border-muted-foreground/20': !hasVariableValue(variable) && !variable.isOptional,
                  'border-dashed border-muted-foreground/10': !hasVariableValue(variable) && variable.isOptional
                }">
                <span class="truncate">
                  {{ getMultiSelectDisplay(variable) }}
                </span>
                <ChevronDown class="h-3 w-3 opacity-50 flex-shrink-0 ml-1" />
              </Button>
            </PopoverTrigger>
            <PopoverContent class="w-[220px] p-2" align="start">
              <ScrollArea class="max-h-[200px]">
                <div class="space-y-1">
                  <div v-for="opt in variable.options" :key="opt.value"
                    class="flex items-center gap-2 px-2 py-1.5 rounded-sm hover:bg-muted cursor-pointer"
                    @click="toggleMultiSelectValue(variable, opt.value)">
                    <Checkbox
                      :checked="isMultiSelectValueSelected(variable, opt.value)"
                      @update:checked="toggleMultiSelectValue(variable, opt.value)"
                      class="h-4 w-4"
                    />
                    <span class="text-xs">{{ opt.label || opt.value }}</span>
                  </div>
                </div>
              </ScrollArea>
              <div v-if="getMultiSelectValues(variable).length > 0" class="border-t mt-2 pt-2">
                <Button variant="ghost" size="sm" class="w-full h-7 text-xs text-muted-foreground"
                  @click="clearMultiSelectValues(variable)">
                  Clear selection
                </Button>
              </div>
            </PopoverContent>
          </Popover>

          <!-- Single-select dropdown -->
          <Select v-else-if="variable.inputType === 'dropdown' && variable.options?.length"
            :model-value="String(variable.value ?? '')"
            @update:model-value="(val) => variable.value = val">
            <SelectTrigger :id="`var-${variable.name}`"
              class="h-7 text-xs w-full transition-colors focus:ring-1 focus:ring-primary/50"
              :class="{
                'border-primary/30 bg-primary/5': hasVariableValue(variable),
                'border-dashed border-muted-foreground/20': !hasVariableValue(variable) && !variable.isOptional,
                'border-dashed border-muted-foreground/10': !hasVariableValue(variable) && variable.isOptional
              }">
              <SelectValue :placeholder="variable.isOptional ? 'Select (optional)' : 'Select...'" class="truncate" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem v-if="variable.isOptional" value="">
                <span class="text-muted-foreground italic">None</span>
              </SelectItem>
              <SelectItem v-for="opt in variable.options" :key="opt.value" :value="opt.value">
                {{ opt.label || opt.value }}
              </SelectItem>
            </SelectContent>
          </Select>

          <!-- Date picker input -->
          <SingleDatePicker
            v-else-if="variable.type === 'date' && variable.inputType === 'datepicker'"
            :model-value="variable.value ? String(variable.value) : null"
            @update:model-value="(val) => { variable.value = val ?? ''; variableStore.upsertVariable(variable); }"
            :include-time="true"
            :placeholder="variable.isOptional ? 'Select date (optional)' : 'Select date...'"
            class="w-full"
          />

          <!-- Text/Number input (default) -->
          <Input v-else :id="`var-${variable.name}`"
            :model-value="String(variable.value ?? '')"
            @update:model-value="(val: string | number) => { variable.value = String(val); variableStore.upsertVariable(variable); }"
            :type="inputTypeFor(variable.type)"
            :placeholder="variable.isOptional ? 'Leave empty to omit' : getPlaceholderForType(variable.type)"
            class="h-7 text-xs w-full focus:border-primary/50 transition-colors"
            :class="{
              'border-primary/30 bg-primary/5': hasVariableValue(variable),
              'border-dashed border-muted-foreground/20': !hasVariableValue(variable) && !variable.isOptional,
              'border-dashed border-muted-foreground/10': !hasVariableValue(variable) && variable.isOptional
            }" />
        </div>
      </div>
    </div>
  </div>
</template>
