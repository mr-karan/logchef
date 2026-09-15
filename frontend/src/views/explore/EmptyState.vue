<script setup lang="ts">
import { useI18n } from "vue-i18n";

import { Search } from 'lucide-vue-next'

const { t } = useI18n();
</script>

<template>
  <div class="flex flex-col items-center justify-center h-full text-center space-y-4">
    <div class="rounded-full bg-muted p-4">
      <Search class="h-8 w-8 text-muted-foreground" />
    </div>
    <h3 class='text-lg font-medium'>{{ t('ui.noLogsFound') }}</h3>
    <p class="text-sm text-muted-foreground max-w-[400px]">
      {{ t('ui.tryAdjustingYourSearchQueryTimeRangeOrSelectedSourceToFind') }}
    </p>
  </div>
</template>
