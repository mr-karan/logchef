<script lang="ts" setup>
import type { DateRange, DateValue } from "reka-ui"
import type { HTMLAttributes } from "vue"
import { computed } from "vue"
import { CalendarDate, getLocalTimeZone, today } from "@internationalized/date"
import { useDateFormatter } from "reka-ui"
import { ChevronLeft, ChevronRight } from "lucide-vue-next"
import { cn } from "@/lib/utils"
import { buttonVariants } from "@/components/ui/button"

type RangeInput = DateValue | DateValue[] | DateRange | null | undefined

interface Props {
  placeholder: DateValue
  modelValue?: RangeInput
  minValue?: DateValue
  maxValue?: DateValue
  locale?: string
  class?: HTMLAttributes["class"]
}

const props = withDefaults(defineProps<Props>(), {
  modelValue: null,
  minValue: undefined,
  maxValue: undefined,
  locale: "en",
  class: undefined,
})

const emit = defineEmits<{
  prev: []
  next: []
  drillUp: []
  pick: [month: number]
}>()

const formatter = useDateFormatter(props.locale)
const todayDate = today(getLocalTimeZone())

const months = computed(() =>
  Array.from(
    { length: 12 },
    (_, i) => new CalendarDate(props.placeholder.year, i + 1, 1),
  ),
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

function isSelected(month: CalendarDate) {
  for (const b of [bounds.value.start, bounds.value.end]) {
    if (b && b.year === month.year && b.month === month.month) return true
  }
  return false
}

function isInRange(month: CalendarDate) {
  const { start, end } = bounds.value
  if (!start || !end) return false
  const m = month.year * 12 + month.month
  const s = start.year * 12 + start.month
  const e = end.year * 12 + end.month
  return m > Math.min(s, e) && m < Math.max(s, e)
}

function isToday(month: CalendarDate) {
  return month.year === todayDate.year && month.month === todayDate.month
}

function isDisabled(month: CalendarDate) {
  const m = month.year * 12 + month.month
  if (props.minValue) {
    const min = props.minValue.year * 12 + props.minValue.month
    if (m < min) return true
  }
  if (props.maxValue) {
    const max = props.maxValue.year * 12 + props.maxValue.month
    if (m > max) return true
  }
  return false
}
</script>

<template>
  <div :class="cn('p-3 min-w-[240px]', props.class)" data-slot="calendar-month-grid">
    <div class="relative pt-0 h-7">
      <nav class="flex items-center gap-1 absolute top-0 inset-x-0 justify-between pointer-events-none">
        <button
          type="button"
          aria-label="Previous year"
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
          aria-label="Next year"
          :class="cn(
            buttonVariants({ variant: 'outline' }),
            'size-7 bg-transparent p-0 opacity-50 hover:opacity-100 pointer-events-auto',
          )"
          @click="emit('next')"
        >
          <ChevronRight class="size-4" />
        </button>
      </nav>
      <button
        type="button"
        aria-label="Switch to year view"
        class="absolute inset-x-0 mx-auto h-7 px-2 text-sm font-medium rounded-md hover:bg-accent w-fit"
        @click="emit('drillUp')"
      >
        {{ placeholder.year }}
      </button>
    </div>

    <div class="grid grid-cols-3 gap-1 mt-4">
      <button
        v-for="month in months"
        :key="month.month"
        type="button"
        :disabled="isDisabled(month)"
        :class="cn(
          'h-9 rounded-md text-sm transition-colors',
          'hover:bg-accent hover:text-accent-foreground',
          'disabled:opacity-40 disabled:pointer-events-none',
          isSelected(month) && 'bg-primary text-primary-foreground hover:bg-primary hover:text-primary-foreground',
          !isSelected(month) && isInRange(month) && 'bg-accent text-accent-foreground',
          !isSelected(month) && !isInRange(month) && isToday(month) && 'ring-1 ring-primary',
        )"
        @click="emit('pick', month.month)"
      >
        {{ formatter.custom(month.toDate(getLocalTimeZone()), { month: 'long' }) }}
      </button>
    </div>
  </div>
</template>
