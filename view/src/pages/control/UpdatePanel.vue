<script setup lang="ts">
import { computed, ref, watch } from 'vue'

import { showError } from '../../stores/toast'
import { checkUpdates, startUpdate, updateInProgress, updates } from '../../stores/updates'

const stepTitles: Record<string, string> = {
  start: 'Запуск',
  prepare: 'Проверка',
  pull: 'Загрузка образов',
  'update-controller': 'Обновление docker-controller',
  backup: 'Резервная копия',
  'update-configurer': 'Обновление конфигуратора',
  'update-compose': 'Обновление compose-файла',
  commit: 'Завершение',
  rollback: 'Откат',
}

const selected = ref('')
const starting = ref(false)

const available = computed(() => updates.check?.available ?? [])

// Выбираем последнюю версию, если выбранной больше нет в списке.
watch(available, (list) => {
  if (!list.some((release) => release.version === selected.value)) {
    selected.value = list[0]?.version ?? ''
  }
}, { immediate: true })

const summary = computed(() => {
  const check = updates.check
  const status = updates.status

  if (updateInProgress.value) {
    return updates.serviceDown ? 'Идёт обновление: сервис перезапускается...' : `Идёт обновление до ${status?.target || status?.to_version || ''}...`
  }

  if (!check) {
    return 'Проверяю версии...'
  }

  if (check.error) {
    return `Текущая версия ${check.current_version || '—'}. Не удалось проверить обновления: ${check.error}`
  }

  if (available.value.length > 0) {
    return `Текущая версия ${check.current_version}. Доступна ${check.latest_version}.`
  }

  return `Текущая версия ${check.current_version} — последняя.`
})

// changelog — изменения всех версий от выбранной до текущей.
const changelog = computed(() => {
  const index = available.value.findIndex((release) => release.version === selected.value)

  return available.value.slice(Math.max(index, 0)).filter((release) => release.changelog)
})

const statusHeader = computed(() => {
  const status = updates.status

  if (!status) {
    return { dot: '', text: '' }
  }

  if (updateInProgress.value) {
    return { dot: 'is-warn', text: `Обновление до ${status.target} выполняется` }
  }

  const results: Record<string, { dot: string; text: string }> = {
    succeeded: { dot: 'is-good', text: `Обновлено ${status.from_version} → ${status.to_version}` },
    rolled_back: { dot: 'is-warn', text: `Обновление до ${status.to_version || status.target} не удалось, выполнен откат` },
    failed: { dot: 'is-bad', text: `Обновление до ${status.to_version || status.target} завершилось ошибкой` },
  }

  return results[status.result ?? ''] ?? { dot: '', text: 'Последнее обновление' }
})

// update запускает обновление до выбранной версии.
async function update(): Promise<void> {
  const current = updates.check?.current_version ?? ''

  if (!selected.value || !confirm(`Обновить ${current} → ${selected.value}?\n\nВеб-интерфейс будет недоступен несколько секунд. sing-box продолжит работать.\nПри ошибке обновление откатится автоматически.`)) {
    return
  }

  starting.value = true

  try {
    await startUpdate(selected.value)
  } catch (error) {
    showError(error)
  } finally {
    starting.value = false
  }
}

// stepLevel возвращает класс строки журнала.
function stepLevel(level: string): string {
  if (level === 'ERROR') {
    return 'is-bad'
  }

  return level === 'WARN' ? 'is-warn' : ''
}
</script>

<template>
  <div class="add-form-card has-progress" :class="{ 'is-loading': updates.checking }">
    <div class="progress-line"></div>
    <div class="update-head">
      <div>
        <div class="form-header" style="margin-bottom: 4px;">Обновление</div>
        <p class="card-hint" style="margin: 0;">{{ summary }}</p>
      </div>
      <button class="btn btn-secondary btn-sm" :disabled="updates.checking" @click="checkUpdates(true)">Проверить</button>
    </div>

    <template v-if="available.length > 0 && !updateInProgress">
      <div class="update-actions">
        <select v-model="selected" class="form-select">
          <option v-for="(release, index) in available" :key="release.version" :value="release.version">
            {{ release.version }}{{ index === 0 ? ' (последняя)' : '' }}
          </option>
        </select>
        <button class="btn btn-warning" :disabled="starting" @click="update">Обновить</button>
      </div>
      <p v-if="changelog.length === 0" class="card-hint" style="margin: 12px 0 0;">Список изменений не указан.</p>
      <div v-for="release in changelog" :key="release.version" class="update-changelog">
        <div class="update-changelog-version">{{ release.version }}</div>
        <pre>{{ release.changelog }}</pre>
      </div>
    </template>

    <div v-if="updates.status?.exists" class="update-status">
      <div class="update-status-head">
        <span class="stat-dot" :class="statusHeader.dot"></span>
        <span>{{ statusHeader.text }}</span>
        <template v-if="updates.status.finished_at && !updateInProgress && !updates.status.finished_at.startsWith('0001')">
          <span class="happ-sep">·</span>
          <span class="card-hint">{{ new Date(updates.status.finished_at).toLocaleString('ru-RU') }}</span>
        </template>
      </div>
      <pre v-if="updates.status.error" class="update-error">{{ updates.status.error }}</pre>
      <details class="happ-servers" :open="updateInProgress">
        <summary>Журнал обновления</summary>
        <div class="update-steps">
          <span v-if="!updates.status.steps?.length" class="card-hint">Пока пусто</span>
          <div v-for="(step, index) in updates.status.steps ?? []" :key="index" class="update-step" :class="stepLevel(step.level)">
            <span class="update-step-time">{{ step.time ? new Date(step.time).toLocaleTimeString('ru-RU') : '' }}</span>
            <span class="update-step-name">{{ stepTitles[step.step] || step.step }}</span>
            <span class="update-step-msg">
              {{ step.message }}
              <template v-if="step.error"><br><span class="update-step-error">{{ step.error }}</span></template>
            </span>
          </div>
        </div>
      </details>
    </div>
  </div>
</template>
