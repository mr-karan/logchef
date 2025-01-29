import type { AxiosError } from 'axios'

export interface APIResponse<T extends object = object> {
  status: 'success' | 'error';
  data: T | { error: string };
}

export function isErrorResponse(response: APIResponse): response is APIResponse<{ error: string }> {
  return response.status === 'error' && 'error' in response.data;
}

export function getErrorMessage(error: unknown): string {
  // If it's our API error response
  if (error && typeof error === 'object' && 'status' in error) {
    const response = error as APIResponse;
    if (isErrorResponse(response)) {
      return response.data.error;
    }
  }

  // If it's an Axios error
  if (error && typeof error === 'object' && 'isAxiosError' in error) {
    const axiosError = error as AxiosError;

    // Network error (no response received)
    if (!axiosError.response) {
      return 'Network error: Unable to connect to the server';
    }

    // Server responded with a non-2xx status
    if (axiosError.response.status >= 400) {
      // Try to get error from response data
      const data = axiosError.response.data;
      if (data && typeof data === 'object' && 'error' in data) {
        return String(data.error);
      }
      // Fallback to status text
      return `Server error: ${axiosError.response.statusText || 'Unknown error'}`;
    }
  }

  // For any other type of error
  if (error instanceof Error) {
    return error.message;
  }

  return 'An unexpected error occurred';
}

export function isSuccessResponse<T extends object>(response: APIResponse<T>): response is APIResponse<T> {
  return response.status === 'success' && !('error' in response.data);
}