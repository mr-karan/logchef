<script setup lang="ts">
import { computed, nextTick, ref, watch } from "vue";
import { Button } from "@/components/ui/button";
import { Radio, Pause, ArrowUp, AlertTriangle, RotateCw } from "lucide-vue-next";
import type { LiveTailStatus } from "@/stores/explore";

const props = defineProps<{
  rows: Record<string, any>[];
  status: LiveTailStatus;
  notice: string | null;
  droppedCount: number;
  endReason: string | null;
  endMessage?: string | null;
  error: string | null;
  timestampField?: string;
}>();

const emit = defineEmits<{
  (e: "resume"): void;
  (e: "stop"): void;
}>();

const scrollRef = ref<HTMLElement | null>(null);
// Auto-scroll is "pinned" to the top; if the user scrolls down we pause it and
// surface a "N new rows" pill instead of yanking the viewport.
const isPinnedToTop = ref(true);
const newRowsCount = ref(0);

const PIN_THRESHOLD_PX = 12;

function onScroll() {
  const el = scrollRef.value;
  if (!el) return;
  const pinned = el.scrollTop <= PIN_THRESHOLD_PX;
  isPinnedToTop.value = pinned;
  if (pinned) newRowsCount.value = 0;
}

function scrollToTop() {
  const el = scrollRef.value;
  if (!el) return;
  el.scrollTo({ top: 0, behavior: "smooth" });
  isPinnedToTop.value = true;
  newRowsCount.value = 0;
}

watch(
  () => props.rows.length,
  (next, prev) => {
    const added = next - (prev ?? 0);
    if (added <= 0) {
      // buffer reset or unchanged
      if (next === 0) newRowsCount.value = 0;
      return;
    }
    if (isPinnedToTop.value) {
      nextTick(() => {
        const el = scrollRef.value;
        if (el) el.scrollTop = 0;
      });
    } else {
      newRowsCount.value += added;
    }
  }
);

const statusLabel = computed(() => {
  switch (props.status) {
    case "connecting":
      return "Connecting…";
    case "streaming":
      return "Live";
    case "ended":
      return "Tail ended";
    case "error":
      return "Error";
    default:
      return "Idle";
  }
});

function fieldPairs(row: Record<string, any>): Array<{ key: string; value: string }> {
  return Object.keys(row)
    .filter((k) => k !== props.timestampField)
    .map((k) => ({ key: k, value: formatValue(row[k]) }));
}

function formatValue(value: unknown): string {
  if (value === null || value === undefined) return "";
  if (typeof value === "object") return JSON.stringify(value);
  return String(value);
}

function timestampOf(row: Record<string, any>): string {
  if (!props.timestampField) return "";
  return formatValue(row[props.timestampField]);
}
</script>

<template>
  <div class="flex h-full flex-col overflow-hidden bg-background" data-testid="live-tail-panel">
    <!-- Header -->
    <div class="flex items-center justify-between gap-2 border-b px-4 py-2">
      <div class="flex items-center gap-2">
        <span
          class="flex items-center gap-1.5 text-xs font-medium"
          :class="{
            'text-emerald-600 dark:text-emerald-400': status === 'streaming',
            'text-muted-foreground': status === 'connecting' || status === 'idle',
            'text-amber-600 dark:text-amber-400': status === 'ended',
            'text-destructive': status === 'error',
          }"
        >
          <Radio
            class="h-3.5 w-3.5"
            :class="status === 'streaming' ? 'animate-pulse' : ''"
          />
          {{ statusLabel }}
        </span>
        <span class="text-xs text-muted-foreground">
          {{ rows.length.toLocaleString() }} row{{ rows.length === 1 ? "" : "s" }}
          <span v-if="rows.length >= 500" class="italic">(buffer full)</span>
        </span>
      </div>
      <Button variant="destructive" size="sm" class="h-7 gap-1.5 px-3" @click="emit('stop')">
        <Pause class="h-3.5 w-3.5" />
        <span class="font-medium">Stop</span>
      </Button>
    </div>

    <!-- Rate-limit notice banner -->
    <div
      v-if="notice"
      class="flex items-center gap-2 border-b bg-amber-50 px-4 py-1.5 text-xs text-amber-800 dark:bg-amber-950/40 dark:text-amber-300"
    >
      <AlertTriangle class="h-3.5 w-3.5 flex-shrink-0" />
      <span>{{ notice }}</span>
      <span v-if="droppedCount > 0" class="ml-auto font-medium">
        {{ droppedCount.toLocaleString() }} dropped
      </span>
    </div>

    <!-- Rows viewport -->
    <div class="relative flex-1 overflow-hidden">
      <!-- "N new rows" pill -->
      <button
        v-if="!isPinnedToTop && newRowsCount > 0"
        class="absolute left-1/2 top-2 z-10 flex -translate-x-1/2 items-center gap-1.5 rounded-full bg-emerald-600 px-3 py-1 text-xs font-medium text-white shadow-md hover:bg-emerald-700"
        @click="scrollToTop"
      >
        <ArrowUp class="h-3.5 w-3.5" />
        {{ newRowsCount.toLocaleString() }} new row{{ newRowsCount === 1 ? "" : "s" }}
      </button>

      <div ref="scrollRef" class="h-full overflow-y-auto font-mono text-xs" @scroll="onScroll">
        <div
          v-for="(row, idx) in rows"
          :key="idx"
          class="flex gap-2 border-b border-border/40 px-4 py-1 hover:bg-muted/40"
        >
          <span v-if="timestampField" class="whitespace-nowrap text-muted-foreground">
            {{ timestampOf(row) }}
          </span>
          <span class="flex flex-wrap gap-x-3 gap-y-0.5">
            <span v-for="pair in fieldPairs(row)" :key="pair.key">
              <span class="text-muted-foreground">{{ pair.key }}=</span><span>{{ pair.value }}</span>
            </span>
          </span>
        </div>

        <!-- Empty / waiting state -->
        <div
          v-if="rows.length === 0"
          class="flex h-full items-center justify-center p-6 text-center"
        >
          <p class="text-sm text-muted-foreground">
            <template v-if="status === 'error'">Live tail failed.</template>
            <template v-else-if="status === 'ended'">Tail ended.</template>
            <template v-else>Waiting for new log lines…</template>
          </p>
        </div>
      </div>
    </div>

    <!-- Error / end affordance -->
    <div
      v-if="status === 'error'"
      class="flex items-center gap-2 border-t bg-destructive/10 px-4 py-2 text-xs text-destructive"
    >
      <AlertTriangle class="h-3.5 w-3.5 flex-shrink-0" />
      <span class="flex-1">{{ error || "Live tail connection failed." }}</span>
      <Button variant="outline" size="sm" class="h-7 gap-1.5" @click="emit('resume')">
        <RotateCw class="h-3.5 w-3.5" />
        Retry
      </Button>
    </div>
    <div
      v-else-if="status === 'ended'"
      class="flex items-center gap-2 border-t bg-amber-50 px-4 py-2 text-xs text-amber-800 dark:bg-amber-950/40 dark:text-amber-300"
    >
      <span class="flex-1">
        Tail ended<template v-if="endReason"> ({{ endReason }})</template>.
        <template v-if="endMessage"> {{ endMessage }}</template>
        No automatic reconnect.
      </span>
      <Button variant="outline" size="sm" class="h-7 gap-1.5" @click="emit('resume')">
        <RotateCw class="h-3.5 w-3.5" />
        Resume
      </Button>
    </div>
  </div>
</template>
