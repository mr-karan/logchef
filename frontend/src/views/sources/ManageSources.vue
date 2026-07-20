<script setup lang="ts">
import { onMounted, ref, computed } from 'vue'
import { storeToRefs } from 'pinia'
import { Button } from '@/components/ui/button'
import ErrorAlert from '@/components/ui/ErrorAlert.vue'
import ConfirmDialog from '@/components/ui/ConfirmDialog.vue'
import { PageHeader, EmptyState, LoadingState } from '@/components/layout'
import { Plus, Trash2, Copy, Pencil, Database, Search, ArrowUpDown, ArrowUp, ArrowDown } from 'lucide-vue-next'
import { useRouter } from 'vue-router'
import { type Source, type VictoriaLogsConnectionInfo, asClickHouseConnection } from '@/api/sources'
import {
    Table,
    TableBody,
    TableCell,
    TableHead,
    TableHeader,
    TableRow,
} from '@/components/ui/table'
import { Input } from '@/components/ui/input'
import { Badge } from '@/components/ui/badge'
import { useSourcesStore } from '@/stores/sources'
import { useTableSearchSort } from '@/composables/useTableSearchSort'
import SourceSparkline from '@/components/visualizations/SourceSparkline.vue'
import { getSourceTypeLabel } from '@/lib/queryMetadata'
import { formatDate, getSourceConnectionDetails } from '@/utils/format'

const router = useRouter()
// This route is only accessible by admins
const sourcesStore = useSourcesStore()

const { error } = storeToRefs(sourcesStore)

// Build a searchable string from a source's connection, handling both
// ClickHouse (database.table @ host) and VictoriaLogs (base_url) shapes.
const connectionSearchText = (s: Source): string => {
    const ch = asClickHouseConnection(s.connection)
    if (ch) return `${ch.database}.${ch.table_name} ${ch.host}`
    const vl = s.connection as VictoriaLogsConnectionInfo
    return vl?.base_url ?? ''
}
const showDeleteDialog = ref(false)
const sourceToDelete = ref<Source | null>(null)

// Check for loading errors
const loadingError = computed(() => {
    if (error.value && typeof error.value === 'object') {
        // Check if the error object has the property as a string key
        return error.value && 'loadAllSourcesForAdmin' in error.value
            ? (error.value as Record<string, any>).loadAllSourcesForAdmin
            : null
    }
    return null
})

// Client-side search + sort over the fully-loaded sources list.
const {
    search: sourceSearch,
    rows: sortedSources,
    sortKey: sourceSortKey,
    sortDir: sourceSortDir,
    toggleSort: toggleSourceSort,
} = useTableSearchSort(() => sourcesStore.sources, {
    searchKeys: [
        'name',
        (s) => s.description,
        (s) => connectionSearchText(s),
    ],
    sortAccessors: {
        name: (s) => (s.name || '').toLowerCase(),
        status: (s) => (s.is_connected ? 1 : 0),
        created: (s) => new Date(s.created_at),
    },
    initialSort: { key: 'name', dir: 'asc' },
})

const handleDelete = (source: Source) => {
    sourceToDelete.value = source
    showDeleteDialog.value = true
}

const handleDuplicate = (source: Source) => {
    router.push({ name: 'NewSource', query: { duplicateFrom: source.id } })
}

const handleEdit = (source: Source) => {
    router.push({ name: 'EditSource', params: { sourceId: source.id } })
}

const retryLoading = async () => {
    await loadSources()
}

const fetchSourceIngestionStats = async () => {
    await Promise.all(
        sourcesStore.sources.map((source) => sourcesStore.getSourceInspection(source.id))
    )
}

// Load sources for admin view
const loadSources = async () => {
    // Reset any previous error
    if (error.value) {
        error.value = null
    }

    // Since this is an admin-only route, directly use the admin function
    await sourcesStore.loadAllSourcesForAdmin()
}

const confirmDelete = async () => {
    if (!sourceToDelete.value) return

    await sourcesStore.deleteSource(sourceToDelete.value.id)

    // Reset UI state - store handles success/error
    showDeleteDialog.value = false
    sourceToDelete.value = null
}

onMounted(async () => {
    // Load admin sources
    await loadSources()
    await fetchSourceIngestionStats()
})
</script>

