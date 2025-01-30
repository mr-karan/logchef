<template>
    <n-card>
        <n-space vertical>
            <n-space justify="space-between" align="center">
                <n-space align="center">
                    <n-select v-model:value="selectedSource" :options="sourceOptions" placeholder="Select a source"
                        :loading="loading" style="width: 200px" />
                    <n-popover trigger="click" placement="bottom">
                        <template #trigger>
                            <n-button size="small">
                                Columns
                            </n-button>
                        </template>
                        <div style="padding: 8px; min-width: 200px; max-height: 400px; overflow-y: auto;">
                            <n-space vertical>
                                <div v-for="col in sourceDetails?.columns" :key="col.name">
                                    <n-checkbox v-if="col.name !== 'timestamp' && col.name !== 'body'"
                                        :checked="visibleColumns.has(col.name)"
                                        @update:checked="checked => toggleColumn(col.name, checked)">
                                        {{ col.name }}
                                    </n-checkbox>
                                </div>
                            </n-space>
                        </div>
                    </n-popover>
                </n-space>
                <n-space align="center">
                    <time-range-selector v-model="timeRange" />
                    <n-select v-model:value="limit" :options="limitOptions" placeholder="Limit" style="width: 120px" />
                </n-space>
            </n-space>

            <n-divider />

            <n-spin :show="loading">
                <div v-if="!selectedSource" class="empty-state">
                    <n-empty description="Select a source to explore logs">
                        <template #icon>
                            <n-icon size="48" color="#ccc">
                                <analytics-outline />
                            </n-icon>
                        </template>
                    </n-empty>
                </div>
                <div v-else>
                    <!-- Log content will go here -->
                    <n-space vertical>
                        <div>Debug - Logs count: {{ logs.length }}</div>
                        <n-data-table :columns="logColumns" :data="logs" :loading="logsLoading" :bordered="false"
                            :single-line="false" size="small" class="log-table" :row-props="rowProps"
                            :resizable="true" />
                        <n-drawer v-model:show="showDrawer" :width="500" placement="right">
                            <n-drawer-content :title="selectedLog?.timestamp || 'Log Details'" closable>
                                <n-code :code="JSON.stringify(selectedLog, null, 2)" language="json" :hljs="hljs" />
                            </n-drawer-content>
                        </n-drawer>
                    </n-space>
                </div>
            </n-spin>
        </n-space>
    </n-card>
</template>

<script setup lang="ts">
import { ref, onMounted, computed, watch, h } from 'vue'
import { AnalyticsOutline } from '@vicons/ionicons5'
import type { DrawerPlacement, DataTableColumns } from 'naive-ui'
import { NCheckbox } from 'naive-ui'
import hljs from 'highlight.js'
import TimeRangeSelector from '@/components/TimeRangeSelector.vue'
import sourcesApi from '@/api/sources'
import logsApi from '@/api/logs'
import type { Source, Log } from '@/api/types'

const loading = ref(true)
const logsLoading = ref(false)
const sources = ref<Source[]>([])
const selectedSource = ref<string | null>(null)
const sourceDetails = ref<Source | null>(null)
const logs = ref<Log[]>([])
const limit = ref(100)
const showDrawer = ref(false)
const selectedLog = ref<Log | null>(null)
const timeRange = ref([Date.now() - 3 * 60 * 60 * 1000, Date.now()])
const visibleColumns = ref<Set<string>>(new Set(['timestamp', 'body']))

const limitOptions = [
    { label: '100 logs', value: 100 },
    { label: '500 logs', value: 500 },
    { label: '1000 logs', value: 1000 },
    { label: '10000 logs', value: 10000 }
]


const rowProps = (row: Log) => {
    return {
        style: 'cursor: pointer;',
        onClick: () => {
            selectedLog.value = row
            showDrawer.value = true
        }
    }
}

