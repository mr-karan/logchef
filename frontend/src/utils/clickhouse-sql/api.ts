/**
 * Validates a ClickHouse SQL query
 * @param query The query string to validate
 * @returns True if valid, false otherwise
 */
export function validateSQL(query: string): boolean {
  // Empty query is not valid for direct execution
  if (!query || !query.trim()) {
    // Return false so submitQuery knows to generate default
    return false;
  }

  try {
    // Basic validation - make sure query has SELECT and FROM
    const hasSelect = /\bSELECT\b/i.test(query);
    const hasFrom = /\bFROM\b/i.test(query);
    
    // Check for timestamp field (either raw or with conversion)
    const hasTimestamp = /\b(timestamp|toDateTime\(|toDateTime64\()/i.test(query);
    
    // Check for required namespace condition - match both quoted and unquoted versions
    const hasNamespace = /\b(namespace\s*=|`namespace`\s*=)/i.test(query);
    
    return hasSelect && hasFrom && 
           hasTimestamp &&
           hasNamespace;
  } catch (error) {
    console.error("Error during SQL validation:", error); // Log error
    return false;
  }
}
