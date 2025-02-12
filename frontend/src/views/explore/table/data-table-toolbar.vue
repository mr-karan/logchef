<script setup lang="ts">
import type { Table } from '@tanstack/vue-table'
import type { QueryStats } from '@/api/explore'

interface Props {
    table: Table<Record<string, any>>
    stats: QueryStats
    totalResults: number
}

defineProps<Props>()
</script>

<template>
    <div class="flex items-center gap-3 text-xs">
        <div class="flex items-center gap-1.5">
            <div class="h-1.5 w-1.5 rounded-full bg-primary"></div>
            <span class="text-foreground">{{ totalResults.toLocaleString() }} logs</span>
        </div>
        <div class="flex items-center gap-1.5">
            <div class="h-1.5 w-1.5 rounded-full bg-primary"></div>
            <span class="text-foreground">{{ stats?.execution_time_ms?.toFixed(2) ?? 0 }}ms</span>
        </div>
    </div>
</template>
