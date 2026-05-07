<script setup lang="ts">
import { onMounted, ref, computed } from 'vue'
import { storeToRefs } from 'pinia'
import { Button } from '@/components/ui/button'
import ErrorAlert from '@/components/ui/ErrorAlert.vue'
import ConfirmDialog from '@/components/ui/ConfirmDialog.vue'
import { PageHeader, EmptyState, LoadingState } from '@/components/layout'
import { Plus, Trash2, Copy, Pencil, Database } from 'lucide-vue-next'
import { useRouter } from 'vue-router'
import { type Source } from '@/api/sources'
import {
    Table,
    TableBody,
    TableCell,
    TableHead,
    TableHeader,
    TableRow,
} from '@/components/ui/table'
import { Badge } from '@/components/ui/badge'
import { useSourcesStore } from '@/stores/sources'
import SourceSparkline from '@/components/visualizations/SourceSparkline.vue'

const router = useRouter()
// This route is only accessible by admins
const sourcesStore = useSourcesStore()

const { error } = storeToRefs(sourcesStore)
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
        sourcesStore.sources.map((source) => sourcesStore.getSourceStats(source.id))
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

// Import formatDate from utils
import { formatDate } from '@/utils/format'
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
                    <Table>
                        <TableHeader>
                            <TableRow>
                                <TableHead class="w-[200px]">Source Name</TableHead>
                                <TableHead class="w-[200px]">Ingestion (24h)</TableHead>
                                <TableHead class="w-[150px]">Table Auto Created</TableHead>
                                <TableHead class="w-[150px]">Timestamp Column</TableHead>
                                <TableHead class="w-[300px]">Connection</TableHead>
                                <TableHead class="w-[100px]">Status</TableHead>
                                <TableHead class="w-[100px]">Created At</TableHead>
                                <TableHead class="w-[120px] text-right">Actions</TableHead>
                            </TableRow>
                        </TableHeader>
                        <TableBody>
                            <TableRow v-for="source in sourcesStore.sources" :key="source.id">
                                <TableCell class="font-medium">
                                    <a @click="router.push({ name: 'SourceStats', query: { sourceId: source.id } })"
                                        class="hover:underline cursor-pointer">
                                        {{ source.name }}
                                    </a>
                                    <div v-if="source.description" class="text-sm text-muted-foreground">
                                        {{ source.description }}
                                    </div>
                                </TableCell>
                                <TableCell>
                                    <div class="space-y-1 min-w-[180px]">
                                        <div class="text-xs text-muted-foreground">
                                            {{ sourcesStore.getSourceStatsById(source.id)?.ingestion_stats?.rows_24h?.toLocaleString() || '0' }} rows
                                        </div>
                                        <SourceSparkline
                                            v-if="sourcesStore.getSourceStatsById(source.id)?.ingestion_stats"
                                            :data="sourcesStore.getSourceStatsById(source.id)?.ingestion_stats?.hourly_buckets || []"
                                            :height="36"
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
                                        <div class="flex items-center space-x-2">
                                            <span class="text-muted-foreground">Host</span>
                                            <span class="font-medium">{{ source.connection.host }}</span>
                                        </div>
                                        <div class="flex items-center space-x-2">
                                            <span class="text-muted-foreground">Database</span>
                                            <span class="font-medium">{{ source.connection.database }}</span>
                                        </div>
                                        <div class="flex items-center space-x-2">
                                            <span class="text-muted-foreground">Table</span>
                                            <span class="font-medium">{{ source.connection.table_name }}</span>
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
            :description="sourceToDelete ? `Delete source &quot;${sourceToDelete.name}&quot;? This only removes the source from LogChef — the underlying ClickHouse data is not deleted.` : undefined"
            confirm-text="Delete"
            destructive
            @update:open="(v) => { if (!v) { showDeleteDialog = false; sourceToDelete = null } }"
            @confirm="confirmDelete"
        />
    </div>
</template>
