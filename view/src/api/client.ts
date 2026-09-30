// HTTP-клиент API конфигуратора. Ошибки сервера приходят как {"error": "..."} или как текст.

import { handleUnauthorized } from '../stores/auth'
import { refreshConfigStatus } from '../stores/configStatus'

export class ApiError extends Error {
  readonly status: number

  constructor(message: string, status: number) {
    super(message)
    this.status = status
  }
}

// Извлекает текст ошибки из ответа сервера.
async function errorMessage(response: Response): Promise<string> {
  const text = await response.text()

  try {
    const data = JSON.parse(text)

    if (data && typeof data.error === 'string' && data.error) {
      return data.error
    }
  } catch {
    // Ответ не в формате JSON.
  }

  return text.trim() || `HTTP ${response.status}`
}

// Выполняет запрос и возвращает разобранный JSON ответа.
async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const response = await fetch(path, {
    method,
    headers: body === undefined ? undefined : { 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
  })

  if (!response.ok) {
    // Сессия истекла или вход включили в другой вкладке — на страницу входа.
    if (response.status === 401 && !path.startsWith('/api/auth/')) {
      handleUnauthorized()
    }

    throw new ApiError(await errorMessage(response), response.status)
  }

  const text = await response.text()

  return (text ? JSON.parse(text) : {}) as T
}

// GET-запрос.
export function get<T>(path: string): Promise<T> {
  return request<T>('GET', path)
}

// POST-запрос. Изменения данных могут поменять итоговый конфиг, поэтому после успешного
// запроса обновляется признак расхождения с рабочим конфигом.
export async function post<T = { success: boolean; message?: string }>(path: string, body: unknown = {}): Promise<T> {
  const result = await request<T>('POST', path, body)

  refreshConfigStatus()

  return result
}

// POST-запрос, который ничего не меняет (например, предпросмотр): статус конфига не обновляется.
export function postQuiet<T>(path: string, body: unknown = {}): Promise<T> {
  return request<T>('POST', path, body)
}
