// Диалог подтверждения действия вместо системного confirm().

import { ref } from 'vue'

export interface ConfirmOptions {
  title: string
  message?: string
  confirmText?: string
  // danger — необратимое действие: кнопка подтверждения красная.
  danger?: boolean
}

interface ConfirmRequest extends ConfirmOptions {
  resolve: (confirmed: boolean) => void
}

export const confirmRequest = ref<ConfirmRequest | null>(null)

// confirmAction показывает диалог и возвращает true, если пользователь подтвердил действие.
export function confirmAction(options: ConfirmOptions): Promise<boolean> {
  return new Promise((resolve) => {
    confirmRequest.value?.resolve(false)
    confirmRequest.value = { ...options, resolve }
  })
}

// resolveConfirm закрывает диалог с ответом пользователя.
export function resolveConfirm(confirmed: boolean): void {
  const request = confirmRequest.value

  confirmRequest.value = null
  request?.resolve(confirmed)
}
