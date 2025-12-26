import { ref } from "vue";
import type { Ref } from "vue";
import type { APIErrorResponse } from "@/api/types";
import { useApiQuery } from "@/composables/useApiQuery";
import { useLoadingState } from "@/composables/useLoadingState";
import { useToast } from "@/composables/useToast";
import { formatErrorTypeToTitle } from "@/api/error-handler";

export interface BaseState<T> {
  data: Ref<T>;
  error: Ref<APIErrorResponse | null>;
}

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

  function handleError(err: Error | APIErrorResponse, operation: string): { success: false; error: APIErrorResponse } {
    console.error(`[${operation} Error]`, err);
    
    const errorMessage = err instanceof Error ? err.message : err.message;
    const errorType = err instanceof Error ? 'UnknownError' : (err.error_type || 'UnknownError');
    const errorData = err instanceof Error ? undefined : err.data;
    
    const apiError: APIErrorResponse = {
      status: 'error',
      message: errorMessage,
      error_type: errorType,
      data: errorData,
    };
    
    error.value = apiError;
    
    const { toast } = useToast();
    toast({
      title: formatErrorTypeToTitle(errorType),
      description: errorMessage,
      variant: 'destructive',
    });
    
    return { success: false as const, error: apiError };
  }

  async function callApi<R>(options: {
    apiCall: () => Promise<any>;
    operationKey?: string;
    onSuccess?: (data: R | null) => void;
    onError?: (error: APIErrorResponse) => void;
    successMessage?: string;
    errorMessage?: string;
    showToast?: boolean;
    defaultData?: R;
  }): Promise<{ success: boolean; data?: R | null; error?: APIErrorResponse }> {
    const showToast = options.showToast !== false;
    
    const executeApiCall = async (): Promise<{ success: boolean; data?: R | null; error?: APIErrorResponse }> => {
      try {
        const result = await execute(options.apiCall, {
          successMessage: options.successMessage,
          errorMessage: options.errorMessage,
          showToast: showToast,
          defaultData: options.defaultData,
          onSuccess: options.onSuccess as ((data: unknown) => void) | undefined,
          onError: (err) => {
            error.value = err;
            options.onError?.(err);
          }
        });
        
        if (result.success) {
          return { success: true, data: result.data as R | null };
        } else {
          return { success: false, error: result.error ?? undefined };
        }
      } catch (err) {
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
