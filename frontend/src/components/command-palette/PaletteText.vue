<script setup lang="ts">
import { computed } from 'vue';
import type { MarkedText } from './palette';
import type { Token } from './parse';
const props = defineProps<{ text: string; marked?: MarkedText; tokens?: readonly Token[] }>();
const pieces = computed(() => {
  const spans = [...(props.marked?.marks ?? []), ...(props.marked?.dim ?? []), ...(props.tokens ?? [])];
  const cuts = [...new Set([0, props.text.length, ...spans.flatMap(span => [span.start, span.end])])].sort((a, b) => a - b);
  return cuts.slice(0, -1).map((start, index) => ({
    start,
    text: props.text.slice(start, cuts[index + 1]),
    hit: props.marked?.marks.some(span => start >= span.start && start < span.end),
    dim: props.marked?.dim.some(span => start >= span.start && start < span.end),
    token: props.tokens?.find(span => start >= span.start && start < span.end)?.kind,
  }));
});
</script>

<template>
  <span v-for="piece in pieces" :key="piece.start" :class="[{ 'font-semibold text-foreground': piece.hit, 'text-muted-foreground': piece.dim && !piece.hit }, piece.token ? `palette-token-${piece.token}` : '']">{{ piece.text }}</span>
</template>

<style>
.palette-token-askMarker, .palette-token-askText { color: var(--color-violet-600); }
.palette-token-tagAt, .palette-token-tagTeam, .palette-token-tagSource { color: var(--color-blue-600); }
.palette-token-rangeKey, .palette-token-rangeValue { color: var(--color-orange-600); }
.palette-token-field { color: var(--color-violet-600); }
.palette-token-value, .palette-token-string { color: var(--color-teal-600); }
.palette-token-keyword, .palette-token-op, .palette-token-tagSlash { color: var(--muted-foreground); }
.palette-token-invalid { color: var(--destructive); text-decoration: underline dotted; }
.dark .palette-token-askMarker, .dark .palette-token-askText { color: var(--color-violet-400); }
.dark .palette-token-tagAt, .dark .palette-token-tagTeam, .dark .palette-token-tagSource { color: var(--color-blue-400); }
.dark .palette-token-rangeKey, .dark .palette-token-rangeValue { color: var(--color-orange-400); }
.dark .palette-token-field { color: var(--color-violet-400); }
.dark .palette-token-value, .dark .palette-token-string { color: var(--color-teal-400); }
</style>
