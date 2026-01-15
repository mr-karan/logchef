<script setup lang="ts">
import { onMounted, ref, computed } from 'vue'
import { storeToRefs } from 'pinia'
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
import { Pencil, Eye, EyeOff, Save, X } from 'lucide-vue-next'
import { Input } from '@/components/ui/input'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import {
  Tabs,
  TabsContent,
  TabsList,
  TabsTrigger,
} from '@/components/ui/tabs'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import { Switch } from '@/components/ui/switch'
import { useSettingsStore } from '@/stores/settings'
import type { SystemSetting, UpdateSettingRequest } from '@/api/settings'
import { settingsApi } from '@/api/settings'
import { toast } from 'vue-sonner'

const settingsStore = useSettingsStore()
const { isLoading } = storeToRefs(settingsStore)

const showEditDialog = ref(false)
const showDeleteDialog = ref(false)
const settingToEdit = ref<SystemSetting | null>(null)
const settingToDelete = ref<SystemSetting | null>(null)
const showSensitiveValues = ref<Record<string, boolean>>({})
const currentTab = ref('alerts')

const editForm = ref<UpdateSettingRequest>({
  value: '',
  value_type: 'string',
  category: 'alerts',
  description: '',
  is_sensitive: false
})

// Get settings by category
const alertsSettings = computed(() => settingsStore.getSettingsByCategory('alerts'))
const aiSettings = computed(() => settingsStore.getSettingsByCategory('ai'))
const authSettings = computed(() => settingsStore.getSettingsByCategory('auth'))
const serverSettings = computed(() => settingsStore.getSettingsByCategory('server'))

const loadSettings = async () => {
  await settingsStore.loadSettings()
}

const handleEdit = (setting: SystemSetting) => {
  settingToEdit.value = setting
  editForm.value = {
    value: setting.value,
    value_type: setting.value_type,
    category: setting.category,
    description: setting.description || '',
    is_sensitive: setting.is_sensitive
  }
  showEditDialog.value = true
}

const confirmEdit = async () => {
  if (!settingToEdit.value) return

  // Ensure value is always a string (type="number" input converts to number)
  const requestData = {
    ...editForm.value,
    value: String(editForm.value.value)
  }

  await settingsStore.updateSetting(settingToEdit.value.key, requestData)

  showEditDialog.value = false
  settingToEdit.value = null
}

const confirmDelete = async () => {
  if (!settingToDelete.value) return

  await settingsStore.deleteSetting(settingToDelete.value.key)

  showDeleteDialog.value = false
  settingToDelete.value = null
}

const toggleShowValue = (key: string) => {
  showSensitiveValues.value[key] = !showSensitiveValues.value[key]
}

const getDisplayValue = (setting: SystemSetting) => {
  if (setting.is_sensitive && !showSensitiveValues.value[setting.key]) {
    return setting.masked_value || '********'
  }
  return setting.value
}

const getCategoryDescription = (category: string) => {
  switch (category) {
    case 'alerts':
      return 'Configure alerting and notification settings'
    case 'ai':
      return 'Configure AI-assisted SQL generation settings'
    case 'auth':
      return 'Configure authentication and session settings'
    case 'server':
      return 'Configure server and application settings'
    default:
      return ''
  }
}

const formatKey = (key: string) => {
  // Remove category prefix and convert to title case
  const parts = key.split('.')
  const name = parts[parts.length - 1]
  const acronyms = ['url', 'api', 'ai', 'tls', 'id', 'smtp']
  return name.split('_').map(word => {
    if (acronyms.includes(word.toLowerCase())) {
      return word.toUpperCase()
    }
    return word.charAt(0).toUpperCase() + word.slice(1)
  }).join(' ')
}

onMounted(() => {
  loadSettings()
})
</script>

