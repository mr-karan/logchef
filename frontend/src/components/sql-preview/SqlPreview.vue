<script setup lang="ts">
import { computed } from 'vue'
import hljs from 'highlight.js/lib/core'
import sql from 'highlight.js/lib/languages/sql'
import { Button } from '@/components/ui/button'
import { Copy } from 'lucide-vue-next'
import { useToast } from '@/components/ui/toast'
import { TOAST_DURATION } from '@/lib/constants'

// Register SQL language for highlighting
hljs.registerLanguage('sql', sql)

interface Props {
    sql: string
    isValid: boolean
    error?: string
}

const props = defineProps<Props>()
const { toast } = useToast()

// Format and highlight SQL
const highlightedSql = computed(() => {
    if (!props.sql) return ''

    const highlighted = hljs.highlight(props.sql, { language: 'sql' }).value
    return props.isValid ? highlighted : `-- Error: ${props.error}\n${highlighted}`
})

// Copy SQL to clipboard
const copyToClipboard = () => {
    navigator.clipboard.writeText(props.sql)
    toast({
        title: 'Copied',
        description: 'SQL query copied to clipboard',
        duration: TOAST_DURATION.SUCCESS,
    })
}
</script>

<template>
    <div class="relative rounded-md border bg-muted/50 p-4">
        <!-- Copy button -->
        <Button variant="ghost" size="icon"
            class="absolute right-4 top-4 h-6 w-6 bg-background/50 hover:bg-background/80 backdrop-blur-sm"
            @click="copyToClipboard" :disabled="!sql" title="Copy SQL">
            <Copy class="h-3 w-3" />
        </Button>

        <!-- SQL Preview -->
        <pre class="text-sm font-mono overflow-auto mt-2">
      <code v-html="highlightedSql" :class="{ 'text-destructive': !isValid }" />
    </pre>
    </div>
</template>

<style>
/* Remove padding and background since we're in a pre tag */
pre code.hljs {
    background: transparent;
    padding: 0;
}
</style>