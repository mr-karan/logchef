<script setup lang="ts">
import { computed } from 'vue'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import SourceSparkline from '@/components/visualizations/SourceSparkline.vue'
import type { SourceActivity } from '@/api/sources'
import { formatDate } from '@/utils/format'

const props = defineProps<{
  activity?: SourceActivity | null
}>()

const summary = computed(() => {
  if (!props.activity) {
    return null
  }

  return {
    rows1h: props.activity.rows_1h ?? 0,
    rows24h: props.activity.rows_24h ?? 0,
    rows7d: props.activity.rows_7d ?? 0,
    latest: props.activity.latest_ts ? formatDate(props.activity.latest_ts) : 'Unavailable',
  }
})
</script>

<template>
  <Card v-if="props.activity">
    <CardHeader>
      <CardTitle>Activity</CardTitle>
      <CardDescription>
        Recent ingestion volume and trend lines for this datasource.
      </CardDescription>
    </CardHeader>
    <CardContent class="space-y-6">
      <div class="grid gap-3 md:grid-cols-2 xl:grid-cols-4">
        <div class="rounded-md border bg-muted/30 p-3">
          <div class="text-xs font-medium uppercase tracking-wider text-muted-foreground">Rows last 1h</div>
          <div class="mt-1 text-2xl font-semibold">{{ summary?.rows1h?.toLocaleString() || '0' }}</div>
        </div>
        <div class="rounded-md border bg-muted/30 p-3">
          <div class="text-xs font-medium uppercase tracking-wider text-muted-foreground">Rows last 24h</div>
          <div class="mt-1 text-2xl font-semibold">{{ summary?.rows24h?.toLocaleString() || '0' }}</div>
        </div>
        <div class="rounded-md border bg-muted/30 p-3">
          <div class="text-xs font-medium uppercase tracking-wider text-muted-foreground">Rows last 7d</div>
          <div class="mt-1 text-2xl font-semibold">{{ summary?.rows7d?.toLocaleString() || '0' }}</div>
        </div>
        <div class="rounded-md border bg-muted/30 p-3">
          <div class="text-xs font-medium uppercase tracking-wider text-muted-foreground">Latest ingest</div>
          <div class="mt-1 text-sm font-medium">{{ summary?.latest }}</div>
        </div>
      </div>

      <div class="grid gap-6 lg:grid-cols-2">
        <div class="space-y-2">
          <div class="text-sm font-medium">Last 24 hours</div>
          <div class="text-xs text-muted-foreground">Hourly buckets</div>
          <SourceSparkline
            :data="props.activity.hourly_buckets"
            :height="64"
            bucket-mode="hourly"
          />
        </div>
        <div class="space-y-2">
          <div class="text-sm font-medium">Last 7 days</div>
          <div class="text-xs text-muted-foreground">Daily buckets</div>
          <SourceSparkline
            :data="props.activity.daily_buckets"
            :height="64"
            bucket-mode="daily"
            color="#16a34a"
          />
        </div>
      </div>
    </CardContent>
  </Card>
</template>
