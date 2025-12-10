<script setup lang="ts">
import {
    Dialog,
    DialogContent,
    DialogHeader,
    DialogTitle,
} from '@/components/ui/dialog'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { useToast } from '@/composables/useToast'
import { TOAST_DURATION } from '@/lib/constants'
import { ref, watch } from 'vue'
import { exploreApi } from '@/api/explore'
import { Clock, ArrowDown, ArrowUp } from 'lucide-vue-next'

const props = defineProps<{
    isOpen: boolean
    sourceId: string
    teamId: number
    log: Record<string, any>
    timestampField?: string
}>()

const emit = defineEmits<{
    (e: 'update:isOpen', value: boolean): void
}>()

const { toast } = useToast()
const isLoading = ref(false)
const loadingMore = ref<'before' | 'after' | null>(null)
const targetTimestamp = ref<number>(0)
const expandedLogId = ref<string | null>(null)
const batchSize = ref<number>(20)
const batchOptions = [10, 20, 50, 100]
const noMoreBefore = ref(false)
const noMoreAfter = ref(false)
// Track offsets for pagination (how many logs we've already loaded)
const beforeOffset = ref(0)
const afterOffset = ref(0)
const contextLogs = ref<{
    before_logs: Record<string, any>[];
    after_logs: Record<string, any>[];
} | null>(null)

// Toggle expanded state for a log
function toggleExpand(logId: string) {
    expandedLogId.value = expandedLogId.value === logId ? null : logId
}

// Generate a unique ID for a log entry
function getLogId(prefix: string, idx: number, log: Record<string, any>): string {
    return `${prefix}-${idx}-${getTimestamp(log)}`
}

// Check if a log is at the target timestamp (for highlighting)
const isTargetLog = (log: Record<string, any>) => {
    const logTs = new Date(getTimestamp(log)).getTime()
    return logTs === targetTimestamp.value
}

// Get the timestamp field from props or use default
const getTimestamp = (log: Record<string, any>) => {
    const tsField = props.timestampField || 'timestamp'
    return log[tsField]
}

// Load context data when modal opens
async function loadContextLogs() {
    const tsValue = getTimestamp(props.log)
    
    if (!tsValue) {
        toast({
            title: 'Error',
            description: 'No timestamp found in log',
            variant: 'destructive',
            duration: TOAST_DURATION.ERROR,
        })
        return
    }
    
    if (!props.sourceId) {
        toast({
            title: 'Error',
            description: 'Source ID is required',
            variant: 'destructive',
            duration: TOAST_DURATION.ERROR,
        })
        return
    }

    if (!props.teamId) {
        toast({
            title: 'Error',
            description: 'Team ID is required',
            variant: 'destructive',
            duration: TOAST_DURATION.ERROR,
        })
        return
    }

    // Reset state before loading
    contextLogs.value = null
    isLoading.value = true
    noMoreBefore.value = false
    noMoreAfter.value = false
    beforeOffset.value = 0
    afterOffset.value = 0
    
    try {
        const timestamp = new Date(tsValue).getTime()
        targetTimestamp.value = timestamp
        const result = await exploreApi.getLogContext(parseInt(props.sourceId), {
            timestamp,
            before_limit: batchSize.value,
            after_limit: batchSize.value
        }, props.teamId)

        if (result.status === 'error') {
            throw new Error(result.data.error)
        }

        contextLogs.value = {
            before_logs: result.data?.before_logs || [],
            after_logs: result.data?.after_logs || []
        }
    } catch (error) {
        toast({
            title: 'Error',
            description: error instanceof Error ? error.message : 'Failed to load log context',
            variant: 'destructive',
            duration: TOAST_DURATION.ERROR,
        })
        emit('update:isOpen', false)
    } finally {
        isLoading.value = false
    }
}

watch(() => props.isOpen, (open) => {
    if (open) {
        loadContextLogs()
    } else {
        // Reset state when modal closes
        contextLogs.value = null
        targetTimestamp.value = 0
        expandedLogId.value = null
        noMoreBefore.value = false
        noMoreAfter.value = false
        beforeOffset.value = 0
        afterOffset.value = 0
    }
}, { immediate: true })

