<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'

import { get, post } from '../../api/client'
import { showError, showMessage } from '../../stores/toast'

interface RestartStatus {
  enabled: boolean
  schedule: string
  timezone: string
  next_run?: string
  last_run?: string
  last_error?: string
}

// presets — частые расписания.
const presets = [
  { schedule: '0 6 * * *', label: 'Каждый день в 06:00' },
  { schedule: '0 4 * * *', label: 'Каждый день в 04:00' },
  { schedule: '0 4 * * 1', label: 'Каждый понедельник в 04:00' },
  { schedule: '0 */12 * * *', label: 'Каждые 12 часов' },
  { schedule: '0 * * * *', label: 'Каждый час' },
]

// timezones — часто используемые часовые пояса, можно ввести любой из базы IANA.
const timezones = ['UTC', 'Europe/Moscow', 'Europe/Kaliningrad', 'Europe/Samara', 'Asia/Yekaterinburg', 'Asia/Novosibirsk', 'Asia/Vladivostok']

const status = ref<RestartStatus | null>(null)
const saving = ref(false)

const form = reactive({
  enabled: true,
  schedule: '',
  timezone: 'UTC',
})

// presetValue — выбранный пресет или пустая строка для своего расписания.
const presetValue = computed({
  get: () => (presets.some((preset) => preset.schedule === form.schedule) ? form.schedule : ''),
  set: (value: string) => {
    if (value) {
      form.schedule = value
    }
  },
})

// load загружает настройки и состояние задачи.
async function load(): Promise<void> {
  try {
    status.value = await get<RestartStatus>('/api/settings/restart')
    form.enabled = status.value.enabled
    form.schedule = status.value.schedule
    form.timezone = status.value.timezone
  } catch (error) {
    showError(error, 'Ошибка загрузки расписания перезагрузки')
  }
}

// save сохраняет расписание: оно действует сразу, без применения конфига.
async function save(): Promise<void> {
  saving.value = true

  try {
    await post('/api/settings/restart', form)
    showMessage('Расписание перезагрузки сохранено')
    await load()
  } catch (error) {
    showError(error)
  } finally {
    saving.value = false
  }
}

// formatRun форматирует время запуска в часовом поясе расписания и в локальном времени браузера.
function formatRun(value?: string): string {
  if (!value) {
    return '—'
  }

  const date = new Date(value)
  const local = date.toLocaleString('ru-RU')

  try {
    const zoned = date.toLocaleString('ru-RU', { timeZone: status.value?.timezone })

    return zoned === local ? local : `${zoned} (${status.value?.timezone}), у вас ${local}`
  } catch {
    return local
  }
}

onMounted(load)
</script>

<template>
  <div class="add-form-card">
    <div class="form-header" style="margin-bottom: 4px;">Плановая перезагрузка sing-box</div>
    <p class="card-hint" style="margin: 0 0 16px;">
      Конфигуратор перезапускает sing-box по расписанию через docker-controller.
      Расписание действует сразу после сохранения.
    </p>

    <form @submit.prevent="save">
      <div class="form-group" style="margin-bottom: 16px;">
        <label class="check-label">
          <input v-model="form.enabled" type="checkbox">
          <span>Перезагружать по расписанию</span>
        </label>
      </div>

      <div class="form-row">
        <div class="form-group">
          <label class="form-label" for="restartPreset">Расписание</label>
          <select id="restartPreset" v-model="presetValue" class="form-select" :disabled="!form.enabled">
            <option v-for="preset in presets" :key="preset.schedule" :value="preset.schedule">{{ preset.label }}</option>
            <option value="">Свое (cron)</option>
          </select>
        </div>
        <div class="form-group">
          <label class="form-label" for="restartSchedule">Cron-выражение</label>
          <input
            id="restartSchedule"
            v-model="form.schedule"
            class="form-input"
            type="text"
            placeholder="0 6 * * *"
            required
            :disabled="!form.enabled"
          >
          <p class="form-help">Минута, час, день месяца, месяц, день недели (0 — воскресенье).</p>
        </div>
        <div class="form-group">
          <label class="form-label" for="restartTimezone">Часовой пояс</label>
          <input
            id="restartTimezone"
            v-model="form.timezone"
            class="form-input"
            type="text"
            list="restartTimezones"
            placeholder="UTC"
            :disabled="!form.enabled"
          >
          <datalist id="restartTimezones">
            <option v-for="zone in timezones" :key="zone" :value="zone" />
          </datalist>
        </div>
      </div>

      <dl v-if="status" class="meta-list" style="margin-bottom: 16px;">
        <div class="meta-row">
          <dt>Следующая перезагрузка</dt>
          <dd>{{ status.enabled ? formatRun(status.next_run) : 'выключена' }}</dd>
        </div>
        <div class="meta-row">
          <dt>Последняя перезагрузка</dt>
          <dd>
            {{ status.last_run ? formatRun(status.last_run) : 'не выполнялась с запуска конфигуратора' }}
            <span v-if="status.last_error" class="status-badge status-error" :title="status.last_error">ошибка</span>
          </dd>
        </div>
      </dl>
      <div v-if="status?.last_error" class="happ-note is-bad">{{ status.last_error }}</div>

      <div class="form-actions">
        <button type="submit" class="btn btn-primary" :disabled="saving">Сохранить</button>
      </div>
    </form>
  </div>
</template>
