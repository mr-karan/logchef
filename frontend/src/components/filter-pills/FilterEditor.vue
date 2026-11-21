<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { X } from "lucide-vue-next";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import SuggestInput from "./SuggestInput.vue";
import { fieldPath } from "@/utils/logchefql/filters";
import {
  filterExpression,
  filterFieldLabel,
  formatFilterValue,
  isCompleteFilter,
  operatorArity,
  parseTypedValue,
  type FilterLanguage,
  type QueryFilter,
  type QueryFilterOperator,
} from "@/utils/queryFilters";
import { filterOperatorLabel } from "./filterLabels";

// Edits one filter pill: its field, operator and values, with a preview of
// what joins the query and an optional custom label.
const props = defineProps<{
  filter?: QueryFilter | null;
  fields: string[];
  language: FilterLanguage;
  // Values to suggest for a field, most common first.
  loadValues?: (field: string) => Promise<string[]>;
}>();

const emit = defineEmits<{
  (e: "save", filter: QueryFilter): void;
  (e: "cancel"): void;
}>();

const { t } = useI18n();

// Each choice is an operator with or without "not". An excluded range is the
// opposite range, so ranges list no "not" form.
interface OperatorChoice { key: string; operator: QueryFilterOperator; negate: boolean }
const choices: OperatorChoice[] = [
  { key: "is", operator: "is", negate: false },
  { key: "is_not", operator: "is", negate: true },
  { key: "is_one_of", operator: "is_one_of", negate: false },
  { key: "is_not_one_of", operator: "is_one_of", negate: true },
  { key: "contains", operator: "contains", negate: false },
  { key: "not_contains", operator: "contains", negate: true },
  { key: "exists", operator: "exists", negate: false },
  { key: "not_exists", operator: "exists", negate: true },
  { key: "gt", operator: "gt", negate: false },
  { key: "gte", operator: "gte", negate: false },
  { key: "lt", operator: "lt", negate: false },
  { key: "lte", operator: "lte", negate: false },
];

const fieldText = ref("");
const choiceKey = ref("is");
const valueText = ref("");
const values = ref<string[]>([]);
const label = ref("");


function choiceFor(filter: QueryFilter): string {
  const negate = !!filter.negate && !["gt", "gte", "lt", "lte"].includes(filter.operator);
  return choices.find(choice => choice.operator === filter.operator && choice.negate === negate)?.key ?? "is";
}

function reset() {
  const filter = props.filter;
  fieldText.value = filter ? filterFieldLabel(filter.field) : "";
  choiceKey.value = filter ? choiceFor(filter) : "is";
  const shown = filter?.values.map(formatFilterValue) ?? [];
  values.value = filter?.operator === "is_one_of" ? shown : [];
  valueText.value = filter && filter.operator !== "is_one_of" ? shown[0] ?? "" : "";
  label.value = filter?.label ?? "";
}
watch(() => props.filter, reset, { immediate: true });

const choice = computed(() => choices.find(item => item.key === choiceKey.value) ?? choices[0]);
const arity = computed(() => operatorArity(choice.value.operator));

// Switching between one value and a list carries the values across.
watch(arity, (next, previous) => {
  if (previous === "one" && next === "many" && valueText.value.trim()) {
    values.value = [valueText.value.trim()];
    valueText.value = "";
  } else if (previous === "many" && next === "one" && !valueText.value && values.value.length) {
    valueText.value = values.value[0];
  }
});

// A field typed as shown keeps its path; a column whose name is no plain
// identifier is quoted, so "http.status" stays one column.
const IDENT = /^@?[a-zA-Z_][a-zA-Z0-9_:@.-]*$/;
function fieldFromText(text: string): string {
  const trimmed = text.trim();
  if (props.filter && trimmed === filterFieldLabel(props.filter.field)) return props.filter.field;
  if (IDENT.test(trimmed)) return trimmed;
  return fieldPath([trimmed]);
}

function addValue() {
  const text = valueText.value.trim();
  if (text && !values.value.includes(text)) values.value = [...values.value, text];
  valueText.value = "";
}

function removeValue(index: number) {
  values.value = values.value.filter((_, i) => i !== index);
}

// Value suggestions for the field, loaded a moment after it stops changing.
const valueSuggestions = ref<string[]>([]);
const loadingValues = ref(false);
let valuesTimer: ReturnType<typeof setTimeout> | undefined;
let valuesRequest = 0;
watch([fieldText, arity], () => {
  clearTimeout(valuesTimer);
  valueSuggestions.value = [];
  const field = fieldText.value.trim();
  if (!field || arity.value === "none" || !props.loadValues) return;
  const request = ++valuesRequest;
  loadingValues.value = true;
  valuesTimer = setTimeout(async () => {
    try {
      const values = await props.loadValues!(fieldFromText(field));
      if (request === valuesRequest) valueSuggestions.value = values;
    } catch {
      if (request === valuesRequest) valueSuggestions.value = [];
    } finally {
      if (request === valuesRequest) loadingValues.value = false;
    }
  }, 250);
}, { immediate: true });
onBeforeUnmount(() => clearTimeout(valuesTimer));

