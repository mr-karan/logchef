<script setup lang="ts">
import { ref, watch } from 'vue'
import { storeToRefs } from 'pinia'
import { Settings, Type, ChevronDown, X, Plus, List, CalendarIcon } from 'lucide-vue-next'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Checkbox } from '@/components/ui/checkbox'
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { SingleDatePicker } from '@/components/date-time-picker'
import { useVariableStore, type VariableState } from '@/stores/variables'
import { hasVariableValue, inputTypeFor, formatVariableValue } from './variableUtils'

interface VariableConfigSheetProps {
  open: boolean
}

defineProps<VariableConfigSheetProps>()

const emit = defineEmits<{
  (e: 'update:open', value: boolean): void
}>()

const variableStore = useVariableStore()
const { allVariables } = storeToRefs(variableStore)

const selectedVariable = ref<VariableState | null>(null)

const closeDrawer = () => {
  selectedVariable.value = null
  emit('update:open', false)
}

const updateVariableType = (variable: VariableState) => {
  switch (variable.type) {
    case 'text':
      variable.value = ''
      variable.inputType = 'input'
      break
    case 'number':
      variable.value = 0
      variable.inputType = 'input'
      break
    case 'date': {
      const now = new Date()
      const year = now.getFullYear()
      const month = String(now.getMonth() + 1).padStart(2, '0')
      const day = String(now.getDate()).padStart(2, '0')
      const hours = String(now.getHours()).padStart(2, '0')
      const minutes = String(now.getMinutes()).padStart(2, '0')
      const seconds = String(now.getSeconds()).padStart(2, '0')
      variable.value = `${year}-${month}-${day} ${hours}:${minutes}:${seconds}`
      variable.inputType = 'datepicker'
      break
    }
  }
  variableStore.upsertVariable(variable)
}

const addDropdownOption = (variable: VariableState) => {
  if (!variable.options) {
    variable.options = []
  }
  variable.options.push({ value: '', label: '' })
  variableStore.upsertVariable(variable)
}

const removeDropdownOption = (variable: VariableState, index: number) => {
  if (variable.options) {
    variable.options.splice(index, 1)
    variableStore.upsertVariable(variable)
  }
}

const toggleDefaultMultiSelectValue = (variable: VariableState, value: string) => {
  const current = Array.isArray(variable.defaultValue) ? [...variable.defaultValue] : []
  const index = current.indexOf(value)
  if (index >= 0) {
    current.splice(index, 1)
  } else {
    current.push(value)
  }
  variable.defaultValue = current
  variableStore.upsertVariable(variable)
}

const isDefaultMultiSelectValueSelected = (variable: VariableState, value: string): boolean => {
  return Array.isArray(variable.defaultValue) && variable.defaultValue.includes(value)
}

const getDefaultMultiSelectCount = (variable: VariableState): number => {
  return Array.isArray(variable.defaultValue) ? variable.defaultValue.length : 0
}

// Watch for changes to selected variable and update the store
watch(
  () => selectedVariable.value,
  (newVariable) => {
    if (newVariable) {
      variableStore.upsertVariable(newVariable)
    }
  },
  { deep: true }
)
</script>

