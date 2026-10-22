import { ref, computed } from "vue";

export function useLoadingState() {
  // Counted per key so overlapping or nested operations on one key stay loading until the last one ends.
  const pendingCounts = ref<Record<string, number>>({});
  const loadingStates = computed<Record<string, boolean>>(() =>
    Object.fromEntries(Object.keys(pendingCounts.value).map((key) => [key, true]))
  );
  const isLoading = computed(() => Object.keys(pendingCounts.value).length > 0);

  function startLoading(key: string): void {
    pendingCounts.value[key] = (pendingCounts.value[key] ?? 0) + 1;
  }

  function stopLoading(key: string): void {
    const remaining = (pendingCounts.value[key] ?? 0) - 1;
    if (remaining > 0) {
      pendingCounts.value[key] = remaining;
    } else {
      delete pendingCounts.value[key];
    }
  }

  function isLoadingOperation(key: string): boolean {
    return key in pendingCounts.value;
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
