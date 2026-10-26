<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { PageSection } from '@/components/layout'
import type { InspectionDetail, InspectionMetric, SourceActivity, SourceSchemaInspection } from '@/api/sources'
import { formatDate } from '@/utils/format'

const props = defineProps<{
  details?: InspectionDetail[] | null
  storage?: InspectionMetric[] | null
  schema?: SourceSchemaInspection | null
  activity?: SourceActivity | null
}>()
const { t, n, locale } = useI18n()

const detailLabels: Record<string, string> = {
  host: 'sources.connectionHost', database: 'sources.database', table: 'sources.tableName',
  timestamp_field: 'sources.timestampField', severity_field: 'sources.severityField',
  engine: 'inspection.engine', base_url: 'sources.baseURL', tenant: 'sources.tenantScope',
  scope: 'sources.immutableScopeQuery', storage_scope: 'inspection.storageScope',
}
const visibleDetails = computed(() => {
  const details = (props.details ?? []).filter(detail => detail.key !== 'backend').map(detail => ({
    ...detail,
    label: detail.key && detailLabels[detail.key] ? t(detailLabels[detail.key]) : detail.label,
    value: detail.key === 'storage_scope' ? t('inspection.localReplica') : detail.value,
  }))
  if (props.schema?.sort_keys?.length) details.push({ key: 'sort_keys', label: t('sources.sortKeys'), value: props.schema.sort_keys.join(', '), monospace: true })
  if (props.schema?.ttl) details.push({ key: 'ttl', label: t('sources.ttl'), value: props.schema.ttl, monospace: true })
  return details
})
const summary = computed(() => {
  const storage = new Map((props.storage ?? []).map(metric => [metric.key, metric]))
  const metrics: InspectionMetric[] = []
  const rows = storage.get('rows')
  if (rows) metrics.push({ key: 'rows', label: t('sources.rows'), value: /^\d+$/.test(rows.value) ? BigInt(rows.value).toLocaleString(locale.value) : rows.value })
  const compressed = storage.get('compressed')
  if (compressed) metrics.push({
    key: 'compressed', label: t('sources.compressed'), value: compressed.value,
    hint: [storage.get('compression_ratio')?.value, storage.get('uncompressed') ? `${storage.get('uncompressed')?.value} ${t('sources.uncompressed')}` : ''].filter(Boolean).join(' · '),
  })
  const parts = storage.get('parts')
  if (parts) metrics.push({ key: 'parts', label: t('inspection.parts'), value: /^\d+$/.test(parts.value) ? BigInt(parts.value).toLocaleString(locale.value) : parts.value })
  if (props.schema?.ttl) {
    const interval = props.schema.ttl.match(/toInterval(Month|Day)\((\d+)\)/i)
    const value = interval ? t(interval[1].toLowerCase() === 'month' ? 'inspection.months' : 'inspection.days', { count: Number(interval[2]) }, Number(interval[2])) : props.schema.ttl
    metrics.push({ key: 'retention', label: t('sources.dataRetention'), value })
  }
  if (props.activity) {
    metrics.push({ key: 'rows_1h', label: t('sources.rowsLastHour'), value: n(props.activity.rows_1h) })
    metrics.push({ key: 'latest', label: t('sources.latestEvent24h'), value: props.activity.latest_ts ? formatDate(props.activity.latest_ts) : '–' })
  }
  return metrics
})
</script>

<template>
  <div class="space-y-6">
    <dl v-if="summary.length" class="grid grid-cols-2 gap-x-6 gap-y-4 border-y py-4 lg:flex lg:flex-wrap lg:justify-between">
      <div v-for="metric in summary" :key="metric.key" class="min-w-0 lg:max-w-56">
        <dt class="text-xs font-medium text-muted-foreground">{{ metric.label }}</dt>
        <dd :class="['mt-1 break-words font-semibold tabular-nums', metric.key === 'latest' || metric.key === 'retention' ? 'text-sm leading-7' : 'text-xl tracking-tight']">{{ metric.value }}</dd>
        <dd v-if="metric.hint" class="mt-1 text-xs text-muted-foreground">{{ metric.hint }}</dd>
      </div>
    </dl>
    <slot />
    <PageSection v-if="visibleDetails.length" :title="t('sources.sourceDetails')">
      <dl class="grid gap-x-10 gap-y-3 md:grid-cols-2">
        <div v-for="detail in visibleDetails" :key="detail.key || detail.label" class="grid grid-cols-[8rem_minmax(0,1fr)] items-baseline gap-3 text-sm">
          <dt class="text-muted-foreground">{{ detail.label }}</dt>
          <dd :class="['whitespace-pre-wrap break-words [overflow-wrap:anywhere]', detail.monospace ? 'font-mono text-xs' : 'font-medium']">{{ detail.value }}</dd>
        </div>
      </dl>
    </PageSection>
  </div>
</template>
