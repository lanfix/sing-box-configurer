// Новые устройства в сети без профиля: точка у пункта меню «Устройства».

import { ref } from 'vue'

// unknownDevices — число найденных устройств, которым не задан профиль.
export const unknownDevices = ref(0)

// refreshDeviceAlerts запрашивает число новых устройств у сервера.
export async function refreshDeviceAlerts(): Promise<void> {
  try {
    const response = await fetch('/api/devices/alerts')

    if (response.ok) {
      const data = await response.json()

      unknownDevices.value = Number(data.unknown) || 0
    }
  } catch {
    // Сервер недоступен — оставляем прежнее значение.
  }
}

// startDeviceAlertsPolling обновляет число новых устройств раз в минуту: таблица соседей читается так же часто.
export function startDeviceAlertsPolling(intervalMs = 60000): void {
  void refreshDeviceAlerts()
  setInterval(() => void refreshDeviceAlerts(), intervalMs)
}
