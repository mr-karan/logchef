<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Bookmark, Check, Columns3, Pencil, Search, Star, TriangleAlert, X } from 'lucide-vue-next'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Dialog, DialogContent, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { DropdownMenuItem } from '@/components/ui/dropdown-menu'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import ConfirmDialog from '@/components/ui/ConfirmDialog.vue'
import { EmptyState, LoadingState, RowActionsMenu, SearchInput, SortHeader } from '@/components/layout'
import type { SavedView, SavedViewScope } from '@/api/savedViews'
import { useTableSearchSort } from '@/composables/useTableSearchSort'
import { useToast } from '@/composables/useToast'
import { missingViewColumns } from '@/lib/savedViewLayout'
import { useSavedViewsStore } from '@/stores/savedViews'

// The columns of the result on screen, or null when no result has rows.
const props = defineProps<{ resultColumns: readonly string[] | null }>()
const open = defineModel<boolean>('open', { required: true })
const emit = defineEmits<{ (e: 'select', viewId: number | null): void }>()

const { t } = useI18n()
const { toast } = useToast()
const store = useSavedViewsStore()

const SCOPE_ORDER: Record<SavedViewScope, number> = { personal: 0, team: 1, source: 2 }
const scopeLabel: Record<SavedViewScope, string> = {
  personal: 'explore.viewGroupPersonal',
  team: 'explore.viewGroupTeam',
  source: 'explore.viewGroupSource',
}

const { search, rows, sortKey, sortDir, toggleSort } = useTableSearchSort(() => store.views, {
  searchKeys: ['name', 'description'],
  sortAccessors: {
    name: view => view.name.toLowerCase(),
    scope: view => SCOPE_ORDER[view.scope],
  },
  initialSort: { key: 'name', dir: 'asc' },
  persistKey: 'saved-views',
})

const loading = ref(false)
const renaming = ref<{ id: number; name: string } | null>(null)
watch(open, async isOpen => {
  if (!isOpen) return
  search.value = ''
  renaming.value = null
  loading.value = true
  try {
    await store.refresh()
  } finally {
    loading.value = false
  }
}, { immediate: true })

// Provisioning owns a managed view; the server also reports can_edit false for it.
const editable = (view: SavedView) => view.can_edit && !view.managed
const missing = (view: SavedView) => props.resultColumns ? missingViewColumns(view.definition, props.resultColumns) : []

async function rename(view: SavedView) {
  const name = renaming.value?.name.trim()
  if (!name || name === view.name) {
    renaming.value = null
    return
  }
  if (await store.updateView(view, { name })) renaming.value = null
}

async function duplicate(view: SavedView) {
  const created = await store.saveAsNew({
    name: t('common.copyName', { name: view.name }),
    description: view.description,
    scope: editable(view) ? view.scope : 'personal',
    definition: view.definition,
  })
  if (created) toast({ title: t('explore.viewSaved', { name: created.name }) })
}

async function removeMissing(view: SavedView) {
  const updated = await store.removeMissingColumns(view, missing(view))
  if (updated) toast({ title: t('explore.viewUpdated', { name: updated.name }) })
}

const toDelete = ref<SavedView | null>(null)
const confirmOpen = computed({
  get: () => toDelete.value !== null,
  set: value => { if (!value) toDelete.value = null },
})
async function confirmDelete() {
  const view = toDelete.value
  toDelete.value = null
  if (!view) return
  if (!(await store.remove(view))) return
  toast({ title: t('explore.viewDeleted', { name: view.name }) })
  if (view.id === store.activeViewId) emit('select', null)
}
</script>

