<script setup lang="ts">
import { computed } from 'vue';

const props = defineProps<{
    sql: string;
    debounce?: number;
}>();

const displaySql = computed(() => {
    return props.sql.replace(/FROM/g, '\nFROM')
        .replace(/WHERE/g, '\nWHERE')
        .replace(/ORDER BY/g, '\nORDER BY')
        .replace(/LIMIT/g, '\nLIMIT');
});
</script>

<template>
    <div class="text-sm font-mono p-2 bg-muted rounded-md mt-2">
        <pre class="whitespace-pre-wrap break-words">{{ displaySql }}</pre>
    </div>
</template>
