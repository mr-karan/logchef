import { computed } from 'vue'
import { useMetaStore } from '@/stores/meta'

const ALL_LIMIT_OPTIONS = [100, 500, 1000, 2000, 5000, 10000, 50000, 100000, 200000, 500000, 1000000]

export function useLimitOptions() {
  const metaStore = useMetaStore()
  
  const limitOptions = computed(() => {
    const maxLimit = metaStore.maxQueryLimit
    return ALL_LIMIT_OPTIONS.filter(limit => limit <= maxLimit)
  })
  
  return {
    limitOptions
  }
}
