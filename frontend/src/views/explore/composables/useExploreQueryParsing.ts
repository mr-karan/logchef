import { computed, type ComputedRef } from "vue";

interface QueryCondition {
  field: string;
  operator: string;
  value: string;
  is_regex: boolean;
}

interface LogchefQLMeta {
  fieldsUsed?: string[];
  conditions?: QueryCondition[];
}

interface LastExecutedState {
  logchefqlMeta?: LogchefQLMeta;
}

const EMPTY_PARSED_QUERY = {
  success: false,
  meta: { fieldsUsed: [], conditions: [] as QueryCondition[] },
};

export function useExploreQueryParsing(
  activeMode: ComputedRef<"logchefql" | "sql">,
  lastExecutedState: ComputedRef<LastExecutedState | undefined>
) {
  const parsedQuery = computed(() => {
    const meta = lastExecutedState.value?.logchefqlMeta;
    if (activeMode.value !== "logchefql" || !meta) {
      return EMPTY_PARSED_QUERY;
    }

    return {
      success: true,
      meta: {
        fieldsUsed: meta.fieldsUsed || [],
        conditions: meta.conditions || [],
      },
    };
  });

  const queryFields = computed(() => {
    if (!parsedQuery.value.success) return [];
    return parsedQuery.value.meta.fieldsUsed;
  });

  const regexHighlights = computed(() => {
    const highlights: Record<string, { pattern: string; isNegated: boolean }> = {};

    if (!parsedQuery.value.success) return highlights;

    for (const condition of parsedQuery.value.meta.conditions) {
      if (!condition.is_regex) {
        continue;
      }

      let pattern = condition.value;
      if (
        (pattern.startsWith('"') && pattern.endsWith('"')) ||
        (pattern.startsWith("'") && pattern.endsWith("'"))
      ) {
        pattern = pattern.slice(1, -1);
      }

      highlights[condition.field] = {
        pattern,
        isNegated: condition.operator === "!~",
      };
    }

    return highlights;
  });

  return {
    parsedQuery,
    queryFields,
    regexHighlights,
  };
}
