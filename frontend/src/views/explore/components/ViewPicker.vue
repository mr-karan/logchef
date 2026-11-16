<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { Bookmark, Check, ChevronDown, TriangleAlert } from 'lucide-vue-next'
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
import { useExploreStore } from '@/stores/explore'
import { useExploreRuns } from '@/stores/exploreRuns'
import { missingViewColumns } from '@/lib/savedViewLayout'
import ManageViewsDialog from './ManageViewsDialog.vue'
import { useSavedViewsStore, type DisplayMode } from '@/stores/savedViews'
import { useTeamsStore } from '@/stores/teams'

const props = defineProps<{ displayMode: DisplayMode }>()
const emit = defineEmits<{ (e: 'select', viewId: number | null): void; (e: 'reset'): void }>()

const { t } = useI18n()
const { toast } = useToast()
const store = useSavedViewsStore()
const auth = useAuthStore()
const teams = useTeamsStore()
const explore = useExploreStore()
const runs = useExploreRuns()

// An aggregation has its own columns, so only a result of log rows is compared with a view.
const resultColumns = computed(() => {
  const results = runs.results
  return results && !results.aggregation && results.rows.length > 0 ? results.columns.map(column => column.name) : null
})
const missingColumns = computed(() => store.activeView && resultColumns.value ? missingViewColumns(store.activeView.definition, resultColumns.value) : [])
const missingTitle = computed(() => `${t('explore.viewMissingColumns')}: ${missingColumns.value.join(', ')}`)
const manageOpen = ref(false)

// A view's filter is LogchefQL, and its range is relative, as the server requires.
const shownFilter = computed(() => explore.activeMode === 'logchefql' && explore.logchefqlCode.trim() ? explore.logchefqlCode.trim() : null)
const shownTimeRange = computed(() => explore.window.kind === 'relative' && /^\d+[smhdw]$/.test(explore.window.spec) ? explore.window.spec : null)

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
// Filter and time range start unchecked: most layouts apply to any query.
const include = ref({ columns: true, sort: true, displayMode: true, filter: false, timeRange: false })

function openSave() {
  name.value = ''
  description.value = ''
  scope.value = 'personal'
  include.value = { columns: true, sort: true, displayMode: true, filter: false, timeRange: false }
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
      definition: store.newDefinition(include.value, { displayMode: props.displayMode, filter: shownFilter.value, timeRange: shownTimeRange.value }),
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
        <span class="truncate">{{ store.activeView?.name ?? (store.sharedLayout ? t('explore.sharedLayout') : t('explore.noView')) }}</span>
        <span v-if="store.isModified" class="h-1.5 w-1.5 shrink-0 rounded-full bg-primary" :title="t('explore.viewModified')" data-testid="view-modified" />
        <span v-if="missingColumns.length" class="shrink-0 text-amber-600 dark:text-amber-400" :title="missingTitle" :aria-label="missingTitle" role="img" data-testid="view-missing-columns">
          <TriangleAlert class="h-3.5 w-3.5" />
        </span>
        <ChevronDown class="h-3 w-3 shrink-0 opacity-60" />
      </Button>
    </DropdownMenuTrigger>
    <DropdownMenuContent align="end" class="w-64">
      <template v-for="group in groups" :key="group.scope">
        <DropdownMenuLabel class="text-xs text-muted-foreground">{{ t(groupLabel[group.scope]) }}</DropdownMenuLabel>
        <DropdownMenuItem v-for="view in group.views" :key="view.id" class="text-xs" @select="emit('select', view.id)">
          <Check class="h-3.5 w-3.5" :class="view.id === store.activeViewId ? 'opacity-100' : 'opacity-0'" />
          <span class="truncate">{{ view.name }}</span>
          <span v-if="view.id === store.defaultViewId" class="ml-auto shrink-0 text-[10px] text-muted-foreground" data-testid="view-default">{{ t('explore.viewDefault') }}</span>
        </DropdownMenuItem>
      </template>
      <DropdownMenuLabel v-if="groups.length === 0" class="text-xs font-normal text-muted-foreground">{{ t('explore.noSavedViews') }}</DropdownMenuLabel>
      <DropdownMenuSeparator />
      <DropdownMenuItem v-if="store.activeView || store.sharedLayout" class="text-xs" @select="emit('select', null)">{{ t('explore.noView') }}</DropdownMenuItem>
      <DropdownMenuItem class="text-xs" @select="openSave">{{ t('explore.saveAsNewView') }}</DropdownMenuItem>
      <DropdownMenuItem v-if="store.activeView?.can_edit && store.isModified" class="text-xs" @select="update">{{ t('explore.updateView') }}</DropdownMenuItem>
      <DropdownMenuItem v-if="store.activeView" class="text-xs" @select="emit('reset')">{{ t('explore.resetToView') }}</DropdownMenuItem>
      <DropdownMenuItem class="text-xs" @select="manageOpen = true">{{ t('explore.manageViews') }}</DropdownMenuItem>
    </DropdownMenuContent>
  </DropdownMenu>

  <ManageViewsDialog v-if="manageOpen" v-model:open="manageOpen" :result-columns="resultColumns" @select="emit('select', $event)" />

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
          <label class="flex items-center gap-2 text-sm" :class="{ 'opacity-50': shownFilter === null }">
            <Checkbox :model-value="include.filter && shownFilter !== null" :disabled="shownFilter === null" data-testid="view-include-filter" @update:model-value="value => (include.filter = value === true)" />
            {{ t('explore.viewIncludeFilter') }}
          </label>
          <label class="flex items-center gap-2 text-sm" :class="{ 'opacity-50': shownTimeRange === null }">
            <Checkbox :model-value="include.timeRange && shownTimeRange !== null" :disabled="shownTimeRange === null" data-testid="view-include-time-range" @update:model-value="value => (include.timeRange = value === true)" />
            {{ t('explore.viewIncludeTimeRange') }}
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
