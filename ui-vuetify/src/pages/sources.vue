<template>
  <div>
    <v-toolbar flat color="background" border="b" class="px-4">
      <v-toolbar-title class="text-h6 font-weight-regular">
        Sources
      </v-toolbar-title>
      <v-spacer />
      <v-btn v-if="activeTab === 'list'" color="primary" prepend-icon="mdi-plus" @click="activeTab = 'add'">
        Add Source
      </v-btn>
      <v-btn v-else variant="text" prepend-icon="mdi-arrow-left" @click="activeTab = 'list'">
        Back to Sources
      </v-btn>
    </v-toolbar>

    <v-window v-model="activeTab">
      <!-- List Sources Tab -->
      <v-window-item value="list">
        <v-container fluid class="pa-4">
          <!-- Loading state -->
          <div v-if="loading" class="text-center py-8">
            <v-progress-circular indeterminate />
          </div>

          <!-- Empty state -->
          <div v-else-if="!sources || sources.length === 0" class="text-center py-12">
            <v-icon size="64" color="grey" class="mb-4">
              mdi-database-off
            </v-icon>
            <div class="text-h6 text-medium-emphasis mb-2">
              No Sources Added
            </div>
            <div class="text-body-1 text-medium-emphasis mb-4">
              Add your first Clickhouse data source to start exploring logs
            </div>
            <v-btn color="primary" prepend-icon="mdi-plus" @click="activeTab = 'add'">
              Add Your First Source
            </v-btn>
          </div>

          <!-- Sources table -->
          <div v-else>
            <v-data-table :headers="headers" :items="sources" :loading="loading">
              <template #[`item.is_connected`]="{ item }">
                <v-chip :color="item.is_connected ? 'success' : 'error'" size="small">
                  {{ item.is_connected ? 'Connected' : 'Disconnected' }}
                </v-chip>
              </template>

              <template #[`item.created_at`]="{ item }">
                {{ new Date(item.created_at).toLocaleString('en-US', {
                  year: 'numeric',
                  month: 'short',
                  day: 'numeric',
                  hour: '2-digit',
                  minute: '2-digit',
                  hour12: false
                }) }}
              </template>

              <template #[`item.actions`]="{ item }">
                <v-btn icon="mdi-delete" variant="text" color="error" size="small" @click="confirmDelete(item)" />
              </template>
            </v-data-table>
          </div>
        </v-container>
      </v-window-item>

      <!-- Add Source Tab -->
      <v-window-item value="add">
        <v-container class="pa-4">
          <v-row>
            <v-col cols="12" md="8" lg="7">
              <div class="text-h6 font-weight-regular mb-6">
                Add New Source
              </div>
              <v-form ref="form" @submit.prevent="handleSubmit">
                <!-- Source Type Section -->
                <v-card variant="outlined" class="mb-6">
                  <v-card-title class="text-subtitle-1 px-4 pt-4 pb-0">
                    Source Type
                  </v-card-title>
                  <v-card-text class="px-4 pt-2">
                    <v-select v-model="formData.schema_type" label="Schema Type" :items="[
                      { title: 'Managed (OTEL)', value: 'managed' },
                      { title: 'Unmanaged (Custom)', value: 'unmanaged' }
                    ]" required :rules="[v => !!v || 'Schema type is required']" variant="outlined"
                      density="comfortable" prepend-inner-icon="mdi-database-cog"
                      hint="Choose how the schema will be managed" persistent-hint />
                  </v-card-text>
                </v-card>

                <!-- Connection Details Section -->
                <v-card variant="outlined" class="mb-6">
                  <v-card-title class="text-subtitle-1 px-4 pt-4 pb-0">
                    Connection Details
                  </v-card-title>
                  <v-card-text class="px-4 pt-2">
                    <v-text-field v-model="formData.connection.host" label="Host" required :rules="[
                      v => !!v || 'Host is required',
                      v => /^[^:]+:\d+$/.test(v) || 'Host must be in format host:port (e.g. localhost:9000)'
                    ]" variant="outlined" density="comfortable" prepend-inner-icon="mdi-server"
                      hint="Format: host:port (e.g. localhost:9000)" persistent-hint placeholder="localhost:9000"
                      class="mb-3" />

                    <v-row>
                      <v-col cols="12" sm="6">
                        <v-text-field v-model="formData.connection.database" label="Database" required
                          :rules="[v => !!v || 'Database is required']" variant="outlined" density="comfortable"
                          prepend-inner-icon="mdi-database" hint="Name of the Clickhouse database" persistent-hint />
                      </v-col>

                      <v-col cols="12" sm="6">
                        <v-text-field v-model="formData.connection.table_name" label="Table Name" required
                          :rules="[v => !!v || 'Table name is required']" variant="outlined" density="comfortable"
                          prepend-inner-icon="mdi-table" hint="Name of the table to store logs" persistent-hint />
                      </v-col>
                    </v-row>
                  </v-card-text>
                </v-card>

                <!-- Authentication Section -->
                <v-card variant="outlined" class="mb-6">
                  <v-card-title class="text-subtitle-1 px-4 pt-4 pb-0">
                    Authentication
                  </v-card-title>
                  <v-card-text class="px-4 pt-2">
                    <v-switch v-model="requiresAuth" label="Requires Authentication" color="primary" hide-details
                      class="mb-3" />

                    <v-slide-y-transition>
                      <v-row v-if="requiresAuth">
                        <v-col cols="12" sm="6">
                          <v-text-field v-model="formData.connection.username" label="Username" required
                            :rules="[v => !requiresAuth || !!v || 'Username is required when authentication is enabled']"
                            variant="outlined" density="comfortable" prepend-inner-icon="mdi-account"
                            hint="Database username" persistent-hint />
                        </v-col>

                        <v-col cols="12" sm="6">
                          <v-text-field v-model="formData.connection.password" label="Password"
                            :type="showPassword ? 'text' : 'password'" required
                            :rules="[v => !requiresAuth || !!v || 'Password is required when authentication is enabled']"
                            variant="outlined" density="comfortable" prepend-inner-icon="mdi-key"
                            :append-icon="showPassword ? 'mdi-eye' : 'mdi-eye-off'" hint="Database password"
                            persistent-hint @click:append="showPassword = !showPassword" />
                        </v-col>
                      </v-row>
                    </v-slide-y-transition>
                  </v-card-text>
                </v-card>

                <!-- Additional Settings Section -->
                <v-card variant="outlined" class="mb-6">
                  <v-card-title class="text-subtitle-1 px-4 pt-4 pb-0">
                    Additional Settings
                  </v-card-title>
                  <v-card-text class="px-4 pt-2">
                    <v-text-field v-model.number="formData.ttl_days" label="TTL (days)" type="number" required :rules="[
                      v => !!v || 'TTL is required',
                      v => v >= 0 || 'TTL must be positive'
                    ]" variant="outlined" density="comfortable" prepend-inner-icon="mdi-timer-outline"
                      hint="Number of days to retain the logs" persistent-hint class="mb-3" />

                    <v-textarea v-model="formData.description" label="Description" rows="3" variant="outlined"
                      density="comfortable" prepend-inner-icon="mdi-text" hint="Optional description for this source"
                      persistent-hint />
                  </v-card-text>
                </v-card>

                <div class="d-flex justify-end">
                  <v-btn color="primary" size="large" :loading="creating" prepend-icon="mdi-plus" @click="handleSubmit">
                    Add Source
                  </v-btn>
                </div>
              </v-form>
            </v-col>
          </v-row>
        </v-container>
      </v-window-item>
    </v-window>

    <!-- Delete confirmation -->
    <v-dialog v-model="deleteDialog" max-width="400">
      <v-card>
        <v-card-title>Delete Source</v-card-title>
        <v-card-text>
          Are you sure you want to delete this source? This action cannot be undone.
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn color="grey-darken-1" variant="text" @click="deleteDialog = false">
            Cancel
          </v-btn>
          <v-btn color="error" :loading="deleting" @click="handleDelete">
            Delete
          </v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>
  </div>
