import { ref } from 'vue'
import { useToast } from '@/components/ui/toast'
import { TOAST_DURATION } from '@/lib/constants'

export function useFormHandling<T, R = any>(
  storeAction: (payload: T) => Promise<R>,
  options?: {
    successMessage?: string;
    errorMessage?: string;
    onSuccess?: (result: R) => void;
    onError?: (error: Error) => void;
  }
) {
  const { toast } = useToast()
  const isSubmitting = ref(false)
  const formError = ref<string | null>(null)
  
  const handleSubmit = async (payload: T) => {
    if (isSubmitting.value) return { success: false }
    
    isSubmitting.value = true
    formError.value = null
    
    try {
      const result = await storeAction(payload)
      
      if (options?.successMessage) {
        toast({
          title: 'Success',
          description: options.successMessage,
          variant: 'default',
          duration: TOAST_DURATION.SUCCESS,
        })
      }
      
      if (options?.onSuccess) {
        options.onSuccess(result)
      }
      
      return { success: true, result }
    } catch (error) {
      const errorMessage = error instanceof Error ? error.message : 'An unknown error occurred'
      formError.value = errorMessage
      
      if (options?.errorMessage) {
        toast({
          title: 'Error',
          description: options.errorMessage,
          variant: 'destructive',
          duration: TOAST_DURATION.ERROR,
        })
      }
      
      if (options?.onError && error instanceof Error) {
        options.onError(error)
      }
      
      return { success: false, error: errorMessage }
    } finally {
      isSubmitting.value = false
    }
  }
  
  return {
    isSubmitting,
    formError,
    handleSubmit,
  }
}
