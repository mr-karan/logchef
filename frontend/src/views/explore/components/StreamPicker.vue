<script setup lang="ts">
import { computed, onUnmounted, ref, watch } from 'vue';
import { useI18n } from 'vue-i18n';
import { Button } from '@/components/ui/button';
import { Checkbox } from '@/components/ui/checkbox';
import { Label } from '@/components/ui/label';
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover';
import { getStreamFields } from '@/api/windowed';
import { sourcesApi, getVictoriaLogsOptimizer, type FieldValuesResult } from '@/api/sources';
import { useSourcesStore } from '@/stores/sources';
import { useTeamsStore } from '@/stores/teams';
import { useExploreWindowedStore } from '@/stores/exploreWindowed';

const { t } = useI18n();
const sources = useSourcesStore();
const teams = useTeamsStore();
const store = useExploreWindowedStore();
const fields = ref<string[]>([]);
const values = ref<Record<string, FieldValuesResult>>({});
const selectedField = ref('');
const loading = ref(false);
const error = ref<string | null>(null);
const enabled = computed(() => Boolean(getVictoriaLogsOptimizer(sources.currentSourceDetails)));
const selectedCount = computed(() => Object.values(store.selectedStreams).reduce((n, values) => n + values.length, 0));
let controller: AbortController | null = null;
onUnmounted(() => controller?.abort());
watch(() => [sources.currentSourceDetails?.id, teams.currentTeamId, enabled.value], async () => {
  controller?.abort();
  const source = sources.currentSourceDetails;
  const teamId = teams.currentTeamId;
  fields.value = [];
  values.value = {};
  selectedField.value = '';
  error.value = null;
  if (!enabled.value || !source || !teamId) return;
  const current = new AbortController();
  controller = current;
  try {
    const result = await getStreamFields(teamId, source.id, current.signal);
    if (controller !== current) return;
    fields.value = result.data ?? [];
    const field = fields.value[0];
    if (field) await loadValues(field);
  } catch (err) { if (!current.signal.aborted) error.value = err instanceof Error ? err.message : 'Could not load stream fields'; }
}, { immediate: true });

async function loadValues(field: string) {
  selectedField.value = field;
  const source = sources.currentSourceDetails;
  const teamId = teams.currentTeamId;
  if (!source || !teamId || !controller) return;
  const current = controller;
  loading.value = true;
  error.value = null;
  const end = new Date();
  try {
    const result = await sourcesApi.getFieldValues(teamId, source.id, field, 'String', new Date(end.getTime() - (getVictoriaLogsOptimizer(source)?.sidebar_lookback_seconds ?? 900) * 1000).toISOString(), end.toISOString(), 'UTC', 100, 'logsql', '*', undefined, current.signal);
    if (controller === current && result.data) values.value = { ...values.value, [field]: result.data };
  } catch (err) { if (!current.signal.aborted) error.value = err instanceof Error ? err.message : 'Could not load streams'; }
  finally { if (controller === current) loading.value = false; }
}
function select(value: string, checked: boolean) {
  const selected = store.selectedStreams[selectedField.value] ?? [];
  store.selectedStreams = { ...store.selectedStreams, [selectedField.value]: checked ? [...new Set([...selected, value])] : selected.filter(item => item !== value) };
}
function selectAll() { store.selectedStreams = { ...store.selectedStreams, [selectedField.value]: (values.value[selectedField.value]?.values ?? []).map(value => value.value) }; }
function clear() { store.selectedStreams = {}; }
</script>

<template>
  <Popover v-if="enabled">
    <PopoverTrigger as-child><Button size="sm" variant="outline">{{ t('sources.windowedStreamsSelected', { count: selectedCount }) }}</Button></PopoverTrigger>
    <PopoverContent class="w-96" align="start">
      <div class="flex flex-col gap-3">
        <p class="text-sm text-muted-foreground">{{ t('sources.windowedStreamPickerDescription') }}</p>
        <div class="flex flex-wrap gap-1"><Button v-for="field in fields" :key="field" size="sm" :variant="field === selectedField ? 'secondary' : 'ghost'" @click="loadValues(field)">{{ field }}</Button></div>
        <div class="flex items-center gap-2"><Button size="sm" variant="ghost" :disabled="loading" @click="selectAll()">{{ t('sources.windowedSelectAllStreams') }}</Button><Button size="sm" variant="ghost" @click="clear()">{{ t('sources.windowedClearStreams') }}</Button></div>
        <p v-if="error" class="text-sm text-destructive">{{ error }}</p>
        <p v-if="loading" class="text-sm text-muted-foreground">{{ t('ui.loading') }}</p>
        <div v-else class="flex max-h-64 flex-col gap-2 overflow-auto">
          <div v-for="(value, index) in values[selectedField]?.values ?? []" :key="value.value" class="flex items-center gap-2">
            <Checkbox :id="`stream-${index}`" :model-value="(store.selectedStreams[selectedField] ?? []).includes(value.value)" @update:model-value="checked => select(value.value, checked === true)" />
            <Label :for="`stream-${index}`" class="flex-1 break-all">{{ value.value }}</Label><span class="text-xs text-muted-foreground">{{ value.count.toLocaleString() }}</span>
          </div>
        </div>
        <p v-if="values[selectedField]" class="text-xs text-muted-foreground">{{ t('sources.windowedStreamValueCount', { shown: values[selectedField]?.values.length, total: values[selectedField]?.total_distinct }) }}</p>
      </div>
    </PopoverContent>
  </Popover>
</template>
