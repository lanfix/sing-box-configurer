// Признак расхождения итогового конфига с рабочим: бейдж «Конфиг» в меню.

import { ref } from 'vue'

// changed — итоговый конфиг отличается от рабочего, нужна перезагрузка sing-box.
export const configChanged = ref(false)

let timer: ReturnType<typeof setTimeout> | null = null

// loadConfigStatus запрашивает статус у сервера.
async function loadConfigStatus(): Promise<void> {
  try {
    const response = await fetch('/api/config/status')

    if (response.ok) {
      const data = await response.json()

      configChanged.value = Boolean(data.changed)
    }
  } catch {
    // Сервер недоступен — оставляем прежнее значение.
  }
}

// refreshConfigStatus обновляет статус с небольшой задержкой, объединяя частые изменения.
export function refreshConfigStatus(): void {
  if (timer) {
    clearTimeout(timer)
  }

  timer = setTimeout(() => {
    timer = null
    void loadConfigStatus()
  }, 300)
}

// startConfigStatusPolling периодически обновляет статус: итоговый конфиг меняется и без действий
// пользователя (например, при фоновом обновлении подписок).
export function startConfigStatusPolling(intervalMs = 30000): void {
  void loadConfigStatus()
  setInterval(() => void loadConfigStatus(), intervalMs)
}
