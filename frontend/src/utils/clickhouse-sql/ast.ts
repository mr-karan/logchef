// Simple AST for SQL queries to help with dynamic updates
export interface SQLNode {
  type: string;
}

export interface SQLQuery extends SQLNode {
  type: "query";
  selectClause: SelectClause;
  fromClause: FromClause;
  whereClause?: WhereClause;
  orderByClause?: OrderByClause;
  limitClause?: LimitClause;
}

export interface SelectClause extends SQLNode {
  type: "select";
  distinct?: boolean;
  columns: string[];
}

export interface FromClause extends SQLNode {
  type: "from";
  table: string;
  alias?: string;
}

export interface WhereClause extends SQLNode {
  type: "where";
  /** Conditions excluding the primary timestamp filter */
  otherConditions: string;
  /** The full timestamp condition string (e.g., `ts BETWEEN ...`) */
  timestampCondition?: string;
}

export interface OrderByClause extends SQLNode {
  type: "orderby";
  columns: OrderByColumn[];
}

export interface OrderByColumn extends SQLNode {
  type: "orderby_column";
  column: string;
  direction: "ASC" | "DESC";
}

export interface LimitClause extends SQLNode {
  type: "limit";
  value: number;
}

/**
 * A simple SQL parser that creates a basic AST for ClickHouse queries
 */
export class SQLParser {
  /**
   * Parse a SQL query string into an AST
   */
  static parse(sql: string, tsField: string = "timestamp"): SQLQuery | null {
    try {
      // Start with a basic query structure
      const query: SQLQuery = {
        type: "query",
        selectClause: { type: "select", columns: [] },
        fromClause: { type: "from", table: "" },
      };

      // Normalize whitespace
      const normalizedSql = sql.replace(/\s+/g, " ").trim();

      // Extract SELECT clause
      const selectMatch = normalizedSql.match(
        /SELECT\s+(DISTINCT\s+)?(.+?)\s+FROM/i
      );
      if (selectMatch) {
        query.selectClause.distinct = !!selectMatch[1];
        query.selectClause.columns = [selectMatch[2].trim()];
      } else {
        query.selectClause.columns = ["*"];
      }

      // Extract FROM clause
      const fromMatch = normalizedSql.match(
        /FROM\s+([a-zA-Z0-9_.]+)(?:\s+|$|\s+WHERE|\s+ORDER|\s+LIMIT)/i
      );
      if (fromMatch) {
        query.fromClause.table = fromMatch[1].trim();
      }

      // Extract WHERE clause and separate timestamp condition
      const whereMatch = normalizedSql.match(/WHERE\s+(.+?)(?:\s+ORDER\s+BY|\s+LIMIT|$)/i);
      if (whereMatch) {
        let conditions = whereMatch[1].trim();
        let timestampCondition: string | undefined = undefined;

        // Regex to find the timestamp condition (more robust)
        // Assumes format: `tsField` BETWEEN toDateTime(...) AND toDateTime(...)
        // Handles potential backticks around tsField
        const tsRegex = new RegExp(
          `(\`?${tsField}\`?\\s+BETWEEN\\s+toDateTime\\('[^']+'\\)\\s+AND\\s+toDateTime\\('[^']+'\\))`,
          "i"
        );
        const tsMatch = conditions.match(tsRegex);

        if (tsMatch) {
          timestampCondition = tsMatch[0];
          // Remove the timestamp condition and surrounding AND if present
          conditions = conditions.replace(tsRegex, '').replace(/^\s*AND\s*/i, '').replace(/\s*AND\s*$/i, '').trim();
        }

        query.whereClause = {
          type: "where",
          otherConditions: conditions,
          timestampCondition: timestampCondition,
        };
      }

      // Extract ORDER BY clause
      const orderByMatch = normalizedSql.match(
        /ORDER\s+BY\s+(.+?)(?:\s+LIMIT|$)/i
      );
      if (orderByMatch) {
        const orderByStr = orderByMatch[1].trim();
        const parts = orderByStr.split(",").map((part) => part.trim());

        const orderColumns: OrderByColumn[] = parts.map((part) => {
          const [column, direction] = part.split(/\s+/);
          return {
            type: "orderby_column",
            column: column.trim(),
            direction:
              direction && direction.toUpperCase() === "DESC" ? "DESC" : "ASC",
          };
        });

        query.orderByClause = {
          type: "orderby",
          columns: orderColumns,
        };
      }

      // Extract LIMIT clause
      const limitMatch = normalizedSql.match(/LIMIT\s+(\d+)/i);
      if (limitMatch) {
        query.limitClause = {
          type: "limit",
          value: parseInt(limitMatch[1]),
        };
      }

      return query;
    } catch (error) {
      console.error("Error parsing SQL:", error);
      return null;
    }
  }

  /**
   * Apply a limit to a query
   */
  static applyLimit(query: SQLQuery, limit: number): SQLQuery {
    return {
      ...query,
      limitClause: {
        type: "limit",
        value: limit,
      },
    };
  }

  /**
   * Applies a new time range condition to a parsed SQL query AST.
   * @param query The query AST.
   * @param tsField The name of the timestamp field.
   * @param startDateTime The start date/time.
   * @param endDateTime The end date/time.
   * @returns The modified query AST.
   */
  static applyTimeRange(
    query: SQLQuery,
    tsField: string,
    startDateTime: CalendarDateTime,
    endDateTime: CalendarDateTime
  ): SQLQuery {
    const newTimeCondition = QueryBuilder.formatTimeCondition(tsField, startDateTime, endDateTime);

    if (query.whereClause) {
      // Update existing where clause
      query.whereClause.timestampCondition = newTimeCondition;
    } else {
      // Create a new where clause
      query.whereClause = {
        type: "where",
        otherConditions: "",
        timestampCondition: newTimeCondition,
      };
    }
    return query;
  }


  /**
   * Convert a query AST back to SQL
   */
  static toSQL(query: SQLQuery): string {
    let sql = "SELECT ";

    if (query.selectClause.distinct) {
      sql += "DISTINCT ";
    }

    // Use backticks for table name
    sql += query.selectClause.columns.join(", ");
    sql += `\nFROM \`${query.fromClause.table}\``;

    // Reconstruct WHERE clause
    if (query.whereClause) {
      const conditions: string[] = [];
      if (query.whereClause.timestampCondition) {
        conditions.push(query.whereClause.timestampCondition);
      }
      if (query.whereClause.otherConditions) {
        conditions.push(query.whereClause.otherConditions);
      }
      if (conditions.length > 0) {
        sql += `\nWHERE ${conditions.join(" AND ")}`;
      }
    }

    // Use backticks for order by columns
    if (query.orderByClause) {
      const orderColumns = query.orderByClause.columns
        .map((col) => `\`${col.column}\` ${col.direction}`) // Add backticks
        .join(", ");
      sql += `\nORDER BY ${orderColumns}`;
    }

    if (query.limitClause) {
      sql += `\nLIMIT ${query.limitClause.value}`;
    }

    return sql;
  }
}