// Load more context logs using offset-based pagination
async function loadMore(direction: 'before' | 'after') {
    if (!contextLogs.value || !props.sourceId || !props.teamId || !targetTimestamp.value) return

    loadingMore.value = direction
    try {
        // Calculate the new offset based on how many logs we already have
        const currentBeforeOffset = direction === 'before' 
            ? contextLogs.value.before_logs.length 
            : 0
        const currentAfterOffset = direction === 'after' 
            ? contextLogs.value.after_logs.length 
            : 0

        const result = await exploreApi.getLogContext(parseInt(props.sourceId), {
            timestamp: targetTimestamp.value,  // Always use the original target timestamp
            before_limit: direction === 'before' ? batchSize.value : 0,
            after_limit: direction === 'after' ? batchSize.value : 0,
            before_offset: currentBeforeOffset,
            after_offset: currentAfterOffset,
        }, props.teamId)

        if (result.status === 'error') {
            throw new Error(result.data.error)
        }

        // Append new logs to existing context
        if (direction === 'before') {
            const newLogs = result.data?.before_logs || []
            if (newLogs.length > 0) {
                // New before logs are older, prepend them
                contextLogs.value.before_logs = [...newLogs, ...contextLogs.value.before_logs]
            } else {
                noMoreBefore.value = true
            }
        } else {
            const newLogs = result.data?.after_logs || []
            if (newLogs.length > 0) {
                // New after logs are newer, append them
                contextLogs.value.after_logs = [...contextLogs.value.after_logs, ...newLogs]
            } else {
                noMoreAfter.value = true
            }
        }
    } catch (error) {
        toast({
            title: 'Error',
            description: error instanceof Error ? error.message : 'Failed to load more logs',
            variant: 'destructive',
            duration: TOAST_DURATION.ERROR,
        })
    } finally {
        loadingMore.value = null
    }
}

// Helper to format timestamp
function formatTime(timestamp: string | number) {
    return new Date(timestamp).toLocaleString()
}

// Format log as compact single-line logfmt style
function formatLogCompact(log: Record<string, any>): string {
    const tsField = props.timestampField || 'timestamp'
    const skipFields = new Set([tsField, 'trace_id', 'span_id', 'trace_flags'])
    
    // Try to get message/body first
    const messageFields = ['body', 'message', 'msg', 'log', 'text']
    let message = ''
    for (const field of messageFields) {
        if (log[field] && typeof log[field] === 'string') {
            message = log[field]
            skipFields.add(field)
            break
        }
    }
    
    // Build key=value pairs for other important fields
    const pairs: string[] = []
    const priorityFields = ['severity_text', 'service_name', 'namespace']
    
    for (const field of priorityFields) {
        if (log[field] && !skipFields.has(field)) {
            pairs.push(`${field}=${formatValue(log[field])}`)
            skipFields.add(field)
        }
    }
    
    // Add remaining fields (limited)
    let remaining = 3
    for (const [key, value] of Object.entries(log)) {
        if (remaining <= 0) break
        if (skipFields.has(key) || key === 'log_attributes') continue
        pairs.push(`${key}=${formatValue(value)}`)
        remaining--
    }
    
    const pairsStr = pairs.join(' ')
    return message ? `${message} ${pairsStr}` : pairsStr
}

function formatValue(value: any): string {
    if (value === null || value === undefined) return 'null'
    if (typeof value === 'string') {
        if (value.length > 50) return `"${value.substring(0, 47)}..."`
        if (value.includes(' ')) return `"${value}"`
        return value
    }
    if (typeof value === 'object') return '{...}'
    return String(value)
}
</script>