<template>
  <div class="space-y-6">
    <Card>
      <CardHeader>
        <CardTitle>System Settings</CardTitle>
        <CardDescription>
          Manage runtime configuration settings for LogChef
        </CardDescription>
      </CardHeader>
      <CardContent>
        <div v-if="isLoading" class="text-center py-4">
          Loading settings...
        </div>
        <Tabs v-else v-model="currentTab" class="w-full">
          <TabsList>
            <TabsTrigger value="alerts">Alerts</TabsTrigger>
            <TabsTrigger value="ai">AI</TabsTrigger>
            <TabsTrigger value="auth">Authentication</TabsTrigger>
            <TabsTrigger value="server">Server</TabsTrigger>
          </TabsList>

          <!-- Alerts Tab -->
          <TabsContent value="alerts" class="space-y-4">
            <div class="text-sm text-muted-foreground">
              {{ getCategoryDescription('alerts') }}
            </div>
            <div class="rounded-md border">
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Setting</TableHead>
                    <TableHead>Value</TableHead>
                    <TableHead>Type</TableHead>
                    <TableHead>Description</TableHead>
                    <TableHead></TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  <TableRow v-for="setting in alertsSettings" :key="setting.key">
                    <TableCell class="font-medium">{{ formatKey(setting.key) }}</TableCell>
                    <TableCell>
                      <div class="flex items-center gap-2">
                        <span class="font-mono text-sm">{{ getDisplayValue(setting) }}</span>
                        <Button
                          v-if="setting.is_sensitive"
                          variant="ghost"
                          size="icon"
                          class="h-6 w-6"
                          @click="toggleShowValue(setting.key)"
                        >
                          <Eye v-if="!showSensitiveValues[setting.key]" class="h-3 w-3" />
                          <EyeOff v-else class="h-3 w-3" />
                        </Button>
                      </div>
                    </TableCell>
                    <TableCell>
                      <code class="text-xs bg-muted px-1 py-0.5 rounded">{{ setting.value_type }}</code>
                    </TableCell>
                    <TableCell class="text-sm text-muted-foreground">{{ setting.description || '-' }}</TableCell>
                    <TableCell>
                      <div class="flex items-center gap-2 justify-end">
                        <Button variant="outline" size="icon" @click="handleEdit(setting)">
                          <Pencil class="h-4 w-4" />
                        </Button>
                      </div>
                    </TableCell>
                  </TableRow>
                </TableBody>
              </Table>
            </div>
          </TabsContent>

          <!-- AI Tab -->
          <TabsContent value="ai" class="space-y-4">
            <div class="text-sm text-muted-foreground">
              {{ getCategoryDescription('ai') }}
            </div>
            <div class="rounded-md border">
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Setting</TableHead>
                    <TableHead>Value</TableHead>
                    <TableHead>Type</TableHead>
                    <TableHead>Description</TableHead>
                    <TableHead></TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  <TableRow v-for="setting in aiSettings" :key="setting.key">
                    <TableCell class="font-medium">{{ formatKey(setting.key) }}</TableCell>
                    <TableCell>
                      <div class="flex items-center gap-2">
                        <span class="font-mono text-sm">{{ getDisplayValue(setting) }}</span>
                        <Button
                          v-if="setting.is_sensitive"
                          variant="ghost"
                          size="icon"
                          class="h-6 w-6"
                          @click="toggleShowValue(setting.key)"
                        >
                          <Eye v-if="!showSensitiveValues[setting.key]" class="h-3 w-3" />
                          <EyeOff v-else class="h-3 w-3" />
                        </Button>
                      </div>
                    </TableCell>
                    <TableCell>
                      <code class="text-xs bg-muted px-1 py-0.5 rounded">{{ setting.value_type }}</code>
                    </TableCell>
                    <TableCell class="text-sm text-muted-foreground">{{ setting.description || '-' }}</TableCell>
                    <TableCell>
                      <div class="flex items-center gap-2 justify-end">
                        <Button variant="outline" size="icon" @click="handleEdit(setting)">
                          <Pencil class="h-4 w-4" />
                        </Button>
                      </div>
                    </TableCell>
                  </TableRow>
                </TableBody>
              </Table>
            </div>
          </TabsContent>

          <!-- Auth Tab -->
          <TabsContent value="auth" class="space-y-4">
            <div class="text-sm text-muted-foreground">
              {{ getCategoryDescription('auth') }}
            </div>
            <div class="rounded-md border">
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Setting</TableHead>
                    <TableHead>Value</TableHead>
                    <TableHead>Type</TableHead>
                    <TableHead>Description</TableHead>
                    <TableHead></TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  <TableRow v-for="setting in authSettings" :key="setting.key">
                    <TableCell class="font-medium">{{ formatKey(setting.key) }}</TableCell>
                    <TableCell>
                      <div class="flex items-center gap-2">
                        <span class="font-mono text-sm">{{ getDisplayValue(setting) }}</span>
                        <Button
                          v-if="setting.is_sensitive"
                          variant="ghost"
                          size="icon"
                          class="h-6 w-6"
                          @click="toggleShowValue(setting.key)"
                        >
                          <Eye v-if="!showSensitiveValues[setting.key]" class="h-3 w-3" />
                          <EyeOff v-else class="h-3 w-3" />
                        </Button>
                      </div>
                    </TableCell>
                    <TableCell>
                      <code class="text-xs bg-muted px-1 py-0.5 rounded">{{ setting.value_type }}</code>
                    </TableCell>
                    <TableCell class="text-sm text-muted-foreground">{{ setting.description || '-' }}</TableCell>
                    <TableCell>
                      <div class="flex items-center gap-2 justify-end">
                        <Button variant="outline" size="icon" @click="handleEdit(setting)">
                          <Pencil class="h-4 w-4" />
                        </Button>
                      </div>
                    </TableCell>
                  </TableRow>
                </TableBody>
              </Table>
            </div>
          </TabsContent>

          <!-- Server Tab -->
          <TabsContent value="server" class="space-y-4">
            <div class="text-sm text-muted-foreground">
              {{ getCategoryDescription('server') }}
            </div>
            <div class="rounded-md border">
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Setting</TableHead>
                    <TableHead>Value</TableHead>
                    <TableHead>Type</TableHead>
                    <TableHead>Description</TableHead>
                    <TableHead></TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  <TableRow v-for="setting in serverSettings" :key="setting.key">
                    <TableCell class="font-medium">{{ formatKey(setting.key) }}</TableCell>
                    <TableCell>
                      <div class="flex items-center gap-2">
                        <span class="font-mono text-sm">{{ getDisplayValue(setting) }}</span>
                        <Button
                          v-if="setting.is_sensitive"
                          variant="ghost"
                          size="icon"
                          class="h-6 w-6"
                          @click="toggleShowValue(setting.key)"
                        >
                          <Eye v-if="!showSensitiveValues[setting.key]" class="h-3 w-3" />
                          <EyeOff v-else class="h-3 w-3" />
                        </Button>
                      </div>
                    </TableCell>
                    <TableCell>
                      <code class="text-xs bg-muted px-1 py-0.5 rounded">{{ setting.value_type }}</code>
                    </TableCell>
                    <TableCell class="text-sm text-muted-foreground">{{ setting.description || '-' }}</TableCell>
                    <TableCell>
                      <div class="flex items-center gap-2 justify-end">
                        <Button variant="outline" size="icon" @click="handleEdit(setting)">
                          <Pencil class="h-4 w-4" />
                        </Button>
                      </div>
                    </TableCell>
                  </TableRow>
                </TableBody>
              </Table>
            </div>
          </TabsContent>
        </Tabs>
      </CardContent>
    </Card>

    <!-- Edit Dialog -->
    <Dialog :open="showEditDialog" @update:open="showEditDialog = false">
      <DialogContent class="sm:max-w-[500px]">
        <DialogHeader>
          <DialogTitle>Edit Setting</DialogTitle>
          <DialogDescription>
            Update the value for {{ settingToEdit?.key }}
          </DialogDescription>
        </DialogHeader>
        <div class="grid gap-4 py-4">
          <div class="grid gap-2">
            <Label for="value">Value</Label>
            <Input
              v-if="editForm.value_type === 'boolean'"
              id="value"
              v-model="editForm.value"
              type="text"
              placeholder="true or false"
            />
            <Input
              v-else-if="editForm.value_type === 'number'"
              id="value"
              v-model="editForm.value"
              type="number"
            />
            <Textarea
              v-else
              id="value"
              v-model="editForm.value"
              rows="3"
            />
            <p class="text-xs text-muted-foreground">
              Type: <code class="bg-muted px-1 py-0.5 rounded">{{ editForm.value_type }}</code>
            </p>
          </div>
          <div class="grid gap-2">
            <Label for="description">Description (optional)</Label>
            <Textarea
              id="description"
              v-model="editForm.description"
              rows="2"
              placeholder="Enter a description for this setting"
            />
          </div>
          <div class="flex items-center space-x-2">
            <Switch
              id="sensitive"
              :checked="editForm.is_sensitive"
              @update:checked="editForm.is_sensitive = $event"
            />
            <Label for="sensitive" class="text-sm font-normal">
              Mark as sensitive (will be masked in responses)
            </Label>
          </div>
        </div>
        <DialogFooter>
          <Button variant="outline" @click="showEditDialog = false">
            <X class="mr-2 h-4 w-4" />
            Cancel
          </Button>
          <Button @click="confirmEdit">
            <Save class="mr-2 h-4 w-4" />
            Save changes
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>

    <!-- Delete Confirmation Dialog -->
    <AlertDialog :open="showDeleteDialog" @update:open="showDeleteDialog = false">
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>Delete Setting</AlertDialogTitle>
          <AlertDialogDescription class="space-y-2">
            <p>Are you sure you want to delete setting "{{ settingToDelete?.key }}"?</p>
            <p class="font-medium text-destructive">
              This action cannot be undone. The system will fall back to the default value from config.toml.
            </p>
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel @click="showDeleteDialog = false">
            Cancel
          </AlertDialogCancel>
          <AlertDialogAction
            @click="confirmDelete"
            class="bg-destructive text-destructive-foreground hover:bg-destructive/90"
          >
            Delete
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  </div>
</template>
