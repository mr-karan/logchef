<script setup lang="ts">
import { useI18n } from 'vue-i18n'
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
import { getSourceTypeLabel } from '@/lib/queryMetadata'
import { formatDate, getSourceConnectionDetails } from '@/utils/format'

const router = useRouter()
const { t } = useI18n()
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

function translateConnectionLabel(label: string) {
    switch (label) {
        case 'Host': return t('sources.connectionHost')
        case 'Database': return t('sources.database')
        case 'Table': return t('sources.tableName')
        case 'Base URL': return t('sources.baseURL')
        case 'Tenant': return t('sources.tenantScope')
        case 'Scope': return t('sources.immutableScopeQuery')
        default: return label
    }
}

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
})
</script>

<template>
    <div class="space-y-6">
        <PageHeader :title="t('ui.sources')" :description="t('sources.manageDescription')">
            <template #actions>
                <Button size="sm" @click="router.push({ name: 'NewSource' })">
                    <Plus class="mr-2 h-4 w-4" />
                    {{ t('sources.addSource') }}
                </Button>
            </template>
        </PageHeader>

        <LoadingState
            v-if="sourcesStore.isLoadingOperation('loadAllSourcesForAdmin')"
            :label="t('sources.loadingSources')"
        />
        <ErrorAlert v-else-if="loadingError" :error="loadingError" :title="t('sources.loadingSourcesFailed')"
            @retry="retryLoading" />
        <EmptyState
            v-else-if="sourcesStore.sources.length === 0"
            :icon="Database"
            :title="t('sources.noSourcesConfigured')"
            :description="t('sources.noSourcesDescription')"
        >
            <template #action>
                <Button size="sm" @click="router.push({ name: 'NewSource' })">
                    <Plus class="mr-2 h-4 w-4" />
                    {{ t('sources.addSource') }}
                </Button>
            </template>
        </EmptyState>
        <div v-else class="space-y-4">
                    <div class="relative max-w-sm">
                        <Search class="absolute left-2.5 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
                        <Input v-model="sourceSearch" :placeholder="t('sources.searchSources')" class="pl-8" />
                    </div>
                    <Table>
                        <TableHeader>
                            <TableRow>
                                <TableHead class="w-[220px]">
                                    <button type="button" class="inline-flex items-center gap-1 hover:text-foreground" @click="toggleSourceSort('name')">
                                        {{ t('sources.source') }}
                                        <component :is="sourceSortKey === 'name' ? (sourceSortDir === 'asc' ? ArrowUp : ArrowDown) : ArrowUpDown" class="size-3.5 opacity-60" />
                                    </button>
                                </TableHead>
                                <TableHead class="w-[120px]">{{ t('sources.type') }}</TableHead>
                                <TableHead class="w-[150px]">{{ t('sources.autoCreated') }}</TableHead>
                                <TableHead class="w-[150px]">{{ t('sources.timestampField') }}</TableHead>
                                <TableHead class="w-[300px]">{{ t('sources.connection') }}</TableHead>
                                <TableHead class="w-[100px]">
                                    <button type="button" class="inline-flex items-center gap-1 hover:text-foreground" @click="toggleSourceSort('status')">
                                        {{ t('sources.status') }}
                                        <component :is="sourceSortKey === 'status' ? (sourceSortDir === 'asc' ? ArrowUp : ArrowDown) : ArrowUpDown" class="size-3.5 opacity-60" />
                                    </button>
                                </TableHead>
                                <TableHead class="w-[100px]">
                                    <button type="button" class="inline-flex items-center gap-1 hover:text-foreground" @click="toggleSourceSort('created')">
                                        {{ t('sources.createdAt') }}
                                        <component :is="sourceSortKey === 'created' ? (sourceSortDir === 'asc' ? ArrowUp : ArrowDown) : ArrowUpDown" class="size-3.5 opacity-60" />
                                    </button>
                                </TableHead>
                                <TableHead class="w-[120px] text-right">{{ t('ui.actions') }}</TableHead>
                            </TableRow>
                        </TableHeader>
                        <TableBody>
                            <TableRow v-if="sortedSources.length === 0">
                                <TableCell colspan="8" class="text-center text-muted-foreground py-6">
                                    {{ t('sources.noSourcesMatch') }}
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
                                    <Badge :variant="source._meta_is_auto_created ? 'default' : 'secondary'"
                                        class="whitespace-nowrap">
                                        {{ source._meta_is_auto_created ? t('sources.yes') : t('sources.no') }}
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
                                            <span class="text-muted-foreground">{{ translateConnectionLabel(detail.label) }}</span>
                                            <span :class="detail.monospace ? 'font-mono text-xs break-all' : 'font-medium break-all'">
                                                {{ detail.value }}
                                            </span>
                                        </div>
                                    </div>
                                </TableCell>
                                <TableCell>
                                    <Badge :variant="source.is_connected ? 'success' : 'destructive'"
                                        class="whitespace-nowrap">
                                        {{ source.is_connected ? t('sources.connected') : t('sources.disconnected') }}
                                    </Badge>
                                </TableCell>
                                <TableCell>{{ formatDate(source.created_at) }}</TableCell>
                                <TableCell class="text-right">
                                    <div class="flex items-center justify-end gap-2">
                                        <Button variant="outline" size="icon" @click="handleEdit(source)"
                                                :title="t('sources.editSource')">
                                            <Pencil class="h-4 w-4" />
                                        </Button>
                                        <Button variant="outline" size="icon" @click="handleDuplicate(source)"
                                                :title="t('sources.duplicateSource')">
                                            <Copy class="h-4 w-4" />
                                        </Button>
                                        <Button variant="destructive" size="icon" @click="handleDelete(source)"
                                                :title="t('sources.deleteSource')">
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
            :title="t('sources.deleteSourceTitle')"
            :description="sourceToDelete ? t('sources.deleteSourceConfirmation', { name: sourceToDelete.name }) : undefined"
            :confirm-text="t('ui.delete')"
            destructive
            @update:open="(v) => { if (!v) { showDeleteDialog = false; sourceToDelete = null } }"
            @confirm="confirmDelete"
        />
    </div>
</template>
