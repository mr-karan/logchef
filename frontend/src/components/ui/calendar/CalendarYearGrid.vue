<script lang="ts" setup>
import type { DateRange, DateValue } from "reka-ui"
import type { HTMLAttributes } from "vue"
import { computed } from "vue"
import { getLocalTimeZone, today } from "@internationalized/date"
import { ChevronLeft, ChevronRight } from "lucide-vue-next"
import { cn } from "@/lib/utils"
import { buttonVariants } from "@/components/ui/button"

type RangeInput = DateValue | DateValue[] | DateRange | null | undefined

interface Props {
  placeholder: DateValue
  modelValue?: RangeInput
  minValue?: DateValue
  maxValue?: DateValue
  class?: HTMLAttributes["class"]
}

const props = withDefaults(defineProps<Props>(), {
  modelValue: null,
  minValue: undefined,
  maxValue: undefined,
  class: undefined,
})

const emit = defineEmits<{
  prev: []
  next: []
  pick: [year: number]
}>()

const todayDate = today(getLocalTimeZone())

const decadeStart = computed(() => {
  const y = props.placeholder.year
  return y - (y % 10) + 1
})
const decadeEnd = computed(() => decadeStart.value + 9)

const years = computed(() =>
  Array.from({ length: 10 }, (_, i) => decadeStart.value + i),
)

const bounds = computed(() => {
  const m = props.modelValue
  if (!m) return { start: null as DateValue | null, end: null as DateValue | null }
  if (Array.isArray(m)) {
    return { start: m[0] ?? null, end: m[1] ?? null }
  }
  if ("start" in m || "end" in m) {
    const r = m as DateRange
    return { start: r.start ?? null, end: r.end ?? null }
  }
  return { start: m as DateValue, end: m as DateValue }
})

function isSelected(year: number) {
  for (const b of [bounds.value.start, bounds.value.end]) {
    if (b && b.year === year) return true
  }
  return false
}

function isInRange(year: number) {
  const { start, end } = bounds.value
  if (!start || !end) return false
  return (
    year > Math.min(start.year, end.year) &&
    year < Math.max(start.year, end.year)
  )
}

function isToday(year: number) {
  return year === todayDate.year
}

function isDisabled(year: number) {
  if (props.minValue && year < props.minValue.year) return true
  if (props.maxValue && year > props.maxValue.year) return true
  return false
}
</script>

<template>
  <div :class="cn('p-3 min-w-[240px]', props.class)" data-slot="calendar-year-grid">
    <div class="relative pt-0 h-7">
      <nav class="flex items-center gap-1 absolute top-0 inset-x-0 justify-between pointer-events-none">
        <button
          type="button"
          aria-label="Previous decade"
          :class="cn(
            buttonVariants({ variant: 'outline' }),
            'size-7 bg-transparent p-0 opacity-50 hover:opacity-100 pointer-events-auto',
          )"
          @click="emit('prev')"
        >
          <ChevronLeft class="size-4" />
        </button>
        <button
          type="button"
          aria-label="Next decade"
          :class="cn(
            buttonVariants({ variant: 'outline' }),
            'size-7 bg-transparent p-0 opacity-50 hover:opacity-100 pointer-events-auto',
          )"
          @click="emit('next')"
        >
          <ChevronRight class="size-4" />
        </button>
      </nav>
      <div class="absolute inset-x-0 mx-auto h-7 px-2 text-sm font-medium leading-7 w-fit">
        {{ decadeStart }} – {{ decadeEnd }}
      </div>
    </div>

    <div class="grid grid-cols-3 gap-1 mt-4">
      <button
        v-for="year in years"
        :key="year"
        type="button"
        :disabled="isDisabled(year)"
        :class="cn(
          'h-9 rounded-md text-sm transition-colors',
          'hover:bg-accent hover:text-accent-foreground',
          'disabled:opacity-40 disabled:pointer-events-none',
          isSelected(year) && 'bg-primary text-primary-foreground hover:bg-primary hover:text-primary-foreground',
          !isSelected(year) && isInRange(year) && 'bg-accent text-accent-foreground',
          !isSelected(year) && !isInRange(year) && isToday(year) && 'ring-1 ring-primary',
        )"
        @click="emit('pick', year)"
      >
        {{ year }}
      </button>
    </div>
  </div>
</template>
