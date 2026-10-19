<script setup lang="ts">
import type { InlineToken } from "@/utils/askChefMarkdown";
import AskChefEvidenceChip from "./AskChefEvidenceChip.vue";

defineProps<{ tokens: InlineToken[] }>();
</script>

<template>
  <template v-for="(token, index) in tokens" :key="index">
    <AskChefEvidenceChip v-if="token.kind === 'citation'" :id="token.id" />
    <code v-else-if="token.kind === 'code'" class="rounded border bg-muted px-1 font-mono text-[0.85em]">{{ token.text }}</code>
    <strong v-else-if="token.bold" class="font-semibold">{{ token.text }}</strong>
    <span v-else>{{ token.text }}</span>
  </template>
</template>
