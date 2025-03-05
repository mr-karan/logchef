import { ref } from "vue";
import { useDebounceFn } from "@vueuse/core";
import type { FilterCondition } from "@/api/explore";
import { debug, error, warn } from "@/utils/debug";

export interface SqlGeneratorOptions {
  database: string;
  table: string;
  start_timestamp: number;
  end_timestamp: number;
  limit: number;
  timestamp_field?: string; // Add support for custom timestamp field from source metadata
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
    // Normalize operator property - handle both 'operator' and 'op' fields
    const field = cond.field;
    const operator = cond.operator || cond.op;
    const value = cond.value;

    if (!field || !operator) {
      throw new Error("Invalid condition: missing field or operator");
    }

    // Helper function to determine if a value should be quoted
    const formatValue = (val: any): string => {
      if (val === null || val === undefined) {
        return 'NULL';
      }
      
      // If it's a number, don't quote it
      if (typeof val === 'number' || (!isNaN(Number(val)) && val !== '')) {
        return String(val);
      }
      
      // Otherwise, it's a string and should be quoted
      return `'${val.toString().replace(/'/g, "''")}'`; // Escape single quotes
    };

    // Process based on operator type
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
        return `${field} IN (${formatValue(value)})`;
      case "not_in":
        if (Array.isArray(value)) {
          const quotedValues = value.map((v) => formatValue(v)).join(", ");
          return `${field} NOT IN (${quotedValues})`;
        }
        return `${field} NOT IN (${formatValue(value)})`;
      case "is_null":
        return `${field} IS NULL`;
      case "is_not_null":
        return `${field} IS NOT NULL`;
      default:
        warn("SQL Generator", `Unsupported operator: ${operator}, using equals instead`);
        return `${field} = ${formatValue(value)}`;
    }
  }

  // Build WHERE clause including timestamp conditions
  function buildWhereClause(conditions: FilterCondition[]): string {
    const clauses: string[] = [];
    
    // Use the custom timestamp field from source metadata or default to 'timestamp'
    const timestampField = options.value.timestamp_field || 'timestamp';

    // Always include timestamp range conditions for consistency
    if (options.value.start_timestamp) {
      const startDate = new Date(options.value.start_timestamp);
      clauses.push(
        `${timestampField} >= toDateTime64('${startDate.toISOString().replace('T', ' ').replace('Z', '')}', 3)`
      );
    }
    if (options.value.end_timestamp) {
      const endDate = new Date(options.value.end_timestamp);
      clauses.push(
        `${timestampField} <= toDateTime64('${endDate.toISOString().replace('T', ' ').replace('Z', '')}', 3)`
      );
    }

    // Add filter conditions - make sure to handle condition errors gracefully
    if (Array.isArray(conditions)) {
      for (const condition of conditions) {
        try {
          if (condition && (condition.operator || condition.op) && condition.field) {
            clauses.push(buildCondition(condition));
          }
        } catch (error) {
          error("SQL Generator", "Error building condition:", error, "Condition:", condition);
          // Continue processing other conditions instead of throwing
        }
      }
    }

    return clauses.length > 0 ? `WHERE ${clauses.join(" AND ")}` : "";
  }

  // Build ORDER BY clause
  function buildOrderClause(): string {
    // Use the custom timestamp field from source metadata or default to 'timestamp'
    const timestampField = options.value.timestamp_field || 'timestamp';
    
    if (options.value.sort?.field && options.value.sort?.order) {
      return `ORDER BY ${options.value.sort.field} ${options.value.sort.order}`;
    }
    return `ORDER BY ${timestampField} DESC`;
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
      // Ensure we're working with an array
      if (!Array.isArray(conditions)) {
        conditions = [];
      }
      
      // Only include valid, complete conditions
      const filteredConditions = conditions.filter(condition => 
        condition && 
        condition.field && 
        (condition.operator || condition.op) && 
        (condition.value !== undefined || 
         condition.operator === 'is_null' || 
         condition.operator === 'is_not_null' ||
         condition.op === 'is_null' || 
         condition.op === 'is_not_null')
      );
      
      // Log the filtered conditions for debugging (but limit to first 300 chars)
      const logOutput = JSON.stringify(filteredConditions);
      debug("SQL Generator", "Processing conditions after filtering:", 
            logOutput.length > 300 ? logOutput.substring(0, 300) + '...' : logOutput);

      // Generate clauses
      const whereClause = buildWhereClause(filteredConditions);
      const orderClause = buildOrderClause();
      const limitClause = buildLimitClause();

      const sql = `SELECT *
FROM ${options.value.database}.${options.value.table}
${whereClause}
${orderClause}
${limitClause}`.trim();

      // Validate and return the SQL
      if (validateSql(sql)) {
        return { sql, isValid: true, error: null };
      }
      
      error("SQL Generator", "SQL validation failed for:", sql);
      return { sql: "", isValid: false, error: "Invalid SQL syntax" };
    } catch (error) {
      error("SQL Generator", "Error generating SQL:", error);
      return {
        sql: "",
        isValid: false,
        error: error instanceof Error ? error.message : "Unknown error",
      };
    }
  }

  // Immediate preview generation (for real-time updates without debounce)
  const generatePreviewSql = (conditions: FilterCondition[]) => {
    debug("SQL Generator", `Generating preview SQL for ${conditions?.length || 0} conditions`);
    
    // For empty condition arrays, always generate a clean simple query
    if (!conditions || conditions.length === 0) {
      debug("SQL Generator", "Empty conditions received, generating default SQL:", 
            JSON.stringify(conditions));
                 
      // Use the custom timestamp field from source metadata or default to 'timestamp'
      const timestampField = options.value.timestamp_field || 'timestamp';
      
      // Format timestamps as human-readable DateTime64
      const startDate = new Date(options.value.start_timestamp);
      const endDate = new Date(options.value.end_timestamp);
      
      const simpleSql = `SELECT *
FROM ${options.value.database}.${options.value.table}
WHERE ${timestampField} >= toDateTime64('${startDate.toISOString().replace('T', ' ').replace('Z', '')}', 3) 
  AND ${timestampField} <= toDateTime64('${endDate.toISOString().replace('T', ' ').replace('Z', '')}', 3)
ORDER BY ${timestampField} DESC
LIMIT ${options.value.limit}`;

      previewSql.value = {
        sql: simpleSql,
        isValid: true,
        error: null
      };
      
      debug("SQL Generator", "Generated default SQL (no conditions)");
    } else {
      // Generate SQL with conditions
      previewSql.value = generateSqlInternal(conditions);
      debug("SQL Generator", "Generated SQL with conditions:", 
            previewSql.value.isValid ? 'valid' : 'invalid', 
            previewSql.value.error || '');
    }
  };

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
    generatePreviewSql, // For UI preview only
    generateQuerySql, // For actual queries
    previewSql, // Reactive preview state
    updateOptions,
    // Add direct access to internal functions for testing
    __internal: {
      buildWhereClause,
      buildOrderClause,
      buildLimitClause,
      validateSql
    }
  };
}
