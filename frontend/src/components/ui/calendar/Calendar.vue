<script lang="ts" setup>
import type { CalendarRootEmits, CalendarRootProps, DateValue } from "reka-ui"
import type { HTMLAttributes, Ref } from "vue"
import { ref } from "vue"
import { getLocalTimeZone, today } from "@internationalized/date"
import { reactiveOmit, useVModel } from "@vueuse/core"
import { CalendarRoot, useDateFormatter, useForwardPropsEmits } from "reka-ui"
import { toDate } from "reka-ui/date"
import {
  CalendarCell,
  CalendarCellTrigger,
  CalendarGrid,
  CalendarGridBody,
  CalendarGridHead,
  CalendarGridRow,
  CalendarHeadCell,
  CalendarHeader,
  CalendarNextButton,
  CalendarPrevButton,
} from "."
import CalendarMonthGrid from "./CalendarMonthGrid.vue"
import CalendarYearGrid from "./CalendarYearGrid.vue"

const props = withDefaults(
  defineProps<CalendarRootProps & { class?: HTMLAttributes["class"] }>(),
  {
    modelValue: undefined,
  },
)
const emits = defineEmits<CalendarRootEmits>()

const delegatedProps = reactiveOmit(props, "class", "placeholder")

const placeholder = useVModel(props, "placeholder", emits, {
  passive: true,
  defaultValue: props.defaultPlaceholder ?? today(getLocalTimeZone()),
}) as Ref<DateValue>

const formatter = useDateFormatter(props.locale ?? "en")

type View = "days" | "months" | "years"
const view = ref<View>("days")

function drillUp() {
  if (view.value === "days") view.value = "months"
  else if (view.value === "months") view.value = "years"
}

function step(direction: -1 | 1) {
  const amount = view.value === "months" ? direction : direction * 10
  placeholder.value = placeholder.value.add({ years: amount })
}

function pickMonth(month: number) {
  placeholder.value = placeholder.value.set({ month })
  view.value = "days"
}

function pickYear(year: number) {
  placeholder.value = placeholder.value.set({ year })
  view.value = "months"
}

const forwarded = useForwardPropsEmits(delegatedProps, emits)
</script>

<template>
  <div :class="props.class" data-slot="calendar">
    <CalendarRoot
      v-show="view === 'days'"
      v-slot="{ grid, weekDays }"
      v-bind="forwarded"
      v-model:placeholder="placeholder"
      class="p-3"
    >
      <CalendarHeader class="pt-0">
        <nav class="flex items-center gap-1 absolute top-0 inset-x-0 justify-between pointer-events-none">
          <CalendarPrevButton class="pointer-events-auto">
            <slot name="calendar-prev-icon" />
          </CalendarPrevButton>
          <CalendarNextButton class="pointer-events-auto">
            <slot name="calendar-next-icon" />
          </CalendarNextButton>
        </nav>
        <button
          type="button"
          aria-label="Switch to month view"
          class="block mx-auto h-7 px-2 text-sm font-medium rounded-md hover:bg-accent"
          @click="drillUp"
        >
          {{ formatter.custom(toDate(placeholder), { month: 'long', year: 'numeric' }) }}
        </button>
      </CalendarHeader>

      <div class="flex flex-col gap-y-4 mt-4 sm:flex-row sm:gap-x-4 sm:gap-y-0">
        <CalendarGrid v-for="month in grid" :key="month.value.toString()">
          <CalendarGridHead>
            <CalendarGridRow>
              <CalendarHeadCell v-for="day in weekDays" :key="day">
                {{ day }}
              </CalendarHeadCell>
            </CalendarGridRow>
          </CalendarGridHead>
          <CalendarGridBody>
            <CalendarGridRow
              v-for="(weekDates, index) in month.rows"
              :key="`weekDate-${index}`"
              class="mt-2 w-full"
            >
              <CalendarCell
                v-for="weekDate in weekDates"
                :key="weekDate.toString()"
                :date="weekDate"
              >
                <CalendarCellTrigger :day="weekDate" :month="month.value" />
              </CalendarCell>
            </CalendarGridRow>
          </CalendarGridBody>
        </CalendarGrid>
      </div>
    </CalendarRoot>

    <CalendarMonthGrid
      v-if="view === 'months'"
      :placeholder="placeholder"
      :model-value="props.modelValue"
      :min-value="props.minValue"
      :max-value="props.maxValue"
      :locale="props.locale"
      @prev="step(-1)"
      @next="step(1)"
      @drill-up="drillUp"
      @pick="pickMonth"
    />

    <CalendarYearGrid
      v-if="view === 'years'"
      :placeholder="placeholder"
      :model-value="props.modelValue"
      :min-value="props.minValue"
      :max-value="props.maxValue"
      @prev="step(-1)"
      @next="step(1)"
      @pick="pickYear"
    />
  </div>
</template>
