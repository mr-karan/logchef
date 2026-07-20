<script setup lang="ts">
import { computed } from 'vue'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import type { InspectionDetail, InspectionMetric } from '@/api/sources'

const props = defineProps<{
  details?: InspectionDetail[] | null
  storage?: InspectionMetric[] | null
}>()

const visibleDetails = computed(() => props.details ?? [])
const visibleStorage = computed(() => props.storage ?? [])
</script>

<template>
  <div class="grid gap-6 lg:grid-cols-[minmax(0,1.35fr)_minmax(0,1fr)]">
    <Card v-if="visibleDetails.length > 0">
      <CardHeader>
        <CardTitle>Source Details</CardTitle>
        <CardDescription>
          Provider-specific metadata and connection identity for this datasource.
        </CardDescription>
      </CardHeader>
      <CardContent class="space-y-3">
        <div
          v-for="detail in visibleDetails"
          :key="detail.key || detail.label"
          class="rounded-md border bg-muted/30 p-3"
        >
          <div class="text-xs font-medium uppercase tracking-wider text-muted-foreground">
            {{ detail.label }}
          </div>
          <pre
            v-if="detail.multiline"
            :class="[
              'mt-1 whitespace-pre-wrap break-all text-sm',
              detail.monospace ? 'font-mono' : 'font-medium',
            ]"
          >{{ detail.value }}</pre>
          <div
            v-else
            :class="[
              'mt-1 break-all text-sm',
              detail.monospace ? 'font-mono' : 'font-medium',
            ]"
          >
            {{ detail.value }}
          </div>
        </div>
      </CardContent>
    </Card>

    <Card v-if="visibleStorage.length > 0">
      <CardHeader>
        <CardTitle>Storage</CardTitle>
        <CardDescription>
          Physical storage and compression metrics available from the datasource.
        </CardDescription>
      </CardHeader>
      <CardContent>
        <div class="grid gap-3 sm:grid-cols-2">
          <div
            v-for="metric in visibleStorage"
            :key="metric.key || metric.label"
            class="rounded-md border bg-muted/30 p-3"
          >
            <div class="text-xs font-medium uppercase tracking-wider text-muted-foreground">
              {{ metric.label }}
            </div>
            <div class="mt-1 text-xl font-semibold">
              {{ metric.value }}
            </div>
            <div v-if="metric.hint" class="mt-1 text-xs text-muted-foreground">
              {{ metric.hint }}
            </div>
          </div>
        </div>
      </CardContent>
    </Card>
  </div>
</template>
