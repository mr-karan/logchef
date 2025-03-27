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
    
    // Check for timestamp conversion functions
    const hasTimeConversion = /\btoDateTime(64)?\(/i.test(query);
    
    // Check for required namespace condition
    const hasNamespace = /\bnamespace\s*=/i.test(query);
    
    return hasSelect && hasFrom && 
           (hasTimeConversion || !/\btimestamp\b/i.test(query)) &&
           hasNamespace;
  } catch (error) {
    console.error("Error during SQL validation:", error); // Log error
    return false;
  }
}
