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
import JsonViewer from '@/components/json-viewer/JsonViewer.vue'
import { Clock, ArrowDown, ArrowUp } from 'lucide-vue-next'

const props = defineProps<{
    isOpen: boolean
    sourceId: string
    log: Record<string, any>
}>()

const emit = defineEmits<{
    (e: 'update:isOpen', value: boolean): void
}>()

const { toast } = useToast()
const isLoading = ref(false)
const loadingMore = ref<'before' | 'after' | null>(null)
const contextLogs = ref<{
    before_logs: Record<string, any>[];
    target_logs: Record<string, any>[];
    after_logs: Record<string, any>[];
} | null>(null)

// Load context data when modal opens
watch(() => props.isOpen, async (open) => {
    if (open && props.log.timestamp) {
        if (!props.sourceId) {
            toast({
                title: 'Error',
                description: 'Source ID is required',
                variant: 'destructive',
                duration: TOAST_DURATION.ERROR,
            })
            return
        }

        isLoading.value = true
        try {
            const timestamp = new Date(props.log.timestamp).getTime()
            const result = await exploreApi.getLogContext(parseInt(props.sourceId), {
                timestamp,
                before_limit: 5,
                after_limit: 5
            })

            if (result.status === 'error') {
                throw new Error(result.data.error)
            }

            contextLogs.value = {
                before_logs: result.data.before_logs,
                target_logs: result.data.target_logs,
                after_logs: result.data.after_logs
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
    } else {
        contextLogs.value = null
    }
})

// Load more context logs
async function loadMore(direction: 'before' | 'after') {
    if (!contextLogs.value || !props.sourceId) return

    loadingMore.value = direction
    try {
        const timestamp = direction === 'before'
            ? new Date(contextLogs.value.before_logs[0]?.timestamp).getTime()
            : new Date(contextLogs.value.after_logs[contextLogs.value.after_logs.length - 1]?.timestamp).getTime()

        const result = await exploreApi.getLogContext(parseInt(props.sourceId), {
            timestamp,
            before_limit: direction === 'before' ? 5 : 0,
            after_limit: direction === 'after' ? 5 : 0
        })

        if (result.status === 'error') {
            throw new Error(result.data.error)
        }

        // Append new logs to existing context
        if (direction === 'before') {
            contextLogs.value.before_logs = [...result.data.before_logs, ...contextLogs.value.before_logs]
        } else {
            contextLogs.value.after_logs = [...contextLogs.value.after_logs, ...result.data.after_logs]
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
</script>

<template>
    <Dialog :open="isOpen" @update:open="emit('update:isOpen', $event)">
        <DialogContent class="max-w-4xl h-[85vh]">
            <DialogHeader>
                <DialogTitle class="flex items-center gap-2">
                    <Clock class="h-5 w-5" />
                    Log Context
                </DialogTitle>
            </DialogHeader>

            <div class="flex-1 overflow-y-auto px-1">
                <!-- Loading State -->
                <div v-if="isLoading" class="space-y-4 py-4">
                    <Skeleton v-for="i in 5" :key="i" class="h-16" />
                </div>

                <!-- Timeline View -->
                <div v-else-if="contextLogs" class="relative space-y-1 py-4">
                    <!-- Timeline Line -->
                    <div class="absolute left-[19px] top-0 bottom-0 w-[2px] bg-border" />

                    <!-- Load More Before -->
                    <div v-if="contextLogs.before_logs.length > 0" class="relative z-10 mb-4">
                        <Button variant="outline" class="w-full" :disabled="loadingMore === 'before'"
                            @click="loadMore('before')">
                            <ArrowUp v-if="!loadingMore" class="mr-2 h-4 w-4" />
                            <Skeleton v-else class="h-4 w-4 rounded-full mr-2" />
                            {{ loadingMore === 'before' ? 'Loading...' : 'Load More Before' }}
                        </Button>
                    </div>

                    <!-- Before Logs -->
                    <div v-for="log in contextLogs.before_logs" :key="log.timestamp"
                        class="relative flex gap-4 group hover:bg-muted/50 rounded-lg p-2">
                        <div class="flex flex-col items-center">
                            <div class="h-4 w-4 rounded-full bg-border group-hover:bg-primary/50 transition-colors" />
                        </div>
                        <div class="flex-1 min-w-0">
                            <div class="text-xs text-muted-foreground mb-1">
                                {{ formatTime(log.timestamp) }}
                            </div>
                            <JsonViewer :value="log" :expanded="false" />
                        </div>
                    </div>

                    <!-- Target Log -->
                    <div v-for="log in contextLogs.target_logs" :key="log.timestamp"
                        class="relative flex gap-4 bg-muted/50 rounded-lg p-2 border-2 border-primary/50">
                        <div class="flex flex-col items-center">
                            <div class="h-4 w-4 rounded-full bg-primary" />
                        </div>
                        <div class="flex-1 min-w-0">
                            <div class="text-xs font-medium mb-1 flex items-center justify-between">
                                <span>{{ formatTime(log.timestamp) }}</span>
                                <span class="bg-primary/10 text-primary px-2 py-0.5 rounded text-[10px] uppercase">
                                    Current Log
                                </span>
                            </div>
                            <JsonViewer :value="log" :expanded="true" />
                        </div>
                    </div>

                    <!-- After Logs -->
                    <div v-for="log in contextLogs.after_logs" :key="log.timestamp"
                        class="relative flex gap-4 group hover:bg-muted/50 rounded-lg p-2">
                        <div class="flex flex-col items-center">
                            <div class="h-4 w-4 rounded-full bg-border group-hover:bg-primary/50 transition-colors" />
                        </div>
                        <div class="flex-1 min-w-0">
                            <div class="text-xs text-muted-foreground mb-1">
                                {{ formatTime(log.timestamp) }}
                            </div>
                            <JsonViewer :value="log" :expanded="false" />
                        </div>
                    </div>

                    <!-- Load More After -->
                    <div v-if="contextLogs.after_logs.length > 0" class="relative z-10 mt-4">
                        <Button variant="outline" class="w-full" :disabled="loadingMore === 'after'"
                            @click="loadMore('after')">
                            <ArrowDown v-if="!loadingMore" class="mr-2 h-4 w-4" />
                            <Skeleton v-else class="h-4 w-4 rounded-full mr-2" />
                            {{ loadingMore === 'after' ? 'Loading...' : 'Load More After' }}
                        </Button>
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

<style scoped>
.timeline-dot {
    @apply h-4 w-4 rounded-full bg-border transition-colors;
}

.timeline-dot-active {
    @apply bg-primary;
}
</style>