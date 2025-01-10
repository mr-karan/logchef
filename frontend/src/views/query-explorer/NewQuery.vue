<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
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
} from '@/components/ui/table'
import { Button } from '@/components/ui/button'
import { RefreshCw } from 'lucide-vue-next'

// Configuration panel state
const selectedSource = ref('')
const batchSize = ref('500')
const refreshRate = ref('0') // 0 means off
const sources = ref<Source[]>([])
const isLoadingSources = ref(true)
const { toast } = useToast()

// Load sources from API
const loadSources = async () => {
    try {
        const response = await sourcesApi.listSources()
        const responseData = response.data
        if (isErrorResponse(responseData)) {
            toast({
                title: 'Error',
                description: getErrorMessage(responseData),
                variant: 'destructive',
                duration: TOAST_DURATION.ERROR,
            })
        } else {
            sources.value = responseData.data as Source[]
        }
    } catch (error) {
        console.error('Error loading sources:', error)
        toast({
            title: 'Error',
            description: getErrorMessage(error),
            variant: 'destructive',
            duration: TOAST_DURATION.ERROR,
        })
    } finally {
        isLoadingSources.value = false
    }
}

onMounted(loadSources)

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

                <!-- Time Range (placeholder for now) -->
                <div class="w-[250px] h-[40px] rounded-md border flex items-center px-3 text-muted-foreground">
                    Time Range Coming Soon
                </div>

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

        <!-- Results Table -->
        <div class="flex-1 min-h-0">
            <Table>
                <TableHeader class="sticky top-0 bg-background">
                    <TableRow>
                        <TableHead class="font-mono">Timestamp</TableHead>
                        <TableHead class="w-[80px]">Severity</TableHead>
                        <TableHead class="w-[120px]">Service</TableHead>
                        <TableHead>Message</TableHead>
                    </TableRow>
                </TableHeader>
                <TableBody>
                    <TableRow class="hover:bg-muted/30">
                        <TableCell class="font-mono text-sm whitespace-nowrap">2024-01-10 12:34:56</TableCell>
                        <TableCell class="text-sm">INFO</TableCell>
                        <TableCell class="text-sm">api-server</TableCell>
                        <TableCell class="text-sm">Sample log message for development that could be quite long and
                            contain a lot of useful information that we want to display in a readable way</TableCell>
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
    font-size: 0.875rem;
    background-color: hsl(var(--background));
}

:deep(td) {
    padding-top: 0.5rem !important;
    padding-bottom: 0.5rem !important;
    white-space: pre-wrap;
    word-break: break-word;
}
</style>
