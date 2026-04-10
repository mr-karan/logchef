<script setup lang="ts">
import JsonViewer from "@/components/json-viewer/JsonViewer.vue";

defineProps<{
  data: Record<string, any>[];
  isLoading?: boolean;
}>();
</script>

<template>
  <div class="h-full overflow-auto p-4">
    <div
      v-if="isLoading"
      class="flex h-full min-h-[240px] items-center justify-center rounded-md border border-dashed text-sm text-muted-foreground"
    >
      Loading results...
    </div>
    <JsonViewer v-else :value="data" :expanded="true" />
  </div>
</template>
