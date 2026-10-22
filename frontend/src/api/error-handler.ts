import { useToast } from "@/composables/useToast";
import { TOAST_DURATION } from "@/lib/constants";
import type { APIErrorResponse } from "@/api/types";

// Error type mapping
const ERROR_TITLES: Record<string, string> = {
  ValidationError: "Validation Error",
  AuthenticationError: "Authentication Error",
  AuthorizationError: "Authorization Error",
  NotFoundError: "Not Found",
  RateLimitError: "Rate Limit Exceeded",
  DemoModeError: "Read-only demo",
  GeneralError: "Error",
  GeneralException: "System Error",
};

/**
 * Formats an error message from an Error, an API error response, or any object with a message
 */
export function formatErrorMessage(error: unknown, fallback = "An unexpected error occurred"): string {
  if (error && typeof error === "object" && "message" in error && typeof error.message === "string") {
    return error.message || fallback;
  }
  return fallback;
}

/**
 * Gets the error type from an error object
 */
function getErrorType(error: unknown): string {
  // API error response
  if (error && typeof error === "object" && "status" in error) {
    const response = error as APIErrorResponse;
    if (response.status === "error" && response.error_type) {
      return response.error_type;
    }
  }

  return "UnknownError";
}

/**
 * Format error type to a readable title
 */
export function formatErrorTypeToTitle(errorType: string): string {
  // Use predefined title if available
  if (errorType in ERROR_TITLES) return ERROR_TITLES[errorType];

  // Format camelCase or snake_case to Title Case
  return errorType
    .replace(/([A-Z])/g, " $1") // Add space before capital letters
    .replace(/_/g, " ")         // Replace underscores with spaces
    .replace(/^./, str => str.toUpperCase()) // Capitalize first letter
    .trim();
}

/**
 * Checks if an error is a canceled request error that should be handled silently
 */
export function isCanceledError(error: unknown): boolean {
  if (error && typeof error === "object") {
    // Check for our custom _silent flag
    if ("_silent" in error && error._silent === true) {
      return true;
    }
    // Check for CanceledError type
    if ("error_type" in error && error.error_type === "CanceledError") {
      return true;
    }
  }
  return false;
}

/** Checks if an error came from an aborted or canceled request (fetch AbortError or our CanceledError). */
export function isAbortError(error: unknown): boolean {
  return isCanceledError(error) || (error !== null && typeof error === "object" && "name" in error && error.name === "AbortError");
}

/** Returns true when an interceptor already surfaced this error to the user. */
export function hasShownErrorToast(error: unknown): boolean {
  return Boolean(error && typeof error === "object" && "_toastShown" in error && (error as APIErrorResponse)._toastShown);
}

/**
 * Shows a toast notification for an error
 */
export function showErrorToast(error: unknown, customMessage?: string): void {
  // Don't show toast for canceled requests (page navigation, reload, etc.)
  if (isCanceledError(error)) {
    return;
  }

  const { toast } = useToast();
  const title = formatErrorTypeToTitle(getErrorType(error));
  const message = customMessage || formatErrorMessage(error);

  toast({
    title,
    description: message,
    variant: "destructive",
    duration: TOAST_DURATION.ERROR
  });
}
