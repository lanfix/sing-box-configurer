<script setup lang="ts">
// Обновление конфигуратора: доступные версии, список изменений, журнал последнего обновления
// и автоматическая проверка обновлений.
import { computed, onMounted, reactive, ref, watch } from 'vue'

import { get, post } from '../../api/client'
import type { UpdateSettings } from '../../api/types'
import SvgIcon from '../../components/SvgIcon.vue'
import FormField from '../../components/ui/FormField.vue'
import SaveBar from '../../components/ui/SaveBar.vue'
import SettingRow from '../../components/ui/SettingRow.vue'
import ToggleSwitch from '../../components/ui/ToggleSwitch.vue'
import { useLeaveGuard, useSavedState } from '../../composables/useSavedState'
import { icons } from '../../icons'
import { confirmAction } from '../../stores/confirm'
import { showError, showMessage } from '../../stores/toast'
import { checkUpdates, startUpdate, updateInProgress, updates } from '../../stores/updates'
import { formatAgo, formatDateTime } from '../../utils/format'

const stepTitles: Record<string, string> = {
  start: 'Запуск',
  prepare: 'Проверка',
  download: 'Загрузка новой версии',
  backup: 'Резервная копия',
  'update-configurer': 'Обновление конфигуратора',
  finish: 'Завершение установки',
  commit: 'Завершение',
  rollback: 'Откат',
  // Шаги журналов прежних версий.
  pull: 'Загрузка образов',
  'update-controller': 'Обновление docker-controller',
  'update-compose': 'Обновление compose-файла',
}