<template>
  <Dialog v-model:open="open">
    <DialogContent class="sm:max-w-3xl">
      <DialogHeader>
        <DialogTitle>{{ t('explore.manageViews') }}</DialogTitle>
      </DialogHeader>

      <LoadingState v-if="loading && store.views.length === 0" :label="t('ui.loading')" />
      <EmptyState v-else-if="store.views.length === 0" :icon="Bookmark" :title="t('explore.noSavedViews')" />
      <div v-else class="space-y-3">
        <SearchInput v-model="search" :placeholder="t('explore.searchViews')" />
        <EmptyState v-if="rows.length === 0" :icon="Search" :title="t('access.noResults')" class="rounded-md border" />
        <div v-else class="max-h-[60vh] overflow-y-auto rounded-md border">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>
                  <SortHeader column="name" :sort-key="sortKey" :sort-dir="sortDir" @toggle="toggleSort">{{ t('explore.viewName') }}</SortHeader>
                </TableHead>
                <TableHead class="w-[120px]">
                  <SortHeader column="scope" :sort-key="sortKey" :sort-dir="sortDir" @toggle="toggleSort">{{ t('explore.viewVisibleTo') }}</SortHeader>
                </TableHead>
                <TableHead class="w-[160px] text-right">{{ t('ui.actions') }}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              <TableRow v-for="view in rows" :key="view.id" :data-testid="`manage-view-${view.id}`">
                <TableCell class="whitespace-normal">
                  <form v-if="renaming?.id === view.id" class="flex items-center gap-1" @submit.prevent="rename(view)">
                    <Input v-model="renaming.name" class="h-8" maxlength="200" :aria-label="t('explore.viewName')" autofocus @keydown.esc.stop="renaming = null" />
                    <Button type="submit" variant="ghost" size="icon" class="h-8 w-8" :aria-label="t('ui.save')" :title="t('ui.save')"><Check class="h-4 w-4" /></Button>
                    <Button type="button" variant="ghost" size="icon" class="h-8 w-8" :aria-label="t('ui.cancel')" :title="t('ui.cancel')" @click="renaming = null"><X class="h-4 w-4" /></Button>
                  </form>
                  <div v-else class="flex flex-wrap items-center gap-2">
                    <span class="font-medium">{{ view.name }}</span>
                    <Badge v-if="view.id === store.defaultViewId" variant="secondary" data-testid="manage-view-default">{{ t('explore.viewDefault') }}</Badge>
                    <Badge v-if="view.managed" variant="outline">{{ t('explore.viewManaged') }}</Badge>
                    <span
                      v-if="missing(view).length"
                      class="inline-flex items-center gap-1 text-xs text-amber-600 dark:text-amber-400"
                      data-testid="manage-view-missing"
                    >
                      <TriangleAlert class="h-3.5 w-3.5" />
                      {{ t('explore.viewMissingColumns') }}: {{ missing(view).join(', ') }}
                    </span>
                  </div>
                  <div v-if="view.description && renaming?.id !== view.id" class="text-xs text-muted-foreground">{{ view.description }}</div>
                </TableCell>
                <TableCell class="text-xs text-muted-foreground">{{ t(scopeLabel[view.scope]) }}</TableCell>
                <TableCell class="text-right">
                  <div class="flex items-center justify-end gap-1">
                    <Button
                      v-if="editable(view)"
                      variant="ghost" size="icon" class="h-8 w-8"
                      :aria-label="t('explore.renameView')" :title="t('explore.renameView')"
                      data-testid="manage-view-rename"
                      @click="renaming = { id: view.id, name: view.name }"
                    >
                      <Pencil class="h-4 w-4" />
                    </Button>
                    <Button
                      v-if="view.can_set_default && !view.managed"
                      variant="ghost" size="icon" class="h-8 w-8"
                      :aria-label="view.is_default ? t('explore.clearDefaultView') : t('explore.setDefaultView')"
                      :title="view.is_default ? t('explore.clearDefaultView') : t('explore.setDefaultView')"
                      :aria-pressed="view.is_default"
                      data-testid="manage-view-default-toggle"
                      @click="store.setDefault(view, !view.is_default)"
                    >
                      <Star class="h-4 w-4" :class="view.is_default ? 'fill-current text-amber-500' : ''" />
                    </Button>
                    <RowActionsMenu v-if="editable(view)" @duplicate="duplicate(view)" @delete="toDelete = view">
                      <DropdownMenuItem v-if="missing(view).length" @click="removeMissing(view)">
                        <Columns3 class="mr-2 h-4 w-4" />
                        {{ t('explore.removeMissingColumns') }}
                      </DropdownMenuItem>
                    </RowActionsMenu>
                    <RowActionsMenu v-else @duplicate="duplicate(view)" />
                  </div>
                </TableCell>
              </TableRow>
            </TableBody>
          </Table>
        </div>
      </div>
    </DialogContent>
  </Dialog>

  <ConfirmDialog
    v-model:open="confirmOpen"
    :title="t('explore.deleteViewTitle')"
    :description="toDelete ? t('explore.deleteViewConfirmation', { name: toDelete.name }) : undefined"
    :confirm-text="t('ui.delete')"
    destructive
    @confirm="confirmDelete"
  />
</template>
