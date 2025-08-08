import { ref, computed } from 'vue'
import { useToast } from '@/composables/useToast'
import { TOAST_DURATION } from '@/lib/constants'
import { useSourcesStore } from '@/stores/sources'

export interface ConnectionInfo {
  host: string;
  username: string;
  password: string;
  database: string;
  table_name: string;
  timestamp_field?: string;
  severity_field?: string;
}

export interface ValidationResult {
  success: boolean;
  message: string;
}

export function useConnectionValidation() {
  const sourcesStore = useSourcesStore()
  const { toast } = useToast()
  
  const isValidating = ref(false)
  const validationResult = ref<ValidationResult | null>(null)
  
  // Compute validation status from store
  const isValidated = computed(() => {
    const connection = currentConnection.value
    if (!connection?.host || !connection?.database || !connection?.table_name) return false
    
    return sourcesStore.isConnectionValidated(
      connection.host,
      connection.database,
      connection.table_name
    )
  })
  
  // Track current connection info
  const currentConnection = ref<ConnectionInfo | null>(null)
  
  const validateConnection = async (connectionInfo: ConnectionInfo) => {
    if (isValidating.value) return { success: false }
    
    // Update current connection
    currentConnection.value = { ...connectionInfo }
    
    if (!connectionInfo.host || !connectionInfo.database || !connectionInfo.table_name) {
      toast({
        title: 'Error',
        description: 'Please fill in host, database and table name fields',
        variant: 'destructive',
        duration: TOAST_DURATION.ERROR,
      })
      return { success: false }
    }
    
    isValidating.value = true
    validationResult.value = null
    
    try {
      const result = await sourcesStore.validateSourceConnection({
        host: connectionInfo.host,
        username: connectionInfo.username || '',
        password: connectionInfo.password || '',
        database: connectionInfo.database,
        table_name: connectionInfo.table_name,
        timestamp_field: connectionInfo.timestamp_field,
        severity_field: connectionInfo.severity_field,
      })
      
      if (result.success) {
        validationResult.value = {
          success: true,
          message: result.data?.message || 'Connection validated successfully'
        }
        
        return { success: true, data: result.data }
      } else {
        validationResult.value = {
          success: false,
          message: result.error || 'Validation failed'
        }
        
        return { success: false, error: result.error }
      }
    } catch (error) {
      const errorMessage = error instanceof Error ? error.message : 'An unknown error occurred'
      validationResult.value = {
        success: false,
        message: errorMessage
      }
      
      return { success: false, error: errorMessage }
    } finally {
      isValidating.value = false
    }
  }
  
  return {
    isValidating,
    validationResult,
    isValidated,
    currentConnection,
    validateConnection,
  }
}
