// Всплывающее уведомление в углу экрана (успех, ошибка, предупреждение).

import { ref } from 'vue'

export type ToastType = 'success' | 'error' | 'warning'

export interface Toast {
  id: number
  text: string
  type: ToastType
}

export const toast = ref<Toast | null>(null)

let timer: ReturnType<typeof setTimeout> | null = null
let sequence = 0

// showMessage показывает уведомление на несколько секунд (ошибки — дольше).
export function showMessage(text: string, type: ToastType = 'success'): void {
  toast.value = { id: ++sequence, text, type }

  if (timer) {
    clearTimeout(timer)
  }

  timer = setTimeout(hideToast, type === 'success' ? 5000 : 12000)
}

// showError показывает текст ошибки.
export function showError(error: unknown, prefix = 'Ошибка'): void {
  const message = error instanceof Error ? error.message : String(error)

  showMessage(`${prefix}: ${message}`, 'error')
}

// hideToast скрывает уведомление.
export function hideToast(): void {
  if (timer) {
    clearTimeout(timer)
    timer = null
  }

  toast.value = null
}
