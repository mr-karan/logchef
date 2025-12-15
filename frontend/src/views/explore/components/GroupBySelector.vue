<script setup lang="ts">
import { computed } from 'vue'
import { useExploreStore } from '@/stores/explore'
import { useSourcesStore } from '@/stores/sources'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectLabel,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'

interface Props {
  availableFields: Array<{name: string, type: string}>
}

defineProps<Props>()
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

// Computed property to check if we have any recommended fields
const hasRecommendedFields = computed(() => {
  return !!sourcesStore.currentSourceDetails?._meta_severity_field || 
         sortKeysToShow.value.length > 0;
});
</script>

<template>
  <div class="flex items-center gap-2">
    <label class="text-xs font-medium whitespace-nowrap flex items-center gap-1">
      <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" class="w-3.5 h-3.5">
        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 6h16M4 12h8m-8 6h16" />
      </svg>
      Group By:
    </label>
    <Select v-model="groupByField" class="max-w-[180px] h-8">
      <SelectTrigger class="h-8 text-xs border-dashed">
        <SelectValue placeholder="No Grouping">
          {{ groupByField === '__none__' ? 'No Grouping' : groupByField }}
        </SelectValue>
      </SelectTrigger>
      <SelectContent>
        <SelectGroup>
          <SelectLabel class="text-xs">Time Series Grouping</SelectLabel>
          <SelectItem value="__none__">No Grouping</SelectItem>
        </SelectGroup>
        
        <!-- Combined recommended fields group -->
        <SelectGroup v-if="hasRecommendedFields">
          <SelectLabel class="text-xs">Recommended Fields</SelectLabel>
          
          <!-- Severity field if available -->
          <SelectItem v-if="sourcesStore.currentSourceDetails?._meta_severity_field" 
                      :value="sourcesStore.currentSourceDetails._meta_severity_field">
            {{ sourcesStore.currentSourceDetails._meta_severity_field }}
          </SelectItem>
          
          <!-- Sort keys (except timestamp and severity fields) -->
          <SelectItem 
            v-for="sortKey in sortKeysToShow"
            :key="sortKey"
            :value="sortKey">
            {{ sortKey }}
          </SelectItem>
        </SelectGroup>
        
        <SelectGroup>
          <SelectLabel class="text-xs">Available Fields</SelectLabel>
          <SelectItem v-for="field in availableFields.filter(f => 
            f.name !== sourcesStore.currentSourceDetails?._meta_severity_field && 
            f.name !== sourcesStore.currentSourceDetails?._meta_ts_field &&
            !sourcesStore.currentSourceDetails?.sort_keys?.includes(f.name))"
            :key="field.name" 
            :value="field.name">
            {{ field.name }}
          </SelectItem>
        </SelectGroup>
      </SelectContent>
    </Select>
  </div>
</template>
