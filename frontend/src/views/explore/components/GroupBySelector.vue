<script setup lang="ts">
import { useI18n } from "vue-i18n";

import { computed } from 'vue'
import { useExploreStore } from '@/stores/explore'
import { useSourcesStore } from '@/stores/sources'
import { SearchableSelect } from '@/components/ui/searchable-select'

const { t } = useI18n();

interface Props {
  availableFields: Array<{name: string, type: string}>
}

const props = defineProps<Props>()
const exploreStore = useExploreStore()
const sourcesStore = useSourcesStore()

// Group by field with computed default - no default grouping
const groupByField = computed({
  get() {
    // Return the stored value or use no grouping as default
    return exploreStore.groupByField || '__none__';
  },
  set(value) {
    exploreStore.setGroupByField(value);
  }
});

// Computed property for sort keys to show (filtered to remove timestamp and severity fields)
const sortKeysToShow = computed(() => {
  if (!sourcesStore.currentSourceDetails?.sort_keys) return [];
  
  return sourcesStore.currentSourceDetails.sort_keys.filter(
    key => key !== sourcesStore.currentSourceDetails?._meta_severity_field && 
           key !== sourcesStore.currentSourceDetails?._meta_ts_field
  );
});

// Sources can expose thousands of fields. The picker renders a bounded
// slice and searches the rest, so it never mounts the whole schema.
const MAX_VISIBLE_FIELDS = 100

const items = computed(() => {
  const severity = sourcesStore.currentSourceDetails?._meta_severity_field
  const timestamp = sourcesStore.currentSourceDetails?._meta_ts_field
  const recommended = [...new Set([severity, ...sortKeysToShow.value].filter((name): name is string => Boolean(name)))]
  const recommendedSet = new Set(recommended)
  return [
    { value: '__none__', label: t('ui.noGrouping') },
    ...recommended.map(name => ({ value: name, label: name, sublabel: t('ui.recommendedFields') })),
    ...props.availableFields.filter(field => field.name !== timestamp && !recommendedSet.has(field.name))
      .map(field => ({ value: field.name, label: field.name })),
  ]
})
</script>

<template>
  <div class="flex items-center gap-2">
    <label class="text-xs font-medium whitespace-nowrap flex items-center gap-1">
      <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" class="w-3.5 h-3.5">
        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 6h16M4 12h8m-8 6h16" />
      </svg>
      {{ t('ui.groupBy') }}
    </label>
    <div class="w-[180px]">
      <SearchableSelect
        v-model="groupByField"
        :items="items"
        :max-visible-items="MAX_VISIBLE_FIELDS"
        :placeholder="t('ui.noGrouping')"
        :search-placeholder="t('ui.searchFields')"
        trigger-class="h-8 text-xs border-dashed"
      />
    </div>
  </div>
</template>
