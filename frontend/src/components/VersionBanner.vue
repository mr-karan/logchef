<script setup lang="ts">
import { onMounted, ref, watch } from "vue";
import { useEventListener } from "@vueuse/core";
import { useI18n } from "vue-i18n";
import { useMetaStore } from "@/stores/meta";
import { Button } from "@/components/ui/button";

const meta = useMetaStore();
const { t } = useI18n();
const pageVersion = document.querySelector<HTMLMetaElement>('meta[name="logchef-version"]')?.content;
const needsReload = ref(false);

if (pageVersion) {
  watch(() => meta.version, (version) => {
    if (version && version !== pageVersion) needsReload.value = true;
  }, { immediate: true });
  onMounted(() => { void meta.loadMeta(); });
  useEventListener(window, "focus", () => { void meta.loadMeta({ force: true }); });
}

function reload() {
  window.location.reload();
}
</script>

<template>
  <div v-if="needsReload" role="status" class="flex shrink-0 items-center justify-center gap-3 border-b border-amber-500/25 bg-amber-500/10 px-3 py-2 text-sm text-foreground">
    <span>{{ t('ui.serverUpdated') }}</span>
    <Button variant="outline" size="sm" @click="reload">{{ t('pages.reloadPage') }}</Button>
  </div>
</template>
