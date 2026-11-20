<script setup lang="ts">
import { computed } from "vue";

// One line per run of days with a value; a lone day is a dot. The y-axis
// starts at zero so a flat line near the top means uniformly slow.
const props = defineProps<{ values: (number | null)[]; label: string }>();

const WIDTH = 72;
const HEIGHT = 18;
const PAD = 1.5;

const shape = computed(() => {
  const top = Math.max(1, ...props.values.map(v => v ?? 0));
  const step = props.values.length > 1 ? (WIDTH - 2 * PAD) / (props.values.length - 1) : 0;
  const point = (value: number, i: number) => ({ x: PAD + i * step, y: HEIGHT - PAD - (value / top) * (HEIGHT - 2 * PAD) });
  const lines: string[] = [];
  const dots: { x: number; y: number }[] = [];
  let run: { x: number; y: number }[] = [];
  const close = () => {
    if (run.length > 1) lines.push(run.map(p => `${p.x.toFixed(1)},${p.y.toFixed(1)}`).join(" "));
    else if (run.length === 1) dots.push(run[0]);
    run = [];
  };
  props.values.forEach((value, i) => {
    if (value === null) close();
    else run.push(point(value, i));
  });
  close();
  return { lines, dots };
});
</script>

<template>
  <svg
    v-if="shape.lines.length || shape.dots.length"
    data-sparkline
    role="img"
    :aria-label="label"
    :width="WIDTH"
    :height="HEIGHT"
    :viewBox="`0 0 ${WIDTH} ${HEIGHT}`"
    class="text-primary"
  >
    <title>{{ label }}</title>
    <polyline v-for="(points, i) in shape.lines" :key="i" :points="points" fill="none" stroke="currentColor" stroke-width="1.25" stroke-linejoin="round" stroke-linecap="round" />
    <circle v-for="(dot, i) in shape.dots" :key="`d${i}`" :cx="dot.x" :cy="dot.y" r="1.5" fill="currentColor" />
  </svg>
</template>
