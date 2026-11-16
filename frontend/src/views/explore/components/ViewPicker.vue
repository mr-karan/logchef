<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { Bookmark, Check, ChevronDown } from 'lucide-vue-next'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import type { SavedViewScope } from '@/api/savedViews'
import { useToast } from '@/composables/useToast'
import { useAuthStore } from '@/stores/auth'
import { useSavedViewsStore, type DisplayMode } from '@/stores/savedViews'
import { useTeamsStore } from '@/stores/teams'

const props = defineProps<{ displayMode: DisplayMode }>()
const emit = defineEmits<{ (e: 'select', viewId: number | null): void }>()

const { t } = useI18n()
const { toast } = useToast()
const store = useSavedViewsStore()
const auth = useAuthStore()
const teams = useTeamsStore()

const scopes: SavedViewScope[] = ['personal', 'team', 'source']
const groups = computed(() => scopes
  .map(scope => ({ scope, views: store.views.filter(view => view.scope === scope) }))
  .filter(group => group.views.length > 0))

const isGlobalAdmin = computed(() => auth.user?.role === 'admin')
const teamRole = computed(() => teams.userTeams.find(team => team.id === teams.currentTeamId)?.role)
const creatableScopes = computed(() => scopes.filter(scope =>
  scope === 'personal'
  || (scope === 'team' && (isGlobalAdmin.value || teamRole.value === 'admin' || teamRole.value === 'editor'))
  || (scope === 'source' && isGlobalAdmin.value)))

const saveOpen = ref(false)
const saving = ref(false)
const name = ref('')
const description = ref('')
const scope = ref<SavedViewScope>('personal')
const include = ref({ columns: true, sort: true, displayMode: true })

function openSave() {
  name.value = ''
  description.value = ''
  scope.value = 'personal'
  include.value = { columns: true, sort: true, displayMode: true }
  saveOpen.value = true
}

async function save() {
  if (!name.value.trim() || saving.value) return
  saving.value = true
  try {
    const created = await store.saveAsNew({
      name: name.value.trim(),
      description: description.value.trim(),
      scope: scope.value,
      definition: store.newDefinition(include.value, props.displayMode),
    })
    if (!created) return
    saveOpen.value = false
    emit('select', created.id)
    toast({ title: t('explore.viewSaved', { name: created.name }) })
  } finally {
    saving.value = false
  }
}

async function update() {
  const updated = await store.updateActive()
  if (updated) toast({ title: t('explore.viewUpdated', { name: updated.name }) })
}

const scopeLabel: Record<SavedViewScope, string> = {
  personal: 'explore.viewScopePersonal',
  team: 'explore.viewScopeTeam',
  source: 'explore.viewScopeSource',
}
const groupLabel: Record<SavedViewScope, string> = {
  personal: 'explore.viewGroupPersonal',
  team: 'explore.viewGroupTeam',
  source: 'explore.viewGroupSource',
}
</script>

<template>
  <DropdownMenu>
    <DropdownMenuTrigger as-child>
      <Button variant="ghost" size="sm" class="h-7 gap-1.5 px-2 text-xs max-w-[14rem]" :aria-label="t('explore.viewPicker')" data-testid="view-picker">
        <Bookmark class="h-3.5 w-3.5 shrink-0" />
        <span class="truncate">{{ store.activeView?.name ?? t('explore.noView') }}</span>
        <span v-if="store.isModified" class="h-1.5 w-1.5 shrink-0 rounded-full bg-primary" :title="t('explore.viewModified')" data-testid="view-modified" />
        <ChevronDown class="h-3 w-3 shrink-0 opacity-60" />
      </Button>
    </DropdownMenuTrigger>
    <DropdownMenuContent align="end" class="w-64">
      <template v-for="group in groups" :key="group.scope">
        <DropdownMenuLabel class="text-xs text-muted-foreground">{{ t(groupLabel[group.scope]) }}</DropdownMenuLabel>
        <DropdownMenuItem v-for="view in group.views" :key="view.id" class="text-xs" @select="emit('select', view.id)">
          <Check class="h-3.5 w-3.5" :class="view.id === store.activeViewId ? 'opacity-100' : 'opacity-0'" />
          <span class="truncate">{{ view.name }}</span>
        </DropdownMenuItem>
      </template>
      <DropdownMenuLabel v-if="groups.length === 0" class="text-xs font-normal text-muted-foreground">{{ t('explore.noSavedViews') }}</DropdownMenuLabel>
      <DropdownMenuSeparator />
      <DropdownMenuItem v-if="store.activeView" class="text-xs" @select="emit('select', null)">{{ t('explore.noView') }}</DropdownMenuItem>
      <DropdownMenuItem class="text-xs" @select="openSave">{{ t('explore.saveAsNewView') }}</DropdownMenuItem>
      <DropdownMenuItem v-if="store.activeView?.can_edit && store.isModified" class="text-xs" @select="update">{{ t('explore.updateView') }}</DropdownMenuItem>
      <DropdownMenuItem v-if="store.activeView" class="text-xs" @select="store.resetActive()">{{ t('explore.resetToView') }}</DropdownMenuItem>
    </DropdownMenuContent>
  </DropdownMenu>

  <Dialog v-model:open="saveOpen">
    <DialogContent class="sm:max-w-md">
      <DialogHeader>
        <DialogTitle>{{ t('explore.saveAsNewView') }}</DialogTitle>
      </DialogHeader>
      <form class="grid gap-4" @submit.prevent="save">
        <div class="grid gap-1.5">
          <Label for="view-name">{{ t('explore.viewName') }}</Label>
          <Input id="view-name" v-model="name" maxlength="200" required />
        </div>
        <div class="grid gap-1.5">
          <Label for="view-description">{{ t('explore.viewDescription') }}</Label>
          <Textarea id="view-description" v-model="description" maxlength="2000" rows="2" />
        </div>
        <div class="grid gap-1.5">
          <Label for="view-scope">{{ t('explore.viewVisibleTo') }}</Label>
          <NativeSelect id="view-scope" v-model="scope">
            <NativeSelectOption v-for="option in creatableScopes" :key="option" :value="option">{{ t(scopeLabel[option]) }}</NativeSelectOption>
          </NativeSelect>
        </div>
        <fieldset class="grid gap-2">
          <legend class="mb-1 text-sm font-medium">{{ t('explore.viewInclude') }}</legend>
          <label class="flex items-center gap-2 text-sm">
            <Checkbox :model-value="include.columns" @update:model-value="value => (include.columns = value === true)" />
            {{ t('explore.viewIncludeColumns') }}
          </label>
          <label class="flex items-center gap-2 text-sm">
            <Checkbox :model-value="include.sort" @update:model-value="value => (include.sort = value === true)" />
            {{ t('explore.viewIncludeSort') }}
          </label>
          <label class="flex items-center gap-2 text-sm">
            <Checkbox :model-value="include.displayMode" @update:model-value="value => (include.displayMode = value === true)" />
            {{ t('explore.viewIncludeDisplayMode') }}
          </label>
        </fieldset>
        <DialogFooter>
          <Button type="button" variant="outline" @click="saveOpen = false">{{ t('ui.cancel') }}</Button>
          <Button type="submit" :disabled="!name.trim() || saving">{{ t('ui.save') }}</Button>
        </DialogFooter>
      </form>
    </DialogContent>
  </Dialog>
</template>
