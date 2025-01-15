<script setup lang="ts">
import { ref, computed, onMounted, watch } from 'vue'
import { sourcesApi, type Source } from '@/api/sources'
import { useToast } from '@/components/ui/toast/use-toast'
import { TOAST_DURATION } from '@/lib/constants'
import { isErrorResponse, getErrorMessage } from '@/api/types'
import { Card, CardContent } from '@/components/ui/card'
import {
    Select,
    SelectContent,
    SelectItem,
    SelectTrigger,
    SelectValue,
} from '@/components/ui/select'
import {
    Table,
    TableBody,
    TableCell,
    TableHead,
    TableHeader,
    TableRow,
    TableCaption,
} from '@/components/ui/table'
import { Button } from '@/components/ui/button'
import { RefreshCw } from 'lucide-vue-next'
import { Calendar } from '@/components/ui/v-calendar'
import {
    Popover,
    PopoverContent,
    PopoverTrigger,
} from '@/components/ui/popover'
import { CalendarIcon } from '@radix-icons/vue'
import { format } from 'date-fns'
import { cn } from '@/lib/utils'
import { exploreApi } from '@/api/explore'
import { CommandEmpty, CommandGroup, CommandItem, CommandList } from '@/components/ui/command'
import { TagsInput, TagsInputInput, TagsInputItem, TagsInputItemDelete, TagsInputItemText } from '@/components/ui/tags-input'
import { ComboboxAnchor, ComboboxContent, ComboboxInput, ComboboxPortal, ComboboxRoot } from 'radix-vue'
import { logsApi } from '@/api/logs'
import type { Log } from '@/api/logs'

interface Column {
    name: string
    type: string
}

// Configuration panel state
const selectedSource = ref('')
const batchSize = ref('500')
const refreshRate = ref('0') // 0 means off
const sources = ref<Source[]>([])
const isLoadingSources = ref(true)
const columns = ref<Column[]>([])
const isLoadingColumns = ref(false)
const selectedColumns = ref<string[]>([]) 
const columnSelectorOpen = ref(false)
const columnSearchTerm = ref('')

const handleColumnUpdate = (event: any[]) => {
    selectedColumns.value = event.map(String)
}

// Logs state
const logs = ref<Log[]>([])
const isLoadingLogs = ref(false)
const logsError = ref<string | null>(null)

// Date range state
const dateRange = ref({
    start: new Date(),
    end: new Date(),
})

// Load sources from API
const loadSources = async () => {
    try {
        isLoadingSources.value = true
        const { data } = await sourcesApi.listSources()
        if (isErrorResponse(data)) {
            throw new Error(data.data.error)
        }
        sources.value = data.data as Source[]

        // Auto select first source
        if (sources.value.length > 0) {
            selectedSource.value = sources.value[0].id
        }
    } catch (error) {
        toast({
            title: 'Error',
            description: getErrorMessage(error),
            duration: TOAST_DURATION.ERROR,
        })
    } finally {
        isLoadingSources.value = false
    }
}

// Fetch columns when source changes
watch(selectedSource, async (newSource) => {
  if (!newSource) return
  
  isLoadingColumns.value = true
  columns.value = []
  
  try {
    const fetchedColumns = await exploreApi.exploreSource(newSource)
    columns.value = fetchedColumns
    selectedColumns.value = fetchedColumns.map(col => col.name)
  } catch (error) {
    console.error('Failed to fetch columns:', error)
  } finally {
    isLoadingColumns.value = false
  }
}, { immediate: true })

// Fetch logs when source changes
watch(selectedSource, async (newSource) => {
  if (!newSource) return
  
  isLoadingLogs.value = true
  logsError.value = null
  
  try {
    const response = await logsApi.queryLogs({
      source: newSource,
      limit: 100
    })
    if (response.status === 'success') {
      logs.value = response.data as Log[]
    } else {
      logsError.value = 'Failed to fetch logs'
      logs.value = []
    }
  } catch (error) {
    logsError.value = error instanceof Error ? error.message : 'Failed to fetch logs'
    logs.value = []
  } finally {
    isLoadingLogs.value = false
  }
}, { immediate: true })

// Filtered columns for combobox
const filteredColumns = computed(() => {
    if (!columnSearchTerm.value.trim()) return columns.value
    return columns.value.filter(col => 
        col.name.toLowerCase().includes(columnSearchTerm.value.toLowerCase().trim())
    )
})

// Available options
const batchSizeOptions = [
    { value: '100', label: '100 logs' },
    { value: '500', label: '500 logs' },
    { value: '1000', label: '1000 logs' },
    { value: '5000', label: '5000 logs' },
]

const refreshRateOptions = [
    { value: '0', label: 'Off' },
    { value: '5', label: '5s' },
    { value: '10', label: '10s' },
    { value: '30', label: '30s' },
    { value: '60', label: '1m' },
    { value: '300', label: '5m' },
]

