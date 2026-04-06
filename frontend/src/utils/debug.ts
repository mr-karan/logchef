/**
 * Debug utility for conditional logging based on environment
 */

function safeLocalStorageFlag(key: string): boolean {
  if (typeof window === "undefined") {
    return false;
  }

  try {
    return window.localStorage.getItem(key) === "true";
  } catch {
    return false;
  }
}

const isDebugLoggingEnabled =
  import.meta.env.DEV || safeLocalStorageFlag("logchef:debug");

/**
 * Log debug messages only in non-production environments
 * @param namespace Namespace/module for the log message
 * @param message Primary message
 * @param args Additional arguments to log
 */
export function debug(namespace: string, message: string, ...args: any[]): void {
  if (isDebugLoggingEnabled) {
    console.log(`[${namespace}]`, message, ...args);
  }
}

/**
 * Log error messages in all environments
 * @param namespace Namespace/module for the error message
 * @param message Primary error message
 * @param args Additional arguments to log
 */
export function error(namespace: string, message: string, ...args: any[]): void {
  console.error(`[${namespace}]`, message, ...args);
}

/**
 * Log warning messages in all environments
 * @param namespace Namespace/module for the warning message
 * @param message Primary warning message
 * @param args Additional arguments to log
 */
export function warn(namespace: string, message: string, ...args: any[]): void {
  console.warn(`[${namespace}]`, message, ...args);
}
