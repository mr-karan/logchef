<script setup lang="ts">
import { computed } from 'vue';
import { useI18n } from 'vue-i18n';
import { Button } from '@/components/ui/button';
import { Alert, AlertDescription } from '@/components/ui/alert';
import { useExploreWindowedStore } from '@/stores/exploreWindowed';

const { t } = useI18n();
const store = useExploreWindowedStore();
const failedSearch = computed(() => store.coverage.filter(w => store.canRetrySearch(w)));
const failedCounts = computed(() => store.coverage.filter(w => w.count === 'failed'));
function range(start: string, end: string) { return `${new Date(start).toLocaleString()} to ${new Date(end).toLocaleString()}`; }
</script>

<template>
  <div v-if="store.active" class="flex flex-col gap-2 border-b px-3 py-2 text-xs">
    <div class="flex flex-wrap items-center gap-3" aria-live="polite">
      <span>{{ t('sources.windowedSearchedWindows', { count: store.searchedWindows, total: store.coverage.length }) }}</span>
      <span v-if="store.countComplete">{{ t('sources.windowedTotalRows', { count: store.total.toLocaleString() }) }}</span>
      <span v-else>{{ t('sources.windowedCountedRows', { count: store.total.toLocaleString() }) }}</span>
      <Button v-if="store.cursor" size="sm" variant="outline" :disabled="store.isSearching" @click="store.loadOlder()">{{ t('sources.windowedLoadOlder') }}</Button>
      <Button v-if="store.skippableWindow" size="sm" variant="outline" @click="store.skipWindow()">{{ t('sources.windowedSkipWindow') }}</Button>
      <Button v-if="store.isSearching || store.isCounting" size="sm" variant="ghost" @click="store.stop()">{{ t('sources.windowedStopSearch') }}</Button>
    </div>
    <Alert v-if="store.error" variant="destructive"><AlertDescription>{{ store.error }}</AlertDescription></Alert>
    <div v-for="window in failedSearch" :key="`search-${window.index}`" class="flex flex-wrap items-center gap-2">
      <span>{{ t(window.search === 'skipped' ? 'sources.windowedSkippedWindow' : window.search === 'partial' ? 'sources.windowedPartialWindow' : 'sources.windowedFailedWindow') }}: {{ range(window.start, window.end) }}<template v-if="window.message">. {{ window.message }}</template></span>
      <Button size="sm" variant="ghost" :disabled="store.isSearching" @click="store.retrySearch(window)">{{ t('sources.windowedRetryWindow') }}</Button>
    </div>
    <div v-for="window in failedCounts" :key="`count-${window.index}`" class="flex flex-wrap items-center gap-2">
      <span>{{ t('sources.windowedFailedCountWindow') }}: {{ range(window.start, window.end) }}</span>
      <Button size="sm" variant="ghost" :disabled="store.isCounting" @click="store.retryCount(window.index)">{{ t('sources.windowedRetryWindow') }}</Button>
    </div>
  </div>
</template>
