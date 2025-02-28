import { ref } from "vue";
import { useDebounceFn } from "@vueuse/core";
import type { FilterCondition } from "@/api/explore";

export interface SqlGeneratorOptions {
  database: string;
  table: string;
  start_timestamp: number;
  end_timestamp: number;
  limit: number;
  sort?: {
    field: string;
    order: "ASC" | "DESC";
  };
}

export interface SqlGeneratorState {
  sql: string;
  isValid: boolean;
  error: string | null;
}

export function useSqlGenerator(initialOptions: SqlGeneratorOptions) {
  const options = ref(initialOptions);
  const previewSql = ref<SqlGeneratorState>({
    sql: "",
    isValid: true,
    error: null,
  });

  // Build a single filter condition
  function buildCondition(cond: FilterCondition): string {
    const { field, operator, value } = cond;

    // Helper function to determine if a value should be quoted
    const formatValue = (val: string | number): string => {
      if (val === null || val === undefined) {
        return 'NULL';
      }
      
      // If it's a number, don't quote it
      if (!isNaN(Number(val)) && val !== '') {
        return String(val);
      }
      
      // Otherwise, it's a string and should be quoted
      return `'${val.toString().replace(/'/g, "''")}'`; // Escape single quotes
    };

    switch (operator) {
      case "=":
        return `${field} = ${formatValue(value)}`;
      case "!=":
        return `${field} != ${formatValue(value)}`;
      case ">":
        return `${field} > ${formatValue(value)}`;
      case "<":
        return `${field} < ${formatValue(value)}`;
      case ">=":
        return `${field} >= ${formatValue(value)}`;
      case "<=":
        return `${field} <= ${formatValue(value)}`;
      case "contains":
      case "~":
        return `position(${field}, ${formatValue(value)}) > 0`;
      case "not_contains":
      case "!~":
        return `position(${field}, ${formatValue(value)}) = 0`;
      case "icontains":
        return `position(lower(${field}), lower(${formatValue(value)})) > 0`;
      case "startswith":
        return `startsWith(${field}, ${formatValue(value)})`;
      case "endswith":
        return `endsWith(${field}, ${formatValue(value)})`;
      case "in":
        if (Array.isArray(value)) {
          const quotedValues = value.map((v) => formatValue(v)).join(", ");
          return `${field} IN (${quotedValues})`;
        }
        throw new Error("Invalid value for IN operator");
      case "not_in":
        if (Array.isArray(value)) {
          const quotedValues = value.map((v) => formatValue(v)).join(", ");
          return `${field} NOT IN (${quotedValues})`;
        }
        throw new Error("Invalid value for NOT IN operator");
      case "is_null":
        return `${field} IS NULL`;
      case "is_not_null":
        return `${field} IS NOT NULL`;
      default:
        throw new Error(`Unsupported operator: ${operator}`);
    }
  }

  // Build WHERE clause including timestamp conditions
  function buildWhereClause(conditions: FilterCondition[]): string {
    const clauses: string[] = [];

    // Convert Unix millisecond timestamps to DateTime64
    if (options.value.start_timestamp) {
      clauses.push(
        `timestamp >= fromUnixTimestamp64Milli(${options.value.start_timestamp})`
      );
    }
    if (options.value.end_timestamp) {
      clauses.push(
        `timestamp <= fromUnixTimestamp64Milli(${options.value.end_timestamp})`
      );
    }

    // Add filter conditions
    conditions.forEach((condition) => {
      try {
        clauses.push(buildCondition(condition));
      } catch (error) {
        console.error("Error building condition:", error);
        throw error;
      }
    });

    return clauses.length > 0 ? `WHERE ${clauses.join(" AND ")}` : "";
  }

  // Build ORDER BY clause
  function buildOrderClause(): string {
    if (options.value.sort?.field && options.value.sort?.order) {
      return `ORDER BY ${options.value.sort.field} ${options.value.sort.order}`;
    }
    return "ORDER BY timestamp DESC";
  }

  // Build LIMIT clause
  function buildLimitClause(): string {
    return `LIMIT ${options.value.limit}`;
  }

  // Validate basic SQL syntax
  function validateSql(sql: string): boolean {
    const hasSelect = sql.trim().toUpperCase().startsWith("SELECT");
    const hasFrom = sql.toUpperCase().includes("FROM");

    return hasSelect && hasFrom;
  }

  // Core SQL generation logic (used by both preview and query)
  function generateSqlInternal(
    conditions: FilterCondition[]
  ): SqlGeneratorState {
    try {
      // Only include non-empty conditions
      const filteredConditions = conditions?.filter(condition => 
        condition && condition.field && condition.operator && condition.value !== undefined
      ) || [];

      const whereClause = buildWhereClause(filteredConditions);
      const orderClause = buildOrderClause();
      const limitClause = buildLimitClause();

      const sql = `SELECT *
FROM ${options.value.database}.${options.value.table}
${whereClause}
${orderClause}
${limitClause}`.trim();

      if (validateSql(sql)) {
        return { sql, isValid: true, error: null };
      }
      return { sql: "", isValid: false, error: "Invalid SQL syntax" };
    } catch (error) {
      return {
        sql: "",
        isValid: false,
        error: error instanceof Error ? error.message : "Unknown error",
      };
    }
  }

  // Debounced preview generation (for UI updates only)
  const generatePreviewSql = useDebounceFn((conditions: FilterCondition[]) => {
    // For empty condition arrays, always generate a clean simple query
    if (!conditions || conditions.length === 0) {
      previewSql.value = {
        sql: `SELECT *
FROM ${options.value.database}.${options.value.table}
WHERE timestamp >= fromUnixTimestamp64Milli(${options.value.start_timestamp}) AND timestamp <= fromUnixTimestamp64Milli(${options.value.end_timestamp})
ORDER BY timestamp DESC
LIMIT ${options.value.limit}`,
        isValid: true,
        error: null
      };
    } else {
      previewSql.value = generateSqlInternal(conditions);
    }
  }, 100); // Reduced debounce time for better responsiveness

  // Immediate SQL generation (for actual queries)
  function generateQuerySql(conditions: FilterCondition[]): SqlGeneratorState {
    return generateSqlInternal(conditions);
  }

  // Function to update options
  function updateOptions(newOptions: Partial<SqlGeneratorOptions>) {
    options.value = { ...options.value, ...newOptions };
  }

  // Initialize preview with empty conditions
  generatePreviewSql([]);

  return {
    generatePreviewSql, // Debounced, for UI preview only
    generateQuerySql, // Immediate, for actual queries
    previewSql, // Reactive preview state
    updateOptions,
  };
}
