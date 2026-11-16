<script setup lang="ts">
import { computed } from "vue";
import { useI18n } from "vue-i18n";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Toggle } from "@/components/ui/toggle";
import { NativeSelect, NativeSelectOption } from "@/components/ui/native-select";
import type { ReportFrequency, ReportSchedule } from "@/api/reports";
import { scheduleFor, scheduleTime, weekdayNames } from "@/utils/reportSchedule";

const schedule = defineModel<ReportSchedule>({ required: true });
const { t, locale } = useI18n();

const frequencies: ReportFrequency[] = ["hourly", "daily", "weekly", "monthly"];
const days = computed(() => weekdayNames(locale.value));
// Monday first, as most calendars show a week.
const dayOrder = [1, 2, 3, 4, 5, 6, 0];

const frequency = computed({
  get: () => schedule.value.frequency,
  set: (value: ReportFrequency) => {
    schedule.value = scheduleFor(value, schedule.value);
  },
});

function setTime(value: string | number) {
  const [h, m] = String(value).split(":").map(Number);
  if (Number.isInteger(h) && Number.isInteger(m)) schedule.value = { ...schedule.value, hour: h, minute: m };
}

function setMinute(value: string | number) {
  const m = Number(value);
  if (Number.isInteger(m) && m >= 0 && m <= 59) schedule.value = { ...schedule.value, minute: m };
}

function setDayOfMonth(value: string | number) {
  const d = Number(value);
  if (Number.isInteger(d) && d >= 1 && d <= 28) schedule.value = { ...schedule.value, day_of_month: d };
}

function toggleDay(day: number, on: boolean) {
  const current = new Set(schedule.value.weekdays ?? []);
  if (on) current.add(day);
  else if (current.size > 1) current.delete(day);
  schedule.value = { ...schedule.value, weekdays: [...current].sort((a, b) => a - b) };
}
</script>

<template>
  <div class="grid gap-3 sm:grid-cols-2">
    <div class="space-y-1.5">
      <Label for="report-frequency">{{ t("reports.frequency") }}</Label>
      <NativeSelect id="report-frequency" class="w-full" v-model="frequency">
        <NativeSelectOption v-for="f in frequencies" :key="f" :value="f">{{ t(`reports.frequency_${f}`) }}</NativeSelectOption>
      </NativeSelect>
    </div>

    <div v-if="schedule.frequency === 'hourly'" class="space-y-1.5">
      <Label for="report-minute">{{ t("reports.minutePastHour") }}</Label>
      <Input id="report-minute" type="number" min="0" max="59" :model-value="schedule.minute" @update:model-value="setMinute" />
    </div>
    <div v-else class="space-y-1.5">
      <Label for="report-time">{{ t("reports.time") }}</Label>
      <Input id="report-time" type="time" step="60" :model-value="scheduleTime(schedule)" @update:model-value="setTime" />
    </div>

    <div v-if="schedule.frequency === 'weekly'" class="space-y-1.5 sm:col-span-2">
      <Label>{{ t("reports.days") }}</Label>
      <div class="flex flex-wrap gap-1">
        <Toggle
          v-for="d in dayOrder"
          :key="d"
          size="sm"
          variant="outline"
          :model-value="schedule.weekdays?.includes(d) ?? false"
          :aria-label="days[d]"
          @update:model-value="(on: boolean) => toggleDay(d, on)"
        >
          {{ days[d] }}
        </Toggle>
      </div>
    </div>

    <div v-if="schedule.frequency === 'monthly'" class="space-y-1.5">
      <Label for="report-day">{{ t("reports.dayOfMonth") }}</Label>
      <Input id="report-day" type="number" min="1" max="28" :model-value="schedule.day_of_month" @update:model-value="setDayOfMonth" />
    </div>
  </div>
</template>
