import { Parser } from './index';

/**
 * Validate a ClickHouse SQL query
 * 
 * @param query The SQL query string to validate
 * @returns Object with validation status and error message if invalid
 */
export function validateClickHouseSQL(query: string): { isValid: boolean; error?: string } {
  if (!query || query.trim() === '') {
    return { isValid: false, error: 'Empty query' };
  }

  try {
    // Parse the query for validation
    const parser = new Parser();
    parser.parse(query);
    return { isValid: true };
  } catch (error) {
    return { 
      isValid: false, 
      error: error instanceof Error ? error.message : 'Unknown error validating query' 
    };
  }
}

/**
 * Format SQL query with basic formatting rules
 * 
 * @param sql The SQL query to format
 * @returns Formatted SQL string
 */
export function formatSQL(sql: string): string {
  if (!sql || sql.trim() === '') {
    return '';
  }

  try {
    const formattedSQL = sql
      .replace(/\s+/g, ' ')
      .replace(/\s*,\s*/g, ', ')
      .replace(/\(\s+/g, '(')
      .replace(/\s+\)/g, ')')
      .replace(/\s*=\s*/g, ' = ')
      .replace(/\s*>\s*/g, ' > ')
      .replace(/\s*<\s*/g, ' < ')
      .replace(/\s*>=\s*/g, ' >= ')
      .replace(/\s*<=\s*/g, ' <= ')
      .replace(/\s*<>\s*/g, ' <> ')
      .replace(/\s*!=\s*/g, ' != ')
      .replace(/\s+AND\s+/gi, ' AND ')
      .replace(/\s+OR\s+/gi, ' OR ')
      .replace(/\s+IN\s+/gi, ' IN ')
      .replace(/\s+FROM\s+/gi, '\nFROM ')
      .replace(/\s+WHERE\s+/gi, '\nWHERE ')
      .replace(/\s+GROUP\s+BY\s+/gi, '\nGROUP BY ')
      .replace(/\s+HAVING\s+/gi, '\nHAVING ')
      .replace(/\s+ORDER\s+BY\s+/gi, '\nORDER BY ')
      .replace(/\s+LIMIT\s+/gi, '\nLIMIT ')
      .replace(/\s+UNION\s+/gi, '\nUNION\n')
      .replace(/\s+JOIN\s+/gi, '\nJOIN ')
      .replace(/\s+LEFT\s+JOIN\s+/gi, '\nLEFT JOIN ')
      .replace(/\s+RIGHT\s+JOIN\s+/gi, '\nRIGHT JOIN ')
      .replace(/\s+INNER\s+JOIN\s+/gi, '\nINNER JOIN ')
      .replace(/\s+ON\s+/gi, '\n  ON ');

    return formattedSQL.trim();
  } catch (error) {
    console.error('Error formatting SQL:', error);
    return sql;
  }
}

/**
 * Add time range conditions to a SQL query if not already present
 * 
 * @param sql The SQL query
 * @param options Time range options
 * @returns SQL query with time range conditions
 */
export function addTimeRangeToSQL(
  sql: string,
  options: {
    timeField?: string;
    startTime?: string | number;
    endTime?: string | number;
    limit?: number;
  } = {}
): string {
  const {
    timeField = 'timestamp',
    startTime,
    endTime,
    limit = 100
  } = options;

  if (!sql || !sql.trim()) {
    return `SELECT * FROM logs LIMIT ${limit}`;
  }

  try {
    // Simple check if query already has WHERE clause
    const hasWhere = /\bWHERE\b/i.test(sql);
    const hasLimit = /\bLIMIT\b/i.test(sql);
    
    let result = sql.trim();
    
    // Add time conditions if requested
    if (startTime || endTime) {
      const timeConditions = [];
      
      if (startTime) {
        const formattedStart = typeof startTime === 'string' 
          ? `'${startTime}'` 
          : `'${new Date(startTime).toISOString().replace('T', ' ').replace('Z', '')}'`;
        timeConditions.push(`${timeField} >= ${formattedStart}`);
      }
      
      if (endTime) {
        const formattedEnd = typeof endTime === 'string'
          ? `'${endTime}'`
          : `'${new Date(endTime).toISOString().replace('T', ' ').replace('Z', '')}'`;
        timeConditions.push(`${timeField} <= ${formattedEnd}`);
      }
      
      if (timeConditions.length > 0) {
        const timeClause = timeConditions.join(' AND ');
        
        if (hasWhere) {
          // Add to existing WHERE clause
          result = result.replace(/\bWHERE\b/i, `WHERE (${timeClause}) AND `);
        } else {
          // Add new WHERE clause before any GROUP BY, ORDER BY, or LIMIT
          const match = result.match(/\b(GROUP BY|ORDER BY|LIMIT)\b/i);
          if (match) {
            const position = match.index;
            result = result.substring(0, position) + 
                    `WHERE ${timeClause} ` + 
                    result.substring(position);
          } else {
            result += ` WHERE ${timeClause}`;
          }
        }
      }
    }
    
    // Add LIMIT if not present
    if (!hasLimit && limit) {
      result += ` LIMIT ${limit}`;
    }
    
    return result;
  } catch (error) {
    console.error('Error adding time range to SQL:', error);
    return sql;
  }
}
