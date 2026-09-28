// Предупреждения о подписках, у которых заканчивается срок или трафик: точки в меню «Подписки».

import { ref } from 'vue'

export type AlertLevel = 'warn' | 'bad'

export interface SubscriptionAlert {
  source: 'happ' | 'amnezia'
  name: string
  level: AlertLevel
  message: string
}

export const subscriptionAlerts = ref<SubscriptionAlert[]>([])

// refreshSubscriptionAlerts запрашивает предупреждения у сервера.
export async function refreshSubscriptionAlerts(): Promise<void> {
  try {
    const response = await fetch('/api/subscriptions/alerts')

    if (response.ok) {
      const data = await response.json()

      subscriptionAlerts.value = data.alerts ?? []
    }
  } catch {
    // Сервер недоступен — оставляем прежние предупреждения.
  }
}

// alertsOf возвращает предупреждения источника (все, если source не задан).
export function alertsOf(source?: SubscriptionAlert['source']): SubscriptionAlert[] {
  return subscriptionAlerts.value.filter((alert) => !source || alert.source === source)
}

// alertLevel возвращает самый высокий уровень предупреждений.
export function alertLevel(alerts: SubscriptionAlert[]): AlertLevel | '' {
  if (alerts.some((alert) => alert.level === 'bad')) {
    return 'bad'
  }

  return alerts.length > 0 ? 'warn' : ''
}

// startSubscriptionAlertsPolling обновляет предупреждения раз в 10 минут: сроки меняются медленно.
export function startSubscriptionAlertsPolling(intervalMs = 600000): void {
  void refreshSubscriptionAlerts()
  setInterval(() => void refreshSubscriptionAlerts(), intervalMs)
}
