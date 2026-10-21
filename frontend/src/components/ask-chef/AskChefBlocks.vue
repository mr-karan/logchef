<script setup lang="ts">
import type { AnswerBlock } from "@/utils/askChefMarkdown";
import AskChefInline from "./AskChefInline.vue";

defineProps<{ blocks: AnswerBlock[] }>();
</script>

<template>
  <template v-for="(block, index) in blocks" :key="index">
    <p v-if="block.kind === 'paragraph'" class="mb-1.5 whitespace-pre-line last:mb-0">
      <AskChefInline :tokens="block.inlines" />
    </p>
    <ul v-else class="mb-1.5 list-disc space-y-0.5 pl-4 last:mb-0">
      <li v-for="(item, itemIndex) in block.items" :key="itemIndex">
        <AskChefInline :tokens="item" />
      </li>
    </ul>
  </template>
</template>
