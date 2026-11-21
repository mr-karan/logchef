<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { Input } from "@/components/ui/input";

// A text input with a themed suggestion list below it. Typing narrows the
// list; arrow keys move through it and Enter picks the highlighted entry.
// Enter with nothing highlighted is passed on as an "enter" event.
const props = withDefaults(defineProps<{
  modelValue: string;
  suggestions: string[];
  loading?: boolean;
  max?: number;
  testId?: string;
}>(), { loading: false, max: 8, testId: undefined });

const emit = defineEmits<{
  (e: "update:modelValue", value: string): void;
  (e: "pick", value: string): void;
  (e: "enter", event: KeyboardEvent): void;
}>();

const open = ref(false);
const highlighted = ref(-1);

const matches = computed(() => {
  const typed = props.modelValue.trim().toLowerCase();
  const list = typed
    ? props.suggestions.filter((value) => value.toLowerCase().includes(typed) && value !== props.modelValue)
    : props.suggestions;
  return list.slice(0, props.max);
});

watch(matches, () => {
  highlighted.value = -1;
});

function pick(value: string) {
  emit("update:modelValue", value);
  emit("pick", value);
  open.value = false;
  highlighted.value = -1;
}

function onInput(value: string | number) {
  emit("update:modelValue", String(value));
  open.value = true;
}

function onKeydown(event: KeyboardEvent) {
  const count = matches.value.length;
  if (event.key === "ArrowDown" && count) {
    event.preventDefault();
    open.value = true;
    highlighted.value = (highlighted.value + 1) % count;
  } else if (event.key === "ArrowUp" && count) {
    event.preventDefault();
    open.value = true;
    highlighted.value = highlighted.value <= 0 ? count - 1 : highlighted.value - 1;
  } else if (event.key === "Enter") {
    if (open.value && highlighted.value >= 0 && highlighted.value < count) {
      event.preventDefault();
      pick(matches.value[highlighted.value]);
    } else {
      emit("enter", event);
    }
  } else if (event.key === "Escape" && open.value && count) {
    // Closes the list only; a second Escape closes the popover.
    event.stopPropagation();
    open.value = false;
  }
}

// Clicks on the list land after the input blurs, so the list closes a beat later.
function onBlur() {
  setTimeout(() => {
    open.value = false;
  }, 150);
}
</script>

<template>
  <div class="relative">
    <Input
      :model-value="modelValue"
      class="h-8"
      autocomplete="off"
      role="combobox"
      :aria-expanded="open && matches.length > 0"
      :data-testid="testId"
      @update:model-value="onInput"
      @focus="open = true"
      @blur="onBlur"
      @keydown="onKeydown"
    />
    <ul
      v-if="open && (matches.length > 0 || loading)"
      role="listbox"
      class="absolute left-0 right-0 top-full z-50 mt-1 max-h-56 overflow-auto rounded-md border bg-popover p-1 text-sm text-popover-foreground shadow-md"
      :data-testid="testId ? `${testId}-suggestions` : undefined"
    >
      <li
        v-for="(value, index) in matches"
        :key="value"
        role="option"
        :aria-selected="index === highlighted"
        class="cursor-pointer truncate rounded px-2 py-1"
        :class="index === highlighted ? 'bg-accent text-accent-foreground' : 'hover:bg-accent hover:text-accent-foreground'"
        :title="value"
        @mousedown.prevent="pick(value)"
      >
        {{ value }}
      </li>
      <li v-if="loading && matches.length === 0" class="px-2 py-1 text-muted-foreground">…</li>
    </ul>
  </div>
</template>
