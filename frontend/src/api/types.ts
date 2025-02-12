import type { AxiosError } from 'axios'

export interface APIResponse<T = any> {
  status: 'success' | 'error'
  data: T | { error: string }
}

export function isErrorResponse<T>(response: APIResponse<T>): response is APIResponse<{ error: string }> & { status: 'error' } {
  return response.status === 'error'
}

export function getErrorMessage(error: unknown): string {
  // If it's an API response object
  if (error && typeof error === 'object' && 'status' in error) {
    const response = error as APIResponse;
    if (isErrorResponse(response)) {
      return response.data.error;
    }
  }

  // If it's an Axios error
  if (error && typeof error === 'object' && 'isAxiosError' in error) {
    const axiosError = error as AxiosError<APIResponse<{ error: string }>>;

    // Network error (no response received)
    if (!axiosError.response) {
      return 'Network error: Unable to connect to the server';
    }

    // Server responded with error
    if (axiosError.response.data && isErrorResponse(axiosError.response.data)) {
      return axiosError.response.data.data.error;
    }

    // Fallback to status text
    return `Server error: ${axiosError.response.statusText || 'Unknown error'}`;
  }

  // For any other type of error
  if (error instanceof Error) {
    return error.message;
  }

  return 'An unexpected error occurred';
}

export function isSuccessResponse<T>(response: APIResponse<T>): response is APIResponse<T> & { status: 'success' } {
  return response.status === 'success';
}
