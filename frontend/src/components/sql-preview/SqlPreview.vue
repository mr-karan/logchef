<script setup lang="ts">
import { computed, ref, watch, onMounted } from 'vue';
import hljs from 'highlight.js/lib/core';
import sql from 'highlight.js/lib/languages/sql';
import 'highlight.js/styles/stackoverflow-light.css';
import { Copy, Check } from 'lucide-vue-next';
import { Button } from '@/components/ui/button';
import { useToast } from '@/components/ui/toast';
import { TOAST_DURATION } from '@/lib/constants';

const props = defineProps<{
    sql: string;
    debounce?: number;
    is_valid?: boolean;
    error?: string;
}>();

// Force our own internal SQL content to prevent stale data
const internalSql = ref('');
onMounted(() => {
    internalSql.value = props.sql;
});

// Track the filters that were used to generate the current SQL
const previousFilterData = ref({
    hasNamespace: false,
    hasBody: false,
    hasRaw: false
});

// Completely remove any filtering or modifying logic
// Just let the SQL pass through directly as provided
function checkForStaleConditions(sql) {
    // Do nothing - just return false to never modify the SQL
    return false;
}

// Watch for changes in the SQL
watch(() => props.sql, (newSql) => {
    // Update our internal SQL directly - no manipulation
    internalSql.value = newSql;
}, { immediate: true });

// Initialize highlight.js with SQL language
hljs.registerLanguage('sql', sql);

// Better SQL formatter with nested WHERE conditions
const displaySql = computed(() => {
    // First clean up extra whitespace
    let cleanSql = internalSql.value.trim().replace(/\s+/g, ' ');
    
    // Extract the WHERE clause if it exists
    const whereMatch = cleanSql.match(/WHERE\s+(.*?)(?=\s+(?:ORDER BY|GROUP BY|HAVING|LIMIT|$))/i);
    
    if (whereMatch && whereMatch[1]) {
        const whereConditions = whereMatch[1];
        
        // Format each AND condition on a new line with indentation
        const formattedWhere = whereConditions.replace(/\s+AND\s+/gi, '\n  AND ');
        
        // Replace the original WHERE clause
        cleanSql = cleanSql.replace(whereMatch[0], `WHERE ${formattedWhere}`);
    }
    
    // Add main clause formatting
    cleanSql = cleanSql
        .replace(/\s*SELECT\s+/i, 'SELECT ')
        .replace(/\s*FROM\s+/i, '\nFROM ')
        .replace(/\s*WHERE\s+/i, '\nWHERE ')
        .replace(/\s*ORDER BY\s+/i, '\nORDER BY ')
        .replace(/\s*GROUP BY\s+/i, '\nGROUP BY ')
        .replace(/\s*HAVING\s+/i, '\nHAVING ')
        .replace(/\s*LIMIT\s+/i, '\nLIMIT ');
    
    return cleanSql;
});

// Apply syntax highlighting
const highlightedSql = computed(() => {
    return hljs.highlight(displaySql.value, { language: 'sql' }).value;
});

// Copy functionality
const { toast } = useToast();
const isCopied = ref(false);

async function copyToClipboard() {
    try {
        await navigator.clipboard.writeText(internalSql.value);
        isCopied.value = true;
        toast({
            title: 'Copied',
            description: 'SQL query copied to clipboard',
            duration: TOAST_DURATION.SUCCESS,
        });
        // Reset copy state after 2 seconds
        setTimeout(() => {
            isCopied.value = false;
        }, 2000);
    } catch (error) {
        toast({
            title: 'Error',
            description: 'Failed to copy to clipboard',
            variant: 'destructive',
            duration: TOAST_DURATION.ERROR,
        });
    }
}
</script>

<template>
    <div class="relative">
        <!-- Copy button -->
        <div class="absolute right-2 top-2 z-10">
            <Button variant="secondary" size="sm" @click.stop.prevent="copyToClipboard" :disabled="isCopied"
                class="h-6 px-2 shadow-sm transition-all duration-200 hover:shadow active:scale-95 cursor-pointer"
                :class="{
                    'bg-green-500/10 text-green-600 hover:bg-green-500/20': isCopied,
                    'hover:bg-muted': !isCopied
                }">
                <Check v-if="isCopied" class="h-3 w-3 mr-1 transition-transform duration-200 animate-in zoom-in" />
                <Copy v-else class="h-3 w-3 mr-1" />
                {{ isCopied ? 'Copied!' : 'Copy' }}
            </Button>
        </div>
        
        <!-- SQL with syntax highlighting -->
        <div class="text-sm font-mono p-3 pr-[85px] pt-10 bg-muted rounded-md mt-2 overflow-auto max-h-[300px] relative">
            <div class="absolute left-3 top-2 text-[10px] uppercase font-sans text-muted-foreground tracking-wider">Generated Query</div>
            <pre class="whitespace-pre"><code v-html="highlightedSql" /></pre>
        </div>
    </div>
</template>

<style>
/* Just remove padding and background since we're in a pre tag already */
pre code.hljs {
    background: transparent;
    padding: 0;
}

/* Animations for the copy button */
.animate-in {
    animation: animate-in 0.2s ease-out;
}

.zoom-in {
    transform-origin: center;
    animation: zoom-in 0.2s ease-out;
}

@keyframes animate-in {
    from {
        opacity: 0;
        transform: translateY(1px);
    }
    to {
        opacity: 1;
        transform: translateY(0);
    }
}

@keyframes zoom-in {
    from {
        opacity: 0;
        transform: scale(0.95);
    }
    to {
        opacity: 1;
        transform: scale(1);
    }
}
</style>
