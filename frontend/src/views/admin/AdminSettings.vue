<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { onMounted, ref, computed } from 'vue'
import { storeToRefs } from 'pinia'
import { Button } from '@/components/ui/button'
import { PageHeader, LoadingState } from '@/components/layout'
import ConfirmDialog from '@/components/ui/ConfirmDialog.vue'
import { Pencil, Eye, EyeOff, Save, X, Mail, Webhook, Loader2 } from 'lucide-vue-next'
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
import { useAuthStore } from '@/stores/auth'
import { useMetaStore } from '@/stores/meta'
import type { SystemSetting, UpdateSettingRequest } from '@/api/settings'
import { useToast } from '@/composables/useToast'

const { toast } = useToast()
const { t } = useI18n()

const authStore = useAuthStore()
const metaStore = useMetaStore()

const settingsStore = useSettingsStore()
const { isLoading } = storeToRefs(settingsStore)

const showEditDialog = ref(false)
const showDeleteDialog = ref(false)
const showTestEmailDialog = ref(false)
const showTestWebhookDialog = ref(false)
const settingToEdit = ref<SystemSetting | null>(null)
const settingToDelete = ref<SystemSetting | null>(null)
const showSensitiveValues = ref<Record<string, boolean>>({})
// Default to Alerts unless the server has disabled alerting; in that case
// fall back to AI so an admin isn't landed on a hidden tab.
const currentTab = ref(metaStore.alertsEnabled ? 'alerts' : 'ai')
const testEmailRecipient = ref('')
const testWebhookUrl = ref('')
const isTestingEmail = ref(false)
const isTestingWebhook = ref(false)

const editForm = ref<UpdateSettingRequest>({
  value: '',
  value_type: 'string',
  category: 'alerts',
  description: '',
  is_sensitive: false
})

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
      return t('admin.categoryAlerts')
    case 'ai':
      return t('admin.categoryAI')
    case 'auth':
      return t('admin.categoryAuth')
    case 'server':
      return t('admin.categoryServer')
    default:
      return ''
  }
}

