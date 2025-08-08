import { toast } from 'vue-sonner'

export function useToast() {
  return {
    toast: (options: {
      title?: string
      description?: string
      variant?: 'default' | 'destructive' | 'success'
      duration?: number
    }) => {
      const message = options.title || ''
      const toastOptions = {
        description: options.description,
        duration: options.duration
      }

      if (options.variant === 'destructive') {
        toast.error(message, toastOptions)
      } else if (options.variant === 'success') {
        toast.success(message, toastOptions)
      } else {
        toast(message, toastOptions)
      }
    }
  }
}