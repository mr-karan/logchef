<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import {
    AlertDialog,
    AlertDialogAction,
    AlertDialogCancel,
    AlertDialogContent,
    AlertDialogDescription,
    AlertDialogFooter,
    AlertDialogHeader,
    AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { Plus, Trash2 } from 'lucide-vue-next'
import { useRouter } from 'vue-router'
import { sourcesApi, type Source } from '@/api/sources'
import {
    Table,
    TableBody,
    TableCell,
    TableHead,
    TableHeader,
    TableRow,
} from '@/components/ui/table'
import { useToast } from '@/components/ui/toast/use-toast'
import { TOAST_DURATION } from '@/lib/constants'
import { isErrorResponse, getErrorMessage } from '@/api/types'
import type { APIResponse } from '@/api/types'
import { Badge } from '@/components/ui/badge'

const router = useRouter()
const { toast } = useToast()
const sources = ref<Source[]>([])
const isLoading = ref(true)
const showDeleteDialog = ref(false)
const sourceToDelete = ref<Source | null>(null)

const loadSources = async () => {
    try {
        const response = await sourcesApi.listSources()
        if (isErrorResponse(response)) {
            toast({
                title: 'Error',
                description: response.data.error,
                variant: 'destructive',
                duration: TOAST_DURATION.ERROR,
            })
            return
        }
        sources.value = response.data.sources
    } catch (error) {
        console.error('Error loading sources:', error)
        toast({
            title: 'Error',
            description: getErrorMessage(error),
            variant: 'destructive',
            duration: TOAST_DURATION.ERROR,
        })
    } finally {
        isLoading.value = false
    }
}

const confirmDelete = async () => {
    if (!sourceToDelete.value) return

    try {
        const response = await sourcesApi.deleteSource(sourceToDelete.value.id)
        if (isErrorResponse(response)) {
            toast({
                title: 'Error',
                description: response.data.error,
                variant: 'destructive',
                duration: TOAST_DURATION.ERROR,
            })
            return
        }

        toast({
            title: 'Success',
            description: 'Source deleted successfully',
            duration: TOAST_DURATION.SUCCESS,
        })
        await loadSources()
    } catch (error) {
        console.error('Error deleting source:', error)
        toast({
            title: 'Error',
            description: getErrorMessage(error),
            variant: 'destructive',
            duration: TOAST_DURATION.ERROR,
        })
    } finally {
        showDeleteDialog.value = false
        sourceToDelete.value = null
    }
}

const handleDelete = (source: Source) => {
    sourceToDelete.value = source
    showDeleteDialog.value = true
}

onMounted(loadSources)

const formatDate = (dateString: string) => {
    return new Date(dateString).toLocaleDateString()
}
</script>

<template>
    <div class="space-y-6">
        <Card>
            <CardHeader>
                <div class="flex items-center justify-between">
                    <div>
                        <CardTitle>Manage Sources</CardTitle>
                        <CardDescription>
                            View and manage your log sources
                        </CardDescription>
                    </div>
                    <Button @click="router.push({ name: 'NewSource' })">
                        <Plus class="mr-2 h-4 w-4" />
                        Add Source
                    </Button>
                </div>
            </CardHeader>
            <CardContent>
                <div v-if="isLoading" class="text-center py-4">
                    Loading sources...
                </div>
                <div v-else-if="sources.length === 0" class="rounded-lg border p-4 text-center">
                    <p class="text-muted-foreground mb-4">No sources configured yet</p>
                    <Button @click="router.push({ name: 'NewSource' })">
                        <Plus class="mr-2 h-4 w-4" />
                        Add Your First Source
                    </Button>
                </div>
                <div v-else class="space-y-4">
                    <Table>
                        <TableHeader>
                            <TableRow>
                                <TableHead class="w-[200px]">Table Name</TableHead>
                                <TableHead class="w-[100px]">Type</TableHead>
                                <TableHead class="w-[300px]">Connection</TableHead>
                                <TableHead class="w-[100px]">Status</TableHead>
                                <TableHead class="w-[100px]">TTL</TableHead>
                                <TableHead class="w-[100px]">Created At</TableHead>
                                <TableHead class="w-[70px] text-right">Actions</TableHead>
                            </TableRow>
                        </TableHeader>
                        <TableBody>
                            <TableRow v-for="source in sources" :key="source.id">
                                <TableCell class="font-medium">
                                    {{ source.connection.table_name }}
                                    <div v-if="source.description" class="text-sm text-muted-foreground">
                                        {{ source.description }}
                                    </div>
                                </TableCell>
                                <TableCell>
                                    <Badge variant="secondary" class="capitalize">
                                        {{ source.schema_type }}
                                    </Badge>
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
                                    </div>
                                </TableCell>
                                <TableCell>
                                    <Badge :variant="source.is_connected ? 'success' : 'destructive'"
                                        class="whitespace-nowrap">
                                        {{ source.is_connected ? 'Connected' : 'Disconnected' }}
                                    </Badge>
                                </TableCell>
                                <TableCell>
                                    {{ source.ttl_days === -1 ? 'Disabled' : `${source.ttl_days} days` }}
                                </TableCell>
                                <TableCell>{{ formatDate(source.created_at) }}</TableCell>
                                <TableCell class="text-right">
                                    <Button variant="destructive" size="icon" @click="handleDelete(source)">
                                        <Trash2 class="h-4 w-4" />
                                    </Button>
                                </TableCell>
                            </TableRow>
                        </TableBody>
                    </Table>
                </div>
            </CardContent>
        </Card>

        <!-- Delete Confirmation Dialog -->
        <AlertDialog :open="showDeleteDialog" @update:open="showDeleteDialog = false">
            <AlertDialogContent>
                <AlertDialogHeader>
                    <AlertDialogTitle>Delete Source</AlertDialogTitle>
                    <AlertDialogDescription class="space-y-2">
                        <p>Are you sure you want to delete source "{{ sourceToDelete?.connection.table_name }}"?</p>
                        <p class="font-medium text-muted-foreground">
                            Note: This will only remove the source configuration from LogChef. The actual data in
                            Clickhouse will not
                            be deleted -
                            you'll need to manage the data cleanup in Clickhouse separately.
                        </p>
                    </AlertDialogDescription>
                </AlertDialogHeader>
                <AlertDialogFooter>
                    <AlertDialogCancel @click="showDeleteDialog = false">
                        Cancel
                    </AlertDialogCancel>
                    <AlertDialogAction @click="confirmDelete"
                        class="bg-destructive text-destructive-foreground hover:bg-destructive/90">
                        Delete
                    </AlertDialogAction>
                </AlertDialogFooter>
            </AlertDialogContent>
        </AlertDialog>
    </div>
</template>
