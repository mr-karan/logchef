<script setup lang="ts">
import { useI18n } from "vue-i18n";
import { Badge } from "@/components/ui/badge";
import type { AlertDeliveryFailure } from "@/stores/alertHistory";

defineProps<{ failure: AlertDeliveryFailure | null }>();
const { t } = useI18n();
</script>

<template>
  <div v-if="failure" class="space-y-1 text-sm" role="status">
    <Badge variant="destructive">{{ t('alerts.deliveryFailed') }}</Badge>
    <p v-if="failure.message" class="text-muted-foreground break-words">{{ failure.message }}</p>
    <p v-if="failure.retriesExhausted" class="text-muted-foreground">{{ t('alerts.deliveryRetriesExhausted') }}</p>
  </div>
</template>