</template>

<script lang="ts" setup>
import { ref, onMounted, inject, watch } from 'vue'
import sourcesApi, { type Source, type CreateSourcePayload } from '@/api/sources'
import type { VForm } from 'vuetify/components'

const loading = ref(false)
const creating = ref(false)
const deleting = ref(false)
const activeTab = ref('list')
const deleteDialog = ref(false)
const sources = ref<Source[]>([])
const sourceToDelete = ref<Source | null>(null)
const form = ref<VForm | null>(null)
const notification = inject('notification')
const requiresAuth = ref(false)
const showPassword = ref(false)

const formData = ref<CreateSourcePayload>({
  schema_type: 'managed',
  connection: {
    host: 'localhost:9000',  // Default with port included
    username: '',
    password: '',
    database: '',
    table_name: ''
  },
  description: '',
  ttl_days: 7
})

const headers = [
  { title: 'Table Name', key: 'connection.table_name' },
  { title: 'Schema Type', key: 'schema_type' },
  { title: 'Database', key: 'connection.database' },
  { title: 'Status', key: 'is_connected' },
  { title: 'Created', key: 'created_at' },
  { title: 'Actions', key: 'actions', sortable: false }
]

async function loadSources() {
  loading.value = true
  try {
    const result = await sourcesApi.listSources()
    sources.value = result || []
  } catch (error) {
    console.error('Failed to load sources:', error)
    if (error instanceof Error) {
      notification?.error(`Failed to load sources: ${error.message}`)
    }
    sources.value = [] // Ensure we have an array even on error
  } finally {
    loading.value = false
  }
}

async function handleSubmit() {
  if (!form.value?.validate()) return

  creating.value = true
  try {
    const source = await sourcesApi.createSource(formData.value)
    activeTab.value = 'list'
    await loadSources()
    notification?.success(`Source "${source.connection.table_name}" created successfully`)
    // Reset form
    resetForm()
  } catch (error) {
    console.error('Failed to create source:', error)
    if (error instanceof Error) {
      notification?.error(`Failed to create source: ${error.message}`)
    }
  } finally {
    creating.value = false
  }
}

function confirmDelete(source: Source) {
  sourceToDelete.value = source
  deleteDialog.value = true
}

async function handleDelete() {
  if (!sourceToDelete.value) return

  deleting.value = true
  try {
    const name = sourceToDelete.value.connection.table_name
    await sourcesApi.deleteSource(sourceToDelete.value.id)
    deleteDialog.value = false
    await loadSources()
    notification?.success(`Source "${name}" deleted successfully`)
  } catch (error) {
    console.error('Failed to delete source:', error)
    if (error instanceof Error) {
      notification?.error(`Failed to delete source: ${error.message}`)
    }
  } finally {
    deleting.value = false
  }
}

// Watch for requiresAuth changes to clear credentials when disabled
watch(requiresAuth, (newValue) => {
  if (!newValue) {
    formData.value.connection.username = ''
    formData.value.connection.password = ''
  }
})

// Reset form
function resetForm() {
  formData.value = {
    schema_type: 'managed',
    connection: {
      host: 'localhost:9000',  // Default with port included
      username: '',
      password: '',
      database: '',
      table_name: ''
    },
    description: '',
    ttl_days: 7
  }
  requiresAuth.value = false
  showPassword.value = false
}

onMounted(() => {
  loadSources()
})
</script>
