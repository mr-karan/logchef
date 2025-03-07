/**
 * Utility functions for use across the application
 */

/**
 * Check if a value is numeric (can be parsed as a number)
 * @param {any} value - The value to check
 * @returns {boolean} - True if value is numeric
 */
export function isNumeric(value) {
  if (typeof value === 'number') return true;
  if (typeof value !== 'string') return false;
  return !isNaN(value) && !isNaN(parseFloat(value));
}

/**
 * Format a Unix timestamp or Date object as a human-readable string
 * @param {number|Date} timestamp - The timestamp to format
 * @returns {string} - Formatted date string
 */
export function formatDate(timestamp) {
  const date = typeof timestamp === 'number' ? new Date(timestamp) : timestamp;
  return date.toLocaleString();
}