<script setup lang="ts">
import { computed } from 'vue'
import { RefreshCw } from 'lucide-vue-next'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import SourceSparkline from '@/components/visualizations/SourceSparkline.vue'
import type { SourceActivity } from '@/api/sources'
import { formatDate } from '@/utils/format'

const props = defineProps<{ activity?: SourceActivity | null; loading?: boolean; error?: string | null }>()
const emit = defineEmits<{ retry: [] }>()
const latest = computed(() => props.activity?.latest_ts ? formatDate(props.activity.latest_ts) : 'Unavailable')
</script>
<template>
  <Card>
    <CardHeader class="flex-row items-start justify-between space-y-0">
      <div><CardTitle>Recent activity</CardTitle><CardDescription>Ingestion volume from the last 24 hours.</CardDescription></div>
      <Button v-if="activity && !loading && !error" variant="ghost" size="icon" title="Refresh recent activity" @click="emit('retry')"><RefreshCw class="size-4" /></Button>
    </CardHeader>
    <CardContent v-if="loading" class="py-8 text-sm text-muted-foreground">Loading recent activity...</CardContent>
    <CardContent v-else-if="error" class="space-y-3"><p class="text-sm text-muted-foreground">{{ error }}</p><Button size="sm" @click="emit('retry')">Retry</Button></CardContent>
    <CardContent v-else-if="activity" class="space-y-6">
      <div class="grid gap-3 md:grid-cols-3"><div class="rounded-md border p-3"><div class="text-xs text-muted-foreground">Rows last 1h</div><div class="text-2xl font-semibold">{{ activity.rows_1h.toLocaleString() }}</div></div><div class="rounded-md border p-3"><div class="text-xs text-muted-foreground">Rows last 24h</div><div class="text-2xl font-semibold">{{ activity.rows_24h.toLocaleString() }}</div></div><div class="rounded-md border p-3"><div class="text-xs text-muted-foreground">Latest event in 24h</div><div class="text-sm font-medium">{{ latest }}</div></div></div>
      <div><div class="mb-2 text-sm font-medium">Hourly activity</div><SourceSparkline :data="activity.hourly_buckets" :height="64" bucket-mode="hourly" /></div>
    </CardContent>
    <CardContent v-else class="py-8 text-sm text-muted-foreground">Recent activity unavailable</CardContent>
  </Card>
</template>
