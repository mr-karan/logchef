<script setup lang="ts">
import { ref } from 'vue'
import hljs from 'highlight.js/lib/core'
import json from 'highlight.js/lib/languages/json'
import 'highlight.js/styles/stackoverflow-light.css'
import { Button } from '@/components/ui/button'
import { Copy, History, Check } from 'lucide-vue-next'
import { useToast } from '@/components/ui/toast'
import { TOAST_DURATION } from '@/lib/constants'

const props = defineProps<{
    value: any
    expanded?: boolean
    showContextButton?: boolean
}>()

const emit = defineEmits<{
    (e: 'showContext'): void
}>()

const { toast } = useToast()
const isExpanded = ref(props.expanded ?? false)
const isCopied = ref(false)

// Initialize highlight.js with JSON language
hljs.registerLanguage('json', json)

// Format JSON with indentation and highlighting
function formatJSON(data: any): string {
    const jsonString = JSON.stringify(data, null, 2)
    return hljs.highlight(jsonString, { language: 'json' }).value
}

// Handle copy to clipboard with toast notification
async function copyToClipboard() {
    const text = JSON.stringify(props.value, null, 2)
    try {
        await navigator.clipboard.writeText(text)
        isCopied.value = true
        toast({
            title: 'Copied',
            description: 'JSON data copied to clipboard',
            duration: TOAST_DURATION.SUCCESS,
        })
        // Reset copy state after 2 seconds
        setTimeout(() => {
            isCopied.value = false
        }, 2000)
    } catch (error) {
        toast({
            title: 'Error',
            description: 'Failed to copy to clipboard',
            variant: 'destructive',
            duration: TOAST_DURATION.ERROR,
        })
    }
}
</script>

<template>
    <div class="relative">
        <!-- Action buttons on the left -->
        <div class="absolute left-3 top-3 flex gap-1.5 z-10">
            <!-- Copy button -->
            <Button variant="secondary" size="sm" @click.stop.prevent="copyToClipboard" :disabled="isCopied"
                class="h-7 px-2.5 shadow-sm transition-all duration-200 hover:shadow active:scale-95 cursor-pointer"
                :class="{
                    'bg-green-500/10 text-green-600 hover:bg-green-500/20': isCopied,
                    'hover:bg-muted': !isCopied
                }">
                <Check v-if="isCopied"
                    class="h-3.5 w-3.5 mr-1.5 transition-transform duration-200 animate-in zoom-in" />
                <Copy v-else class="h-3.5 w-3.5 mr-1.5" />
                {{ isCopied ? 'Copied!' : 'Copy' }}
            </Button>

            <!-- Context button -->
            <Button v-if="showContextButton" variant="secondary" size="sm" @click.stop.prevent="emit('showContext')"
                class="h-7 px-2.5 shadow-sm transition-all duration-200 hover:shadow hover:bg-muted active:scale-95 cursor-pointer">
                <History class="h-3.5 w-3.5 mr-1.5" />
                Show Context
            </Button>
        </div>

        <!-- Highlighted JSON -->
        <div class="relative">
            <pre :class="{ 'max-h-[500px]': !isExpanded }"
                class="text-sm font-mono overflow-auto pt-14 px-3"><code v-html="formatJSON(value)" /></pre>

            <!-- Expand/collapse overlay -->
            <div v-if="!isExpanded && formatJSON(value).split('\n').length > 20"
                class="absolute bottom-0 left-0 right-0 h-12 bg-gradient-to-t from-background to-transparent flex items-end justify-center pb-2">
                <Button variant="ghost" size="sm" @click.stop.prevent="isExpanded = true"
                    class="transition-transform hover:scale-105 active:scale-95 cursor-pointer">
                    Show More
                </Button>
            </div>
        </div>
    </div>
</template>

<style>
/* Just remove padding and background since we're in a pre tag already */
pre code.hljs {
    background: transparent;
    padding: 0;
}

/* Add smooth transition for expand/collapse */
pre {
    transition: all 0.3s ease-in-out;
}

/* Add animation utilities */
.animate-in {
    animation: animate-in 0.2s ease-out;
}

.zoom-in {
    transform-origin: center;
    animation: zoom-in 0.2s ease-out;
}

/* Ensure buttons are clickable */
button {
    pointer-events: all !important;
    cursor: pointer !important;
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