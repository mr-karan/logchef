import { ref } from "vue";
import type { ComputedRef, Ref } from "vue";
import type { APIErrorResponse } from "@/api/types";
import { useApiQuery } from "@/composables/useApiQuery";
import { useLoadingState } from "@/composables/useLoadingState";
import { hasShownErrorToast, isAbortError, showErrorToast } from "@/api/error-handler";

export interface BaseState<T> {
  data: Ref<T>;
  error: Ref<APIErrorResponse | null>;
}

export function useBaseStore<T>(initialState: T): BaseState<T> & {
  isLoading: ComputedRef<boolean>;
  loadingStates: ComputedRef<Readonly<Record<string, boolean>>>;
  withLoading: <R>(key: string, fn: () => Promise<R>) => Promise<R>;
  isLoadingOperation: (key: string) => boolean;
  startLoading: (key: string) => void;
  stopLoading: (key: string) => void;
  handleError: (error: Error | APIErrorResponse, operation: string) => { success: false, error: APIErrorResponse };
  callApi: <R>(options: {
    apiCall: () => Promise<any>;
    operationKey?: string;
    onSuccess?: (data: R | null) => void | Promise<void>;
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

  function handleError(err: Error | APIErrorResponse, operation: string): { success: false; error: APIErrorResponse } {
    // Handle canceled requests silently - don't show error or store in state
    if (isAbortError(err)) {
      const canceledError: APIErrorResponse = {
        status: 'error',
        message: 'Request canceled',
        error_type: 'CanceledError',
      };
      return { success: false as const, error: canceledError };
    }

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

    if (!hasShownErrorToast(err)) {
      showErrorToast(apiError);
    }

    return { success: false as const, error: apiError };
  }

  async function callApi<R>(options: {
    apiCall: () => Promise<any>;
    operationKey?: string;
    onSuccess?: (data: R | null) => void | Promise<void>;
    onError?: (error: APIErrorResponse) => void;
    successMessage?: string;
    errorMessage?: string;
    showToast?: boolean;
    defaultData?: R;
  }): Promise<{ success: boolean; data?: R | null; error?: APIErrorResponse }> {
    const showToast = options.showToast !== false;
    const { execute } = useApiQuery<R>();
    error.value = null;

    const executeApiCall = async (): Promise<{ success: boolean; data?: R | null; error?: APIErrorResponse }> => {
      try {
        const result = await execute(options.apiCall, {
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
        
        if (result.aborted) {
          return handleError(new DOMException("Request canceled", "AbortError"), options.operationKey || 'api');
        }
        if (result.success) {
          return { success: true, data: result.data };
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

interface LiveRequest<A, R> {
  request: string;
  args: A;
  controller: AbortController;
  replacement?: Promise<R>;
}

/**
 * Loads without a caller signal supersede the previous load in their slot.
 * Only these loads can replace an older caller's result for the same request.
 * Caller-controlled loads leave earlier loads running so their cancellation stays local.
 */
export function createLatestRequests<A extends unknown[], R>(options: {
  request: (...args: A) => string;
  slot?: (...args: NoInfer<A>) => string;
  load: (signal: AbortSignal, ...args: A) => Promise<R>;
}) {
  const live = new Map<string, LiveRequest<A, R>>();
  const pending = new Set<LiveRequest<A, R>>();

  function run(args: A, callerSignal?: AbortSignal): Promise<R> {
    if (callerSignal?.aborted) return options.load(callerSignal, ...args);

    const request = options.request(...args);
    const slot = options.slot?.(...args) ?? request;
    const current: LiveRequest<A, R> = { request, args, controller: new AbortController() };
    callerSignal?.addEventListener("abort", () => current.controller.abort(), { once: true });

    const previous = live.get(slot);
    live.set(slot, current);
    pending.add(current);
    if (!callerSignal) previous?.controller.abort();

    const result = options.load(current.controller.signal, ...args)
      .then(
        (value) => current.replacement ?? value,
        (error: unknown) => {
          if (current.replacement) return current.replacement;
          throw error;
        },
      )
      .finally(() => {
        pending.delete(current);
        if (live.get(slot) === current) live.delete(slot);
      });
    if (!callerSignal && previous?.request === request) previous.replacement = result;
    return result;
  }

  // After a mutation, a live load may hold a response from before it; restart it so its callers adopt a fresh one.
  function invalidate() {
    for (const current of [...live.values()]) void run(current.args).catch(() => undefined);
  }

  function cancelAll() {
    for (const request of pending) request.controller.abort();
    pending.clear();
    live.clear();
  }

  return { run, invalidate, cancelAll };
}
