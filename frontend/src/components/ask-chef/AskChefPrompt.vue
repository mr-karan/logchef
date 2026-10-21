<script setup lang="ts">
import { computed, ref } from "vue";
import { useI18n } from "vue-i18n";
import { ArrowUp, Square } from "lucide-vue-next";
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";
import { MAX_MESSAGE_LENGTH, useAskChefStore } from "@/stores/askChef";

const { t } = useI18n();
const askChef = useAskChefStore();
const draft = ref("");

const streaming = computed(() => askChef.status.kind === "streaming");
const unavailable = computed(() => !askChef.context || askChef.status.kind === "disabled");

function submit() {
  const text = draft.value.trim();
  if (!text || streaming.value || unavailable.value) return;
  draft.value = "";
  void askChef.send(text);
}

function onKeydown(event: KeyboardEvent) {
  if (event.key !== "Enter" || event.shiftKey || event.isComposing) return;
  event.preventDefault();
  submit();
}
</script>

<template>
  <form class="flex items-end gap-1.5 border-t px-3 py-2.5" @submit.prevent="submit">
    <Textarea
      :model-value="draft"
      :placeholder="askChef.transcript.length ? t('askChef.placeholderFollowUp') : t('askChef.placeholder')"
      :maxlength="MAX_MESSAGE_LENGTH"
      :disabled="unavailable"
      rows="1"
      class="max-h-32 min-h-9 resize-none py-1.5 text-xs md:text-xs"
      @update:model-value="(value) => (draft = String(value))"
      @keydown="onKeydown"
    />
    <Button v-if="streaming" type="button" variant="outline" size="icon-sm" :aria-label="t('askChef.stop')" :title="t('askChef.stop')" @click="askChef.stop()">
      <Square class="h-3.5 w-3.5 fill-current" />
    </Button>
    <Button v-else type="submit" size="icon-sm" :disabled="!draft.trim() || unavailable" :aria-label="t('askChef.send')" :title="t('askChef.send')">
      <ArrowUp class="h-4 w-4" />
    </Button>
  </form>
</template>
