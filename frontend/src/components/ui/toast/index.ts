import type { ToastRootProps } from 'radix-vue'
import type { HTMLAttributes } from 'vue'

export { default as Toast } from './Toast.vue'
export { default as ToastAction } from './ToastAction.vue'
export { default as ToastClose } from './ToastClose.vue'
export { default as ToastDescription } from './ToastDescription.vue'
export { default as Toaster } from './Toaster.vue'
export { default as ToastProvider } from './ToastProvider.vue'
export { default as ToastTitle } from './ToastTitle.vue'
export { default as ToastViewport } from './ToastViewport.vue'
export { toast, useToast } from './use-toast'

import { cva, type VariantProps } from 'class-variance-authority'

export const toastVariants = cva(
  'group pointer-events-auto relative flex w-full items-center justify-between space-x-4 overflow-hidden rounded-md border p-4 pr-7 shadow-lg transition-all data-[swipe=cancel]:translate-x-0 data-[swipe=end]:translate-x-[--radix-toast-swipe-end-x] data-[swipe=move]:translate-x-[--radix-toast-swipe-move-x] data-[swipe=move]:transition-none data-[state=open]:animate-in data-[state=closed]:animate-out data-[swipe=end]:animate-out data-[state=closed]:fade-out-80 data-[state=closed]:slide-out-to-right-full data-[state=open]:slide-in-from-right-full',
  {
    variants: {
      variant: {
        default: 'border-l-4 bg-white dark:bg-gray-800 text-foreground shadow-md',
        destructive: 'border-l-4 border-l-destructive bg-white dark:bg-gray-800 text-gray-800 dark:text-gray-200 shadow-md',
        success: 'border-l-4 border-l-green-600 bg-white dark:bg-gray-800 text-gray-800 dark:text-gray-200 shadow-md',
        warning: 'border-l-4 border-l-orange-500 bg-white dark:bg-gray-800 text-gray-800 dark:text-gray-200 shadow-md',
        info: 'border-l-4 border-l-blue-500 bg-white dark:bg-gray-800 text-gray-800 dark:text-gray-200 shadow-md',
      },
    },
    defaultVariants: {
      variant: 'default',
    },
  },
)

type ToastVariants = VariantProps<typeof toastVariants>

export interface ToastProps extends ToastRootProps {
  class?: HTMLAttributes['class']
  variant?: ToastVariants['variant']
  onOpenChange?: ((value: boolean) => void) | undefined
}
