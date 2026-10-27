<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue';
import { ChefHat, Search, SquareTerminal } from 'lucide-vue-next';
import { Kbd } from '@/components/ui/kbd';
import type { InputEdit, Span, Token } from './parse';
import PaletteText from './PaletteText.vue';
const props = defineProps<{ text: string; tokens: readonly Token[]; query: boolean; ask: boolean; ghost: string; label: string; activeId?: string }>();
const emit = defineEmits<{ change: [text: string, selection: Span]; selection: [selection: Span]; key: [event: KeyboardEvent] }>();
const input = ref<HTMLInputElement | null>(null);
const scroll = ref(0);
const painted = computed(() => ({ transform: `translateX(${-scroll.value}px)` }));
function selection() {
  const element = input.value;
  if (element) emit('selection', { start: element.selectionStart ?? 0, end: element.selectionEnd ?? 0 });
}
function change() {
  const element = input.value;
  if (element) emit('change', element.value, { start: element.selectionStart ?? 0, end: element.selectionEnd ?? 0 });
}
function syncScroll() { scroll.value = input.value?.scrollLeft ?? 0; }
async function focus(edit?: InputEdit) {
  await nextTick();
  input.value?.focus();
  if (edit) input.value?.setSelectionRange(edit.caret, edit.caret);
  selection();
  syncScroll();
}
watch(() => props.text, () => { void nextTick(syncScroll); });
defineExpose({ focus });
</script>

<template>
  <div class="flex h-[52px] shrink-0 items-center gap-3 border-b px-4">
    <ChefHat v-if="ask" class="size-4 shrink-0 text-violet-600 dark:text-violet-400" />
    <SquareTerminal v-else-if="query" class="size-4 shrink-0 text-blue-500" />
    <Search v-else class="size-4 shrink-0 text-muted-foreground" />
    <div class="relative min-w-0 flex-1 overflow-hidden text-[13px]" :class="{ 'font-mono': query }" dir="ltr">
      <div v-if="query || ask" aria-hidden="true" class="pointer-events-none absolute inset-y-0 left-0 flex items-center whitespace-pre" :style="painted"><span><PaletteText :text="text" :tokens="tokens" /><span class="text-muted-foreground/50">{{ ghost }}</span></span></div>
      <input ref="input" :value="text" :aria-label="label" :placeholder="label" role="combobox" aria-autocomplete="list" aria-expanded="true" aria-controls="palette-results" :aria-activedescendant="activeId" autocomplete="off" spellcheck="false" class="h-[51px] w-full bg-transparent outline-none placeholder:text-muted-foreground" :class="{ 'text-transparent caret-foreground selection:bg-blue-500/25': query || ask }" @input="change" @select="selection" @keyup="selection" @pointerup="selection" @scroll="syncScroll" @keydown.capture="emit('key', $event)" />
    </div>
    <Kbd class="text-[10px]">esc</Kbd>
  </div>
</template>
