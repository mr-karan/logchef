<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import ErrorAlert from '@/components/ui/ErrorAlert.vue'
import { Button } from '@/components/ui/button'
import { PageHeader, EmptyState, LoadingState } from '@/components/layout'
import { RefreshCw } from 'lucide-vue-next'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { isRetryableSourceError, useSourcesStore } from '@/stores/sources'
import { getSourceTypeLabel } from '@/lib/queryMetadata'
import SourceInspectionOverview from './components/SourceInspectionOverview.vue'
import SourceInspectionActivity from './components/SourceInspectionActivity.vue'
import SourceInspectionSchema from './components/SourceInspectionSchema.vue'

const route = useRoute()
const router = useRouter()
const { t } = useI18n()
const sourcesStore = useSourcesStore()

const selectedSourceId = ref<string>('')
const failedInspection = ref<{ sourceId: string; retryable: boolean } | null>(null)

const selectedSource = computed(() => {
  if (!selectedSourceId.value) {
    return null
  }
  return sourcesStore.visibleSources.find(source => source.id === Number(selectedSourceId.value)) ?? null
})

const sourceDescription = computed(() => {
  const source = selectedSource.value
  if (!source) return t('sources.inspectionDescription')
  const connection = source.connection
  const location = 'database' in connection && 'table_name' in connection
    ? `${connection.database}.${connection.table_name}`
    : 'base_url' in connection ? connection.base_url : ''
  return [getSourceTypeLabel(source), location].filter(Boolean).join(' · ')
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

const activity = computed(() => selectedSourceId.value ? sourcesStore.getSourceActivityById(Number(selectedSourceId.value)) : null)
const activityError = computed(() => selectedSourceId.value ? sourcesStore.getSourceActivityErrorById(Number(selectedSourceId.value)) : null)
const isLoadingActivity = computed(() => sourcesStore.isLoadingOperation(`getSourceActivity-${selectedSourceId.value}`))

const inspectionError = computed(() => {
  if (isLoadingInspection.value || inspection.value || failedInspection.value?.sourceId !== selectedSourceId.value) {
    return null
  }
  return t('sources.inspectionLoadFailed')
})

async function loadSourceInspection(sourceId: string, refresh: boolean) {
  failedInspection.value = null
  const result = await sourcesStore.getSourceInspection(Number(sourceId), refresh)
  if (!result.success && sourceId === selectedSourceId.value) {
    failedInspection.value = { sourceId, retryable: isRetryableSourceError(result.error?.error_type) }
  }
}

async function fetchSourceData(refresh = false) {
  if (!selectedSourceId.value) return
  await Promise.allSettled([
    loadSourceInspection(selectedSourceId.value, refresh),
    sourcesStore.getSourceActivity(Number(selectedSourceId.value), refresh),
  ])
}

async function fetchSourceActivity() {
  if (selectedSourceId.value) await sourcesStore.getSourceActivity(Number(selectedSourceId.value), true)
}

async function fetchSourceInspection() {
  if (!selectedSourceId.value) return

  await loadSourceInspection(selectedSourceId.value, true)
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
    await fetchSourceData()
  },
)
</script>

<template>
  <div class="space-y-6">
    <PageHeader :title="selectedSource?.name || t('sources.sourceInspection')" :description="sourceDescription">
      <template #actions>
        <Select v-model="selectedSourceId">
          <SelectTrigger class="w-52" :aria-label="t('sources.selectSource')">
            <SelectValue :placeholder="t('sources.selectSource')" />
          </SelectTrigger>
          <SelectContent>
            <SelectItem v-for="source in sourcesStore.visibleSources" :key="source.id" :value="String(source.id)">
              {{ source.name }}
            </SelectItem>
          </SelectContent>
        </Select>
        <Button variant="outline" :disabled="!selectedSourceId || isLoadingInspection || isLoadingActivity" @click="fetchSourceData(true)">
          <RefreshCw class="size-4" :class="{ 'animate-spin': isLoadingInspection || isLoadingActivity }" />
          {{ isLoadingInspection || isLoadingActivity ? t('sources.refreshing') : t('ui.refresh') }}
        </Button>
      </template>
    </PageHeader>

    <ErrorAlert v-if="inspectionError" :error="inspectionError" :title="t('sources.inspectionLoadFailedTitle')" :retryable="failedInspection?.retryable ?? true" @retry="fetchSourceInspection" />
    <LoadingState v-else-if="!inspection && isLoadingInspection" :label="t('ui.loading')" />
    <EmptyState v-else-if="!inspection" :description="t('sources.selectSourceForInspection')" />

    <SourceInspectionOverview v-if="selectedSourceId" :details="inspection?.details" :storage="inspection?.storage" :schema="inspection?.schema" :activity="activityError ? null : activity">
      <SourceInspectionActivity :source="selectedSource" :activity="activity" :loading="isLoadingActivity" :error="activityError" @retry="fetchSourceActivity" />
    </SourceInspectionOverview>
    <SourceInspectionSchema v-if="inspection" :source="selectedSource" :schema="inspection.schema" />
  </div>
</template>