// Computed values for actual use
const currentBatchSize = computed(() => parseInt(batchSize.value, 10))
const currentRefreshRate = computed(() => parseInt(refreshRate.value, 10))

const { toast } = useToast()

onMounted(loadSources)
</script>

<template>
    <div class="h-full flex flex-col">
        <!-- Config panel -->
        <div class="border-b bg-background/95 backdrop-blur supports-[backdrop-filter]:bg-background/60">
            <div class="py-3 flex items-center space-x-3">
                <!-- Source Selector -->
                <Select v-model="selectedSource">
                    <SelectTrigger class="w-[180px]">
                        <SelectValue :placeholder="isLoadingSources ? 'Loading...' : 'Select source'" />
                    </SelectTrigger>
                    <SelectContent>
                        <SelectItem v-for="source in sources" :key="source.id" :value="source.id">
                            {{ source.table_name }}
                        </SelectItem>
                    </SelectContent>
                </Select>

                <!-- Column Selector -->
                <TagsInput 
                    class="px-0 gap-0 w-[300px]" 
                    :model-value="selectedColumns" 
                    @update:modelValue="handleColumnUpdate">
                    <div class="flex gap-2 items-center px-3 h-9 overflow-hidden">
                        <TagsInputItem v-for="(column, index) in selectedColumns.slice(0, 2)" :key="column" :value="column" class="shrink-0">
                            <TagsInputItemText />
                            <TagsInputItemDelete />
                        </TagsInputItem>
                        <span v-if="selectedColumns.length > 2" class="inline-flex items-center justify-center px-2 py-0.5 text-xs font-medium bg-muted text-muted-foreground rounded-full shrink-0">
                            +{{ selectedColumns.length - 2 }}
                        </span>
                    </div>

                    <ComboboxRoot v-model="selectedColumns" v-model:open="columnSelectorOpen" v-model:search-term="columnSearchTerm" class="w-full">
                        <ComboboxAnchor as-child>
                            <ComboboxInput :placeholder="isLoadingColumns ? 'Loading columns...' : 'Select columns...'" as-child>
                                <TagsInputInput 
                                    class="w-full px-3" 
                                    :class="selectedColumns.length > 0 ? 'mt-2' : ''" 
                                    @keydown.enter.prevent
                                    @focus="columnSelectorOpen = true" 
                                />
                            </ComboboxInput>
                        </ComboboxAnchor>

                        <ComboboxPortal>
                            <ComboboxContent>
                                <CommandList position="popper"
                                    class="w-[--radix-popper-anchor-width] rounded-md mt-2 border bg-popover text-popover-foreground shadow-md outline-none">
                                    <CommandEmpty v-if="!isLoadingColumns && filteredColumns.length === 0" class="p-2 text-sm text-muted-foreground">
                                        No columns found.
                                    </CommandEmpty>
                                    <CommandGroup v-else>
                                        <CommandItem v-for="column in filteredColumns" :key="column.name" :value="column.name"
                                            class="relative flex items-center text-sm px-8 py-1.5 cursor-pointer outline-none transition-colors hover:bg-accent hover:text-accent-foreground data-[selected]:bg-accent/50"
                                            :data-selected="selectedColumns.includes(column.name)"
                                            @select.prevent="(ev) => {
                                                if (typeof ev.detail.value === 'string') {
                                                    columnSearchTerm = ''
                                                    if (selectedColumns.includes(ev.detail.value)) {
                                                        selectedColumns.splice(selectedColumns.indexOf(ev.detail.value), 1)
                                                    } else {
                                                        selectedColumns.push(ev.detail.value)
                                                    }
                                                }
                                            }">
                                            <span class="absolute left-2 w-4 inline-flex items-center justify-center opacity-70" v-if="selectedColumns.includes(column.name)">
                                                ✓
                                            </span>
                                            <span class="truncate">{{ column.name }}</span>
                                            <span class="ml-2 text-xs text-muted-foreground">{{ column.type }}</span>
                                        </CommandItem>
                                    </CommandGroup>
                                </CommandList>
                            </ComboboxContent>
                        </ComboboxPortal>
                    </ComboboxRoot>
                </TagsInput>

                <!-- Time Range Picker -->
                <Popover>
                    <PopoverTrigger as-child>
                        <Button id="date" :variant="'outline'" :class="cn(
                            'w-[330px] justify-start text-left font-normal',
                            !dateRange && 'text-muted-foreground',
                        )">
                            <CalendarIcon class="mr-2 h-4 w-4" />
                            <span>
                                {{ dateRange.start ? (
                                    dateRange.end ? `${format(dateRange.start, 'LLL dd, y HH:mm')} - ${format(dateRange.end,
                                        'LLL dd, y HH:mm')}`
                                        : format(dateRange.start, 'LLL dd, y HH:mm')
                                ) : 'Pick a date range' }}
                            </span>
                        </Button>
                    </PopoverTrigger>
                    <PopoverContent class="w-auto p-0" align="start">
                        <Calendar v-model.range="dateRange" mode="date" :columns="2">
                            <template #footer>
                                <div class="flex w-full mt-6 border-t border-accent pt-4">
                                    <div class="w-1/2">
                                        <strong>Entry time</strong>
                                        <Calendar v-model="dateRange.start" mode="time" hide-time-header />
                                    </div>
                                    <div class="w-1/2">
                                        <strong>Exit time</strong>
                                        <Calendar v-model="dateRange.end" mode="time" hide-time-header />
                                    </div>
                                </div>
                            </template>
                        </Calendar>
                    </PopoverContent>
                </Popover>

                <!-- Batch Size -->
                <Select v-model="batchSize">
                    <SelectTrigger class="w-[120px]">
                        <SelectValue placeholder="Batch size" />
                    </SelectTrigger>
                    <SelectContent>
                        <SelectItem v-for="option in batchSizeOptions" :key="option.value" :value="option.value">
                            {{ option.label }}
                        </SelectItem>
                    </SelectContent>
                </Select>

                <!-- Refresh Rate -->
                <Select v-model="refreshRate">
                    <SelectTrigger class="w-[100px]">
                        <SelectValue placeholder="Refresh" />
                    </SelectTrigger>
                    <SelectContent>
                        <SelectItem v-for="option in refreshRateOptions" :key="option.value" :value="option.value">
                            {{ option.label }}
                        </SelectItem>
                    </SelectContent>
                </Select>

                <!-- Manual Refresh -->
                <Button variant="outline" size="icon" class="shrink-0">
                    <RefreshCw class="h-4 w-4" />
                </Button>
            </div>
        </div>

        <!-- Content -->
        <div class="flex-1 min-h-0">
            <Table>
                <TableCaption v-if="!selectedSource">Select a source to view logs</TableCaption>
                <TableCaption v-else-if="isLoadingLogs">Loading logs...</TableCaption>
                <TableCaption v-else-if="logsError">{{ logsError }}</TableCaption>
                <TableCaption v-else-if="logs.length === 0">No logs found</TableCaption>
                <TableCaption v-else>Showing {{ logs.length }} log entries</TableCaption>

                <TableHeader>
                    <TableRow>
                        <TableHead class="w-[180px]">Timestamp</TableHead>
                        <TableHead class="w-[80px]">Level</TableHead>
                        <TableHead>Message</TableHead>
                        <TableHead v-for="field in selectedColumns" :key="field" class="text-right">
                            {{ field }}
                        </TableHead>
                    </TableRow>
                </TableHeader>

                <TableBody>
                    <TableRow v-for="log in logs" :key="log._timestamp + log.msg" :class="{
                        'bg-red-50 dark:bg-red-900/20': log.level?.toLowerCase() === 'error',
                        'bg-yellow-50 dark:bg-yellow-900/20': log.level?.toLowerCase() === 'warn',
                        'bg-blue-50 dark:bg-blue-900/20': log.level?.toLowerCase() === 'debug'
                    }">
                        <TableCell class="font-mono text-sm">
                            {{ new Date(log._timestamp).toLocaleString() }}
                        </TableCell>
                        <TableCell>
                            <span :class="{
                                'text-red-600 dark:text-red-400': log.level?.toLowerCase() === 'error',
                                'text-yellow-600 dark:text-yellow-400': log.level?.toLowerCase() === 'warn',
                                'text-blue-600 dark:text-blue-400': log.level?.toLowerCase() === 'debug',
                                'text-gray-600 dark:text-gray-400': !log.level
                            }">
                                {{ log.level || 'info' }}
                            </span>
                        </TableCell>
                        <TableCell class="font-mono text-sm whitespace-pre-wrap">
                            {{ log.msg }}
                        </TableCell>
                        <TableCell v-for="field in selectedColumns" :key="field" class="text-right font-mono text-sm">
                            {{ log[field] }}
                        </TableCell>
                    </TableRow>
                </TableBody>
            </Table>
        </div>
    </div>
</template>

<style scoped>
:deep(th) {
    padding-top: 0.75rem !important;
    padding-bottom: 0.75rem !important;
}

:deep(td) {
    padding-top: 0.5rem !important;
    padding-bottom: 0.5rem !important;
}

:deep(.v-calendar) {
    --vc-font-family: inherit;
    --vc-rounded: 0.5rem;
    --vc-header-padding: 1rem;
    --vc-weekday-color: hsl(var(--muted-foreground));
    --vc-weekday-font-weight: 500;
    --vc-weekday-padding: 0.5rem;
    --vc-day-content-font-size: 0.875rem;
    --vc-day-content-width: 2rem;
    --vc-day-content-height: 2rem;
}
</style>
