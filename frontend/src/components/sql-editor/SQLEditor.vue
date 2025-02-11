<script setup lang="ts">
import { ref, computed } from 'vue'
import { Button } from '@/components/ui/button'
import { Textarea } from '@/components/ui/textarea'
import { Label } from '@/components/ui/label'
import { Code, X } from 'lucide-vue-next'
import { useToast } from '@/components/ui/toast'
import { TOAST_DURATION } from '@/lib/constants'

const { toast } = useToast()

interface Props {
    modelValue: string
    sourceDatabase?: string
    sourceTable?: string
    startTimestamp: number
    endTimestamp: number
}

const props = defineProps<Props>()
const emit = defineEmits<{
    (e: 'update:modelValue', value: string): void
    (e: 'execute'): void
}>()

const sqlQuery = computed({
    get: () => props.modelValue,
    set: (value) => emit('update:modelValue', value)
})

// Get full table name with database
const fullTableName = computed(() => {
    if (!props.sourceDatabase || !props.sourceTable) return ''
    return `${props.sourceDatabase}.${props.sourceTable}`
})

// SQL templates based on source
const getSqlTemplates = computed(() => {
    if (!fullTableName.value) return []

    const startTime = props.startTimestamp / 1000 // Convert to seconds for ClickHouse
    const endTime = props.endTimestamp / 1000

    return [
        {
            label: 'Last 3 Days',
            sql: `SELECT *
            FROM ${fullTableName.value}
            WHERE timestamp >= today() - INTERVAL 3 DAY
            ORDER BY timestamp DESC`
        },
        {
            label: 'Today Only',
            sql: `SELECT *
            FROM ${fullTableName.value}
            WHERE timestamp >= today()
            ORDER BY timestamp DESC`
        },
        {
            label: 'Last Hour',
            sql: `SELECT *
            FROM ${fullTableName.value}
            WHERE timestamp >= now() - INTERVAL 1 HOUR
            ORDER BY timestamp DESC`
        },
        {
            label: 'Severity Distribution (Last 24h)',
            sql: `SELECT
              severity_text,
              count(*) as count,
              min(timestamp) as first_seen,
              max(timestamp) as last_seen
            FROM ${fullTableName.value}
            WHERE timestamp >= now() - INTERVAL 24 HOUR
            GROUP BY severity_text
            ORDER BY count DESC`
        },
        {
            label: 'Recent Errors',
            sql: `SELECT *
            FROM ${fullTableName.value}
            WHERE timestamp >= now() - INTERVAL 6 HOUR
              AND (severity_text = 'ERROR' OR severity_text = 'FATAL')
            ORDER BY timestamp DESC`
        },
        {
            label: 'Service Distribution (This Week)',
            sql: `SELECT
              service_name,
              count(*) as count,
              min(timestamp) as first_seen,
              max(timestamp) as last_seen
            FROM ${fullTableName.value}
            WHERE timestamp >= toStartOfWeek(now())
            GROUP BY service_name
            ORDER BY count DESC`
        },
        {
            label: 'Custom Time Range',
            sql: `SELECT *
            FROM ${fullTableName.value}
            WHERE timestamp BETWEEN toDateTime64(${startTime}, 3) AND toDateTime64(${endTime}, 3)
            ORDER BY timestamp DESC`
        }
    ]
})

// Clear SQL query
const clearSql = () => {
    sqlQuery.value = ''
}

// Insert template
const insertTemplate = (sql: string) => {
    sqlQuery.value = sql
}

// Execute query with validation
const executeQuery = () => {
    const sql = sqlQuery.value.trim().toLowerCase()
    if (!sql) {
        toast({
            title: 'Error',
            description: 'SQL query cannot be empty',
            variant: 'destructive',
            duration: TOAST_DURATION.ERROR,
        })
        return
    }

    if (!sql.startsWith('select')) {
        toast({
            title: 'Error',
            description: 'Only SELECT queries are allowed',
            variant: 'destructive',
            duration: TOAST_DURATION.ERROR,
        })
        return
    }

    emit('execute')
}

// Recent queries history (stored in memory)
const recentQueries = ref<Array<{ sql: string; timestamp: number }>>([])

// Save query to history after execution
const saveToHistory = (query: string) => {
    const trimmed = query.trim()
    if (trimmed) {
        recentQueries.value = [
            { sql: trimmed, timestamp: Date.now() },
            ...recentQueries.value.slice(0, 4)
        ]
    }
}
</script>

<template>
    <div class="w-full">
        <div class="flex flex-col gap-4">
            <!-- SQL Editor -->
            <div class="flex flex-col gap-2">
                <div class="flex items-center justify-between">
                    <Label>SQL Query</Label>
                    <div class="flex items-center gap-2">
                        <Button variant="ghost" size="sm" @click="clearSql">
                            <X class="w-4 h-4 mr-2" />
                            Clear
                        </Button>
                    </div>
                </div>
                <Textarea v-model="sqlQuery" placeholder="SELECT * FROM logs.table WHERE severity_text = 'ERROR'"
                    class="font-mono h-24 resize-y" :spellcheck="false" @keydown.ctrl.enter.prevent="executeQuery"
                    @keydown.meta.enter.prevent="executeQuery" />
                <p class="text-sm text-muted-foreground">
                    Only SELECT queries are allowed. Press Ctrl+Enter (Cmd+Enter on Mac) to execute.
                </p>
            </div>

            <!-- Quick Templates -->
            <div v-if="getSqlTemplates.length > 0" class="space-y-2">
                <Label class="text-sm">Quick Templates</Label>
                <div class="flex gap-2 flex-wrap">
                    <Button v-for="template in getSqlTemplates" :key="template.label" variant="outline" size="sm"
                        @click="insertTemplate(template.sql)">
                        {{ template.label }}
                    </Button>
                </div>
            </div>

            <!-- Recent Queries -->
            <div v-if="recentQueries.length > 0" class="space-y-2">
                <Label class="text-sm">Recent Queries</Label>
                <div class="flex flex-col gap-2">
                    <Button v-for="query in recentQueries" :key="query.timestamp" variant="ghost" size="sm"
                        class="h-auto py-2 justify-start font-mono text-xs" @click="insertTemplate(query.sql)">
                        {{ query.sql }}
                    </Button>
                </div>
            </div>
        </div>
    </div>
</template>