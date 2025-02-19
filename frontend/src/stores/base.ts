import { ref } from "vue";
import type { Ref } from "vue";
import { useToast } from "@/components/ui/toast/use-toast";
import { TOAST_DURATION } from "@/lib/constants";
import {
  isErrorResponse,
  type APIResponse,
  getErrorMessage,
} from "@/api/types";

export interface BaseState<T> {
  data: Ref<T>;
  isLoading: Ref<boolean>;
  error: Ref<string | null>;
}

export function createBaseStore<T>(initialState: T): BaseState<T> {
  return {
    data: ref(initialState) as Ref<T>,
    isLoading: ref(false),
    error: ref(null),
  };
}

export async function handleApiCall<T, R = void>(options: {
  apiCall: () => Promise<APIResponse<T>>;
  onSuccess?: (response: T) => R;
  onError?: (error: string) => void;
  successMessage?: string;
  errorMessage?: string;
  showToast?: boolean;
}): Promise<{ success: boolean; data?: T; result?: R }> {
  const {
    apiCall,
    onSuccess,
    onError,
    successMessage,
    errorMessage,
    showToast = true,
  } = options;
  const { toast } = useToast();

  try {
    const response = await apiCall();

    if (isErrorResponse(response)) {
      const message = errorMessage || response.data.error;
      if (showToast) {
        toast({
          title: "Error",
          description: message,
          variant: "destructive",
          duration: TOAST_DURATION.ERROR,
        });
      }
      onError?.(message);
      return { success: false };
    }

    if (showToast && successMessage) {
      toast({
        title: "Success",
        description: successMessage,
        duration: TOAST_DURATION.SUCCESS,
      });
    }

    const result = onSuccess?.(response.data);
    return { success: true, data: response.data, result };
  } catch (err) {
    const message = errorMessage || getErrorMessage(err);
    if (showToast) {
      toast({
        title: "Error",
        description: message,
        variant: "destructive",
        duration: TOAST_DURATION.ERROR,
      });
    }
    onError?.(message);
    return { success: false };
  }
}

export function useBaseStore<T>(initialState: T) {
  const state = createBaseStore(initialState);

  const withLoading = async <R>(
    fn: () => Promise<R>,
    options: { setError?: boolean } = {}
  ): Promise<R> => {
    const { setError = true } = options;
    try {
      state.isLoading.value = true;
      if (setError) {
        state.error.value = null;
      }
      return await fn();
    } catch (err) {
      if (setError) {
        state.error.value = getErrorMessage(err);
      }
      throw err;
    } finally {
      state.isLoading.value = false;
    }
  };

  return {
    ...state,
    withLoading,
  };
}
