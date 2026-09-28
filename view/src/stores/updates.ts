// Проверка и выполнение обновлений конфигуратора. Состояние общее для меню и страницы «Управление».

import { computed, reactive } from 'vue'

import { get, post } from '../api/client'
import type { UpdateCheck, UpdateStatus } from '../api/types'
import { showMessage } from './toast'

export const updates = reactive({
  check: null as UpdateCheck | null,
  status: null as UpdateStatus | null,
  checking: false,
  polling: null as ReturnType<typeof setInterval> | null,
  // Сервис недоступен: конфигуратор перезапускается во время обновления.
  serviceDown: false,
  // Версия, с которой загружена страница: после обновления страница перезагружается.
  loadedVersion: '',
})

// inProgress — обновление выполняется.
export const updateInProgress = computed(() => Boolean(updates.polling) || Boolean(updates.status?.running))

// updateAvailable — есть версии новее текущей (бейдж «Управление» в меню).
export const updateAvailable = computed(() => (updates.check?.available?.length ?? 0) > 0 && !updateInProgress.value)

// checkUpdates запрашивает доступные версии. force сбрасывает кэш сервера.
export async function checkUpdates(force: boolean): Promise<void> {
  updates.checking = true

  try {
    updates.check = await get<UpdateCheck>(`/api/update/check${force ? '?force=true' : ''}`)

    if (!updates.loadedVersion) {
      updates.loadedVersion = updates.check.current_version
    }
  } catch (error) {
    updates.check = {
      current_version: updates.check?.current_version ?? '',
      latest_version: '',
      available: [],
      error: error instanceof Error ? error.message : String(error),
    }
  } finally {
    updates.checking = false
  }
}

// refreshUpdateStatus запрашивает состояние последнего обновления.
export async function refreshUpdateStatus(): Promise<void> {
  try {
    updates.status = await get<UpdateStatus>('/api/update/status')
    updates.serviceDown = false
  } catch {
    // Во время обновления конфигуратор перезапускается — это ожидаемо.
    if (updates.polling) {
      updates.serviceDown = true
    }
  }
}

// startUpdatePolling следит за выполнением обновления.
export function startUpdatePolling(): void {
  if (updates.polling) {
    return
  }

  updates.polling = setInterval(async () => {
    await refreshUpdateStatus()

    const status = updates.status

    if (!updates.serviceDown && status?.exists && !status.running && status.result) {
      stopUpdatePolling()
      await onUpdateFinished(status)
    }
  }, 2000)
}

// stopUpdatePolling прекращает слежение за обновлением.
function stopUpdatePolling(): void {
  if (updates.polling) {
    clearInterval(updates.polling)
  }

  updates.polling = null
}

// onUpdateFinished сообщает результат и перезагружает страницу, если версия сменилась.
async function onUpdateFinished(status: UpdateStatus): Promise<void> {
  if (status.result === 'succeeded') {
    showMessage(`Обновлено до ${status.to_version}`, 'success')
  } else if (status.result === 'rolled_back') {
    showMessage('Обновление не удалось, выполнен откат на прежнюю версию', 'error')
  } else {
    showMessage('Обновление завершилось ошибкой', 'error')
  }

  await checkUpdates(true)

  // Новая версия отдает новый интерфейс.
  if (updates.check && updates.check.current_version !== updates.loadedVersion) {
    setTimeout(() => window.location.reload(), 3000)
  }
}

// startUpdate запускает обновление до версии version.
export async function startUpdate(version: string): Promise<void> {
  updates.status = await post<UpdateStatus>('/api/update/start', { version })
  showMessage(`Обновление до ${version} запущено`, 'success')
  startUpdatePolling()
}

// initUpdates проверяет обновления при загрузке приложения и продолжает следить за начатым обновлением.
export async function initUpdates(): Promise<void> {
  await checkUpdates(false)
  await refreshUpdateStatus()

  if (updates.status?.running) {
    startUpdatePolling()
  }
}