// platformTitles — способы установки.
const platformTitles: Record<string, string> = {
  docker: 'Docker',
  systemd: 'systemd',
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

// state описывает текущее состояние версий для заголовка карточки.
const state = computed(() => {
  const check = updates.check

  if (updateInProgress.value) {
    return {
      dot: 'is-warn',
      title: 'Идет обновление',
      text: updates.serviceDown ? 'Сервис перезапускается, страница обновится сама.' : `Обновление до ${updates.status?.target || updates.status?.to_version || ''}...`,
    }
  }

  if (!check) {
    return { dot: '', title: 'Проверяю версии...', text: '' }
  }

  if (check.unsupported) {
    const latest = check.latest_version ? ` Последний релиз — ${check.latest_version}.` : ''

    return { dot: '', title: 'Обновление недоступно', text: `${check.unsupported}${latest}` }
  }

  if (check.error) {
    return { dot: 'is-bad', title: 'Не удалось проверить обновления', text: check.error }
  }

  if (available.value.length > 0) {
    return { dot: 'is-warn', title: `Доступна версия ${check.latest_version}`, text: `Доступно обновлений: ${available.value.length}.` }
  }

  return { dot: 'is-good', title: 'Установлена последняя версия', text: '' }
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

  if (!selected.value) {
    return
  }

  const confirmed = await confirmAction({
    title: `Обновить ${current} → ${selected.value}?`,
    message: 'Веб-интерфейс будет недоступен несколько секунд, sing-box продолжит работать. При ошибке обновление откатится автоматически.',
    confirmText: 'Обновить',
  })

  if (!confirmed) {
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

// checkIntervals — интервалы автоматической проверки, ч.
const checkIntervals = [
  { hours: 1, label: 'Каждый час' },
  { hours: 3, label: 'Каждые 3 часа' },
  { hours: 6, label: 'Каждые 6 часов' },
  { hours: 12, label: 'Каждые 12 часов' },
  { hours: 24, label: 'Раз в сутки' },
]

const savingSettings = ref(false)

const autoCheck = reactive<UpdateSettings>({
  auto_check: false,
  interval_hours: 6,
})

const autoCheckState = useSavedState(autoCheck)

useLeaveGuard(() => autoCheckState.dirty.value)

// nextCheckText — когда сервер проверит обновления в следующий раз (для сохраненных настроек).
const nextCheckText = computed(() => {
  const next = updates.check?.next_check_at

  if (autoCheckState.dirty.value || !updates.check?.auto_check || !next) {
    return 'Обновление не устанавливается само — только по кнопке «Обновить».'
  }

  const at = new Date(next).getTime() <= Date.now() ? 'в ближайшую минуту' : formatDateTime(next)

  return `Следующая проверка — ${at}. Обновление не устанавливается само — только по кнопке «Обновить».`
})

// loadSettings загружает настройки автоматической проверки.
async function loadSettings(): Promise<void> {
  try {
    Object.assign(autoCheck, await get<UpdateSettings>('/api/update/settings'))
    autoCheckState.markSaved()
  } catch (error) {
    showError(error, 'Ошибка загрузки настроек проверки обновлений')
  }
}

// saveSettings сохраняет настройки автоматической проверки: они действуют сразу.
async function saveSettings(): Promise<void> {
  savingSettings.value = true

  try {
    await post('/api/update/settings', autoCheck)
    showMessage('Настройки проверки обновлений сохранены')
    autoCheckState.markSaved()

    // Обновляем время следующей проверки: результат берется из кэша сервера.
    await checkUpdates(false)
  } catch (error) {
    showError(error)
  } finally {
    savingSettings.value = false
  }
}

onMounted(() => void loadSettings())

// stepLevel возвращает класс строки журнала.
function stepLevel(level: string): string {
  if (level === 'ERROR') {
    return 'is-bad'
  }

  return level === 'WARN' ? 'is-warn' : ''
}
</script>

<template>
  <Teleport to="#topbar-actions" defer>
    <button class="btn btn-secondary" :disabled="updates.checking || updateInProgress" @click="checkUpdates(true)">
      <SvgIcon class="btn-icon" :path="icons.refresh" />
      {{ updates.checking ? 'Проверка...' : 'Проверить обновления' }}
    </button>
  </Teleport>

  <div class="page-narrow">
    <div class="settings-card has-progress" :class="{ 'is-loading': updates.checking || updateInProgress }">
      <div class="progress-line"></div>
      <div class="settings-card-body">
        <div class="stat-row update-stats">
          <div class="stat-tile">
            <div class="stat-label">Установлена</div>
            <div class="stat-value">{{ updates.check?.current_version || '—' }}</div>
            <div class="stat-foot">
              <template v-if="updates.check?.checked_at">Проверено {{ formatAgo(updates.check.checked_at) }}</template>
              <template v-if="updates.check?.platform"> · {{ platformTitles[updates.check.platform] ?? updates.check.platform }}</template>
            </div>
          </div>
          <div class="stat-tile">
            <div class="stat-label">Состояние</div>
            <div class="update-state">
              <span class="stat-dot" :class="state.dot"></span>
              <span>{{ state.title }}</span>
            </div>
            <div class="stat-foot">{{ state.text }}</div>
          </div>
        </div>

        <template v-if="available.length > 0 && !updateInProgress">
          <div class="update-actions">
            <FormField label="Версия" input-id="updateVersion">
              <select id="updateVersion" v-model="selected" class="form-select">
                <option v-for="(release, index) in available" :key="release.version" :value="release.version">
                  {{ release.version }}{{ index === 0 ? ' — последняя' : '' }}
                </option>
              </select>
            </FormField>
            <button class="btn btn-warning" :disabled="starting" @click="update">
              <SvgIcon class="btn-icon" :path="icons.update" />
              Обновить до {{ selected }}
            </button>
          </div>

          <div class="update-changelog-title">Что изменится</div>
          <p v-if="changelog.length === 0" class="card-hint">Список изменений не указан.</p>
          <div v-for="release in changelog" :key="release.version" class="update-changelog">
            <div class="update-changelog-version">{{ release.version }}</div>
            <pre>{{ release.changelog }}</pre>
          </div>
        </template>
      </div>
    </div>

    <form class="settings-card" @submit.prevent="saveSettings">
      <div class="settings-card-head">
        <div class="settings-card-title">Автоматическая проверка</div>
        <p class="settings-card-description">
          Конфигуратор сам проверяет новые версии, даже если панель не открыта, и пишет о новой версии в свой
          журнал. В открытой панели у пункта «Система» появляется точка без перезагрузки страницы. Настройка
          действует сразу после сохранения.
        </p>
      </div>

      <SettingRow title="Проверять обновления в фоне" :description="nextCheckText">
        <ToggleSwitch v-model="autoCheck.auto_check" :label="autoCheck.auto_check ? 'Включено' : 'Выключено'" />
      </SettingRow>

      <SettingRow v-if="autoCheck.auto_check" title="Как часто" input-id="updateCheckInterval">
        <select id="updateCheckInterval" v-model.number="autoCheck.interval_hours" class="form-select">
          <option v-for="interval in checkIntervals" :key="interval.hours" :value="interval.hours">{{ interval.label }}</option>
        </select>
      </SettingRow>

      <SaveBar :dirty="autoCheckState.dirty.value" :saving="savingSettings" @reset="autoCheckState.reset" />
    </form>

    <div v-if="updates.status?.exists" class="settings-card">
      <div class="settings-card-head">
        <div class="update-status-head">
          <span class="stat-dot" :class="statusHeader.dot"></span>
          <span class="settings-card-title">{{ statusHeader.text }}</span>
          <template v-if="updates.status.finished_at && !updateInProgress && !updates.status.finished_at.startsWith('0001')">
            <span class="happ-sep">·</span>
            <span class="card-hint">{{ new Date(updates.status.finished_at).toLocaleString('ru-RU') }}</span>
          </template>
        </div>
      </div>
      <div class="settings-card-body">
        <pre v-if="updates.status.error" class="update-error" style="margin: 0 0 12px;">{{ updates.status.error }}</pre>
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
  </div>
</template>
