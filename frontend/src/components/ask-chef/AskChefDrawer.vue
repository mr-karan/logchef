<script setup lang="ts">
import { computed, nextTick, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { SquarePen, X } from "lucide-vue-next";
import { Button } from "@/components/ui/button";
import { ResizableHandle, ResizablePanel } from "@/components/ui/resizable";
import { useAskChefStore } from "@/stores/askChef";
import { useMetaStore } from "@/stores/meta";
import AskChefAnswer from "./AskChefAnswer.vue";
import AskChefContextCard from "./AskChefContextCard.vue";
import AskChefPrompt from "./AskChefPrompt.vue";

const { t } = useI18n();
const askChef = useAskChefStore();
const metaStore = useMetaStore();
const body = ref<HTMLElement | null>(null);

const streaming = computed(() => askChef.status.kind === "streaming");
const lastTurn = computed(() => askChef.transcript[askChef.transcript.length - 1]);
const showSuggestions = computed(() => askChef.status.kind === "idle" && lastTurn.value?.role === "assistant" && lastTurn.value.done !== null);
const suggestions = computed(() => [t("askChef.suggestCompare"), t("askChef.suggestSamples"), t("askChef.suggestHosts")]);

// Follow new output only while the reader is already at the bottom.
const contentSize = computed(() => {
  const turn = lastTurn.value;
  return `${askChef.transcript.length}:${turn?.role === "assistant" ? `${turn.steps.length}:${turn.text.length}:${turn.done !== null}` : ""}`;
});
let following = true;
function onScroll() {
  const el = body.value;
  if (el) following = el.scrollHeight - el.scrollTop - el.clientHeight < 48;
}
watch(contentSize, async () => {
  if (!following) return;
  await nextTick();
  body.value?.scrollTo({ top: body.value.scrollHeight });
});
</script>

<template>
  <template v-if="metaStore.askChefEnabled && askChef.open">
    <ResizableHandle with-handle />
    <ResizablePanel id="ask-chef" :order="2" size-unit="px" :default-size="320" :min-size="280" :max-size="720">
      <aside class="flex h-full flex-col bg-background" :aria-label="t('askChef.title')">
        <header class="flex items-center gap-1.5 border-b px-3 py-2">
          <span class="text-violet-600" aria-hidden="true">✦</span>
          <h2 class="text-sm font-semibold">{{ t("askChef.title") }}</h2>
          <span class="flex-1" />
          <Button variant="ghost" size="sm" class="h-7 gap-1 px-2 text-xs" :disabled="!askChef.transcript.length" @click="askChef.reset()">
            <SquarePen class="h-3.5 w-3.5" />
            {{ t("askChef.newChat") }}
          </Button>
          <Button variant="ghost" size="icon-sm" class="h-7 w-7" :aria-label="t('askChef.close')" :title="t('askChef.close')" @click="askChef.close()">
            <X class="h-4 w-4" />
          </Button>
        </header>

        <div ref="body" class="flex flex-1 flex-col gap-2.5 overflow-y-auto p-3 text-xs" @scroll="onScroll">
          <AskChefContextCard v-if="askChef.context" :context="askChef.context" />
          <p v-else class="text-muted-foreground">{{ t("askChef.noContext") }}</p>

          <p v-if="askChef.context && !askChef.transcript.length && askChef.status.kind !== 'disabled'" class="text-muted-foreground">
            {{ t("askChef.emptyHint") }}
          </p>

          <template v-for="(turn, index) in askChef.transcript" :key="index">
            <div v-if="turn.role === 'user'" class="max-w-[88%] self-end whitespace-pre-wrap rounded-[10px] rounded-br-sm bg-foreground px-2.5 py-1.5 text-background">
              {{ turn.text }}
            </div>
            <AskChefAnswer v-else :turn="turn" :live="streaming && index === askChef.transcript.length - 1" />
          </template>

          <p v-if="askChef.status.kind === 'error'" role="alert" class="rounded-lg border border-destructive/30 bg-destructive/10 px-2.5 py-2 text-destructive">
            {{ askChef.status.message }}
          </p>
          <p v-else-if="askChef.status.kind === 'disabled'" role="alert" class="rounded-lg border bg-muted/40 px-2.5 py-2 text-muted-foreground">
            {{ t("askChef.disabled") }}
          </p>

          <div v-if="showSuggestions" class="flex flex-wrap gap-1.5">
            <button
              v-for="suggestion in suggestions"
              :key="suggestion"
              type="button"
              class="rounded-full border px-2.5 py-0.5 text-muted-foreground hover:border-violet-500 hover:text-violet-600"
              @click="askChef.send(suggestion)"
            >
              {{ suggestion }}
            </button>
          </div>
        </div>

        <AskChefPrompt />
      </aside>
    </ResizablePanel>
  </template>
</template>
