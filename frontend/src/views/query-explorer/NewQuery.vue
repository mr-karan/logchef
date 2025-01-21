<script setup lang="ts">
import { ref, computed, watch, onMounted } from 'vue'
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
import { CommandEmpty, CommandGroup, CommandItem, CommandList } from '@/components/ui/command'
import { TagsInputInput, TagsInputItem, TagsInputItemDelete, TagsInputItemText, TagsInputRoot } from 'radix-vue'
import { ComboboxAnchor, ComboboxContent, ComboboxInput, ComboboxPortal, ComboboxRoot, ComboboxTrigger } from 'radix-vue'
import { logsApi } from '@/api/logs'
import type { Log, LogField } from '@/api/logs'
import { X, Check, ChevronDown } from 'lucide-vue-next'
import MultiSelect from '@/components/ui/MultiSelect.vue'

// Configuration panel state
const selectedSource = ref('')
const batchSize = ref('500')
const refreshRate = ref('0') // 0 means off
const sources = ref<Source[]>([])
const isLoadingSources = ref(true)
const fields = ref<LogField[]>([])
const isLoadingFields = ref(false)
const selectedFields = ref<string[]>([])
const fieldSelectorOpen = ref(false)
const fieldSearchTerm = ref('')

// Watch for field changes to clear search
watch(selectedFields, () => {
    fieldSearchTerm.value = ''
}, { deep: true })

// Handle field updates
const handleFieldUpdate = (value: string[]) => {
    selectedFields.value = value
}

// Handle field deletion
const handleFieldDelete = (fieldToDelete: string) => {
    selectedFields.value = selectedFields.value.filter(field => field !== fieldToDelete)
}

// Filtered fields for combobox
const filteredFields = computed(() => {
    if (!fieldSearchTerm.value.trim()) {
        return fields.value
    }
    return fields.value.filter(field =>
        field.name.toLowerCase().includes(fieldSearchTerm.value.toLowerCase().trim())
    )
})

// Handle field toggle
const handleFieldToggle = (fieldName: string) => {
    const currentFields = [...selectedFields.value]
    const index = currentFields.indexOf(fieldName)
    if (index === -1) {
        selectedFields.value = [...currentFields, fieldName]
    } else {
        currentFields.splice(index, 1)
        selectedFields.value = currentFields
    }
    fieldSearchTerm.value = ''
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

// Fetch fields when source changes
watch(selectedSource, async (newSource) => {
    if (!newSource) return

    isLoadingFields.value = true
    fields.value = []

    try {
        const response = await logsApi.exploreSource(newSource)
        if (response.status === 'success') {
            fields.value = response.data
            selectedFields.value = fields.value.map(field => field.name)
        }
    } catch (error) {
        console.error('Failed to fetch fields:', error)
        toast({
            title: 'Error',
            description: 'Failed to fetch fields',
            duration: TOAST_DURATION.ERROR,
        })
    } finally {
        isLoadingFields.value = false
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
            limit: parseInt(batchSize.value)
        })
        if (response.status === 'success') {
            logs.value = response.data.logs
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

// Computed values for table
const visibleFields = computed(() =>
    fields.value.filter(field => selectedFields.value.includes(field.name))
)

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

                <!-- Field Selector -->
                <MultiSelect
                    v-model="selectedFields"
                    :options="fields"
                    title="Select Fields"
                    placeholder="Select fields..."
                    search-placeholder="Search fields..."
                />

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
                    <SelectTrigger class="w-[120px]">
                        <SelectValue placeholder="Refresh rate" />
                    </SelectTrigger>
                    <SelectContent>
                        <SelectItem v-for="option in refreshRateOptions" :key="option.value" :value="option.value">
                            {{ option.label }}
                        </SelectItem>
                    </SelectContent>
                </Select>

                <!-- Time Range Picker -->
                <Popover>
                    <PopoverTrigger as-child>
                        <Button id="date" :variant="'outline'" :class="cn(
                            'w-[330px] justify-start text-left font-normal',
                            !dateRange && 'text-muted-foreground'
                        )">
                            <CalendarIcon class="mr-2 h-4 w-4" />
                            <span>
                                {{ dateRange?.start ? format(dateRange.start, 'LLL dd, y') : 'Pick a date' }}
                                {{ dateRange?.end ? ' - ' + format(dateRange.end, 'LLL dd, y') : '' }}
                            </span>
                        </Button>
                    </PopoverTrigger>
                    <PopoverContent class="w-auto p-0" align="start">
                        <Calendar v-model="dateRange" mode="range" />
                    </PopoverContent>
                </Popover>

                <!-- Refresh Button -->
                <Button variant="outline" size="icon" @click="loadSources">
                    <RefreshCw class="h-4 w-4" />
                </Button>
            </div>
        </div>

        <!-- Content -->
        <div class="flex-1 min-h-0">
            <Table>
                <TableHeader>
                    <TableRow>
                        <TableHead v-for="field in visibleFields" :key="field.name">
                            {{ field.name }}
                            <span v-if="field.description" class="ml-1 text-xs text-muted-foreground"
                                :title="field.description">ℹ️</span>
                        </TableHead>
                    </TableRow>
                </TableHeader>
                <TableBody>
                    <TableRow v-for="log in logs" :key="log.id">
                        <TableCell v-for="field in visibleFields" :key="field.name">
                            {{ log[field.name] }}
                        </TableCell>
                    </TableRow>
                    <TableRow v-if="logs.length === 0">
                        <TableCell :colspan="visibleFields.length" class="text-center">
                            {{ isLoadingLogs ? 'Loading logs...' : (logsError || 'No logs found') }}
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
    font-weight: 600 !important;
    text-align: left !important;
    vertical-align: bottom !important;
    line-height: 1.25 !important;
}

:deep(td) {
    padding-top: 0.75rem !important;
    padding-bottom: 0.75rem !important;
    line-height: 1.25 !important;
}

.table-container {
    overflow-x: auto;
    max-width: 100%;
}

.table-wrapper {
    min-width: 100%;
    width: max-content;
}
</style>