const formatKey = (key: string) => {
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

const openTestEmailDialog = () => {
  testEmailRecipient.value = authStore.user?.email || ''
  showTestEmailDialog.value = true
}

const openTestWebhookDialog = () => {
  testWebhookUrl.value = ''
  showTestWebhookDialog.value = true
}

const handleTestEmail = async () => {
  isTestingEmail.value = true
  try {
    const result = await settingsStore.testEmail(testEmailRecipient.value || undefined)
    if (result?.success) {
      showTestEmailDialog.value = false
    }
  } finally {
    isTestingEmail.value = false
  }
}

const handleTestWebhook = async () => {
  if (!testWebhookUrl.value.trim()) {
    toast({ title: t('admin.webhookURLRequired'), variant: 'destructive' })
    return
  }
  isTestingWebhook.value = true
  try {
    const result = await settingsStore.testWebhook(testWebhookUrl.value)
    if (result?.success) {
      showTestWebhookDialog.value = false
    }
  } finally {
    isTestingWebhook.value = false
  }
}

onMounted(() => {
  loadSettings()
})
</script>

<template>
  <div class="space-y-6">
    <PageHeader :title="t('routes.systemSettings')" :description="t('admin.settingsDescription')" />

    <LoadingState v-if="isLoading" :label="t('admin.loadingSettings')" />
    <Tabs v-else v-model="currentTab" class="w-full">
          <TabsList>
            <TabsTrigger v-if="metaStore.alertsEnabled" value="alerts">{{ t('ui.alerts') }}</TabsTrigger>
            <TabsTrigger value="ai">{{ t('admin.ai') }}</TabsTrigger>
            <TabsTrigger value="auth">{{ t('sources.authentication') }}</TabsTrigger>
            <TabsTrigger value="server">{{ t('admin.server') }}</TabsTrigger>
          </TabsList>

          <!-- Alerts Tab -->
          <TabsContent v-if="metaStore.alertsEnabled" value="alerts" class="space-y-4">
            <div class="flex items-center justify-between">
              <div class="text-sm text-muted-foreground">
                {{ getCategoryDescription('alerts') }}
              </div>
              <div class="flex gap-2">
                <Button variant="outline" size="sm" @click="openTestEmailDialog">
                  <Mail class="mr-2 h-4 w-4" />
                  {{ t('admin.testEmail') }}
                </Button>
                <Button variant="outline" size="sm" @click="openTestWebhookDialog">
                  <Webhook class="mr-2 h-4 w-4" />
                  {{ t('admin.testWebhook') }}
                </Button>
              </div>
            </div>
            <div class="rounded-md border">
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>{{ t('admin.setting') }}</TableHead>
                    <TableHead>{{ t('ui.value2') }}</TableHead>
                    <TableHead>{{ t('admin.type') }}</TableHead>
                    <TableHead>{{ t('ui.description') }}</TableHead>
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
                    <TableHead>{{ t('admin.setting') }}</TableHead>
                    <TableHead>{{ t('ui.value2') }}</TableHead>
                    <TableHead>{{ t('admin.type') }}</TableHead>
                    <TableHead>{{ t('ui.description') }}</TableHead>
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
                    <TableHead>{{ t('admin.setting') }}</TableHead>
                    <TableHead>{{ t('ui.value2') }}</TableHead>
                    <TableHead>{{ t('admin.type') }}</TableHead>
                    <TableHead>{{ t('ui.description') }}</TableHead>
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
                    <TableHead>{{ t('admin.setting') }}</TableHead>
                    <TableHead>{{ t('ui.value2') }}</TableHead>
                    <TableHead>{{ t('admin.type') }}</TableHead>
                    <TableHead>{{ t('ui.description') }}</TableHead>
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

    <!-- Edit Dialog -->
    <Dialog :open="showEditDialog" @update:open="showEditDialog = false">
      <DialogContent class="sm:max-w-[500px]">
        <DialogHeader>
          <DialogTitle>{{ t('admin.editSetting') }}</DialogTitle>
          <DialogDescription>
            {{ t('admin.updateSettingValue', { key: settingToEdit?.key ?? '' }) }}
          </DialogDescription>
        </DialogHeader>
        <div class="grid gap-4 py-4">
          <div class="grid gap-2">
            <Label for="value">{{ t('ui.value2') }}</Label>
            <Input
              v-if="editForm.value_type === 'boolean'"
              id="value"
              v-model="editForm.value"
              type="text"
              :placeholder="t('admin.booleanExample')"
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
              {{ t('admin.settingType') }} <code class="bg-muted px-1 py-0.5 rounded">{{ editForm.value_type }}</code>
            </p>
          </div>
          <div class="grid gap-2">
            <Label for="description">{{ t('admin.optionalDescription') }}</Label>
            <Textarea
              id="description"
              v-model="editForm.description"
              rows="2"
              :placeholder="t('admin.enterSettingDescription')"
            />
          </div>
          <div class="flex items-center space-x-2">
            <Switch
              id="sensitive"
              :model-value="editForm.is_sensitive"
              @update:model-value="editForm.is_sensitive = $event"
            />
            <Label for="sensitive" class="text-sm font-normal">
              {{ t('admin.markSensitive') }}
            </Label>
          </div>
        </div>
        <DialogFooter>
          <Button variant="outline" @click="showEditDialog = false">
            <X class="mr-2 h-4 w-4" />
            {{ t('ui.cancel') }}
          </Button>
          <Button @click="confirmEdit">
            <Save class="mr-2 h-4 w-4" />
            {{ t('ui.saveChanges2') }}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>

    <ConfirmDialog
      :open="showDeleteDialog"
      :title="t('admin.deleteSettingTitle')"
      :description="settingToDelete ? t('admin.deleteSettingConfirmation', { key: settingToDelete.key }) : undefined"
      :confirm-text="t('ui.delete')"
      destructive
      @update:open="(v) => { if (!v) { showDeleteDialog = false; settingToDelete = null } }"
      @confirm="confirmDelete"
    />

    <!-- Test Email Dialog -->
    <Dialog :open="showTestEmailDialog" @update:open="showTestEmailDialog = false">
      <DialogContent class="sm:max-w-[425px]">
        <DialogHeader>
          <DialogTitle>{{ t('admin.testEmailConfiguration') }}</DialogTitle>
          <DialogDescription>
            {{ t('admin.testEmailDescription') }}
          </DialogDescription>
        </DialogHeader>
        <div class="grid gap-4 py-4">
          <div class="grid gap-2">
            <Label for="test-email">{{ t('admin.recipientEmail') }}</Label>
            <Input
              id="test-email"
              v-model="testEmailRecipient"
              type="email"
              :placeholder="t('admin.enterEmailAddress')"
            />
            <p class="text-xs text-muted-foreground">
              {{ t('admin.emailRecipientHint') }}
            </p>
          </div>
        </div>
        <DialogFooter>
          <Button variant="outline" @click="showTestEmailDialog = false" :disabled="isTestingEmail">
            {{ t('ui.cancel') }}
          </Button>
          <Button @click="handleTestEmail" :disabled="isTestingEmail">
            <Loader2 v-if="isTestingEmail" class="mr-2 h-4 w-4 animate-spin" />
            <Mail v-else class="mr-2 h-4 w-4" />
            {{ t('admin.sendTestEmail') }}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>

    <!-- Test Webhook Dialog -->
    <Dialog :open="showTestWebhookDialog" @update:open="showTestWebhookDialog = false">
      <DialogContent class="sm:max-w-[425px]">
        <DialogHeader>
          <DialogTitle>{{ t('admin.testWebhookConfiguration') }}</DialogTitle>
          <DialogDescription>
            {{ t('admin.testWebhookDescription') }}
          </DialogDescription>
        </DialogHeader>
        <div class="grid gap-4 py-4">
          <div class="grid gap-2">
            <Label for="test-webhook">{{ t('admin.webhookURL') }}</Label>
            <Input
              id="test-webhook"
              v-model="testWebhookUrl"
              type="url"
              placeholder="https://example.com/webhook"
            />
            <p class="text-xs text-muted-foreground">
              {{ t('admin.webhookURLHint') }}
            </p>
          </div>
        </div>
        <DialogFooter>
          <Button variant="outline" @click="showTestWebhookDialog = false" :disabled="isTestingWebhook">
            {{ t('ui.cancel') }}
          </Button>
          <Button @click="handleTestWebhook" :disabled="isTestingWebhook || !testWebhookUrl.trim()">
            <Loader2 v-if="isTestingWebhook" class="mr-2 h-4 w-4 animate-spin" />
            <Webhook v-else class="mr-2 h-4 w-4" />
            {{ t('admin.sendTestWebhook') }}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  </div>
</template>
