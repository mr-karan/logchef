<script setup lang="ts">
import { computed } from "vue";
import { useI18n } from "vue-i18n";
import type { EvidenceId } from "@/api/askChef";
import { useAskChefStore } from "@/stores/askChef";

const props = defineProps<{ id: EvidenceId }>();

const { t } = useI18n();
const askChef = useAskChefStore();
const summary = computed(() => askChef.evidence.get(props.id)?.summary);
</script>

<template>
  <button
    type="button"
    :title="summary"
    :aria-label="t('askChef.openEvidence', { id })"
    :class="[
      'mx-px inline-flex h-4 min-w-4 items-center justify-center rounded-[5px] border px-1 align-[1px] text-[10px] font-semibold leading-none',
      askChef.activeEvidenceId === id
        ? 'border-blue-600 bg-blue-600 text-white'
        : 'border-blue-200 bg-blue-50 text-blue-600 hover:bg-blue-100 dark:border-blue-800 dark:bg-blue-950 dark:text-blue-300',
    ]"
    @click.stop="askChef.openEvidence(id)"
  >
    {{ id.slice(1) }}
  </button>
</template>
