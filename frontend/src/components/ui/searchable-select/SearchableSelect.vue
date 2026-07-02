<script setup lang="ts">
// SearchableSelect — a full-width, type-to-filter picker for choosing one item
// from a potentially large list (users, sources, service accounts, ...). Built
// on Popover + a filtered list so it degrades gracefully and stays keyboard
// navigable. v-model binds the selected item's `value` (string).
import { ref, computed, watch, nextTick } from "vue"
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover"
import { Check, ChevronsUpDown, Search } from "lucide-vue-next"
import { cn } from "@/lib/utils"

export interface SearchableItem {
  value: string
  label: string
  sublabel?: string
}

const props = withDefaults(
  defineProps<{
    modelValue: string
    items: SearchableItem[]
    placeholder?: string
    searchPlaceholder?: string
    emptyText?: string
    disabled?: boolean
  }>(),
  {
    placeholder: "Select…",
    searchPlaceholder: "Search…",
    emptyText: "No results found.",
    disabled: false,
  },
)

const emit = defineEmits<{ "update:modelValue": [value: string] }>()

const open = ref(false)
const search = ref("")
const highlighted = ref(0)
const searchInput = ref<HTMLInputElement | null>(null)
const listEl = ref<HTMLElement | null>(null)

const filtered = computed(() => {
  const q = search.value.trim().toLowerCase()
  if (!q) return props.items
  return props.items.filter(
    (i) =>
      i.label.toLowerCase().includes(q) ||
      (i.sublabel?.toLowerCase().includes(q) ?? false),
  )
})

const selectedLabel = computed(
  () => props.items.find((i) => i.value === props.modelValue)?.label ?? "",
)

watch(open, (isOpen) => {
  if (isOpen) {
    search.value = ""
    highlighted.value = 0
    nextTick(() => searchInput.value?.focus())
  }
})
watch(filtered, () => {
  highlighted.value = 0
})

function select(value: string) {
  emit("update:modelValue", value)
  open.value = false
}

function scrollHighlightedIntoView() {
  nextTick(() => {
    const el = listEl.value?.querySelectorAll<HTMLElement>("[data-item]")[highlighted.value]
    el?.scrollIntoView({ block: "nearest" })
  })
}

function onKeydown(e: KeyboardEvent) {
  if (e.key === "ArrowDown") {
    e.preventDefault()
    highlighted.value = Math.min(highlighted.value + 1, filtered.value.length - 1)
    scrollHighlightedIntoView()
  } else if (e.key === "ArrowUp") {
    e.preventDefault()
    highlighted.value = Math.max(highlighted.value - 1, 0)
    scrollHighlightedIntoView()
  } else if (e.key === "Enter") {
    e.preventDefault()
    const item = filtered.value[highlighted.value]
    if (item) select(item.value)
  } else if (e.key === "Escape") {
    open.value = false
  }
}
</script>

<template>
  <Popover v-model:open="open">
    <PopoverTrigger as-child>
      <button
        type="button"
        role="combobox"
        :aria-expanded="open"
        :disabled="disabled"
        :class="cn(
          'border-input flex h-9 w-full items-center justify-between gap-2 rounded-md border bg-transparent px-3 py-2 text-sm shadow-xs transition-[color,box-shadow] outline-none focus-visible:border-ring focus-visible:ring-ring/50 focus-visible:ring-[3px] disabled:cursor-not-allowed disabled:opacity-50 dark:bg-input/30 dark:hover:bg-input/50',
          !selectedLabel && 'text-muted-foreground',
        )"
      >
        <span class="line-clamp-1 text-left">{{ selectedLabel || placeholder }}</span>
        <ChevronsUpDown class="size-4 shrink-0 opacity-50" />
      </button>
    </PopoverTrigger>
    <PopoverContent
      align="start"
      class="min-w-[16rem] w-[var(--reka-popover-trigger-width)] p-0"
    >
      <div class="flex items-center gap-2 border-b px-3">
        <Search class="size-4 shrink-0 opacity-50" />
        <input
          ref="searchInput"
          v-model="search"
          :placeholder="searchPlaceholder"
          class="flex h-9 w-full bg-transparent py-2 text-sm outline-none placeholder:text-muted-foreground"
          @keydown="onKeydown"
        />
      </div>
      <div ref="listEl" class="max-h-[300px] overflow-y-auto p-1">
        <div
          v-if="filtered.length === 0"
          class="py-6 text-center text-sm text-muted-foreground"
        >
          {{ emptyText }}
        </div>
        <button
          v-for="(item, idx) in filtered"
          :key="item.value"
          data-item
          type="button"
          :class="cn(
            'flex w-full cursor-default items-center gap-2 rounded-sm px-2 py-1.5 text-left text-sm',
            idx === highlighted && 'bg-accent text-accent-foreground',
          )"
          @click="select(item.value)"
          @mouseenter="highlighted = idx"
        >
          <Check
            :class="cn('size-4 shrink-0', item.value === modelValue ? 'opacity-100' : 'opacity-0')"
          />
          <span class="flex min-w-0 flex-col">
            <span class="truncate font-medium">{{ item.label }}</span>
            <span v-if="item.sublabel" class="truncate text-xs text-muted-foreground">{{ item.sublabel }}</span>
          </span>
        </button>
      </div>
    </PopoverContent>
  </Popover>
</template>
