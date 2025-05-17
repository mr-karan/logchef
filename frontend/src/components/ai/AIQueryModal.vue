<script setup lang="ts">
import { ref, watch } from "vue";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";
import { WandSparkles, Terminal } from "lucide-vue-next";
import hljs from 'highlight.js/lib/core';
import sql from 'highlight.js/lib/languages/sql';
import 'highlight.js/styles/stackoverflow-light.css';

// Register SQL language for highlight.js
hljs.registerLanguage('sql', sql);

const props = defineProps<{
  isOpen: boolean;
  isLoading: boolean;
  errorMessage: string | null;
  generatedSql: string;
  currentSourceId: number | null;
}>();

const emit = defineEmits<{
  close: [];
  "generate-sql": [{ naturalLanguageQuery: string }];
  "insert-sql": [{ sql: string }];
}>();

const naturalLanguageQuery = ref("");

function handleGenerate() {
  if (naturalLanguageQuery.value.trim()) {
    emit("generate-sql", { naturalLanguageQuery: naturalLanguageQuery.value });
  }
}

function handleInsert() {
  if (props.generatedSql) {
    emit("insert-sql", { sql: props.generatedSql });
  }
}

function onKeyDown(e: KeyboardEvent) {
  // Submit on Ctrl+Enter or Cmd+Enter
  if ((e.ctrlKey || e.metaKey) && e.key === "Enter") {
    handleGenerate();
  }
}

// Function to highlight SQL code
function highlightSql(code: string): string {
  try {
    return hljs.highlight(code, { language: 'sql' }).value;
  } catch (e) {
    // Fallback in case of highlighting errors
    return code;
  }
}

// Clear the input when the modal is closed
watch(
  () => props.isOpen,
  (isOpen) => {
    if (!isOpen) {
      naturalLanguageQuery.value = "";
    }
  }
);
</script>

<template>
  <Dialog :open="isOpen" @update:open="emit('close')">
    <DialogContent class="sm:max-w-[700px] max-h-[90vh] overflow-hidden flex flex-col">
      <DialogHeader>
        <DialogTitle class="flex items-center gap-2">
          <WandSparkles class="h-5 w-5 text-primary" />
          AI SQL Assistant
        </DialogTitle>
        <DialogDescription>
          Describe the data you want to retrieve in natural language, and I'll generate SQL for you.
        </DialogDescription>
      </DialogHeader>

      <div class="grid gap-4 py-4 px-4 flex-1 min-h-0 overflow-hidden">
        <!-- Natural Language Input -->
        <div class="space-y-2">
          <label class="text-sm font-medium" for="nl-query">
            What data are you looking for?
          </label>
          <Textarea id="nl-query" v-model="naturalLanguageQuery" :disabled="isLoading"
            placeholder="Example: Show me all error logs from the last hour, excluding database-related errors"
            class="resize-none h-28" @keydown="onKeyDown" />
        </div>

        <!-- Generated SQL Output -->
        <div class="space-y-2 overflow-hidden flex flex-col flex-1 min-h-0">
          <label class="text-sm font-medium flex justify-between items-center">
            <span class="flex items-center gap-1.5">
              <Terminal class="h-4 w-4" />
              Generated SQL
            </span>
          </label>
          <div class="border rounded-md bg-muted/40 p-3 overflow-auto h-full font-mono text-sm">
            <p v-if="isLoading" class="text-muted-foreground animate-pulse">
              Generating SQL query...
            </p>
            <p v-else-if="errorMessage" class="text-destructive">
              Error: {{ errorMessage }}
            </p>
            <pre v-else-if="generatedSql"
              class="whitespace-pre-wrap"><code v-html="highlightSql(generatedSql)" class="hljs language-sql"></code></pre>
            <p v-else class="text-muted-foreground italic">
              Generated SQL will appear here...
            </p>
          </div>
        </div>
      </div>

      <DialogFooter class="flex-col sm:flex-row gap-3 sm:gap-2">
        <div class="flex gap-2 w-full sm:w-auto ml-auto">
          <Button variant="outline" @click="emit('close')">Cancel</Button>
          <Button @click="handleGenerate" :disabled="!naturalLanguageQuery.trim() || isLoading">
            <span v-if="isLoading" class="flex items-center gap-1.5">
              <span
                class="h-4 w-4 border-2 border-current border-t-transparent rounded-full inline-block animate-spin"></span>
              Generating...
            </span>
            <span v-else>Generate SQL</span>
          </Button>
          <Button v-if="generatedSql && !errorMessage" @click="handleInsert" variant="default" :disabled="isLoading">
            Insert into Editor
          </Button>
        </div>
      </DialogFooter>
    </DialogContent>
  </Dialog>
</template>

<style scoped>
/* Allow highlight.js styles to work with scoped styles */
:deep(.hljs) {
  background: transparent;
  padding: 0;
}
</style>
