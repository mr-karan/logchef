<script setup lang="ts">
import { computed } from "vue";
import { formatCompactCount } from "@/utils/histogram-chart";

// A "stat" panel: one big number — the total match count over the window
// (sum of the histogram bucket counts, computed in the store).
interface Props {
  value: number;
}
const props = defineProps<Props>();

const compact = computed(() => formatCompactCount(props.value));
const exact = computed(() => Math.round(props.value).toLocaleString());
// Only surface the exact value in the title tooltip once compacting hides digits.
const showExactTitle = computed(() => compact.value !== exact.value);
</script>

<template>
  <div class="panel-stat">
    <span
      class="panel-stat__value"
      :title="showExactTitle ? exact : undefined"
    >{{ compact }}</span>
  </div>
</template>

<style scoped>
.panel-stat {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 100%;
  height: 100%;
  padding: 0.5rem;
  container-type: inline-size;
}
.panel-stat__value {
  font-size: clamp(1.75rem, 6cqw, 3rem);
  font-weight: 650;
  line-height: 1.1;
  font-variant-numeric: tabular-nums;
  color: var(--foreground);
  letter-spacing: -0.02em;
}
</style>
