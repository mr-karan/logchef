import type { SQLAst, TimeRange } from '@/types/query';
import { createTimeRangeCondition, timeRangeToCalendarDateTime } from '@/utils/time-utils';

/**
 * SQL Builder class for parsing, manipulating and generating SQL queries
 */
export class SQLBuilder {
  /**
   * Parse a SQL query string into an AST
   * @param sql The SQL query to parse
   * @param tsField Optional timestamp field name for identifying time conditions
   * @returns Parsed SQLAst object
   */
  static parse(sql: string, tsField?: string): SQLAst | null {
    if (!sql.trim()) {
      return null;
    }

    try {
      // Initialize AST with default structure
      const ast: SQLAst = {
        selectClause: {
          columns: ['*'],
          distinct: false
        },
        fromClause: {
          table: ''
        },
        whereClause: {
          conditions: []
        }
      };

      // Parse SELECT clause
      const selectMatch = sql.match(/SELECT\s+(DISTINCT\s+)?(.+?)\s+FROM/i);
      if (selectMatch) {
        ast.selectClause.distinct = !!selectMatch[1];
        const columns = selectMatch[2].trim();
        ast.selectClause.columns = columns === '*' ? ['*'] : columns.split(',').map(c => c.trim());
      }

      // Parse FROM clause
      const fromMatch = sql.match(/FROM\s+(.+?)(?:\s+WHERE|\s+GROUP\s+BY|\s+ORDER\s+BY|\s+LIMIT|$)/i);
      if (fromMatch) {
        ast.fromClause.table = fromMatch[1].trim();
      }

      // Parse WHERE clause
      const whereMatch = sql.match(/WHERE\s+(.+?)(?:\s+GROUP\s+BY|\s+ORDER\s+BY|\s+LIMIT|$)/i);
      if (whereMatch) {
        const whereConditions = whereMatch[1].trim();

        // If tsField is provided, try to extract time condition
        if (tsField) {
          const timeConditionRegex = new RegExp(`\`?${tsField}\`?\\s+BETWEEN\\s+toDateTime\\(.+?\\)\\s+AND\\s+toDateTime\\(.+?\\)`, 'i');
          const timeConditionMatch = whereConditions.match(timeConditionRegex);

          if (timeConditionMatch) {
            ast.whereClause.timeCondition = timeConditionMatch[0];

            // Remove time condition from other conditions
            const otherConditions = whereConditions
              .replace(timeConditionMatch[0], '')
              .replace(/^\s*AND\s+|\s+AND\s*$/gi, '') // Remove leading/trailing ANDs
              .trim();

            if (otherConditions) {
              ast.whereClause.conditions = [otherConditions];
            }
          } else {
            ast.whereClause.conditions = [whereConditions];
          }
        } else {
          ast.whereClause.conditions = [whereConditions];
        }
      }

      // Parse ORDER BY clause
      const orderByMatch = sql.match(/ORDER\s+BY\s+(.+?)(?:\s+LIMIT|$)/i);
      if (orderByMatch) {
        const orderParts = orderByMatch[1].trim().split(',');
        ast.orderByClause = {
          columns: orderParts.map(part => {
            const [field, direction] = part.trim().split(/\s+/);
            return {
              field: field.trim(),
              direction: (direction?.toUpperCase() === 'DESC' ? 'DESC' : 'ASC') as 'ASC' | 'DESC'
            };
          })
        };
      }

      // Parse LIMIT clause
      const limitMatch = sql.match(/LIMIT\s+(\d+)/i);
      if (limitMatch) {
        ast.limitClause = {
          limit: parseInt(limitMatch[1], 10)
        };
      }

      return ast;
    } catch (error) {
      console.error("Error parsing SQL:", error);
      return null;
    }
  }

