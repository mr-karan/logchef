<script setup lang="ts">
import { computed } from "vue";
import { useI18n } from "vue-i18n";
import { Badge } from "@/components/ui/badge";
import type { AlertDelivery, AlertHistoryEntry } from "@/api/alerts";

const props = defineProps<{ entry: AlertHistoryEntry }>();
const { t } = useI18n();

// A sent delivery needs no line; the others show their state and last error.
const unsettled = computed(() => (props.entry.deliveries ?? []).filter((delivery) => delivery.status !== "sent"));

function target(delivery: AlertDelivery): string {
  if (!delivery.recipient || delivery.recipient.startsWith("user:")) return delivery.destination_name;
  return `${delivery.destination_name} (${delivery.recipient})`;
}

function pending(delivery: AlertDelivery): boolean {
  return delivery.status === "pending" || delivery.status === "attempting";
}

function statusText(delivery: AlertDelivery): string {
  if (pending(delivery)) return t("alerts.delivering");
  return delivery.status === "unknown" ? t("alerts.deliveryInterrupted") : t("alerts.deliveryFailed");
}
</script>

<template>
  <ul v-if="unsettled.length" class="space-y-1 text-sm" role="status">
    <li v-for="(delivery, index) in unsettled" :key="`${delivery.destination_id}:${delivery.recipient}:${delivery.event}:${index}`" class="space-y-0.5">
      <div class="flex flex-wrap items-center gap-2">
        <span class="font-medium break-all">{{ target(delivery) }}</span>
        <Badge :variant="pending(delivery) ? 'secondary' : 'destructive'">{{ statusText(delivery) }}</Badge>
      </div>
      <p v-if="delivery.last_error" class="text-muted-foreground break-words whitespace-pre-line">{{ delivery.last_error }}</p>
    </li>
  </ul>
</template>
