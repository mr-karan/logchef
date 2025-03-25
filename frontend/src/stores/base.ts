import { ref } from "vue";
import type { Ref } from "vue";
import type { APIErrorResponse } from "@/api/types";
import { useApiQuery } from "@/composables/useApiQuery";
import { useLoadingState } from "@/composables/useLoadingState";
import { useToast } from "@/components/ui/toast/use-toast";
import { formatErrorMessage, getErrorType, formatErrorTypeToTitle } from "@/api/error-handler";

export interface BaseState<T> {
  data: Ref<T>;
  error: Ref<APIErrorResponse | null>;
}

/**
 * Creates a base store with common state management functionality
 */
export function useBaseStore<T>(initialState: T): BaseState<T> & {
  isLoading: Ref<boolean>;
  loadingStates: Ref<Record<string, boolean>>;
  withLoading: <R>(key: string, fn: () => Promise<R>) => Promise<R>;
  isLoadingOperation: (key: string) => boolean;
  startLoading: (key: string) => void;
  stopLoading: (key: string) => void;
  handleError: (error: Error | APIErrorResponse, operation: string) => { success: false, error: APIErrorResponse };
  callApi: <R>(options: {
    apiCall: () => Promise<any>;
    operationKey?: string;
    onSuccess?: (data: R | null) => void;
    onError?: (error: APIErrorResponse) => void;
    successMessage?: string;
    errorMessage?: string;
    showToast?: boolean;
    defaultData?: R;
  }) => Promise<{ success: boolean; data?: R | null; error?: APIErrorResponse }>;
} {
  const data = ref(initialState) as Ref<T>;
  const error = ref<APIErrorResponse | null>(null);
  
  // Use our new loading state composable
  const { 
    isLoading, 
    loadingStates, 
    withLoading, 
    isLoadingOperation,
    startLoading,
    stopLoading
  } = useLoadingState();

  // Use our new API query composable
  const { execute } = useApiQuery();

  /**
   * Centralized error handling function for stores
   */
  function handleError(error: Error | APIErrorResponse, operation: string) {
    console.error(`[${operation} Error]`, error);
    
    const errorMessage = error instanceof Error ? error.message : error.message;
    const errorType = error instanceof Error ? 'UnknownError' : (error.error_type || 'UnknownError');
    const errorData = error instanceof Error ? undefined : error.data;
    
    // Update store error state
    error.value = {
      message: errorMessage,
      error_type: errorType,
      data: errorData,
      operation
    };
    
    // Show toast notification
    const { toast } = useToast();
    toast({
      title: formatErrorTypeToTitle(errorType),
      description: errorMessage,
      variant: 'destructive',
    });
    
    return { 
      success: false,
      error: error.value
    };
  }

  /**
   * Simplified API call function that combines withLoading and API execution
   */
  async function callApi<R>(options: {
    apiCall: () => Promise<any>;
    operationKey?: string;
    onSuccess?: (data: R | null) => void;
    onError?: (error: APIErrorResponse) => void;
    successMessage?: string;
    errorMessage?: string;
    showToast?: boolean;
    defaultData?: R;
  }) {
    // Default showToast to true unless explicitly set to false
    const showToast = options.showToast !== false;
    
    const executeApiCall = async () => {
      try {
        const result = await execute<R>(options.apiCall, {
          successMessage: options.successMessage,
          errorMessage: options.errorMessage,
          showToast: showToast,
          defaultData: options.defaultData,
          onSuccess: options.onSuccess,
          onError: (err) => {
            error.value = err;
            options.onError?.(err);
          }
        });
        
        return result;
      } catch (err) {
        // Use the centralized error handler
        return handleError(err as Error | APIErrorResponse, options.operationKey || 'api');
      }
    };

    if (options.operationKey) {
      return await withLoading(options.operationKey, executeApiCall);
    } else {
      return await executeApiCall();
    }
  }

  return {
    data,
    error,
    isLoading,
    loadingStates,
    withLoading,
    isLoadingOperation,
    startLoading,
    stopLoading,
    handleError,
    callApi,
  };
}
