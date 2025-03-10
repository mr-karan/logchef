import { useToast } from "@/components/ui/toast/use-toast";
import { TOAST_DURATION } from "@/lib/constants";
import type { AxiosError } from "axios";

export interface APIErrorResponse {
  status: "error";
  message: string;
  error_type: string;
  data?: any;
}

/**
 * Formats an error message from various error types
 */
export function formatErrorMessage(error: unknown): string {
  // If it's an API error response object
  if (error && typeof error === "object" && "status" in error) {
    const response = error as APIErrorResponse;
    if (response.status === "error") {
      return response.message;
    }
  }

  // If it's an Axios error
  if (error && typeof error === "object" && "isAxiosError" in error) {
    const axiosError = error as AxiosError<APIErrorResponse>;

    // Network error (no response received)
    if (!axiosError.response) {
      return "Network error: Unable to connect to the server";
    }

    // Server responded with error
    if (
      axiosError.response.data &&
      axiosError.response.data.status === "error"
    ) {
      return axiosError.response.data.message;
    }

    // Fallback to status text
    return `Server error: ${axiosError.response.statusText || "Unknown error"}`;
  }

  // For any other type of error
  if (error instanceof Error) {
    return error.message;
  }

  return "An unexpected error occurred";
}

/**
 * Gets the error type from an error object
 */
export function getErrorType(error: unknown): string {
  // If it's an API error response object
  if (error && typeof error === "object" && "status" in error) {
    const response = error as APIErrorResponse;
    if (response.status === "error" && response.error_type) {
      return response.error_type;
    }
  }

  // If it's an Axios error with API error response
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
function formatErrorTypeToTitle(errorType: string): string {
  // Handle common error types
  switch (errorType) {
    case "ValidationError":
      return "Validation Error";
    case "AuthenticationError":
      return "Authentication Error";
    case "AuthorizationError":
      return "Authorization Error";
    case "NotFoundError":
      return "Not Found";
    case "RateLimitError":
      return "Rate Limit Exceeded";
    case "GeneralError":
      return "Error";
    case "GeneralException":
      return "System Error";
    default:
      // Format camelCase or snake_case to Title Case
      return errorType
        .replace(/([A-Z])/g, " $1") // Insert space before capital letters
        .replace(/_/g, " ") // Replace underscores with spaces
        .replace(/^./, (str) => str.toUpperCase()) // Capitalize first letter
        .trim();
  }
}

/**
 * Shows a toast notification for an error
 */
export function showErrorToast(error: unknown, customMessage?: string): void {
  const { toast } = useToast();
  const message = customMessage || formatErrorMessage(error);
  const errorType = getErrorType(error);

  // Use error_type as the title, with proper formatting
  let title = formatErrorTypeToTitle(errorType);
  let duration = TOAST_DURATION.ERROR;

  toast({
    title,
    description: message,
    variant: "destructive",
    duration,
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
    duration: TOAST_DURATION.SUCCESS,
  });
}
