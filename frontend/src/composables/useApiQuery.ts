import { ref } from "vue";
import type { APIResponse, APIErrorResponse } from "@/api/types";
import { showErrorToast, isCanceledError } from "@/api/error-handler";

export function useApiQuery<T>() {
  const isLoading = ref(false);
  const error = ref<APIErrorResponse | null>(null);
  const data = ref<T | null>(null);

  const execute = async (
    apiCall: () => Promise<APIResponse<T>>,
    options?: {
      successMessage?: string;
      errorMessage?: string;
      showToast?: boolean;
      onSuccess?: (data: T | null) => void;  // Allow null in success handler
      onError?: (error: APIErrorResponse) => void;
      defaultData?: T;  // New option for fallback data
    }
  ) => {
    isLoading.value = true;
    error.value = null;
    
    try {
      const response = await apiCall();

      if (response && response.status === "success") {
        // Standard API response format with status and data fields
        const resultData = response.data ?? options?.defaultData ?? null;
        data.value = resultData;

        // Pass data to success handler
        options?.onSuccess?.(resultData);
        return { success: true, data: resultData };
      } else {
        // Handle API error response
        error.value = response as APIErrorResponse;
        if (options?.showToast !== false) {
          showErrorToast(response, options?.errorMessage);
        }
        options?.onError?.(response as APIErrorResponse);
        return { success: false, error: response as APIErrorResponse };
      }
    } catch (err) {
      // Handle canceled requests (AbortError, CanceledError) gracefully
      // This happens on page navigation, reload, or manual query cancellation
      if (
        (err instanceof Error && err.name === 'AbortError') ||
        isCanceledError(err)
      ) {
        // Don't treat user-initiated aborts as operational errors
        // Don't show error toast or call onError handler for cancellations
        return { success: false, error: null, aborted: true };
      }

      // Handle unexpected errors
      error.value = err as APIErrorResponse;
      if (options?.showToast !== false) {
        showErrorToast(err, options?.errorMessage);
      }
      options?.onError?.(error.value);
      return { success: false, error: error.value };
    } finally {
      isLoading.value = false;
    }
  };

  return { isLoading, error, data, execute };
}
