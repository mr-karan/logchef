<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { Plus, X } from "lucide-vue-next";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { Toggle } from "@/components/ui/toggle";
import { NativeSelect, NativeSelectOption } from "@/components/ui/native-select";
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { useSilencesStore } from "@/stores/silences";
import { usePreferencesStore } from "@/stores/preferences";
import type { Alert } from "@/api/alerts";
import type { AlertSilence, AlertSilenceRequest, SilenceScheduleKind } from "@/api/silences";
import { fromDateTimeInput, toDateTimeInput } from "@/utils/alertSilences";
import { weekdayNames } from "@/utils/reportSchedule";

const MAX_MATCHERS = 5;
const HOUR_MS = 3_600_000;

const open = defineModel<boolean>("open", { required: true });
const props = defineProps<{
  sourceId: number;
  /** The rules of the source, for the rule select. */
  rules: Alert[];
  /** The silence to edit; null creates one. */
  silence: AlertSilence | null;
  /** The values a new silence starts with. */
  preset?: AlertSilenceRequest | null;
}>();
const emit = defineEmits<{ (e: "saved", silence: AlertSilence): void }>();

const { t, locale } = useI18n();
const store = useSilencesStore();
const preferences = usePreferencesStore();
const zone = computed(() => preferences.zone);
const knownZones = typeof Intl.supportedValuesOf === "function" ? Intl.supportedValuesOf("timeZone") : [];
const days = computed(() => weekdayNames(locale.value));
// Monday first, as most calendars show a week.
const dayOrder = [1, 2, 3, 4, 5, 6, 0];

const ruleId = ref("");
const matchers = ref<{ name: string; value: string }[]>([]);
const comment = ref("");
const kind = ref<SilenceScheduleKind>("once");
const startsAt = ref("");
const endsAt = ref("");
const weekdays = ref<number[]>([]);
const weeklyStart = ref("02:00");
const durationMinutes = ref(120);
const timezone = ref("UTC");
const until = ref("");
const formError = ref("");
// Some browsers leave UTC out of the supported list; the chosen zone is always an option.
const zones = computed(() => (knownZones.includes(timezone.value) ? knownZones : [timezone.value, ...knownZones]));

const saving = computed(() => store.isLoadingOperation("saveSilence"));

function load() {
  const now = Date.now();
  const base: AlertSilenceRequest = props.silence
    ? {
        source_id: props.silence.source_id,
        alert_id: props.silence.alert_id ?? null,
        matchers: props.silence.matchers ?? {},
        comment: props.silence.comment,
        schedule_kind: props.silence.schedule_kind,
        starts_at: props.silence.starts_at ?? null,
        ends_at: props.silence.ends_at ?? null,
        weekly: props.silence.weekly ?? null,
      }
    : props.preset ?? {
        source_id: props.sourceId, alert_id: null, matchers: {}, comment: "",
        schedule_kind: "once", starts_at: null, ends_at: null, weekly: null,
      };
  ruleId.value = base.alert_id ? String(base.alert_id) : "";
  matchers.value = Object.entries(base.matchers).map(([name, value]) => ({ name, value }));
  comment.value = base.comment;
  kind.value = base.schedule_kind;
  const start = base.schedule_kind === "once" && base.starts_at ? new Date(base.starts_at).getTime() : now;
  const end = base.schedule_kind === "once" && base.ends_at ? new Date(base.ends_at).getTime() : start + HOUR_MS;
  startsAt.value = toDateTimeInput(start, zone.value);
  endsAt.value = toDateTimeInput(end, zone.value);
  weekdays.value = base.weekly ? [...base.weekly.weekdays] : [6, 0];
  weeklyStart.value = base.weekly?.start ?? "02:00";
  durationMinutes.value = base.weekly?.duration_minutes ?? 120;
  timezone.value = base.weekly?.timezone ?? zone.value;
  until.value = base.schedule_kind === "weekly" && base.ends_at ? toDateTimeInput(new Date(base.ends_at).getTime(), zone.value) : "";
  formError.value = "";
}

watch(open, (isOpen) => {
  if (isOpen) load();
}, { immediate: true });

function addMatcher() {
  if (matchers.value.length < MAX_MATCHERS) matchers.value.push({ name: "", value: "" });
}

function toggleDay(day: number, on: boolean) {
  const set = new Set(weekdays.value);
  if (on) set.add(day);
  else set.delete(day);
  weekdays.value = [...set].sort((a, b) => a - b);
}

/** The request, or null after setting formError for a time that does not parse. */
function request(): AlertSilenceRequest | null {
  const base = {
    source_id: props.sourceId,
    alert_id: ruleId.value ? Number(ruleId.value) : null,
    matchers: Object.fromEntries(
      matchers.value.filter((m) => m.name.trim()).map((m) => [m.name.trim(), m.value]),
    ),
    comment: comment.value.trim(),
  };
  if (kind.value === "once") {
    const start = fromDateTimeInput(startsAt.value, zone.value);
    const end = fromDateTimeInput(endsAt.value, zone.value);
    if (!start || !end) {
      formError.value = t("silences.invalidTime");
      return null;
    }
    return { ...base, schedule_kind: "once", starts_at: start, ends_at: end, weekly: null };
  }
  const last = until.value ? fromDateTimeInput(until.value, zone.value) : null;
  if (until.value && !last) {
    formError.value = t("silences.invalidTime");
    return null;
  }
  return {
    ...base,
    schedule_kind: "weekly",
    starts_at: null,
    ends_at: last,
    weekly: { weekdays: weekdays.value, start: weeklyStart.value, duration_minutes: Number(durationMinutes.value), timezone: timezone.value },
  };
}

