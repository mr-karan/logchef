<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { Badge } from "@/components/ui/badge";
import { alertDeliveryFailure } from "@/stores/alertHistory";
import type { AlertHistoryEntry } from "@/api/alerts";

const props = defineProps<{ entry: AlertHistoryEntry }>();
const now = ref(Date.now());
const deliveryActive = computed(() => props.entry.status === "triggered"
  && !props.entry.resolved_at && props.entry.payload?.delivery_in_progress === true);
watch(deliveryActive, (active, _, onCleanup) => {
  now.value = Date.now();
  if (active) {
    const timer = setInterval(() => { now.value = Date.now(); }, 1000);
    onCleanup(() => { clearInterval(timer); });
  }
}, { immediate: true });
const interrupted = computed(() => {
  const deadline = props.entry.payload?.delivery_deadline_at;
  const parsed = typeof deadline === "string" ? Date.parse(deadline) : Number.NaN;
  // Older reservations only stored the original trigger time.
  const deadlineMs = Number.isNaN(parsed)
    ? Date.parse(props.entry.triggered_at) + 3 * (60 + 30 + 2) * 1000
    : parsed;
  return deliveryActive.value && now.value >= deadlineMs;
});
const failure = computed(() => alertDeliveryFailure(props.entry));
const inProgress = computed(() => deliveryActive.value && !interrupted.value);
const { t } = useI18n();
</script>

<template>
  <div v-if="inProgress" role="status">
    <Badge variant="secondary">{{ t('alerts.delivering') }}</Badge>
  </div>
  <div v-else-if="interrupted" role="status">
    <Badge variant="destructive">{{ t('alerts.deliveryInterrupted') }}</Badge>
  </div>
  <div v-else-if="failure" class="space-y-1 text-sm" role="status">
    <Badge variant="destructive">{{ t('alerts.deliveryFailed') }}</Badge>
    <p v-if="failure.message" class="text-muted-foreground break-words whitespace-pre-line">{{ failure.message }}</p>
    <p v-if="failure.retriesExhausted" class="text-muted-foreground">{{ t('alerts.deliveryRetriesExhausted') }}</p>
  </div>
</template>
