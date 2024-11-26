<script setup>
import { ref, onMounted, onUnmounted } from 'vue'
import { api } from '@/services/api'
import { useToast } from 'primevue/usetoast'
import { useConfirm } from "primevue/useconfirm"
import DataTable from 'primevue/datatable'
import Column from 'primevue/column'
import Button from 'primevue/button'
import Tag from 'primevue/tag'
import ConfirmDialog from 'primevue/confirmdialog'
import Dialog from 'primevue/dialog'
import InputText from 'primevue/inputtext'
import Dropdown from 'primevue/dropdown'
import Toast from 'primevue/toast'

const toast = useToast()
const confirm = useConfirm()
const showAddDialog = ref(false)
const formData = ref({
  name: '',
  schema_type: '',
  dsn: '',
  ttl_days: 90
})

const schemaTypes = [
  { value: 'http', label: 'HTTP Logs' },
  { value: 'application', label: 'Application Logs' }
]

const handleSubmit = async () => {
  try {
    const response = await fetch('/api/sources', {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json'
      },
      body: JSON.stringify(formData.value)
    })

    const data = await response.json()

    if (!response.ok) {
      throw new Error(data.message || 'Failed to create source')
    }

    toast.add({
      severity: 'success',
      summary: 'Success',
      detail: 'Source created successfully',
      life: 3000
    })

    showAddDialog.value = false
    await fetchSources()

    // Reset form
    formData.value = {
      name: '',
      schema_type: '',
      dsn: '',
      ttl_days: 90
    }
  } catch (error) {
    toast.add({
      severity: 'error',
      summary: 'Error',
      detail: error.message,
      life: 3000
    })
    console.error('Error creating source:', error)
  }
}
const confirmDelete = (source) => {
  confirm.require({
    message: `Are you sure you want to delete source "${source.Name}"?`,
    header: 'Delete Confirmation',
    icon: 'pi pi-exclamation-triangle',
    accept: async () => {
      try {
        await api.deleteSource(source.ID)
        toast.add({
          severity: 'success',
          summary: 'Success',
          detail: 'Source deleted successfully',
          life: 3000
        });
        await fetchSources();
      } catch (err) {
        toast.add({
          severity: 'error',
          summary: 'Error',
          detail: err.message,
          life: 3000
        });
      }
    }
  });
};
const selectedSource = ref(null)
const sources = ref([])
const loading = ref(true)
const error = ref(null)
const filters = ref({})

const fetchSources = async () => {
  try {
    loading.value = true
    sources.value = await api.fetchSources()
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
  <div class="p-8 max-w-7xl mx-auto">
    <div class="mb-8 flex items-center justify-between">
      <div>
        <h1 class="text-2xl font-semibold text-gray-900">Sources</h1>
        <p class="text-sm text-gray-500 mt-1">Manage your log sources and their configurations.</p>
      </div>
      <Button @click="showAddDialog = true" severity="primary" raised>
        <i class="pi pi-plus mr-2"></i>
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

    <div v-else class="bg-white rounded-lg shadow-sm border border-gray-200">
      <DataTable
        :value="sources"
        :paginator="true"
        :rows="10"
        :rowsPerPageOptions="[10, 20, 50]"
        tableStyle="min-width: 50rem"
        class="p-datatable-sm"
        responsiveLayout="scroll"
        stripedRows
        :pt="{
          wrapper: 'bg-white',
          table: 'bg-white',
          bodyRow: 'hover:bg-gray-50',
          headerRow: 'bg-gray-50 text-gray-700',
          bodyCell: 'text-gray-700',
          headerCell: 'text-gray-700 font-semibold'
        }"
        v-model:filters="filters"
        filterDisplay="menu"
        :globalFilterFields="['Name', 'SchemaType']"
      >
        <Column field="Name" header="Name" sortable></Column>
        <Column field="SchemaType" header="Type" sortable>
          <template #body="slotProps">
            <Tag :value="slotProps.data.SchemaType" 
                 severity="info"
                 rounded
            />
          </template>
        </Column>
        <Column header="Status">
          <template #body>
            <Tag value="Connected"
                 severity="success"
                 rounded
            />
          </template>
        </Column>
        <Column field="UpdatedAt" header="Last Synced" sortable>
          <template #body="slotProps">
            {{ new Date(slotProps.data.UpdatedAt).toLocaleString() }}
          </template>
        </Column>
        <Column header="Actions">
          <template #body="slotProps">
            <div class="flex gap-2">
              <button 
                @click="confirmDelete(slotProps.data)"
                class="p-2 text-gray-500 hover:text-red-600 rounded-md hover:bg-red-50 transition-colors"
                title="Delete Source"
              >
                <i class="pi pi-trash"></i>
              </button>
            </div>
          </template>
        </Column>
      </DataTable>
    </div>

    <Dialog
      v-model:visible="showAddDialog"
      modal
      header="Add New Source"
      :style="{ width: '500px' }"
      class="p-fluid"
      :modal-style="{ padding: '2rem' }"
    >
      <form @submit.prevent="handleSubmit" class="flex flex-col gap-4">
        <div class="flex flex-col gap-2">
          <label for="name">Name</label>
          <InputText
            id="name"
            v-model="formData.name"
            placeholder="my-nginx-logs"
            required
            :minlength="4"
            :maxlength="30"
          />
        </div>

        <div class="flex flex-col gap-2">
          <label for="schema-type">Schema Type</label>
          <Dropdown
            id="schema-type"
            v-model="formData.schema_type"
            :options="schemaTypes"
            optionLabel="label"
            optionValue="value"
            placeholder="Select schema type"
            required
          />
        </div>

        <div class="flex flex-col gap-2">
          <label for="dsn">DSN</label>
          <InputText
            id="dsn"
            v-model="formData.dsn"
            placeholder="clickhouse://localhost:9000/logs"
            required
          />
        </div>

        <div class="flex flex-col gap-2">
          <label for="ttl">TTL (days)</label>
          <InputText
            id="ttl"
            type="number"
            v-model="formData.ttl_days"
          />
        </div>

        <div class="flex justify-end gap-2 mt-4">
          <Button
            type="button"
            label="Cancel"
            severity="secondary"
            text
            @click="showAddDialog = false"
          />
          <Button
            type="submit"
            label="Create Source"
          />
        </div>
      </form>
    </Dialog>


    <ConfirmDialog />
    <Toast />
  </div>
</template>
