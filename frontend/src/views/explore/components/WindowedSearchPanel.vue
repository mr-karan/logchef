<script setup lang="ts">
import { computed, ref } from 'vue';
import { useI18n } from 'vue-i18n';
import { usePreferencesStore } from '@/stores/preferences';
import { epochMs, formatInZone } from '@/utils/timeWindow';
import { Button } from '@/components/ui/button';
import { Alert, AlertDescription } from '@/components/ui/alert';
import type { Paging } from '@/stores/exploreWindowed';
import { useExploreRuns } from '@/stores/exploreRuns';

const { t } = useI18n();
defineEmits<{ stop: []; page: [action: Paging] }>();
const runs = useExploreRuns();
const store = computed(() => runs.windowed);
const skipping = computed(() => runs.current?.status === "running" ? runs.current.search?.skippableWindow : store.value?.skippableWindow);
const preferences = usePreferencesStore();
const failedSearch = computed(() => (store.value?.coverage ?? []).filter(w => store.value?.canRetrySearch(w)));
const failedCounts = computed(() => (store.value?.coverage ?? []).filter(w => w.count === 'failed'));
const showFailedCounts = ref(false);
function range(start: string, end: string) {
  const format = (iso: string) => formatInZone(epochMs(Date.parse(iso)), preferences.zone, { dateStyle: 'medium', timeStyle: 'medium' });
  return `${format(start)} to ${format(end)}`;
}
</script>

<template>
  <div v-if="store" class="flex flex-col gap-2 border-b px-3 py-2 text-xs">
    <div class="flex flex-wrap items-center gap-3" aria-live="polite">
      <template v-if="runs.shown?.search">
        <span>{{ t('sources.windowedSearchedWindows', { count: store.searchedWindows, total: store.coverage.length }) }}</span>
        <span v-if="store.countComplete">{{ t('sources.windowedTotalRows', { count: store.total.toLocaleString() }) }}</span>
        <span v-else>{{ t('sources.windowedCountedRows', { count: store.total.toLocaleString() }) }}</span>
        <Button v-if="store.cursor" size="sm" variant="outline" :disabled="store.isSearching" @click="$emit('page', 'older')">{{ t('sources.windowedLoadOlder') }}</Button>
      </template>
      <Button v-if="skipping" size="sm" variant="outline" @click="$emit('page', 'skip')">{{ t('sources.windowedSkipWindow') }}</Button>
      <Button v-if="runs.isRunning || store.isCounting" size="sm" variant="ghost" @click="$emit('stop')">{{ t('sources.windowedStopSearch') }}</Button>
    </div>
    <Alert v-if="store.error" variant="destructive"><AlertDescription>{{ store.error }}</AlertDescription></Alert>
    <template v-if="runs.shown?.search">
      <div v-for="window in failedSearch" :key="`search-${window.index}`" class="flex flex-wrap items-center gap-2">
        <span>{{ t(window.search === 'skipped' ? 'sources.windowedSkippedWindow' : window.search === 'partial' ? 'sources.windowedPartialWindow' : 'sources.windowedFailedWindow') }}: {{ range(window.start, window.end) }}<template v-if="window.message">. {{ window.message }}</template></span>
        <Button size="sm" variant="ghost" :disabled="store.isSearching" @click="$emit('page', { retry: window })">{{ t('sources.windowedRetryWindow') }}</Button>
      </div>
      <div v-if="failedCounts.length" class="flex flex-wrap items-center gap-2">
        <span>{{ t('sources.windowedFailedCounts', { count: failedCounts.length }) }}</span>
        <Button size="sm" variant="ghost" :disabled="store.isCounting" @click="store.retryFailedCounts()">{{ t('sources.windowedRetryFailedCounts') }}</Button>
        <Button size="sm" variant="ghost" :aria-expanded="showFailedCounts" @click="showFailedCounts = !showFailedCounts">{{ t(showFailedCounts ? 'sources.windowedHideFailedCounts' : 'sources.windowedShowFailedCounts') }}</Button>
      </div>
      <template v-if="showFailedCounts">
        <div v-for="window in failedCounts" :key="`count-${window.index}`" class="flex flex-wrap items-center gap-2">
          <span>{{ t('sources.windowedFailedCountWindow') }}: {{ range(window.start, window.end) }}</span>
          <Button size="sm" variant="ghost" :disabled="store.isCounting" @click="store.retryCount(window.index)">{{ t('sources.windowedRetryWindow') }}</Button>
        </div>
      </template>
    </template>
  </div>
</template>
