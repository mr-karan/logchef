import { useToast } from "@/components/ui/toast/use-toast";
import { TOAST_DURATION } from "@/lib/constants";
import type { AxiosError } from "axios";

export interface APIErrorResponse {
  status: "error";
  message: string;
  error_type: string;
  data?: any;
}

// Error type mapping
const ERROR_TITLES: Record<string, string> = {
  ValidationError: "Validation Error",
  AuthenticationError: "Authentication Error",
  AuthorizationError: "Authorization Error",
  NotFoundError: "Not Found",
  RateLimitError: "Rate Limit Exceeded",
  GeneralError: "Error",
  GeneralException: "System Error",
};

/**
 * Formats an error message from various error types
 */
export function formatErrorMessage(error: unknown): string {
  // API error response object
  if (error && typeof error === "object" && "status" in error) {
    const response = error as APIErrorResponse;
    if (response.status === "error") return response.message;
  }

  // Axios error
  if (error && typeof error === "object" && "isAxiosError" in error) {
    const axiosError = error as AxiosError<APIErrorResponse>;

    // Network error (no response)
    if (!axiosError.response) {
      return "Network error: Unable to connect to the server";
    }

    // Server error with proper format
    if (axiosError.response.data?.status === "error") {
      return axiosError.response.data.message;
    }

    // Fallback to status text
    return `Server error: ${axiosError.response.statusText || "Unknown error"}`;
  }

  // Standard Error object
  if (error instanceof Error) return error.message;

  // Unknown error type
  return "An unexpected error occurred";
}

/**
 * Gets the error type from an error object
 */
export function getErrorType(error: unknown): string {
  // API error response
  if (error && typeof error === "object" && "status" in error) {
    const response = error as APIErrorResponse;
    if (response.status === "error" && response.error_type) {
      return response.error_type;
    }
  }

  // Axios error
  if (error && typeof error === "object" && "isAxiosError" in error) {
    const axiosError = error as AxiosError<APIErrorResponse>;
    if (axiosError.response?.data?.error_type) {
      return axiosError.response.data.error_type;
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
 * Parse error to get both title and message
 */
export function parseError(error: unknown): { title: string; message: string } {
  return {
    title: formatErrorTypeToTitle(getErrorType(error)),
    message: formatErrorMessage(error)
  };
}

/**
 * Shows a toast notification for an error
 */
export function showErrorToast(error: unknown, customMessage?: string): void {
  const { toast } = useToast();
  const { title } = parseError(error);
  const message = customMessage || formatErrorMessage(error);

  toast({
    title,
    description: message,
    variant: "destructive",
    duration: TOAST_DURATION.ERROR
  });
}

/**
 * Shows a success toast notification
 */
export function showSuccessToast(message: string): void {
  const { toast } = useToast();
  toast({
    title: "Success",
    description: message,
    variant: "success",
    duration: TOAST_DURATION.SUCCESS
  });
}