  /**
   * Generate a SQL string from an AST
   * @param ast The SQLAst object
   * @returns SQL query string
   */
  static generateSQL(ast: SQLAst): string {
    if (!ast.fromClause.table) {
      throw new Error('FROM clause must specify a table');
    }

    // Build SELECT clause
    const selectClause = `SELECT ${ast.selectClause.distinct ? 'DISTINCT ' : ''}${ast.selectClause.columns.join(', ')}`;

    // Build FROM clause
    const formattedTable = ast.fromClause.table.includes('`') || ast.fromClause.table.includes('.')
      ? ast.fromClause.table
      : `\`${ast.fromClause.table}\``;
    const fromClause = `FROM ${formattedTable}`;

    // Build WHERE clause
    let whereClause = '';
    const conditions = [...ast.whereClause.conditions];
    if (ast.whereClause.timeCondition) {
      conditions.unshift(ast.whereClause.timeCondition);
    }

    if (conditions.length > 0) {
      whereClause = `WHERE ${conditions.join(' AND ')}`;
    }

    // Build GROUP BY clause
    let groupByClause = '';
    if (ast.groupByClause?.columns.length) {
      groupByClause = `GROUP BY ${ast.groupByClause.columns.join(', ')}`;
    }

    // Build ORDER BY clause
    let orderByClause = '';
    if (ast.orderByClause?.columns.length) {
      const orderParts = ast.orderByClause.columns.map(
        col => `${col.field} ${col.direction}`
      );
      orderByClause = `ORDER BY ${orderParts.join(', ')}`;
    }

    // Build LIMIT clause
    let limitClause = '';
    if (ast.limitClause) {
      limitClause = `LIMIT ${ast.limitClause.limit}`;
    }

    // Combine all parts
    return [
      selectClause,
      fromClause,
      whereClause,
      groupByClause,
      orderByClause,
      limitClause
    ].filter(Boolean).join('\n');
  }

  /**
   * Apply a time range to an existing SQL AST
   * @param ast The SQLAst to modify
   * @param tsField The timestamp field name
   * @param timeRange The time range to apply
   * @returns Updated SQLAst with new time range
   */
  static applyTimeRange(ast: SQLAst, tsField: string, timeRange: TimeRange): SQLAst {
    // Create a deep clone of the AST to avoid mutations
    const newAst = JSON.parse(JSON.stringify(ast)) as SQLAst;

    // Create the time condition
    const timeCondition = createTimeRangeCondition(tsField, timeRange);

    // Update the time condition in the WHERE clause
    newAst.whereClause.timeCondition = timeCondition;

    return newAst;
  }

  /**
   * Apply a limit to an existing SQL AST
   * @param ast The SQLAst to modify
   * @param limit The limit to apply
   * @returns Updated SQLAst with new limit
   */
  static applyLimit(ast: SQLAst, limit: number): SQLAst {
    // Create a deep clone of the AST to avoid mutations
    const newAst = JSON.parse(JSON.stringify(ast)) as SQLAst;

    // Update the limit
    newAst.limitClause = { limit };

    return newAst;
  }

  /**
   * Add or update WHERE conditions in an existing SQL AST
   * @param ast The SQLAst to modify
   * @param conditions The conditions to apply
   * @param replace Whether to replace existing conditions
   * @returns Updated SQLAst with new/updated conditions
   */
  static applyWhere(ast: SQLAst, conditions: string[], replace = false): SQLAst {
    // Create a deep clone of the AST to avoid mutations
    const newAst = JSON.parse(JSON.stringify(ast)) as SQLAst;

    if (replace) {
      newAst.whereClause.conditions = [...conditions];
    } else {
      newAst.whereClause.conditions = [...newAst.whereClause.conditions, ...conditions];
    }

    return newAst;
  }

  /**
   * Create a default SQLAst object
   * @param tableName The table name
   * @param tsField The timestamp field
   * @param timeRange The time range
   * @param limit The result limit
   * @returns A new SQLAst with default structure
   */
  static createDefault(tableName: string, tsField: string, timeRange: TimeRange, limit: number): SQLAst {
    const timeCondition = createTimeRangeCondition(tsField, timeRange);

    return {
      selectClause: {
        columns: ['*'],
        distinct: false
      },
      fromClause: {
        table: tableName
      },
      whereClause: {
        conditions: [],
        timeCondition
      },
      orderByClause: {
        columns: [
          {
            field: tsField.includes('`') ? tsField : `\`${tsField}\``,
            direction: 'DESC'
          }
        ]
      },
      limitClause: {
        limit
      }
    };
  }
}