<template>
    <Dialog :open="isOpen" @update:open="emit('update:isOpen', $event)">
        <DialogContent class="max-w-4xl h-[85vh]">
            <DialogHeader>
                <div class="flex items-center justify-between">
                    <DialogTitle class="flex items-center gap-2">
                        <Clock class="h-5 w-5" />
                        Log Context
                    </DialogTitle>
                    <div class="flex items-center gap-2 text-sm">
                        <span class="text-muted-foreground">Batch:</span>
                        <select 
                            v-model="batchSize" 
                            class="h-7 px-2 rounded border bg-background text-xs"
                            @change="loadContextLogs()"
                        >
                            <option v-for="size in batchOptions" :key="size" :value="size">{{ size }}</option>
                        </select>
                    </div>
                </div>
            </DialogHeader>

            <div class="flex-1 overflow-y-auto px-1">
                <!-- Loading State -->
                <div v-if="isLoading" class="space-y-4 py-4">
                    <Skeleton v-for="i in 5" :key="i" class="h-16" />
                </div>

                <!-- Timeline View -->
                <div v-else-if="contextLogs" class="relative space-y-0.5 py-2">

                    <!-- Load More Before -->
                    <div class="relative z-10 mb-3">
                        <Button 
                            v-if="!noMoreBefore"
                            variant="outline" 
                            class="w-full h-7 text-xs" 
                            :disabled="loadingMore === 'before'"
                            @click="loadMore('before')">
                            <ArrowUp v-if="loadingMore !== 'before'" class="mr-1 h-3 w-3" />
                            <Skeleton v-else class="h-3 w-3 rounded-full mr-1" />
                            {{ loadingMore === 'before' ? 'Loading...' : `Load ${batchSize} Before` }}
                        </Button>
                        <div v-else class="text-center text-xs text-muted-foreground py-1">
                            — Beginning of logs —
                        </div>
                    </div>

                    <!-- Before Logs (includes target timestamp logs at the end) -->
                    <div v-for="(log, idx) in contextLogs.before_logs" :key="getLogId('before', idx, log)"
                        class="relative rounded-md px-2 py-1.5 font-mono text-xs cursor-pointer"
                        :class="isTargetLog(log) 
                            ? 'bg-primary/10 border border-primary/30' 
                            : 'hover:bg-muted/50'"
                        @click="toggleExpand(getLogId('before', idx, log))">
                        <div class="flex gap-3">
                            <div class="flex-shrink-0 pt-0.5">
                                <div class="h-2 w-2 rounded-full"
                                    :class="isTargetLog(log) ? 'bg-primary' : 'bg-muted-foreground/30'" />
                            </div>
                            <div class="flex-shrink-0 text-muted-foreground w-[140px]">
                                {{ formatTime(getTimestamp(log)) }}
                            </div>
                            <div class="flex-1 min-w-0" :class="expandedLogId === getLogId('before', idx, log) ? '' : 'truncate'">
                                <span v-if="isTargetLog(log)" class="text-primary font-medium">{{ formatLogCompact(log) }}</span>
                                <span v-else class="text-foreground">{{ formatLogCompact(log) }}</span>
                            </div>
                            <span v-if="isTargetLog(log)" 
                                class="flex-shrink-0 bg-primary/20 text-primary px-1.5 py-0.5 rounded text-[10px] uppercase font-medium">
                                ●
                            </span>
                        </div>
                        <!-- Expanded JSON view -->
                        <div v-if="expandedLogId === getLogId('before', idx, log)" 
                            class="mt-2 ml-5 p-2 bg-muted/50 rounded text-[11px] overflow-x-auto">
                            <pre class="whitespace-pre-wrap break-all">{{ JSON.stringify(log, null, 2) }}</pre>
                        </div>
                    </div>

                    <!-- After Logs -->
                    <div v-for="(log, idx) in contextLogs.after_logs" :key="getLogId('after', idx, log)"
                        class="relative rounded-md px-2 py-1.5 font-mono text-xs hover:bg-muted/50 cursor-pointer"
                        @click="toggleExpand(getLogId('after', idx, log))">
                        <div class="flex gap-3">
                            <div class="flex-shrink-0 pt-0.5">
                                <div class="h-2 w-2 rounded-full bg-muted-foreground/30" />
                            </div>
                            <div class="flex-shrink-0 text-muted-foreground w-[140px]">
                                {{ formatTime(getTimestamp(log)) }}
                            </div>
                            <div class="flex-1 min-w-0 text-foreground" :class="expandedLogId === getLogId('after', idx, log) ? '' : 'truncate'">
                                {{ formatLogCompact(log) }}
                            </div>
                        </div>
                        <!-- Expanded JSON view -->
                        <div v-if="expandedLogId === getLogId('after', idx, log)" 
                            class="mt-2 ml-5 p-2 bg-muted/50 rounded text-[11px] overflow-x-auto">
                            <pre class="whitespace-pre-wrap break-all">{{ JSON.stringify(log, null, 2) }}</pre>
                        </div>
                    </div>

                    <!-- Load More After -->
                    <div class="relative z-10 mt-3">
                        <Button 
                            v-if="!noMoreAfter"
                            variant="outline" 
                            class="w-full h-7 text-xs" 
                            :disabled="loadingMore === 'after'"
                            @click="loadMore('after')">
                            <ArrowDown v-if="loadingMore !== 'after'" class="mr-1 h-3 w-3" />
                            <Skeleton v-else class="h-3 w-3 rounded-full mr-1" />
                            {{ loadingMore === 'after' ? 'Loading...' : `Load ${batchSize} After` }}
                        </Button>
                        <div v-else class="text-center text-xs text-muted-foreground py-1">
                            — End of logs —
                        </div>
                    </div>
                </div>

                <!-- Empty State -->
                <div v-else class="flex flex-col items-center justify-center py-12 text-muted-foreground">
                    <Clock class="h-12 w-12 mb-4" />
                    <p>No context logs available</p>
                </div>
            </div>
        </DialogContent>
    </Dialog>
</template>
