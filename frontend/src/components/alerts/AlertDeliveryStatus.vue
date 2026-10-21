<script setup lang="ts">
import { computed } from "vue";
import { useI18n } from "vue-i18n";
import { Badge } from "@/components/ui/badge";
import { alertDeliveryFailure } from "@/stores/alertHistory";
import type { AlertHistoryEntry } from "@/api/alerts";

const props = defineProps<{ entry: AlertHistoryEntry }>();
const failure = computed(() => alertDeliveryFailure(props.entry));
const inProgress = computed(() => props.entry.status === "triggered" && !props.entry.resolved_at && props.entry.payload?.delivery_in_progress === true);
const { t } = useI18n();
</script>

<template>
  <div v-if="inProgress" role="status">
    <Badge variant="secondary">{{ t('alerts.delivering') }}</Badge>
  </div>
  <div v-else-if="failure" class="space-y-1 text-sm" role="status">
    <Badge variant="destructive">{{ t('alerts.deliveryFailed') }}</Badge>
    <p v-if="failure.message" class="text-muted-foreground break-words whitespace-pre-line">{{ failure.message }}</p>
    <p v-if="failure.retriesExhausted" class="text-muted-foreground">{{ t('alerts.deliveryRetriesExhausted') }}</p>
  </div>
</template>