const logColumns = computed<DataTableColumns>(() => {
    if (!sourceDetails.value?.columns) return []

    const baseColumns = [
        {
            title: 'Timestamp',
            key: 'timestamp',
            resizable: true,
            width: 220,
            minWidth: 160,
            maxWidth: 400
        },
        {
            title: 'Body',
            key: 'body',
            resizable: true,
            minWidth: 200,
            maxWidth: 800,
            ellipsis: {
                tooltip: true
            }
        }
    ]

    // Dynamic columns based on visibility
    const dynamicColumns = sourceDetails.value.columns
        .filter(col =>
            col.name !== 'timestamp' &&
            col.name !== 'body' &&
            visibleColumns.value.has(col.name)
        )
        .map(col => ({
            title: col.name.replace(/_/g, ' ').replace(/\b\w/g, l => l.toUpperCase()),
            key: col.name,
            resizable: true,
            minWidth: 100,
            maxWidth: 300
        }))

    return [...baseColumns, ...dynamicColumns]
})

// Add debug watcher
watch(logs, (newLogs) => {
    console.log('Logs updated:', newLogs)
    if (newLogs.length > 0) {
        console.log('Sample log entry:', newLogs[0])
    }
})

function toggleColumn(colName: string, checked?: boolean) {
    if (checked === undefined) {
        // Toggle when called from dropdown select
        if (visibleColumns.value.has(colName)) {
            visibleColumns.value.delete(colName)
        } else {
            visibleColumns.value.add(colName)
        }
    } else {
        // Direct checkbox toggle
        if (checked) {
            visibleColumns.value.add(colName)
        } else {
            visibleColumns.value.delete(colName)
        }
    }
    // Create new Set to trigger reactivity
    visibleColumns.value = new Set(visibleColumns.value)
}

const sourceOptions = computed(() =>
    sources.value.map(source => ({
        label: `${source.connection.database}.${source.connection.table_name}`,
        value: source.id
    }))
)

async function loadSources() {
    try {
        loading.value = true
        sources.value = await sourcesApi.listSources()
        if (sources.value.length > 0) {
            selectedSource.value = sources.value[0].id
        }
    } catch (error) {
        console.error('Failed to load sources:', error)
    } finally {
        loading.value = false
    }
}

async function loadSourceDetails(id: string) {
    try {
        loading.value = true
        sourceDetails.value = await sourcesApi.getSource(id)
        await loadLogs()
    } catch (error) {
        console.error('Failed to load source details:', error)
    } finally {
        loading.value = false
    }
}

// Watch for source changes
watch(selectedSource, async (newId) => {
    if (newId) {
        await loadSourceDetails(newId)
    } else {
        sourceDetails.value = null
        logs.value = []
    }
}, { immediate: true })

async function loadLogs() {
    if (!selectedSource.value || !timeRange.value) return

    try {
        logsLoading.value = true
        const response = await logsApi.getLogs(selectedSource.value, {
            start_timestamp: timeRange.value[0],
            end_timestamp: timeRange.value[1],
            limit: limit.value
        })
        console.log('API Response:', response)
        console.log('Logs:', response.logs)
        logs.value = response.logs || []
    } catch (error) {
        console.error('Failed to load logs:', error)
        logs.value = []
    } finally {
        logsLoading.value = false
    }
}

// Watch for changes that should trigger log reload
watch([timeRange, limit], () => {
    if (selectedSource.value) {
        loadLogs()
    }
})

onMounted(() => {
    loadSources()
})
</script>

<style scoped>
.empty-state {
    padding: 48px;
    text-align: center;
}

.log-table :deep(td) {
    font-family: 'JetBrains Mono', monospace;
    font-size: 12px;
    padding: 4px 8px !important;
    white-space: nowrap;
}

.log-table :deep(th) {
    font-size: 12px;
    padding: 6px 8px !important;
    background: #f5f5f5;
}

.log-table :deep(.n-data-table-td) {
    overflow: hidden;
    text-overflow: ellipsis;
}
</style>