<template>
    <div class="space-y-6">
        <PageHeader title="Sources" description="View and manage all log sources.">
            <template #actions>
                <Button size="sm" @click="router.push({ name: 'NewSource' })">
                    <Plus class="mr-2 h-4 w-4" />
                    Add source
                </Button>
            </template>
        </PageHeader>

        <LoadingState
            v-if="sourcesStore.isLoadingOperation('loadAllSourcesForAdmin')"
            label="Loading sources…"
        />
        <ErrorAlert v-else-if="loadingError" :error="loadingError" title="Failed to load sources"
            @retry="retryLoading" />
        <EmptyState
            v-else-if="sourcesStore.sources.length === 0"
            :icon="Database"
            title="No sources configured"
            description="Connect your first ClickHouse log source to get started."
        >
            <template #action>
                <Button size="sm" @click="router.push({ name: 'NewSource' })">
                    <Plus class="mr-2 h-4 w-4" />
                    Add source
                </Button>
            </template>
        </EmptyState>
        <div v-else class="space-y-4">
                    <div class="relative max-w-sm">
                        <Search class="absolute left-2.5 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
                        <Input v-model="sourceSearch" placeholder="Search sources…" class="pl-8" />
                    </div>
                    <Table>
                        <TableHeader>
                            <TableRow>
                                <TableHead class="w-[220px]">
                                    <button type="button" class="inline-flex items-center gap-1 hover:text-foreground" @click="toggleSourceSort('name')">
                                        Source
                                        <component :is="sourceSortKey === 'name' ? (sourceSortDir === 'asc' ? ArrowUp : ArrowDown) : ArrowUpDown" class="size-3.5 opacity-60" />
                                    </button>
                                </TableHead>
                                <TableHead class="w-[120px]">Type</TableHead>
                                <TableHead class="w-[200px]">Activity (24h)</TableHead>
                                <TableHead class="w-[150px]">Auto Created</TableHead>
                                <TableHead class="w-[150px]">Timestamp Field</TableHead>
                                <TableHead class="w-[300px]">Connection</TableHead>
                                <TableHead class="w-[100px]">
                                    <button type="button" class="inline-flex items-center gap-1 hover:text-foreground" @click="toggleSourceSort('status')">
                                        Status
                                        <component :is="sourceSortKey === 'status' ? (sourceSortDir === 'asc' ? ArrowUp : ArrowDown) : ArrowUpDown" class="size-3.5 opacity-60" />
                                    </button>
                                </TableHead>
                                <TableHead class="w-[100px]">
                                    <button type="button" class="inline-flex items-center gap-1 hover:text-foreground" @click="toggleSourceSort('created')">
                                        Created At
                                        <component :is="sourceSortKey === 'created' ? (sourceSortDir === 'asc' ? ArrowUp : ArrowDown) : ArrowUpDown" class="size-3.5 opacity-60" />
                                    </button>
                                </TableHead>
                                <TableHead class="w-[120px] text-right">Actions</TableHead>
                            </TableRow>
                        </TableHeader>
                        <TableBody>
                            <TableRow v-if="sortedSources.length === 0">
                                <TableCell colspan="8" class="text-center text-muted-foreground py-6">
                                    No sources match your search
                                </TableCell>
                            </TableRow>
                            <TableRow v-for="source in sortedSources" :key="source.id">
                                <TableCell class="font-medium">
                                    <a @click="router.push({ name: 'SourceInspection', query: { sourceId: source.id } })"
                                        class="hover:underline cursor-pointer">
                                        {{ source.name }}
                                    </a>
                                    <div v-if="source.description" class="text-sm text-muted-foreground">
                                        {{ source.description }}
                                    </div>
                                </TableCell>
                                <TableCell>
                                    <Badge variant="outline">{{ getSourceTypeLabel(source) }}</Badge>
                                </TableCell>
                                <TableCell>
                                    <div class="space-y-1 min-w-[180px]">
                                        <div class="text-xs text-muted-foreground">
                                            {{ sourcesStore.getSourceInspectionById(source.id)?.activity?.rows_24h?.toLocaleString() || '0' }} rows
                                        </div>
                                        <SourceSparkline
                                            v-if="sourcesStore.getSourceInspectionById(source.id)?.activity"
                                            :data="sourcesStore.getSourceInspectionById(source.id)?.activity?.hourly_buckets || []"
                                            :height="36"
                                            bucket-mode="hourly"
                                        />
                                    </div>
                                </TableCell>
                                <TableCell>
                                    <Badge :variant="source._meta_is_auto_created ? 'default' : 'secondary'"
                                        class="whitespace-nowrap">
                                        {{ source._meta_is_auto_created ? 'Yes' : 'No' }}
                                    </Badge>
                                </TableCell>
                                <TableCell>
                                    <code
                                        class="font-mono text-xs bg-muted px-2 py-1 rounded">{{ source._meta_ts_field }}</code>
                                </TableCell>
                                <TableCell>
                                    <div class="text-sm space-y-1">
                                        <div
                                            v-for="detail in getSourceConnectionDetails(source)"
                                            :key="`${source.id}-${detail.label}`"
                                            class="flex items-start space-x-2"
                                        >
                                            <span class="text-muted-foreground">{{ detail.label }}</span>
                                            <span :class="detail.monospace ? 'font-mono text-xs break-all' : 'font-medium break-all'">
                                                {{ detail.value }}
                                            </span>
                                        </div>
                                    </div>
                                </TableCell>
                                <TableCell>
                                    <Badge :variant="source.is_connected ? 'success' : 'destructive'"
                                        class="whitespace-nowrap">
                                        {{ source.is_connected ? 'Connected' : 'Disconnected' }}
                                    </Badge>
                                </TableCell>
                                <TableCell>{{ formatDate(source.created_at) }}</TableCell>
                                <TableCell class="text-right">
                                    <div class="flex items-center justify-end gap-2">
                                        <Button variant="outline" size="icon" @click="handleEdit(source)"
                                                title="Edit source">
                                            <Pencil class="h-4 w-4" />
                                        </Button>
                                        <Button variant="outline" size="icon" @click="handleDuplicate(source)"
                                                title="Duplicate source">
                                            <Copy class="h-4 w-4" />
                                        </Button>
                                        <Button variant="destructive" size="icon" @click="handleDelete(source)"
                                                title="Delete source">
                                            <Trash2 class="h-4 w-4" />
                                        </Button>
                                    </div>
                                </TableCell>
                            </TableRow>
                        </TableBody>
                    </Table>
        </div>

        <ConfirmDialog
            :open="showDeleteDialog"
            title="Delete source?"
            :description="sourceToDelete ? `Delete source &quot;${sourceToDelete.name}&quot;? This only removes the source from LogChef — the underlying data stays in the configured backend and must be managed there separately.` : undefined"
            confirm-text="Delete"
            destructive
            @update:open="(v) => { if (!v) { showDeleteDialog = false; sourceToDelete = null } }"
            @confirm="confirmDelete"
        />
    </div>
</template>
