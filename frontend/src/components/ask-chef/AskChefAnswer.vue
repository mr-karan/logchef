<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { ChevronRight } from "lucide-vue-next";
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible";
import type { EvidenceRecord } from "@/api/askChef";
import { useAskChefStore, type AssistantTurn, type ToolStep } from "@/stores/askChef";
import { parseAnswer } from "@/utils/askChefMarkdown";
import AskChefBlocks from "./AskChefBlocks.vue";

const props = defineProps<{ turn: AssistantTurn; live: boolean }>();

const { t } = useI18n();
const askChef = useAskChefStore();

const stepsOpen = ref(!props.turn.done);
watch(
  () => props.turn.done,
  (done) => {
    if (done) stepsOpen.value = false;
  }
);

const answer = computed(() => parseAnswer(props.turn.text, askChef.evidence));
const evidence = computed(() =>
  props.turn.evidenceIds.flatMap((id) => {
    const record = askChef.evidence.get(id);
    return record ? [record] : [];
  })
);

const stepsSummary = computed(() => {
  const done = props.turn.done;
  if (done) {
    const seconds = (done.duration_ms / 1000).toFixed(1);
    return t("askChef.toolSummary", { count: done.tool_calls, seconds }, done.tool_calls);
  }
  return props.live ? t("askChef.investigating") : t("askChef.stopped");
});

const stopNote = computed(() => {
  const reason = props.turn.done?.stop_reason;
  if (reason === "tool_limit") return t("askChef.stopToolLimit");
  if (reason === "deadline") return t("askChef.stopDeadline");
  if (reason === "max_tokens") return t("askChef.stopMaxTokens");
  if (reason === "budget") return t("askChef.stopBudget");
  if (reason === "preempted") return t("askChef.stopPreempted");
  return "";
});

function dotClass(step: ToolStep): string {
  if (step.status === "ok") return "bg-green-600";
  if (step.status === "failed") return "bg-red-600";
  return props.live ? "animate-pulse bg-amber-500" : "bg-muted-foreground/40";
}

function evidenceRowClass(record: EvidenceRecord): string {
  return askChef.activeEvidenceId === record.id ? "bg-blue-50 dark:bg-blue-950/50" : "hover:bg-blue-50 dark:hover:bg-blue-950/50";
}
</script>

<template>
  <div class="flex flex-col gap-2.5">
    <Collapsible v-if="turn.steps.length || live" v-model:open="stepsOpen" class="rounded-lg border">
      <CollapsibleTrigger class="flex w-full items-center gap-1.5 px-2.5 py-1.5 text-left text-xs text-muted-foreground">
        <ChevronRight class="h-3.5 w-3.5 transition-transform" :class="{ 'rotate-90': stepsOpen }" />
        <span>{{ stepsSummary }}</span>
      </CollapsibleTrigger>
      <CollapsibleContent>
        <div v-for="step in turn.steps" :key="step.callId" class="flex items-start gap-2 border-t px-2.5 py-1.5">
          <span class="mt-0.5 h-3 w-3 flex-none rounded-full" :class="dotClass(step)" />
          <div class="min-w-0">
            <code class="rounded border bg-muted px-1 font-mono text-[10.5px]">{{ step.tool }}</code>
            <div v-if="step.status !== 'running'" class="text-[11px] text-muted-foreground">{{ step.summary }}</div>
          </div>
        </div>
      </CollapsibleContent>
    </Collapsible>

    <div v-if="answer.body.length" class="text-[12.5px] leading-relaxed">
      <AskChefBlocks :blocks="answer.body" />
    </div>

    <div
      v-if="answer.notEstablished"
      class="rounded-lg border border-dashed border-amber-300 bg-amber-50 px-2.5 py-2 text-[11.5px] text-amber-900 dark:border-amber-700 dark:bg-amber-950/40 dark:text-amber-200"
    >
      <AskChefBlocks :blocks="answer.notEstablished" />
    </div>

    <p v-if="stopNote" class="text-[11px] text-muted-foreground">{{ stopNote }}</p>

    <div v-if="evidence.length" class="overflow-hidden rounded-lg border">
      <div class="border-b bg-muted/50 px-2.5 py-1.5 text-[11px] text-muted-foreground">{{ t("askChef.evidence") }}</div>
      <button
        v-for="record in evidence"
        :key="record.id"
        type="button"
        class="flex w-full items-start gap-2 border-t px-2.5 py-1.5 text-left first-of-type:border-t-0"
        :class="evidenceRowClass(record)"
        :aria-label="t('askChef.openEvidence', { id: record.id })"
        @click="askChef.openEvidence(record.id)"
      >
        <span
          class="mt-0.5 inline-flex h-4 min-w-4 flex-none items-center justify-center rounded-[5px] border border-blue-200 bg-blue-50 px-1 text-[10px] font-semibold text-blue-600 dark:border-blue-800 dark:bg-blue-950 dark:text-blue-300"
        >
          {{ record.id.slice(1) }}
        </span>
        <span class="min-w-0 text-xs">
          <span class="block">{{ record.summary }}</span>
          <span class="block truncate font-mono text-[10.5px] text-muted-foreground">{{ record.tool }} · {{ record.query }}</span>
        </span>
      </button>
    </div>
  </div>
</template>
