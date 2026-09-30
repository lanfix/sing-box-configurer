// Вход в панель: включен ли вход по логину и паролю и выполнен ли он.
// Запросы здесь идут через fetch напрямую: ответ 401 для них — не повод уводить на страницу входа.

import { reactive } from 'vue'

interface AuthStatus {
  enabled: boolean
  authenticated: boolean
  username: string
}

export const auth = reactive({
  // loaded — статус получен с сервера (до этого роутер не пускает на страницы).
  loaded: false,
  enabled: false,
  authenticated: false,
  username: '',
})

let unauthorizedHandler: (() => void) | null = null

// onUnauthorized задает действие при ответе 401 от API (переход на страницу входа).
export function onUnauthorized(handler: () => void): void {
  unauthorizedHandler = handler
}

// handleUnauthorized вызывается API-клиентом, когда сессия недействительна.
export function handleUnauthorized(): void {
  auth.enabled = true
  auth.authenticated = false
  auth.username = ''

  unauthorizedHandler?.()
}

// loadAuthStatus запрашивает состояние входа.
export async function loadAuthStatus(): Promise<void> {
  try {
    const response = await fetch('/api/auth/status')

    if (response.ok) {
      const data = (await response.json()) as AuthStatus

      auth.enabled = data.enabled
      auth.authenticated = data.authenticated
      auth.username = data.username
    }
  } catch {
    // Сервер недоступен: страницы откроются, а запросы к API сами вернут ошибку.
  } finally {
    auth.loaded = true
  }
}

// login входит в панель. При ошибке бросает исключение с текстом сервера.
export async function login(username: string, password: string): Promise<void> {
  const response = await fetch('/api/auth/login', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ username, password }),
  })

  if (!response.ok) {
    let message = `HTTP ${response.status}`

    try {
      const data = await response.json()

      if (typeof data?.error === 'string' && data.error) {
        message = data.error
      }
    } catch {
      // Ответ не в формате JSON.
    }

    throw new Error(message)
  }

  await loadAuthStatus()
}

// logout выходит из панели.
export async function logout(): Promise<void> {
  try {
    await fetch('/api/auth/logout', { method: 'POST' })
  } finally {
    auth.authenticated = false
    auth.username = ''
  }
}

// safeRedirect возвращает адрес внутри панели, на который вернуться после входа.
export function safeRedirect(value: unknown): string {
  if (typeof value !== 'string' || !value.startsWith('/') || value.startsWith('//') || value.startsWith('/login')) {
    return '/'
  }

  return value
}
