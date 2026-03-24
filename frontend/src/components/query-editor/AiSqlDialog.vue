<script setup lang="ts">
import { ref, watch, nextTick } from 'vue'
import { Wand2, AlertCircle } from 'lucide-vue-next'
import { Button } from '@/components/ui/button'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { useToast } from '@/composables/useToast'

interface AiSqlDialogProps {
  open: boolean
  isGenerating: boolean
  error: string | null
  generatedSql: string | null
}

const props = defineProps<AiSqlDialogProps>()

const emit = defineEmits<{
  (e: 'update:open', value: boolean): void
  (e: 'submit', payload: { naturalLanguageQuery: string; currentQuery: string }): void
  (e: 'insert', sql: string): void
}>()

const aiNaturalQuery = ref('')
const aiTextareaRef = ref<HTMLTextAreaElement | null>(null)

// Auto-focus when dialog opens
watch(() => props.open, (isOpen) => {
  if (isOpen) {
    nextTick(() => {
      aiTextareaRef.value?.focus()
    })
  }
})

const handleSubmit = () => {
  if (!aiNaturalQuery.value.trim()) return
  emit('submit', {
    naturalLanguageQuery: aiNaturalQuery.value.trim(),
    currentQuery: '',
  })
}

const handleInsert = () => {
  if (!props.generatedSql) return
  emit('insert', props.generatedSql)
  resetDialog()
}

const resetDialog = () => {
  emit('update:open', false)
  aiNaturalQuery.value = ''
}

const setExamplePrompt = (prompt: string) => {
  aiNaturalQuery.value = prompt
  nextTick(() => {
    aiTextareaRef.value?.focus()
  })
}

const copyToClipboard = async (text: string) => {
  try {
    await navigator.clipboard.writeText(text)
  } catch (error) {
    console.error('Failed to copy to clipboard:', error)
    const { toast } = useToast()
    toast({
      title: 'Copy failed',
      description: 'Unable to copy to clipboard',
      variant: 'destructive',
      duration: 3000,
    })
  }
}
</script>

