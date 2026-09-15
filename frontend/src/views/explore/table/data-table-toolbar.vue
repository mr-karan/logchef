<script setup lang="ts">
import { useI18n } from "vue-i18n";

import type { Table } from './tableFeatures'
import type { QueryStats } from '@/api/explore'

const { t } = useI18n();

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
            <span class='text-foreground'>{{ totalResults.toLocaleString() }} {{ t('ui.logs') }}</span>
        </div>
        <div class="flex items-center gap-1.5">
            <div class="h-1.5 w-1.5 rounded-full bg-primary"></div>
            <span class='text-foreground'>{{ stats?.execution_time_ms?.toFixed(2) ?? 0 }}{{ t('ui.ms') }}</span>
        </div>
    </div>
</template>
