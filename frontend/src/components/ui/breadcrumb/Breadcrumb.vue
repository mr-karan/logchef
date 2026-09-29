<script lang="ts" setup>
import { useI18n } from "vue-i18n"
import type { HTMLAttributes } from "vue"

const props = defineProps<{
  class?: HTMLAttributes["class"]
}>()
const { t } = useI18n()
</script>

<template>
  <nav
    :aria-label="t('pages.breadcrumbNavigation')"
    data-slot="breadcrumb"
    :class="props.class"
  >
    <slot />
  </nav>
</template>
