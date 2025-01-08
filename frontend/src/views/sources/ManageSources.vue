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
  TableCaption,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { useToast } from '@/components/ui/toast/use-toast'

const router = useRouter()
const { toast } = useToast()
const sources = ref<Source[]>([])
const isLoading = ref(true)
const showDeleteDialog = ref(false)
const sourceToDelete = ref<Source | null>(null)

const loadSources = async () => {
  try {
    const response = await sourcesApi.getSources()
    sources.value = response.data
  } catch (error) {
    toast({
      title: 'Error',
      description: 'Failed to fetch sources',
      variant: 'destructive',
    })
  } finally {
    isLoading.value = false
  }
}

const confirmDelete = async () => {
  if (!sourceToDelete.value) return
  
  try {
    await sourcesApi.deleteSource(sourceToDelete.value.ID)
    toast({
      title: 'Success',
      description: 'Source deleted successfully',
    })
    await loadSources()
  } catch (error) {
    toast({
      title: 'Error',
      description: 'Failed to delete source',
      variant: 'destructive',
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
        <CardTitle>Manage Sources</CardTitle>
        <CardDescription>
          View and manage your log sources
        </CardDescription>
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
            <TableCaption>A list of your configured log sources</TableCaption>
            <TableHeader>
              <TableRow>
                <TableHead>Name</TableHead>
                <TableHead>Schema Type</TableHead>
                <TableHead>DSN</TableHead>
                <TableHead>Created At</TableHead>
                <TableHead class="text-right">Actions</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              <TableRow v-for="source in sources" :key="source.ID">
                <TableCell class="font-medium">{{ source.Name }}</TableCell>
                <TableCell>{{ source.SchemaType }}</TableCell>
                <TableCell>{{ source.DSN }}</TableCell>
                <TableCell>{{ formatDate(source.CreatedAt) }}</TableCell>
                <TableCell class="text-right">
                  <Button 
                    variant="destructive" 
                    size="icon"
                    @click="handleDelete(source)"
                  >
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
          <AlertDialogDescription>
            Are you sure you want to delete source "{{ sourceToDelete?.Name }}"?
            This action cannot be undone.
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel @click="showDeleteDialog = false">
            Cancel
          </AlertDialogCancel>
          <AlertDialogAction @click="confirmDelete" class="bg-destructive text-destructive-foreground hover:bg-destructive/90">
            Delete
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  </div>
</template>
