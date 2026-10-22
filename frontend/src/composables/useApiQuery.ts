import type { APIResponse, APIErrorResponse } from "@/api/types";
import { showErrorToast, isAbortError, hasShownErrorToast } from "@/api/error-handler";
import { useToast } from "@/composables/useToast";
import { TOAST_DURATION } from "@/lib/constants";

export function useApiQuery<T>() {
  const execute = async (
    apiCall: () => Promise<APIResponse<T>>,
    options?: {
      successMessage?: string;
      errorMessage?: string;
      showToast?: boolean;
      onSuccess?: (data: T | null) => void | Promise<void>;
      onError?: (error: APIErrorResponse) => void;
      defaultData?: T;  // New option for fallback data
    }
  ) => {
    try {
      const response = await apiCall();

      if (response && response.status === "success") {
        // Standard API response format with status and data fields
        const resultData = response.data ?? options?.defaultData ?? null;

        // Pass data to success handler
        await options?.onSuccess?.(resultData);
        if (options?.successMessage && options.showToast !== false) {
          useToast().toast({ title: options.successMessage, duration: TOAST_DURATION.SUCCESS });
        }
        return { success: true, data: resultData };
      } else {
        // Handle API error response
        if (options?.showToast !== false) {
          showErrorToast(response, options?.errorMessage);
        }
        options?.onError?.(response as APIErrorResponse);
        return { success: false, error: response as APIErrorResponse };
      }
    } catch (err) {
      // Handle canceled requests (AbortError, CanceledError) gracefully
      // This happens on page navigation, reload, or manual query cancellation
      if (isAbortError(err)) {
        // Don't treat user-initiated aborts as operational errors
        // Don't show error toast or call onError handler for cancellations
        return { success: false, error: null, aborted: true };
      }

      // Handle unexpected errors
      const error = err as APIErrorResponse;
      if (options?.showToast !== false && !hasShownErrorToast(err)) {
        showErrorToast(err, options?.errorMessage);
      }
      options?.onError?.(error);
      return { success: false, error };
    }
  };

  return { execute };
}