// A value already in the list is not suggested again.
const shownValueSuggestions = computed(() => valueSuggestions.value.filter((value) => !values.value.includes(value)));

function onPickValue(value: string) {
  if (arity.value === "many") {
    valueText.value = value;
    addValue();
  }
}

const draft = computed<QueryFilter>(() => {
  const { operator, negate } = choice.value;
  let filterValues: QueryFilter["values"] = [];
  if (arity.value === "one" && valueText.value !== "") filterValues = [parseTypedValue(valueText.value)];
  if (arity.value === "many") {
    const pending = valueText.value.trim();
    filterValues = [...values.value, ...(pending && !values.value.includes(pending) ? [pending] : [])].map(parseTypedValue);
  }
  return {
    field: fieldFromText(fieldText.value),
    operator,
    values: filterValues,
    ...(negate ? { negate: true } : {}),
    ...(props.filter?.disabled ? { disabled: true } : {}),
    ...(label.value.trim() ? { label: label.value.trim() } : {}),
  };
});

const complete = computed(() => fieldText.value.trim() !== "" && isCompleteFilter(draft.value));
const preview = computed(() => (complete.value ? filterExpression(draft.value, props.language) : ""));

function save() {
  if (complete.value) emit("save", draft.value);
}

function onFieldEnter(event: KeyboardEvent) {
  event.preventDefault();
}

function onValueEnter(event: KeyboardEvent) {
  if (arity.value === "many") {
    event.preventDefault();
    addValue();
  } else {
    save();
  }
}
</script>

<template>
  <form class="flex flex-col gap-3" data-testid="filter-editor" @submit.prevent="save">
    <div class="text-sm font-semibold">{{ filter ? t('filters.editFilter') : t('filters.addFilter') }}</div>
    <div class="grid gap-2 sm:grid-cols-[minmax(0,1.4fr)_minmax(0,1fr)]">
      <div class="flex flex-col gap-1">
        <Label class="text-xs">{{ t('filters.field') }}</Label>
        <SuggestInput v-model="fieldText" :suggestions="fields" test-id="filter-field" @enter="onFieldEnter" />
      </div>
      <div class="flex flex-col gap-1">
        <Label class="text-xs">{{ t('filters.operator') }}</Label>
        <Select v-model="choiceKey">
          <SelectTrigger size="sm" class="w-full" data-testid="filter-operator">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem v-for="item in choices" :key="item.key" :value="item.key" :data-testid="`filter-operator-${item.key}`">
              {{ filterOperatorLabel(t, item.operator, item.negate) }}
            </SelectItem>
          </SelectContent>
        </Select>
      </div>
    </div>

    <div v-if="arity !== 'none'" class="flex flex-col gap-1">
      <Label class="text-xs">{{ arity === 'many' ? t('filters.values') : t('filters.value') }}</Label>
      <div v-if="arity === 'many' && values.length" class="flex flex-wrap gap-1">
        <span
          v-for="(value, index) in values"
          :key="value"
          class="inline-flex items-center gap-1 rounded border bg-muted px-1.5 py-0.5 text-xs"
        >
          {{ value }}
          <button type="button" class="text-muted-foreground hover:text-foreground" :aria-label="t('ui.delete')" @click="removeValue(index)">
            <X class="h-3 w-3" />
          </button>
        </span>
      </div>
      <SuggestInput
        v-model="valueText"
        :suggestions="shownValueSuggestions"
        :loading="loadingValues"
        test-id="filter-value"
        @pick="onPickValue"
        @enter="onValueEnter"
      />
      <p v-if="arity === 'many'" class="text-xs text-muted-foreground">{{ t('filters.valuesHint') }}</p>
    </div>

    <div v-if="preview" class="flex flex-col gap-1">
      <Label class="text-xs">{{ t('filters.preview') }}</Label>
      <code class="break-all rounded bg-muted px-2 py-1 text-xs" data-testid="filter-preview">{{ preview }}</code>
    </div>

    <div class="flex flex-col gap-1">
      <Label class="text-xs">{{ t('filters.customLabel') }}</Label>
      <Input v-model="label" class="h-8" :placeholder="t('filters.customLabelPlaceholder')" data-testid="filter-label" />
    </div>

    <div class="flex justify-end gap-2">
      <Button type="button" variant="ghost" size="sm" @click="emit('cancel')">{{ t('ui.cancel') }}</Button>
      <Button type="submit" size="sm" :disabled="!complete" data-testid="filter-save">
        {{ filter ? t('filters.updateFilter') : t('filters.addFilter') }}
      </Button>
    </div>
  </form>
</template>