<template>
  <Sheet :open="open" @update:open="(val) => !val && closeDrawer()">
    <SheetContent class="w-[480px] max-w-[90vw]">
      <SheetHeader class="pb-6">
        <SheetTitle class="text-lg flex items-center gap-2">
          <div class="w-2 h-2 bg-primary rounded-full"></div>
          <Settings class="h-5 w-5" />
          Variable Configuration
        </SheetTitle>
        <SheetDescription class="text-sm">
          Configure variables used in your query. Variables are replaced with actual values when the query runs.
        </SheetDescription>
      </SheetHeader>

      <div v-if="allVariables && allVariables.length > 0" class="space-y-6 overflow-y-auto pr-2" style="max-height: calc(100vh - 180px);">
        <div v-for="(variable, index) in allVariables" :key="variable.name" class="space-y-4">
          <!-- Enhanced Variable Card -->
          <div class="border border-border rounded-lg p-4 bg-card hover:shadow-sm transition-all duration-200">
            <!-- Variable Header -->
            <div class="flex items-center justify-between mb-4">
              <div class="flex items-center gap-3">
                <div class="w-2 h-2 bg-primary/60 rounded-full flex-shrink-0"></div>
                <div>
                  <h4 class="font-medium text-foreground">{{ variable.name }}</h4>
                  <p class="text-xs text-muted-foreground">Variable {{ index + 1 }} of {{ allVariables.length }}</p>
                </div>
              </div>
              <div class="flex items-center gap-2">
                <span class="text-xs px-2 py-1 bg-muted text-muted-foreground rounded font-mono">
                  {{ variable.type }}
                </span>
              </div>
            </div>

            <!-- Variable Configuration -->
            <div class="space-y-4">
              <!-- Variable Type -->
              <div class="space-y-2">
                <Label class="text-sm font-medium flex items-center gap-2">
                  <div class="w-1 h-1 bg-muted-foreground/40 rounded-full"></div>
                  Data Type
                </Label>
                <Select v-model="variable.type" @update:model-value="() => updateVariableType(variable)">
                  <SelectTrigger class="h-9">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="text">
                      <div class="flex items-center gap-2">
                        <div class="w-2 h-2 bg-blue-500 rounded-full"></div>
                        Text
                      </div>
                    </SelectItem>
                    <SelectItem value="number">
                      <div class="flex items-center gap-2">
                        <div class="w-2 h-2 bg-green-500 rounded-full"></div>
                        Number
                      </div>
                    </SelectItem>
                    <SelectItem value="date">
                      <div class="flex items-center gap-2">
                        <div class="w-2 h-2 bg-purple-500 rounded-full"></div>
                        Date
                      </div>
                    </SelectItem>
                  </SelectContent>
                </Select>
              </div>

              <!-- Widget Type -->
              <div class="space-y-2">
                <Label class="text-sm font-medium flex items-center gap-2">
                  <div class="w-1 h-1 bg-muted-foreground/40 rounded-full"></div>
                  Input Widget
                </Label>
                <Select
                  v-model="variable.inputType"
                  @update:model-value="() => variableStore.upsertVariable(variable)"
                  :disabled="variable.type === 'date'"
                >
                  <SelectTrigger class="h-9">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem v-if="variable.type !== 'date'" value="input">
                      <div class="flex items-center gap-2">
                        <Type class="w-3.5 h-3.5 text-muted-foreground" />
                        Text Input
                      </div>
                    </SelectItem>
                    <SelectItem v-if="variable.type === 'date'" value="datepicker">
                      <div class="flex items-center gap-2">
                        <CalendarIcon class="w-3.5 h-3.5 text-muted-foreground" />
                        Date Picker
                      </div>
                    </SelectItem>
                    <SelectItem v-if="variable.type !== 'date'" value="dropdown">
                      <div class="flex items-center gap-2">
                        <ChevronDown class="w-3.5 h-3.5 text-muted-foreground" />
                        Dropdown List
                      </div>
                    </SelectItem>
                    <SelectItem v-if="variable.type !== 'date'" value="multiselect">
                      <div class="flex items-center gap-2">
                        <List class="w-3.5 h-3.5 text-muted-foreground" />
                        Multi-Select
                      </div>
                    </SelectItem>
                  </SelectContent>
                </Select>
              </div>

              <!-- Dropdown/Multi-Select Options -->
              <div v-if="variable.inputType === 'dropdown' || variable.inputType === 'multiselect'" class="space-y-3">
                <Label class="text-sm font-medium flex items-center gap-2">
                  <div class="w-1 h-1 bg-muted-foreground/40 rounded-full"></div>
                  {{ variable.inputType === 'multiselect' ? 'Multi-Select Options' : 'Dropdown Options' }}
                </Label>

                <div class="bg-muted/30 rounded-md p-3 border border-border/50 space-y-3">
                  <!-- Header -->
                  <div v-if="variable.options?.length" class="grid grid-cols-[1fr_1fr_32px] gap-2 px-1">
                    <span class="text-[10px] font-medium text-muted-foreground uppercase tracking-wider">Value</span>
                    <span class="text-[10px] font-medium text-muted-foreground uppercase tracking-wider">Label</span>
                    <span></span>
                  </div>

                  <!-- Options List -->
                  <div class="space-y-2">
                    <div v-for="(opt, optIndex) in (variable.options || [])" :key="optIndex"
                      class="grid grid-cols-[1fr_1fr_auto] gap-2 items-center group">
                      <Input v-model="opt.value" placeholder="Value" class="h-8 text-xs bg-background"
                        @input="() => variableStore.upsertVariable(variable)" />
                      <Input v-model="opt.label" placeholder="Label" class="h-8 text-xs bg-background"
                        @input="() => variableStore.upsertVariable(variable)" />
                      <Button variant="ghost" size="icon"
                        class="h-8 w-8 text-muted-foreground hover:text-destructive hover:bg-destructive/10"
                        @click="removeDropdownOption(variable, optIndex)">
                        <X class="h-4 w-4" />
                      </Button>
                    </div>
                  </div>

                  <Button variant="outline" size="sm"
                    class="w-full h-8 text-xs border-dashed text-muted-foreground hover:text-foreground bg-transparent hover:bg-muted/50"
                    @click="addDropdownOption(variable)">
                    <Plus class="h-3.5 w-3.5 mr-1" />
                    Add Option
                  </Button>
                </div>

                <p class="text-xs text-muted-foreground">
                  Enter values users can select from. Label is optional display text.
                </p>
              </div>

              <!-- Display Label -->
              <div class="space-y-2">
                <Label class="text-sm font-medium flex items-center gap-2">
                  <div class="w-1 h-1 bg-muted-foreground/40 rounded-full"></div>
                  Display Label
                </Label>
                <Input v-model="variable.label" placeholder="Enter display name..." class="h-9"
                  @input="() => variableStore.upsertVariable(variable)" />
              </div>

              <!-- Default Value -->
              <div class="space-y-2">
                <Label class="text-sm font-medium flex items-center gap-2">
                  <div class="w-1 h-1 bg-muted-foreground/40 rounded-full"></div>
                  Default Value{{ variable.inputType === 'multiselect' ? 's' : '' }}
                </Label>
                <!-- Multi-select default values -->
                <div v-if="variable.inputType === 'multiselect' && variable.options?.length" class="space-y-2">
                  <div class="bg-muted/30 rounded-md p-2 border border-border/50 max-h-[120px] overflow-y-auto">
                    <div v-for="opt in variable.options" :key="opt.value"
                      class="flex items-center gap-2 px-2 py-1 rounded-sm hover:bg-muted cursor-pointer"
                      @click="toggleDefaultMultiSelectValue(variable, opt.value)">
                      <Checkbox
                        :checked="isDefaultMultiSelectValueSelected(variable, opt.value)"
                        @update:checked="toggleDefaultMultiSelectValue(variable, opt.value)"
                        class="h-3.5 w-3.5"
                      />
                      <span class="text-xs">{{ opt.label || opt.value }}</span>
                    </div>
                  </div>
                  <p class="text-xs text-muted-foreground">
                    Select default values for multi-select. {{ getDefaultMultiSelectCount(variable) }} selected.
                  </p>
                </div>
                <!-- Single-select dropdown default -->
                <Select v-else-if="variable.inputType === 'dropdown' && variable.options?.length"
                  :model-value="String(variable.defaultValue ?? '')"
                  @update:model-value="(val) => { variable.defaultValue = val; variableStore.upsertVariable(variable); }">
                  <SelectTrigger class="h-9">
                    <SelectValue placeholder="No default" />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="">
                      <span class="text-muted-foreground italic">No default</span>
                    </SelectItem>
                    <SelectItem v-for="opt in variable.options" :key="opt.value" :value="opt.value">
                      {{ opt.label || opt.value }}
                    </SelectItem>
                  </SelectContent>
                </Select>
                <!-- Date picker default -->
                <SingleDatePicker
                  v-else-if="variable.type === 'date' && variable.inputType === 'datepicker'"
                  :model-value="variable.defaultValue ? String(variable.defaultValue) : null"
                  @update:model-value="(val) => { variable.defaultValue = val ?? ''; variableStore.upsertVariable(variable); }"
                  :include-time="true"
                  placeholder="Select default date..."
                  class="w-full"
                />
                <!-- Text/number input default -->
                <Input v-else
                  :model-value="String(variable.defaultValue ?? '')"
                  @update:model-value="(val: string | number) => { variable.defaultValue = String(val); variableStore.upsertVariable(variable); }"
                  :type="inputTypeFor(variable.type)"
                  :placeholder="'Default ' + variable.type + ' value'"
                  class="h-9" />
                <p v-if="variable.inputType !== 'multiselect'" class="text-xs text-muted-foreground">
                  Pre-filled when loading the query. Leave empty for no default.
                </p>
              </div>

              <!-- Current Value Preview -->
              <div class="space-y-2">
                <Label class="text-sm font-medium flex items-center gap-2">
                  <div class="w-1 h-1 bg-muted-foreground/40 rounded-full"></div>
                  Current Value
                </Label>
                <div class="px-3 py-2 bg-muted/30 rounded-md border text-sm font-mono min-h-[36px] flex items-center">
                  <span v-if="hasVariableValue(variable)" class="text-foreground">
                    {{ formatVariableValue(variable) }}
                  </span>
                  <span v-else class="text-muted-foreground italic">
                    No value set
                  </span>
                </div>
              </div>
            </div>
          </div>
        </div>
      </div>

      <!-- Enhanced Empty State -->
      <div v-else class="text-center py-12 text-muted-foreground">
        <div class="w-12 h-12 bg-muted/50 rounded-full flex items-center justify-center mx-auto mb-4">
          <Settings class="h-6 w-6 opacity-50" />
        </div>
        <p class="text-sm font-medium mb-2">No variables found in your query</p>
        <p class="text-xs">Use <code class="bg-muted px-1.5 py-0.5 rounded">&#123;&#123;variable_name&#125;&#125;</code>
          syntax to create variables</p>
        <div class="mt-4 text-xs text-muted-foreground/60">
          <p>Example: <code class="bg-muted px-1.5 py-0.5 rounded">namespace=&#123;&#123;env&#125;&#125;</code></p>
        </div>
      </div>
    </SheetContent>
  </Sheet>
</template>
