import { reactive } from 'vue'

export interface NotificationState {
  show: boolean
  message: string
  color: string
}

export function useNotification() {
  const state = reactive<NotificationState>({
    show: false,
    message: '',
    color: 'success'
  })

  function success(msg: string) {
    state.message = msg
    state.color = 'success'
    state.show = true
  }

  function error(msg: string) {
    state.message = msg
    state.color = 'error'
    state.show = true
  }

  function warning(msg: string) {
    state.message = msg
    state.color = 'warning'
    state.show = true
  }

  return {
    state,
    success,
    error,
    warning
  }
}
