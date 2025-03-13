// Simple AST for SQL queries to help with dynamic updates
export interface SQLNode {
  type: string;
}

export interface SQLQuery extends SQLNode {
  type: 'query';
  selectClause: SelectClause;
  fromClause: FromClause;
  whereClause?: WhereClause;
  orderByClause?: OrderByClause;
  limitClause?: LimitClause;
}

export interface SelectClause extends SQLNode {
  type: 'select';
  distinct?: boolean;
  columns: string[]; // Column names or expressions as strings
}

export interface FromClause extends SQLNode {
  type: 'from';
  table: string;
  alias?: string;
}

export interface WhereClause extends SQLNode {
  type: 'where';
  conditions: string; // Raw condition string
  hasTimestampFilter: boolean;
}

export interface OrderByClause extends SQLNode {
  type: 'orderby';
  columns: OrderByColumn[];
}

export interface OrderByColumn extends SQLNode {
  type: 'orderby_column';
  column: string;
  direction: 'ASC' | 'DESC';
}

export interface LimitClause extends SQLNode {
  type: 'limit';
  value: number;
}

/**
 * A simple SQL parser that creates a basic AST for ClickHouse queries
 */
export class SQLParser {
  /**
   * Parse a SQL query string into an AST
   * @param sql The SQL query string to parse
   * @returns SQLQuery AST or null if parsing fails
   */
  static parse(sql: string, tsField: string = 'timestamp'): SQLQuery | null {
    try {
      // Start with a basic query structure
      const query: SQLQuery = {
        type: 'query',
        selectClause: { type: 'select', columns: [] },
        fromClause: { type: 'from', table: '' }
      };

      // Normalize whitespace
      const normalizedSql = sql.replace(/\s+/g, ' ').trim();
      
      // Extract SELECT clause
      const selectMatch = normalizedSql.match(/SELECT\s+(DISTINCT\s+)?(.+?)\s+FROM/i);
      if (selectMatch) {
        query.selectClause.distinct = !!selectMatch[1];
        // Simple approach - just store the whole columns part as a string
        query.selectClause.columns = [selectMatch[2].trim()];
      } else {
        query.selectClause.columns = ['*']; // Default to SELECT *
      }

      // Extract FROM clause
      const fromMatch = normalizedSql.match(/FROM\s+([a-zA-Z0-9_.]+)(?:\s+|$|\s+WHERE|\s+ORDER|\s+LIMIT)/i);
      if (fromMatch) {
        query.fromClause.table = fromMatch[1].trim();
      }

      // Extract WHERE clause
      const whereMatch = normalizedSql.match(/WHERE\s+(.+?)(?:\s+ORDER\s+BY|\s+LIMIT|$)/i);
      if (whereMatch) {
        // Check if there's a timestamp filter
        const hasTimestampFilter = new RegExp(`${tsField}\\s+BETWEEN`, 'i').test(whereMatch[1]);
        
        query.whereClause = {
          type: 'where',
          conditions: whereMatch[1].trim(),
          hasTimestampFilter
        };
      }

      // Extract ORDER BY clause
      const orderByMatch = normalizedSql.match(/ORDER\s+BY\s+(.+?)(?:\s+LIMIT|$)/i);
      if (orderByMatch) {
        const orderByStr = orderByMatch[1].trim();
        const parts = orderByStr.split(',').map(part => part.trim());
        
        const orderColumns: OrderByColumn[] = parts.map(part => {
          const [column, direction] = part.split(/\s+/);
          return {
            type: 'orderby_column',
            column: column.trim(),
            direction: (direction && direction.toUpperCase() === 'DESC') ? 'DESC' : 'ASC'
          };
        });
        
        query.orderByClause = {
          type: 'orderby',
          columns: orderColumns
        };
      }

      // Extract LIMIT clause
      const limitMatch = normalizedSql.match(/LIMIT\s+(\d+)/i);
      if (limitMatch) {
        query.limitClause = {
          type: 'limit',
          value: parseInt(limitMatch[1])
        };
      }

      return query;
    } catch (error) {
      console.error('Error parsing SQL:', error);
      return null;
    }
  }

  /**
   * Apply time range filter to a query
   * @param query The query AST
   * @param tsField The timestamp field name
   * @param startTime Start time as ISO string
   * @param endTime End time as ISO string
   * @returns Updated query AST
   */
  static applyTimeRange(query: SQLQuery, tsField: string, startTime: string, endTime: string): SQLQuery {
    const newQuery = { ...query };
    
    // Format the time filter
    const timeFilter = `${tsField} BETWEEN '${startTime}' AND '${endTime}'`;
    
    if (!newQuery.whereClause) {
      // Add a new WHERE clause
      newQuery.whereClause = {
        type: 'where',
        conditions: timeFilter,
        hasTimestampFilter: true
      };
    } else if (!newQuery.whereClause.hasTimestampFilter) {
      // Append to existing WHERE with AND
      newQuery.whereClause = {
        ...newQuery.whereClause,
        conditions: `${newQuery.whereClause.conditions} AND ${timeFilter}`,
        hasTimestampFilter: true
      };
    } else {
      // Replace existing time filter using regex
      const regex = new RegExp(`${tsField}\\s+BETWEEN\\s+'[^']+'\\s+AND\\s+'[^']+'`, 'i');
      newQuery.whereClause = {
        ...newQuery.whereClause,
        conditions: newQuery.whereClause.conditions.replace(regex, timeFilter)
      };
    }
    
    return newQuery;
  }

  /**
   * Apply a limit to a query
   * @param query The query AST
   * @param limit The limit value
   * @returns Updated query AST
   */
  static applyLimit(query: SQLQuery, limit: number): SQLQuery {
    return {
      ...query,
      limitClause: {
        type: 'limit',
        value: limit
      }
    };
  }

  /**
   * Convert a query AST back to SQL
   * @param query The query AST
   * @returns SQL string
   */
  static toSQL(query: SQLQuery): string {
    let sql = 'SELECT ';
    
    if (query.selectClause.distinct) {
      sql += 'DISTINCT ';
    }
    
    sql += query.selectClause.columns.join(', ');
    
    sql += `\nFROM ${query.fromClause.table}`;
    
    if (query.whereClause) {
      sql += `\nWHERE ${query.whereClause.conditions}`;
    }
    
    if (query.orderByClause) {
      const orderColumns = query.orderByClause.columns.map(col => 
        `${col.column} ${col.direction}`
      ).join(', ');
      
      sql += `\nORDER BY ${orderColumns}`;
    }
    
    if (query.limitClause) {
      sql += `\nLIMIT ${query.limitClause.value}`;
    }
    
    return sql;
  }
}
