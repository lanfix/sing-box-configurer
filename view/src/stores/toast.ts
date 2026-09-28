// Сообщение над содержимым страницы (успех, ошибка, предупреждение).

import { ref } from 'vue'

export type ToastType = 'success' | 'error' | 'warning'

export interface Toast {
  text: string
  type: ToastType
}

export const toast = ref<Toast | null>(null)

let timer: ReturnType<typeof setTimeout> | null = null

// showMessage показывает сообщение на несколько секунд (ошибки — дольше).
export function showMessage(text: string, type: ToastType = 'success'): void {
  toast.value = { text, type }

  if (timer) {
    clearTimeout(timer)
  }

  timer = setTimeout(() => {
    toast.value = null
  }, type === 'success' ? 5000 : 10000)
}

// showError показывает текст ошибки.
export function showError(error: unknown, prefix = 'Ошибка'): void {
  const message = error instanceof Error ? error.message : String(error)

  showMessage(`${prefix}: ${message}`, 'error')
}
