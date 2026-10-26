<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { Info } from 'lucide-vue-next'
import { Button } from '@/components/ui/button'
import { PageSection, LoadingState, EmptyState } from '@/components/layout'
import SourceSparkline from '@/components/visualizations/SourceSparkline.vue'
import type { Source, SourceActivity } from '@/api/sources'
import type { SourceActivityError } from '@/stores/sources'

defineProps<{
  source?: Pick<Source, 'source_type'> | null
  activity?: SourceActivity | null
  loading?: boolean
  error?: SourceActivityError | null
}>()
const emit = defineEmits<{ retry: [] }>()
const { t, n } = useI18n()
</script>

<template>
  <LoadingState v-if="loading && !activity" :label="t('sources.loadingRecentActivity')" />
  <p v-else-if="error?.kind === 'unsupported'" class="flex items-start gap-2 text-sm text-muted-foreground">
    <Info class="mt-0.5 size-4 shrink-0" />
    {{ t(source?.source_type === 'clickhouse' ? 'inspection.activitySortKey' : 'sources.activityUnavailable') }}
  </p>
  <div v-else-if="error" class="flex flex-wrap items-center gap-3 text-sm" role="status">
    <p class="text-muted-foreground">{{ error.message }}</p>
    <Button v-if="error.kind === 'transient'" variant="outline" size="sm" :disabled="loading" @click="emit('retry')">{{ t('ui.retry') }}</Button>
  </div>
  <PageSection v-else-if="activity" :title="t('sources.recentActivity')" :description="t('sources.ingestionVolume24h')">
    <template #actions>
      <span class="text-sm tabular-nums text-muted-foreground">{{ n(activity.rows_24h) }} {{ t('sources.rowsLastDay') }}</span>
    </template>
    <EmptyState v-if="activity.rows_24h === 0" :description="t('inspection.noRecentEvents')" class="py-6" />
    <div v-else class="py-2" :aria-label="t('sources.hourlyActivity')" role="img">
      <SourceSparkline :data="activity.hourly_buckets" :height="96" bucket-mode="hourly" />
    </div>
  </PageSection>
  <p v-else class="text-sm text-muted-foreground">{{ t('sources.activityUnavailable') }}</p>
</template>
