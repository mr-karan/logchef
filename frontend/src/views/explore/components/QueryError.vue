<script setup lang="ts">
import { useI18n } from "vue-i18n";

import { computed } from 'vue'
import { useExploreStore } from '@/stores/explore'

const { t } = useI18n();

interface Props {
  queryError?: string
}

const props = defineProps<Props>()
const exploreStore = useExploreStore()

// Combined error display from props and store
const displayError = computed(() => props.queryError || exploreStore.error?.message)
</script>

<template>
  <div v-if="displayError" class="mt-2 text-sm text-destructive bg-destructive/10 p-2 rounded">
    <div class='font-medium'>{{ t('ui.queryError') }}</div>
    <div>{{ displayError }}</div>
    <div v-if="displayError.includes('Missing boolean operator')"
      class="mt-1.5 pt-1.5 border-t border-destructive/20 text-xs">
      <div class='font-medium'>{{ t('ui.hint') }}</div>
      <i18n-t keypath="explore.conditionHint" tag="div" scope="global">
        <template #and><code class="bg-muted px-1 rounded">and</code></template>
        <template #or><code class="bg-muted px-1 rounded">or</code></template>
      </i18n-t>
      <div class="mt-1">{{ t('ui.example') }} <code class='bg-muted px-1 rounded'>level="error" and
    service_name="api-gateway"</code></div>
    </div>
  </div>
</template>
