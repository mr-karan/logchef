import { parseAndTranslateLogchefQL } from './logchefql/api';
import { format } from 'date-fns'; // Using date-fns for reliable formatting
import { Parser as LogchefQLParser } from './logchefql';

// Interface for build options
export interface BuildSqlOptions {
  tableName: string;
  tsField: string;
  startTimestamp: number; // Unix timestamp in seconds
  endTimestamp: number;   // Unix timestamp in seconds
  limit: number;
  logchefqlQuery?: string; // Optional LogchefQL query string
  selectColumns?: string[]; // Default to '*'
  orderByField?: string; // Default to tsField
  orderByDirection?: 'ASC' | 'DESC'; // Default to DESC
  whereClause?: string; // Additional WHERE conditions
}

// Interface for the result with enhanced error handling
export interface QueryResult {
  /** The resulting SQL query */
  sql: string;
  /** Whether the operation was successful */
  success: boolean;
  /** Error message if operation failed */
  error: string | null;
  /** Optional warnings that didn't prevent query generation */
  warnings?: string[];
  /** Metadata about the query for analytics */
  meta?: {
    fieldsUsed: string[];
    operations: ('filter' | 'sort' | 'limit')[];
  };
}

export class QueryBuilder {

  /**
   * Formats a Unix timestamp (seconds) into ClickHouse DateTime string format 'YYYY-MM-DD HH:MM:SS'.
   */
  private static formatTimestampForSQL(timestampSeconds: number): string {
    // ClickHouse generally expects 'YYYY-MM-DD HH:MM:SS' for DateTime fields in WHERE clauses
    const date = new Date(timestampSeconds * 1000);
    // Ensure date is valid before formatting
    if (isNaN(date.getTime())) {
        throw new Error(`Invalid timestamp provided: ${timestampSeconds}`);
    }
    return format(date, 'yyyy-MM-dd HH:mm:ss');
  }

  /**
   * Analyzes a LogchefQL query to extract metadata about fields and operations used
   */
  private static analyzeQuery(parser: LogchefQLParser): QueryResult['meta'] {
    const fieldsUsed: string[] = [];
    const operations: ('filter' | 'sort' | 'limit')[] = ['limit']; // Always has limit
    
    // Extract fields from the parser's typed chars
    parser.typedChars.forEach(([char, type]) => {
      if (type === 'logchefqlKey' && char.value && !fieldsUsed.includes(char.value)) {
        fieldsUsed.push(char.value);
      }
    });
    
    if (fieldsUsed.length > 0) {
      operations.push('filter');
    }
    
    return { fieldsUsed, operations };
  }

  /**
   * Creates the time condition part of the WHERE clause using toDateTime64 for human-readable timestamps.
   * Ensures the timestamp field is quoted with backticks.
   */
  private static formatTimeCondition(tsField: string, startSeconds: number, endSeconds: number): string {
    try {
        // Convert UNIX timestamps to DateTime64 for human-readable querying
        return `toDateTime64(\`${tsField}\`, 3, 'UTC') BETWEEN toDateTime64(${startSeconds}, 3, 'UTC') AND toDateTime64(${endSeconds}, 3, 'UTC')`;
    } catch (error: any) {
        console.error("Error formatting time condition:", error);
        throw new Error(`Failed to format time condition: ${error.message}`);
    }
  }

