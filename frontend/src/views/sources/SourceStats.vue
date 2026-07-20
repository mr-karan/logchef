<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { storeToRefs } from 'pinia'
import { useRoute, useRouter } from 'vue-router'
import { useToast } from '@/composables/useToast'
import ErrorAlert from '@/components/ui/ErrorAlert.vue'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Badge } from '@/components/ui/badge'
import { useSourcesStore } from '@/stores/sources'
import { getSourceTypeLabel } from '@/lib/queryMetadata'
import SourceInspectionOverview from './components/SourceInspectionOverview.vue'
import SourceInspectionActivity from './components/SourceInspectionActivity.vue'
import SourceInspectionSchema from './components/SourceInspectionSchema.vue'

const route = useRoute()
const router = useRouter()
const sourcesStore = useSourcesStore()
const { toast } = useToast()
const { error: storeError } = storeToRefs(sourcesStore)

const selectedSourceId = ref<string>('')

const selectedSource = computed(() => {
  if (!selectedSourceId.value) {
    return null
  }
  return sourcesStore.visibleSources.find(source => source.id === Number(selectedSourceId.value)) ?? null
})

const inspection = computed(() => {
  if (!selectedSourceId.value) {
    return null
  }
  return sourcesStore.getSourceInspectionById(Number(selectedSourceId.value)) ?? null
})

const isLoadingInspection = computed(() =>
  sourcesStore.isLoadingOperation(`getSourceInspection-${selectedSourceId.value}`)
)

const inspectionError = computed(() => {
  const hasInspection = !!inspection.value
  if (!isLoadingInspection.value && storeError.value && selectedSourceId.value && !hasInspection) {
    return 'Failed to load source inspection. Please try again.'
  }
  return null
})

async function fetchSourceInspection() {
  if (!selectedSourceId.value) {
    toast({
      title: 'Error',
      description: 'Please select a source first',
      variant: 'destructive',
    })
    return
  }

  await sourcesStore.getSourceInspection(Number(selectedSourceId.value))
}

function syncRouteSelection(sourceId: string) {
  const nextQuery = { ...route.query }
  if (sourceId) {
    nextQuery.sourceId = sourceId
  } else {
    delete nextQuery.sourceId
  }
  router.replace({ query: nextQuery })
}

onMounted(async () => {
  await sourcesStore.hydrate()

  const sourceIdFromQuery = typeof route.query.sourceId === 'string' ? route.query.sourceId : ''
  if (sourceIdFromQuery) {
    selectedSourceId.value = sourceIdFromQuery
    return
  }

  if (sourcesStore.visibleSources.length > 0) {
    selectedSourceId.value = String(sourcesStore.visibleSources[0].id)
  }
})

watch(
  () => route.query.sourceId,
  (newSourceId) => {
    const nextValue = typeof newSourceId === 'string' ? newSourceId : ''
    if (!nextValue || nextValue === selectedSourceId.value) {
      return
    }
    selectedSourceId.value = nextValue
  },
)

watch(
  () => selectedSourceId.value,
  async (newSourceId, oldSourceId) => {
    if (!newSourceId || newSourceId === oldSourceId) {
      return
    }
    if (route.query.sourceId !== newSourceId) {
      syncRouteSelection(newSourceId)
    }
    await fetchSourceInspection()
  },
)
</script>

<template>
  <div class="space-y-6">
    <Card>
      <CardHeader>
        <CardTitle>Source Inspection</CardTitle>
        <CardDescription>
          Inspect datasource metadata, storage characteristics, activity, and schema across backends.
        </CardDescription>
      </CardHeader>
      <CardContent class="space-y-6">
        <div class="flex flex-col gap-4 lg:flex-row lg:items-end">
          <div class="space-y-2 flex-1">
            <label for="source" class="block text-sm font-medium">Select Source</label>
            <Select v-model="selectedSourceId">
              <SelectTrigger class="w-full">
                <SelectValue placeholder="Select a source" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem
                  v-for="source in sourcesStore.visibleSources"
                  :key="source.id"
                  :value="String(source.id)"
                >
                  {{ source.name }} - {{ getSourceTypeLabel(source) }}
                </SelectItem>
              </SelectContent>
            </Select>
          </div>
          <div class="flex items-center gap-2">
            <Badge v-if="selectedSource" variant="outline">
              {{ getSourceTypeLabel(selectedSource) }}
            </Badge>
            <Button @click="fetchSourceInspection" :disabled="isLoadingInspection">
              <span v-if="isLoadingInspection">Refreshing...</span>
              <span v-else>Refresh Inspection</span>
            </Button>
          </div>
        </div>

        <ErrorAlert
          v-if="inspectionError"
          :error="inspectionError"
          title="Failed to load inspection"
          @retry="fetchSourceInspection"
        />

        <div
          v-else-if="!inspection && !isLoadingInspection"
          class="rounded-lg border border-dashed p-8 text-center text-muted-foreground"
        >
          Select a source to inspect its backend metadata and schema.
        </div>

        <template v-else-if="inspection">
          <SourceInspectionOverview
            :details="inspection.details"
            :storage="inspection.storage"
          />
          <SourceInspectionActivity :activity="inspection.activity" />
          <SourceInspectionSchema
            :source="selectedSource"
            :schema="inspection.schema"
          />
        </template>
      </CardContent>
    </Card>
  </div>
</template>
