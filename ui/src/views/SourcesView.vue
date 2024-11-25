<script setup>
import { ref, onMounted } from 'vue'
import { api } from '@/services/api'
import { useToast } from '@/components/ui/toast/use-toast'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow
} from '@/components/ui/table'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import AddSourceDialog from '@/components/dialogs/AddSourceDialog.vue'
import UpdateTTLDialog from '@/components/dialogs/UpdateTTLDialog.vue'
import DeleteSourceDialog from '@/components/dialogs/DeleteSourceDialog.vue'

const { toast } = useToast()
const showAddDialog = ref(false)
const showUpdateTTLDialog = ref(false)
const showDeleteDialog = ref(false)
const selectedSource = ref(null)
const sources = ref([])
const loading = ref(true)
const error = ref(null)

const fetchSources = async () => {
  try {
    loading.value = true
    const data = await api.fetchSources()
    sources.value = Array.isArray(data) ? data : data.data || []
    error.value = null
  } catch (err) {
    error.value = err.message
    console.error('Error fetching sources:', err)
    sources.value = []
  } finally {
    loading.value = false
  }
}

onMounted(() => {
  fetchSources()
})

const getStatusColor = (status) => {
  switch (status) {
    case 'connected':
      return 'bg-green-500'
    case 'error':
      return 'bg-red-500'
    default:
      return 'bg-yellow-500'
  }
}
</script>

<template>
  <div class="container py-8">
    <div class="mb-8 flex items-center justify-between">
      <div>
        <h1 class="text-3xl font-semibold tracking-tight">Sources</h1>
        <p class="text-sm text-muted-foreground mt-1">Manage your log sources and their configurations.</p>
      </div>
      <Button @click="showAddDialog = true" size="sm">
        Add Source
      </Button>
    </div>

    <div v-if="error" class="mb-4 p-4 text-red-500 bg-red-50 rounded-md">
      {{ error }}
    </div>

    <div v-if="loading && !sources.length" class="text-center py-8">
      Loading sources...
    </div>

    <div v-else-if="sources.length === 0" class="text-center py-8">
      <div class="max-w-md mx-auto space-y-4">
        <h3 class="text-lg font-semibold">No sources found</h3>
        <p class="text-muted-foreground">
          Get started by adding your first log source. Click the "Add Source" button above to begin collecting and analyzing your logs.
        </p>
        <Button @click="showAddDialog = true" variant="outline" class="mt-4">
          Add Your First Source
        </Button>
      </div>
    </div>

    <div v-else class="rounded-md border">
      <Table>
      <TableHeader>
        <TableRow>
          <TableHead>Name</TableHead>
          <TableHead>Type</TableHead>
          <TableHead>Status</TableHead>
          <TableHead>Last Synced</TableHead>
          <TableHead>Actions</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        <TableRow v-for="source in sources" :key="source.ID">
          <TableCell>{{ source.Name }}</TableCell>
          <TableCell>
            <Badge>{{ source.SchemaType }}</Badge>
          </TableCell>
          <TableCell>
            <Badge>Connected</Badge>
          </TableCell>
          <TableCell>{{ new Date(source.UpdatedAt).toLocaleString() }}</TableCell>
          <TableCell class="space-x-2">
            <Button 
              variant="outline" 
              size="sm"
              @click="() => {
                selectedSource = source;
                showUpdateTTLDialog = true;
              }"
            >
              Update TTL
            </Button>
            <Button 
              variant="destructive" 
              size="sm"
              @click="() => {
                selectedSource = source;
                showDeleteDialog = true;
              }"
            >
              Delete
            </Button>
          </TableCell>
        </TableRow>
      </TableBody>
    </Table>
    </div>

    <AddSourceDialog 
      v-model:open="showAddDialog" 
      @source-added="fetchSources"
    />
    
    <UpdateTTLDialog
      v-if="selectedSource"
      v-model:open="showUpdateTTLDialog"
      :source-id="selectedSource.ID"
      :source-name="selectedSource.Name"
      @ttl-updated="fetchSources"
    />

    <DeleteSourceDialog
      v-if="selectedSource"
      v-model:open="showDeleteDialog"
      :source="selectedSource"
      @confirm="async () => {
        try {
          await api.deleteSource(selectedSource.ID)
          toast({
            title: 'Success',
            description: 'Source deleted successfully'
          });
          showDeleteDialog = false;
          await fetchSources();
        } catch (err) {
          toast({
            title: 'Error',
            description: err.message,
            variant: 'destructive'
          });
        }
      }"
    />
  </div>
</template>
