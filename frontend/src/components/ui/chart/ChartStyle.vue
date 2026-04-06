<script setup lang="ts">
import { computed } from "vue";
import type { ChartConfig } from "./types";

interface ChartStyleProps {
  id: string;
  config: ChartConfig;
}

const props = defineProps<ChartStyleProps>();

const styleText = computed(() => {
  const lightDeclarations: string[] = [];
  const darkDeclarations: string[] = [];

  for (const [key, item] of Object.entries(props.config)) {
    if (item.theme) {
      lightDeclarations.push(`--color-${key}: ${item.theme.light};`);
      darkDeclarations.push(`--color-${key}: ${item.theme.dark};`);
      continue;
    }

    if (item.color) {
      lightDeclarations.push(`--color-${key}: ${item.color};`);
      darkDeclarations.push(`--color-${key}: ${item.color};`);
    }
  }

  if (lightDeclarations.length === 0 && darkDeclarations.length === 0) {
    return "";
  }

  const selector = `[data-chart="${props.id}"]`;

  return [
    `${selector} { ${lightDeclarations.join(" ")} }`,
    darkDeclarations.length > 0 ? `.dark ${selector} { ${darkDeclarations.join(" ")} }` : "",
  ]
    .filter(Boolean)
    .join("\n");
});
</script>

<template>
  <component :is="'style'" v-if="styleText">
    {{ styleText }}
  </component>
</template>