async function save() {
  formError.value = "";
  const payload = request();
  if (!payload) return;
  const result = props.silence
    ? await store.updateSilence(props.silence.id, payload)
    : await store.createSilence(payload);
  // A failure shows the server's message as a toast and keeps the form open.
  if (result.success && result.data) {
    emit("saved", result.data);
    open.value = false;
  }
}
</script>

<template>
  <Dialog v-model:open="open">
    <DialogContent class="sm:max-w-xl max-h-[90vh] overflow-y-auto">
      <DialogHeader>
        <DialogTitle>{{ silence ? t("silences.editSilence") : t("silences.newSilence") }}</DialogTitle>
      </DialogHeader>

      <form class="space-y-5" @submit.prevent="save">
        <div class="space-y-1.5">
          <Label for="silence-rule">{{ t("silences.rule") }}</Label>
          <NativeSelect id="silence-rule" v-model="ruleId" class="w-full">
            <NativeSelectOption value="">{{ t("silences.allRules") }}</NativeSelectOption>
            <NativeSelectOption v-for="rule in rules" :key="rule.id" :value="String(rule.id)">{{ rule.name }}</NativeSelectOption>
          </NativeSelect>
        </div>

        <div class="space-y-1.5">
          <Label>{{ t("silences.matchers") }}</Label>
          <div v-for="(matcher, i) in matchers" :key="i" class="flex items-center gap-2">
            <Input v-model="matcher.name" :aria-label="t('silences.labelName')" :placeholder="t('silences.labelName')" class="font-mono text-xs" />
            <span class="text-muted-foreground">=</span>
            <Input v-model="matcher.value" :aria-label="t('silences.labelValue')" :placeholder="t('silences.labelValue')" class="font-mono text-xs" />
            <Button type="button" variant="ghost" size="icon" class="h-8 w-8 shrink-0" :aria-label="t('silences.removeMatcher')" :title="t('silences.removeMatcher')" @click="matchers.splice(i, 1)">
              <X class="h-4 w-4" />
            </Button>
          </div>
          <Button v-if="matchers.length < MAX_MATCHERS" type="button" variant="outline" size="sm" @click="addMatcher">
            <Plus class="mr-1.5 h-4 w-4" />{{ t("silences.addMatcher") }}
          </Button>
        </div>

        <div class="space-y-1.5">
          <Label for="silence-comment">{{ t("silences.comment") }}</Label>
          <Textarea id="silence-comment" v-model="comment" maxlength="1000" rows="2" :placeholder="t('silences.commentPlaceholder')" />
        </div>

        <div class="space-y-3">
          <div class="space-y-1.5">
            <Label for="silence-kind">{{ t("silences.schedule") }}</Label>
            <NativeSelect id="silence-kind" v-model="kind" class="w-full">
              <NativeSelectOption value="once">{{ t("silences.kindOnce") }}</NativeSelectOption>
              <NativeSelectOption value="weekly">{{ t("silences.kindWeekly") }}</NativeSelectOption>
            </NativeSelect>
          </div>

          <div v-if="kind === 'once'" class="grid gap-3 sm:grid-cols-2">
            <div class="space-y-1.5">
              <Label for="silence-starts">{{ t("silences.startsAt", { zone }) }}</Label>
              <Input id="silence-starts" v-model="startsAt" type="datetime-local" />
            </div>
            <div class="space-y-1.5">
              <Label for="silence-ends">{{ t("silences.endsAt", { zone }) }}</Label>
              <Input id="silence-ends" v-model="endsAt" type="datetime-local" />
            </div>
          </div>

          <template v-else>
            <div class="space-y-1.5">
              <Label>{{ t("silences.days") }}</Label>
              <div class="flex flex-wrap gap-1">
                <Toggle
                  v-for="d in dayOrder"
                  :key="d"
                  size="sm"
                  variant="outline"
                  :model-value="weekdays.includes(d)"
                  :aria-label="days[d]"
                  @update:model-value="(on: boolean) => toggleDay(d, on)"
                >
                  {{ days[d] }}
                </Toggle>
              </div>
            </div>
            <div class="grid gap-3 sm:grid-cols-2">
              <div class="space-y-1.5">
                <Label for="silence-weekly-start">{{ t("silences.startTime") }}</Label>
                <Input id="silence-weekly-start" v-model="weeklyStart" type="time" step="60" />
              </div>
              <div class="space-y-1.5">
                <Label for="silence-duration">{{ t("silences.durationMinutes") }}</Label>
                <Input id="silence-duration" v-model.number="durationMinutes" type="number" min="1" max="1440" />
              </div>
              <div class="space-y-1.5">
                <Label for="silence-timezone">{{ t("silences.timezone") }}</Label>
                <NativeSelect id="silence-timezone" v-model="timezone" class="w-full">
                  <NativeSelectOption v-for="z in zones" :key="z" :value="z">{{ z }}</NativeSelectOption>
                </NativeSelect>
              </div>
              <div class="space-y-1.5">
                <Label for="silence-until">{{ t("silences.until", { zone }) }}</Label>
                <Input id="silence-until" v-model="until" type="datetime-local" />
              </div>
            </div>
          </template>
        </div>

        <p v-if="formError" role="alert" class="text-sm text-destructive">{{ formError }}</p>

        <DialogFooter>
          <Button type="button" variant="outline" @click="open = false">{{ t("ui.cancel") }}</Button>
          <Button type="submit" :disabled="saving || !comment.trim()">
            {{ silence ? t("silences.save") : t("silences.create") }}
          </Button>
        </DialogFooter>
      </form>
    </DialogContent>
  </Dialog>
</template>
