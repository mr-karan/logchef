import { ref, computed } from "vue";

export function useLoadingState() {
  const loadingStates = ref<Record<string, boolean>>({});
  const isLoading = computed(() => Object.values(loadingStates.value).some(state => state));

  function startLoading(key: string): void {
    loadingStates.value[key] = true;
  }

  function stopLoading(key: string): void {
    loadingStates.value[key] = false;
  }

  function isLoadingOperation(key: string): boolean {
    return !!loadingStates.value[key];
  }

  async function withLoading<T>(
    key: string,
    fn: () => Promise<T>
  ): Promise<T> {
    startLoading(key);
    try {
      return await fn();
    } finally {
      stopLoading(key);
    }
  }

  return {
    loadingStates,
    isLoading,
    startLoading,
    stopLoading,
    isLoadingOperation,
    withLoading
  };
}