<template>
  <Dialog :open="open" @update:open="emit('update:open', $event)">
    <DialogContent class="sm:max-w-3xl max-h-[90vh] overflow-hidden">
      <!-- Header -->
      <DialogHeader class="border-b pb-4">
        <DialogTitle class="flex items-center gap-3">
          <Wand2 class="h-6 w-6 text-purple-600" />
          <span class="text-xl font-semibold text-foreground">AI SQL Assistant</span>
        </DialogTitle>
        <DialogDescription class="text-muted-foreground mt-2">
          Describe the data you want to retrieve in natural language, and I'll generate SQL for you.
        </DialogDescription>
      </DialogHeader>

      <div class="flex flex-col gap-6 py-4 overflow-y-auto max-h-[60vh]">
        <!-- Input Section -->
        <div class="space-y-3">
          <Label class="text-sm font-medium text-foreground">What data are you looking for?</Label>
          <Textarea
            ref="aiTextareaRef"
            v-model="aiNaturalQuery"
            placeholder="show logs from syslog namespace for the Scarface service from the past 12 hours."
            class="min-h-[120px] resize-y border-2 border-input focus-visible:border-purple-500 focus-visible:ring-2 focus-visible:ring-purple-500 shadow-sm"
            @keydown.meta.enter="handleSubmit"
            @keydown.ctrl.enter="handleSubmit"
          />
          <div class="flex items-center justify-between text-xs text-muted-foreground">
            <div>
              Press <kbd class="px-1.5 py-0.5 bg-muted rounded font-mono">Ctrl+Enter</kbd> to generate
            </div>
            <details class="text-xs">
              <summary class="cursor-pointer hover:text-foreground font-medium">Examples</summary>
              <div class="absolute z-10 mt-2 right-0 bg-popover border border-border rounded-md shadow-lg p-3 w-80">
                <div class="space-y-2">
                  <div
                    @click="setExamplePrompt('Show me all error logs from the past hour')"
                    class="cursor-pointer p-2 hover:bg-muted rounded text-sm border border-border"
                  >
                    Show me all error logs from the past hour
                  </div>
                  <div
                    @click="setExamplePrompt('Count log entries by level for today')"
                    class="cursor-pointer p-2 hover:bg-muted rounded text-sm border border-border"
                  >
                    Count log entries by level for today
                  </div>
                  <div
                    @click="setExamplePrompt('Find logs containing authentication failed in the past 24 hours')"
                    class="cursor-pointer p-2 hover:bg-muted rounded text-sm border border-border"
                  >
                    Find logs containing "authentication failed" in the past 24 hours
                  </div>
                  <div
                    @click="setExamplePrompt('Show top 10 most frequent error messages this week')"
                    class="cursor-pointer p-2 hover:bg-muted rounded text-sm border border-border"
                  >
                    Show top 10 most frequent error messages this week
                  </div>
                </div>
              </div>
            </details>
          </div>
        </div>

        <!-- Generated SQL Section -->
        <div class="space-y-3">
          <div class="flex items-center gap-2">
            <span class="text-sm font-medium text-foreground">Generated SQL</span>
            <div v-if="isGenerating" class="flex items-center gap-2 text-xs text-muted-foreground">
              <div class="w-3 h-3 border-2 border-border border-t-purple-500 rounded-full animate-spin"></div>
              Generating...
            </div>
          </div>

          <!-- SQL Preview Container -->
          <div class="bg-muted/40 border-2 border-border rounded-md overflow-hidden shadow-sm">
            <!-- Loading State -->
            <div v-if="isGenerating" class="p-4 space-y-2">
              <div class="h-4 bg-muted rounded animate-pulse"></div>
              <div class="h-4 bg-muted rounded animate-pulse w-3/4"></div>
              <div class="h-4 bg-muted rounded animate-pulse w-1/2"></div>
            </div>

            <!-- Empty State -->
            <div v-else-if="!generatedSql && !error" class="p-8 text-center text-muted-foreground">
              <Wand2 class="h-8 w-8 mx-auto mb-2 opacity-50" />
              <p class="text-sm">Your generated SQL will appear here</p>
            </div>

            <!-- Generated SQL Display -->
            <div v-else-if="generatedSql" class="relative">
              <pre class="p-4 text-sm font-mono text-foreground overflow-auto max-h-60 whitespace-pre-wrap leading-relaxed"><code>{{ generatedSql }}</code></pre>
              <Button
                variant="ghost"
                size="sm"
                class="absolute top-2 right-2 h-6 w-6 p-0"
                @click="copyToClipboard(generatedSql)"
                title="Copy to clipboard"
              >
                <div class="h-3 w-3">📋</div>
              </Button>
            </div>

            <!-- Error State -->
            <div v-else-if="error" class="p-4 text-sm text-destructive bg-destructive/10 border-l-4 border-destructive/40">
              <div class="flex items-start gap-2">
                <AlertCircle class="h-4 w-4 flex-shrink-0 mt-0.5" />
                <div>
                  <div class="font-medium">Generation Failed</div>
                  <div class="text-xs mt-1 text-destructive/80">{{ error }}</div>
                </div>
              </div>
            </div>
          </div>
        </div>
      </div>

      <!-- Footer Actions -->
      <DialogFooter class="border-t pt-4 flex justify-between items-center">
        <Button variant="outline" @click="resetDialog">
          Cancel
        </Button>

        <div class="flex gap-2">
          <Button
            variant="outline"
            @click="handleSubmit"
            :disabled="!aiNaturalQuery.trim() || isGenerating"
            class="border-purple-200 text-purple-700 hover:bg-purple-50"
          >
            <Wand2 v-if="!isGenerating" class="h-4 w-4 mr-2" />
            <div v-if="isGenerating" class="w-4 h-4 border-2 border-purple-300 border-t-purple-600 rounded-full animate-spin mr-2"></div>
            {{ isGenerating ? 'Generating...' : (generatedSql ? 'Regenerate' : 'Generate SQL') }}
          </Button>

          <Button
            @click="handleInsert"
            :disabled="!generatedSql || isGenerating"
            class="bg-purple-600 hover:bg-purple-700 text-white font-medium"
          >
            Insert into Editor
          </Button>
        </div>
      </DialogFooter>
    </DialogContent>
  </Dialog>
</template>
