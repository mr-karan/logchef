<script setup lang="ts">
import { computed, watch, ref } from "vue";
import { storeToRefs } from "pinia";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet";
import { Button } from "@/components/ui/button";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Badge } from "@/components/ui/badge";
import { Textarea } from "@/components/ui/textarea";
import { formatDate } from "@/utils/format";
import { useAlertHistoryStore } from "@/stores/alertHistory";
import { useAlertsStore } from "@/stores/alerts";
import type { Alert } from "@/api/alerts";

const props = defineProps<{
  open: boolean;
  alert: Alert | null;
  teamId: number | null;
  sourceId: number | null;
}>();

const emit = defineEmits<{
  (e: "close"): void;
}>();

const alertHistoryStore = useAlertHistoryStore();
const alertsStore = useAlertsStore();

const { entries } = storeToRefs(alertHistoryStore);

const resolveMessage = ref("");

const isLoadingHistory = computed(() => {
  if (!props.alert) return false;
  return alertHistoryStore.isLoadingOperation(`loadHistory-${props.alert.id}`);
});

const isResolving = computed(() => {
  if (!props.alert) return false;
  return alertHistoryStore.isLoadingOperation(`resolveAlert-${props.alert.id}`);
});

const hasActiveIncident = computed(() => {
  return entries.value.some((entry) => entry.status === "triggered");
});

const alertSummary = computed(() => {
  if (!props.alert) return "";
  return `${props.alert.query_type === "sql" ? "SQL query" : "Log condition"} • ${props.alert.threshold_operator} ${props.alert.threshold_value}, every ${props.alert.frequency_seconds}s`;
});

async function ensureHistoryLoaded() {
  if (!props.open || !props.alert || !props.teamId || !props.sourceId) return;
  await alertHistoryStore.loadHistory(props.alert.id, props.teamId, props.sourceId);
}

async function handleResolve() {
  if (!props.alert || !props.teamId || !props.sourceId) return;
  await alertHistoryStore.resolveCurrentAlert(resolveMessage.value.trim() || undefined);
  await alertsStore.refreshAlert(props.teamId, props.sourceId, props.alert.id);
  resolveMessage.value = "";
}

function handleClose() {
  emit("close");
  resolveMessage.value = "";
}

watch(
  () => props.open,
  (open) => {
    if (open) {
      ensureHistoryLoaded();
    } else {
      resolveMessage.value = "";
    }
  },
  { immediate: true }
);

watch(
  () => props.alert?.id,
  async () => {
    if (props.open) {
      await ensureHistoryLoaded();
    }
  }
);
</script>

<template>
  <Sheet :open="open" @update:open="(value) => !value && handleClose()">
    <SheetContent class="w-[480px] max-w-[90vw]">
      <SheetHeader>
        <SheetTitle v-if="alert">
          {{ alert.name }}
        </SheetTitle>
        <SheetDescription v-if="alert">
          {{ alertSummary }}
        </SheetDescription>
      </SheetHeader>

      <div class="mt-6 flex flex-1 flex-col gap-6">
        <div v-if="isLoadingHistory" class="rounded-lg border border-dashed py-8 text-center text-sm text-muted-foreground">
          Loading history…
        </div>

        <div v-else-if="!entries.length" class="rounded-lg border border-dashed py-8 text-center text-sm text-muted-foreground">
          No alert activity recorded yet.
        </div>

        <ScrollArea v-else class="max-h-[50vh] rounded-lg border p-4">
          <div class="space-y-4">
            <div v-for="entry in entries" :key="entry.id"
              class="rounded-lg border bg-muted/40 p-3">
              <div class="flex items-center justify-between">
                <Badge :variant="entry.status === 'triggered' ? 'destructive' : 'secondary'">
                  {{ entry.status }}
                </Badge>
                <span class="text-xs text-muted-foreground">
                  Triggered {{ formatDate(entry.triggered_at) }}
                </span>
              </div>
              <div class="mt-3 space-y-3 text-xs text-muted-foreground">
                <div class="flex items-center justify-between">
                  <span>Latest value</span>
                  <span class="font-medium text-foreground">
                    {{ entry.value != null ? entry.value : "—" }}
                  </span>
                </div>
                <div v-if="entry.resolved_at">
                  Resolved {{ formatDate(entry.resolved_at) }}
                </div>
                <p v-if="entry.message" class="text-foreground">
                  {{ entry.message }}
                </p>
                <div v-if="entry.payload && (entry.payload.labels || entry.payload.annotations)" class="space-y-2">
                  <div v-if="entry.payload.labels">
                    <h4 class="text-[11px] font-semibold uppercase tracking-wide text-muted-foreground">Labels</h4>
                    <div class="mt-1 grid gap-1">
                      <div
                        v-for="(labelValue, labelKey) in (entry.payload.labels as Record<string, string>)"
                        :key="`label-${labelKey}`"
                        class="flex items-center justify-between gap-2 rounded-md border bg-background px-2 py-1 text-[11px] text-muted-foreground"
                      >
                        <span class="font-medium text-foreground">{{ labelKey }}</span>
                        <span class="truncate">{{ labelValue }}</span>
                      </div>
                    </div>
                  </div>
                  <div v-if="entry.payload.annotations">
                    <h4 class="text-[11px] font-semibold uppercase tracking-wide text-muted-foreground">Annotations</h4>
                    <div class="mt-1 grid gap-1">
                      <div
                        v-for="(annotationValue, annotationKey) in (entry.payload.annotations as Record<string, string>)"
                        :key="`annotation-${annotationKey}`"
                        class="flex items-center justify-between gap-2 rounded-md border bg-background px-2 py-1 text-[11px] text-muted-foreground"
                      >
                        <span class="font-medium text-foreground">{{ annotationKey }}</span>
                        <span class="truncate">{{ annotationValue }}</span>
                      </div>
                    </div>
                  </div>
                </div>
              </div>
            </div>
          </div>
        </ScrollArea>

        <div v-if="alert && hasActiveIncident" class="rounded-lg border bg-muted/40 p-4 space-y-3">
          <div>
            <h3 class="text-sm font-medium">Resolve alert</h3>
            <p class="text-xs text-muted-foreground">
              Provide optional context for the resolution. This will be stored alongside the alert history.
            </p>
          </div>
          <Textarea v-model="resolveMessage" placeholder="Resolved after scaling worker pool…" :rows="3" />
          <div class="flex justify-end gap-2">
            <Button variant="outline" size="sm" @click="resolveMessage = ''">
              Clear
            </Button>
            <Button size="sm" @click="handleResolve" :disabled="isResolving">
              {{ isResolving ? "Resolving…" : "Resolve alert" }}
            </Button>
          </div>
        </div>
      </div>

      <SheetFooter class="mt-6">
        <Button variant="ghost" @click="handleClose">Close</Button>
      </SheetFooter>
    </SheetContent>
  </Sheet>
</template>
