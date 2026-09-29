<script lang="ts" setup>
import { useI18n } from "vue-i18n"
import type { HTMLAttributes } from "vue"
import { MoreHorizontal } from "lucide-vue-next"
import { cn } from "@/lib/utils"

const props = defineProps<{
  class?: HTMLAttributes["class"]
}>()
const { t } = useI18n()
</script>

<template>
  <span
    data-slot="breadcrumb-ellipsis"
    role="presentation"
    aria-hidden="true"
    :class="cn('flex size-9 items-center justify-center', props.class)"
  >
    <slot>
      <MoreHorizontal class="size-4" />
    </slot>
    <span class="sr-only">{{ t('pages.more') }}</span>
  </span>
</template>