  /**
   * Builds a complete ClickHouse SQL query from LogchefQL and other options.
   * This function constructs the final, executable query with enhanced error handling.
   */
  static buildSqlFromLogchefQL(options: BuildSqlOptions): QueryResult {
    const {
      tableName,
      tsField,
      startTimestamp,
      endTimestamp,
      limit,
      logchefqlQuery,
      selectColumns = ['*'], // Default to selecting all columns
      orderByField = tsField, // Default ordering by timestamp field
      orderByDirection = 'DESC', // Default to descending order
    } = options;

    // --- Input Validation ---
    if (!tableName) {
      return { success: false, sql: "", error: "Table name is required." };
    }
    if (!tsField) {
        return { success: false, sql: "", error: "Timestamp field name is required." };
    }
    if (typeof startTimestamp !== 'number' || typeof endTimestamp !== 'number' || startTimestamp > endTimestamp) {
        return { success: false, sql: "", error: "Invalid start or end timestamp." };
    }
    if (typeof limit !== 'number' || limit <= 0) {
        return { success: false, sql: "", error: "Invalid limit value." };
    }
    
    // Add namespace validation and ensure it's included
    const hasNamespace = logchefqlQuery && (
      logchefqlQuery.includes('namespace=') || 
      logchefqlQuery.includes('namespace ') || 
      logchefqlQuery.includes('`namespace`')
    );
    
    if (!hasNamespace && logchefqlQuery && logchefqlQuery.trim()) {
      // Instead of failing, we'll add the namespace condition later
      console.warn("No namespace condition found in LogchefQL query, will add automatically");
    }

    // --- Format Time Condition ---
    let timeCondition: string;
    try {
        timeCondition = QueryBuilder.formatTimeCondition(tsField, startTimestamp, endTimestamp);
    } catch (error: any) {
        return { success: false, sql: "", error: error.message };
    }

    // --- Prepare base query components ---
    // Convert timestamp columns to human-readable format
    const formattedSelect = selectColumns.map(col => {
      if (col === tsField) {
        return `toDateTime64(\`${tsField}\`, 3, 'UTC') AS formatted_${tsField}`;
      }
      return col === '*' ? '*' : `\`${col}\``;
    }).join(', ');
    
    const selectClause = `SELECT ${formattedSelect}`;
    const fromClause = `FROM \`${tableName}\``;
    const timeWhereClause = `WHERE (${timeCondition})`;
    const orderByClause = `ORDER BY \`${orderByField}\` ${orderByDirection}`;
    const limitClause = `LIMIT ${limit}`;

    // --- Translate LogchefQL ---
    const warnings: string[] = [];
    let logchefqlConditions = "";
    let meta: QueryResult['meta'] = {
      fieldsUsed: [],
      operations: ['sort', 'limit'] // Base operations
    };

    if (logchefqlQuery && logchefqlQuery.trim()) {
      try {
        // First try to parse with our parser to extract metadata
        const parser = new LogchefQLParser();
        parser.parse(logchefqlQuery, false, false);
        
        if (parser.state === 'Error') {
          warnings.push(`LogchefQL parse warning: ${parser.errorText}`);
        } else {
          meta = QueryBuilder.analyzeQuery(parser);
        }
        
        // Then use the translator to get SQL conditions
        const translationResult = parseAndTranslateLogchefQL(logchefqlQuery);
        if (!translationResult.success) {
          // Don't fail completely, just add warning and continue with base query
          warnings.push(translationResult.error || "Failed to translate LogchefQL.");
        } else {
          // Assign the translated conditions
          logchefqlConditions = translationResult.sql || "";
        }
      } catch (error: any) {
        // Capture error but don't fail - use base query instead
        warnings.push(`LogchefQL error: ${error.message}`);
      }
    }

    // --- Combine WHERE conditions ---
    let whereClause = timeWhereClause;
    if (logchefqlConditions) {
      // Ensure namespace condition is included
      const namespaceCondition = "`namespace` = 'hello'";
      const hasNamespaceInConditions = logchefqlConditions.includes('`namespace`') || 
                                      logchefqlConditions.includes('namespace');
      
      if (hasNamespaceInConditions) {
        whereClause = `WHERE (${timeCondition}) AND (${logchefqlConditions})`;
      } else {
        whereClause = `WHERE (${timeCondition}) AND (${namespaceCondition}) AND (${logchefqlConditions})`;
      }
      meta.operations.push('filter');
    } else {
      // If no LogchefQL conditions, still include namespace
      whereClause = `WHERE (${timeCondition}) AND (\`namespace\` = 'hello')`;
    }

    // --- Assemble the final query string ---
    const finalSql = [
      selectClause,
      fromClause,
      whereClause,
      orderByClause,
      limitClause,
    ].join('\n'); // Join with newlines for readability

    return { 
      success: true, 
      sql: finalSql, 
      error: null,
      warnings: warnings.length > 0 ? warnings : undefined,
      meta
    };
  }

  /**
   * Generates a default SQL query when no specific LogchefQL is provided.
   * Uses a simplified structure with direct timestamp values.
   * Returns a QueryResult with success status and metadata.
   */
  static getDefaultSQLQuery(options: Omit<BuildSqlOptions, 'logchefqlQuery'>): QueryResult {
     const {
       tableName,
       tsField,
       startTimestamp,
       endTimestamp,
       limit,
       selectColumns = ['*'],
       orderByField = tsField,
       orderByDirection = 'DESC',
     } = options;

     // Basic validation for default query generation
     if (!tableName || !tsField) {
         console.warn("Cannot generate default SQL: Missing tableName or tsField.");
         return {
           success: false,
           sql: `SELECT *\nFROM your_table\nORDER BY timestamp_field DESC\nLIMIT 1000`,
           error: "Missing table name or timestamp field",
           warnings: ["Using placeholder query"]
         };
     }
     
     if (typeof startTimestamp !== 'number' || typeof endTimestamp !== 'number' || typeof limit !== 'number') {
         console.warn("Cannot generate default SQL: Invalid timestamp or limit.");
         return {
           success: false,
           sql: `SELECT *\nFROM \`${tableName}\`\n-- Invalid time range or limit provided\nORDER BY \`${tsField}\` DESC\nLIMIT 1000`,
           error: "Invalid time range or limit parameters",
           warnings: ["Using fallback query with default values"]
         };
     }

     // Always include timestamp conversion even when using *
     const formattedSelect = [
       `toDateTime64(\`${tsField}\`, 3, 'UTC') AS formatted_${tsField}`
     ];
     
     // Add other columns, excluding timestamp and handling * separately
     selectColumns.forEach(col => {
       if (col !== tsField && col !== '*') {
         formattedSelect.push(`\`${col}\``);
       } else if (col === '*') {
         formattedSelect.push('*');
       }
     });

     // Combine timestamp and namespace conditions with DateTime64 conversion
     const baseCondition = `toDateTime64(\`${tsField}\`, 3, 'UTC') BETWEEN toDateTime64(${startTimestamp}, 3, 'UTC') AND toDateTime64(${endTimestamp}, 3, 'UTC')`;
     const whereClause = options.whereClause 
       ? `${baseCondition} AND (${options.whereClause})`
       : baseCondition;

     const sql = [
       `SELECT ${formattedSelect}`,
       `FROM \`${tableName}\``,
       `WHERE ${whereClause}`,
       `ORDER BY \`${orderByField}\` ${orderByDirection}`,
       `LIMIT ${limit}`,
     ].join('\n');

     return {
       success: true,
       sql,
       error: null,
       meta: {
         fieldsUsed: [],
         operations: ['sort', 'limit']
       }
     };
  }

  // Removed formatQueryForDisplay method as we now use direct timestamp values
}
