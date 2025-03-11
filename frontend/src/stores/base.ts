import { ref } from "vue";
import type { Ref } from "vue";
import { showErrorToast, showSuccessToast } from "@/api/error-handler";

export interface BaseState<T> {
  data: Ref<T>;
  loading: Ref<boolean>;
  error: Ref<string | null>;
}

/**
 * Creates a base store with common state management functionality
 */
export function useBaseStore<T>(initialState: T): BaseState<T> & {
  isLoading: Ref<boolean>;
  withLoading: <R>(fn: () => Promise<R>) => Promise<R>;
  callApi: <R>(options: {
    apiCall: () => Promise<any>;
    onSuccess?: (data: R) => void;
    onError?: (error: string) => void;
    successMessage?: string;
    errorMessage?: string;
    showToast?: boolean;
  }) => Promise<{ success: boolean; data?: R; error?: string }>;
} {
  const data = ref(initialState) as Ref<T>;
  const loading = ref(false);
  const error = ref<string | null>(null);
  const isLoading = ref(false);

  /**
   * Execute an async function with loading state management
   */
  async function withLoading<R>(fn: () => Promise<R>): Promise<R> {
    try {
      isLoading.value = true;
      loading.value = true;
      error.value = null;
      return await fn();
    } catch (err) {
      if (err instanceof Error) {
        error.value = err.message;
      } else {
        error.value = "An unexpected error occurred";
      }
      throw err;
    } finally {
      isLoading.value = false;
      loading.value = false;
    }
  }

  /**
   * Simplified API call function that combines withLoading and handleApiCall
   */
  async function callApi<R>(options: {
    apiCall: () => Promise<any>;
    onSuccess?: (data: R) => void;
    onError?: (error: string) => void;
    successMessage?: string;
    errorMessage?: string;
    showToast?: boolean;
  }): Promise<{ success: boolean; data?: R; error?: string }> {
    return await withLoading(async () => {
      return handleApiCall<R>(options);
    });
  }

  return {
    data,
    loading,
    error,
    isLoading,
    withLoading,
    callApi,
  };
}

/**
 * Handle API calls with consistent error handling
 */
export async function handleApiCall<T>({
  apiCall,
  onSuccess,
  onError,
  successMessage,
  errorMessage,
  showToast = true,
}: {
  apiCall: () => Promise<any>;
  onSuccess?: (data: T) => void;
  onError?: (error: string) => void;
  successMessage?: string;
  errorMessage?: string;
  showToast?: boolean;
}): Promise<{ success: boolean; data?: T; error?: string }> {
  try {
    const response = await apiCall();

    // Handle API error responses (status: "error")
    if (response.status === "error") {
      // Use the specific error message from the API
      const specificErrorMsg = response.message;

      if (showToast) {
        // Don't pass the errorMessage parameter to showErrorToast
        // This ensures the specific API error message is used
        showErrorToast(response);
      }

      onError?.(specificErrorMsg);
      return { success: false, error: specificErrorMsg };
    }

    // Handle successful responses
    if (showToast && successMessage) {
      showSuccessToast(successMessage);
    }

    onSuccess?.(response.data);
    return { success: true, data: response.data };
  } catch (err) {
    // For network errors or other exceptions
    const errorMsg =
      errorMessage || (err instanceof Error ? err.message : "Unknown error");

    if (showToast) {
      showErrorToast(err, errorMessage);
    }

    onError?.(errorMsg);
    return { success: false, error: errorMsg };
  }
}
