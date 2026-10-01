<script setup lang="ts">
// Системные настройки: параметры sing-box, Clash API, подписки Happ и плановая перезагрузка.
import { computed, onMounted, reactive, ref } from 'vue'

import { get, post } from '../../api/client'
import type { Settings } from '../../api/types'
import SvgIcon from '../../components/SvgIcon.vue'
import ChipsInput from '../../components/ui/ChipsInput.vue'
import IconButton from '../../components/ui/IconButton.vue'
import SaveBar from '../../components/ui/SaveBar.vue'
import SettingRow from '../../components/ui/SettingRow.vue'
import ToggleSwitch from '../../components/ui/ToggleSwitch.vue'
import { useLeaveGuard, useSavedState } from '../../composables/useSavedState'
import { icons } from '../../icons'
import { confirmAction } from '../../stores/confirm'
import { showError, showMessage } from '../../stores/toast'

interface RestartStatus {
  enabled: boolean
  schedule: string
  timezone: string
  next_run?: string
  last_run?: string
  last_error?: string
}

// logLevels — уровни логов sing-box с пояснениями.
const logLevels = [
  { value: 'trace', label: 'trace — все подряд, очень подробно' },
  { value: 'debug', label: 'debug — отладочные сообщения' },
  { value: 'info', label: 'info — соединения и события' },
  { value: 'warn', label: 'warn — предупреждения и ошибки' },
  { value: 'error', label: 'error — только ошибки' },
  { value: 'fatal', label: 'fatal — только критические ошибки' },
  { value: 'panic', label: 'panic — только аварийные остановки' },
]

// presets — частые расписания перезагрузки.
const presets = [
  { schedule: '0 4 * * *', label: 'Каждый день в 04:00' },
  { schedule: '0 6 * * *', label: 'Каждый день в 06:00' },
  { schedule: '0 4 * * 1', label: 'Каждый понедельник в 04:00' },
  { schedule: '0 */12 * * *', label: 'Каждые 12 часов' },
  { schedule: '0 */6 * * *', label: 'Каждые 6 часов' },
  { schedule: '0 * * * *', label: 'Каждый час' },
]

// customPreset — значение пункта «свое расписание» в списке.
const customPreset = 'custom'

// timezones — часто используемые часовые пояса, можно ввести любой из базы IANA.
const timezones = ['UTC', 'Europe/Moscow', 'Europe/Kaliningrad', 'Europe/Samara', 'Asia/Yekaterinburg', 'Asia/Omsk', 'Asia/Novosibirsk', 'Asia/Krasnoyarsk', 'Asia/Irkutsk', 'Asia/Vladivostok']

// browserTimezone — часовой пояс браузера для быстрой подстановки.
const browserTimezone = Intl.DateTimeFormat().resolvedOptions().timeZone

const secret = ref('')
const showSecret = ref(false)
const savingGeneral = ref(false)
const savingRestart = ref(false)
const savingHapp = ref(false)
const restarting = ref(false)

const happ = reactive({
  auto_apply: true,
})

const general = reactive({
  log_level: 'warn',
  allow_origins: [] as string[],
})

const restart = reactive({
  enabled: true,
  schedule: '',
  timezone: 'UTC',
})

const restartStatus = ref<RestartStatus | null>(null)

// customSchedule — выбран ввод своего cron-выражения.
const customSchedule = ref(false)

const generalState = useSavedState(general)
const restartState = useSavedState(restart)
const happState = useSavedState(happ)

useLeaveGuard(() => generalState.dirty.value || restartState.dirty.value || happState.dirty.value)

// presetValue — выбранный пресет расписания или пункт «свое расписание».
const presetValue = computed({
  get: () => (!customSchedule.value && presets.some((preset) => preset.schedule === restart.schedule) ? restart.schedule : customPreset),
  set: (value: string) => {
    customSchedule.value = value === customPreset

    if (!customSchedule.value) {
      restart.schedule = value
    }
  },
})

// loadGeneral загружает общие настройки.
async function loadGeneral(): Promise<void> {
  try {
    const data = await get<Settings>('/api/settings')

    secret.value = data.clash_api.secret
    general.log_level = data.log_level
    general.allow_origins = [...data.clash_api.allow_origins]
    generalState.markSaved()

    // Несохраненный переключатель подписок не сбрасывается при сохранении соседней карточки.
    if (!happState.dirty.value) {
      happ.auto_apply = data.happ.auto_apply
      happState.markSaved()
    }
  } catch (error) {
    showError(error, 'Ошибка загрузки настроек')
  }
}

// loadRestart загружает расписание перезагрузки и состояние задачи.
async function loadRestart(): Promise<void> {
  try {
    const data = await get<RestartStatus>('/api/settings/restart')

    restartStatus.value = data
    restart.enabled = data.enabled
    restart.schedule = data.schedule
    restart.timezone = data.timezone
    customSchedule.value = !presets.some((preset) => preset.schedule === data.schedule)
    restartState.markSaved()
  } catch (error) {
    showError(error, 'Ошибка загрузки расписания перезагрузки')
  }
}

// saveGeneral сохраняет уровень логов и CORS-origin-ы.
async function saveGeneral(): Promise<void> {
  savingGeneral.value = true

  try {
    await post('/api/settings', general)
    showMessage('Настройки сохранены. Они вступят в силу после применения конфига.')
    await loadGeneral()
  } catch (error) {
    showError(error)
  } finally {
    savingGeneral.value = false
  }
}

// saveRestart сохраняет расписание: оно действует сразу, без применения конфига.
async function saveRestart(): Promise<void> {
  savingRestart.value = true

  try {
    await post('/api/settings/restart', restart)
    showMessage('Расписание перезагрузки сохранено')
    await loadRestart()
  } catch (error) {
    showError(error)
  } finally {
    savingRestart.value = false
  }
}

// saveHapp сохраняет применение обновлений подписок Happ: действует сразу.
async function saveHapp(): Promise<void> {
  savingHapp.value = true

  try {
    await post('/api/settings/happ', happ)
    showMessage('Настройки подписок сохранены')
    happState.markSaved()
  } catch (error) {
    showError(error)
  } finally {
    savingHapp.value = false
  }
}

// resetRestart отменяет изменения расписания.
function resetRestart(): void {
  restartState.reset()
  customSchedule.value = !presets.some((preset) => preset.schedule === restart.schedule)
}

// regenerate создает новый токен Clash API.
async function regenerate(): Promise<void> {
  const confirmed = await confirmAction({
    title: 'Создать новый токен Clash API?',
    message: 'Старый токен перестанет работать после применения конфига: внешние панели (yacd и т.п.) нужно будет перенастроить.',
    confirmText: 'Создать токен',
  })

  if (!confirmed) {
    return
  }

  try {
    const result = await post('/api/settings/regenerate-secret')

    showMessage(result.message ?? 'Токен обновлен')
    await loadGeneral()
  } catch (error) {
    showError(error)
  }
}

// copySecret копирует токен Clash API.
async function copySecret(): Promise<void> {
  try {
    await navigator.clipboard.writeText(secret.value)
    showMessage('Токен скопирован')
  } catch {
    showMessage('Не удалось скопировать токен: браузер запретил доступ к буферу обмена', 'error')
  }
}

// restartNow перезапускает sing-box с рабочим конфигом.
async function restartNow(): Promise<void> {
  const confirmed = await confirmAction({
    title: 'Перезагрузить sing-box?',
    message: 'sing-box перезапустится с текущим рабочим конфигом. Открытые соединения будут разорваны.',
    confirmText: 'Перезагрузить',
    danger: true,
  })

  if (!confirmed) {
    return
  }

  restarting.value = true

  try {
    const result = await post('/api/control/reload')

    showMessage(result.message ?? 'sing-box перезапущен')
    await loadRestart()
  } catch (error) {
    showError(error)
  } finally {
    restarting.value = false
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
    const zoned = date.toLocaleString('ru-RU', { timeZone: restartStatus.value?.timezone })

    return zoned === local ? local : `${zoned} (${restartStatus.value?.timezone}), у вас ${local}`
  } catch {
    return local
  }
}

onMounted(() => {
  void loadGeneral()
  void loadRestart()
})
</script>

<template>
  <Teleport to="#topbar-actions" defer>
    <button class="btn btn-warning" :disabled="restarting" @click="restartNow">
      <SvgIcon class="btn-icon" :path="icons.restart" />
      {{ restarting ? 'Перезагрузка...' : 'Перезагрузить sing-box' }}
    </button>
  </Teleport>

  <div class="page-narrow">
    <form class="settings-card" @submit.prevent="saveGeneral">
      <div class="settings-card-head">
        <div class="settings-card-title">sing-box и Clash API</div>
        <p class="settings-card-description">
          Попадают в итоговый конфиг и начинают действовать после его
          <RouterLink :to="{ name: 'config' }">применения</RouterLink>.
        </p>
      </div>

      <SettingRow title="Уровень логов" input-id="logLevel" description="Насколько подробно sing-box пишет в журнал. Для постоянной работы достаточно warn: подробные уровни быстро раздувают журнал.">
        <select id="logLevel" v-model="general.log_level" class="form-select">
          <option v-for="level in logLevels" :key="level.value" :value="level.value">{{ level.label }}</option>
        </select>
      </SettingRow>

      <SettingRow title="Токен Clash API" class="is-top">
        <template #description>
          Clash API слушает порт <code>9090</code> и пускает только с заголовком <code>Authorization: Bearer &lt;токен&gt;</code>.
          Конфигуратор берет токен из рабочего конфига сам — он нужен только для внешних панелей.
        </template>
        <div class="input-group">
          <input
            class="form-input is-mono"
            :type="showSecret ? 'text' : 'password'"
            :value="secret"
            readonly
            aria-label="Токен Clash API"
          >
          <span class="input-addon secret-actions">
            <IconButton :icon="showSecret ? 'eyeOff' : 'eye'" :title="showSecret ? 'Скрыть' : 'Показать'" @click="showSecret = !showSecret" />
            <IconButton icon="copy" title="Копировать" @click="copySecret" />
            <IconButton icon="refresh" title="Создать новый токен" @click="regenerate" />
          </span>
        </div>
      </SettingRow>

      <SettingRow title="Разрешенные сайты (CORS)" input-id="allowOrigins" class="is-top">
        <template #description>
          Адреса веб-панелей (например, yacd), которым браузер разрешит обращаться к Clash API.
          <code>*</code> — любой сайт. Пусто — поведение sing-box по умолчанию. Самому конфигуратору CORS не нужен.
        </template>
        <ChipsInput v-model="general.allow_origins" input-id="allowOrigins" placeholder="http://yacd.example или *" />
        <span class="field-hint">Enter или пробел добавляют адрес.</span>
      </SettingRow>

      <SaveBar :dirty="generalState.dirty.value" :saving="savingGeneral" @reset="generalState.reset" />
    </form>

    <form class="settings-card" @submit.prevent="saveHapp">
      <div class="settings-card-head">
        <div class="settings-card-title">Подписки Happ</div>
        <p class="settings-card-description">
          Подписки обновляются в фоне с интервалом, который задает сервер подписки (не чаще раза в 10 минут),
          и по кнопке «Обновить». Настройка действует сразу после сохранения.
        </p>
      </div>

      <SettingRow title="Применять обновления сразу" class="is-top">
        <template #description>
          Изменившиеся серверы подписок переносятся и в итоговый, и в работающий конфиг — sing-box перезапускается.
          Другие неприменённые изменения при этом не применяются и ждут
          <RouterLink :to="{ name: 'config' }">применения</RouterLink>. Если выключено, обновленные серверы попадают
          только в итоговый конфиг.
        </template>
        <ToggleSwitch v-model="happ.auto_apply" :label="happ.auto_apply ? 'Включено' : 'Выключено'" />
      </SettingRow>

      <SaveBar :dirty="happState.dirty.value" :saving="savingHapp" @reset="happState.reset" />
    </form>

    <form class="settings-card" @submit.prevent="saveRestart">
      <div class="settings-card-head">
        <div class="settings-card-title">Плановая перезагрузка</div>
        <p class="settings-card-description">
          Конфигуратор перезапускает sing-box по расписанию — это освобождает память и
          сбрасывает зависшие соединения. Расписание действует сразу после сохранения.
        </p>
      </div>

      <SettingRow title="Перезагружать по расписанию">
        <ToggleSwitch v-model="restart.enabled" :label="restart.enabled ? 'Включено' : 'Выключено'" />
      </SettingRow>

      <template v-if="restart.enabled">
        <SettingRow title="Когда" input-id="restartPreset" class="is-top" description="Лучше выбрать время, когда сетью никто не пользуется.">
          <select id="restartPreset" v-model="presetValue" class="form-select">
            <option v-for="preset in presets" :key="preset.schedule" :value="preset.schedule">{{ preset.label }}</option>
            <option :value="customPreset">Свое расписание (cron)</option>
          </select>
          <template v-if="presetValue === customPreset">
            <input
              id="restartSchedule"
              v-model="restart.schedule"
              class="form-input is-mono"
              type="text"
              placeholder="0 4 * * *"
              required
              aria-label="Cron-выражение"
            >
            <span class="field-hint">Пять полей: минута, час, день месяца, месяц, день недели (0 — воскресенье). Например, <code>30 3 * * 0</code> — по воскресеньям в 03:30.</span>
          </template>
        </SettingRow>

        <SettingRow title="Часовой пояс" input-id="restartTimezone" description="В этом поясе считается время расписания.">
          <input
            id="restartTimezone"
            v-model="restart.timezone"
            class="form-input"
            type="text"
            list="restartTimezones"
            placeholder="UTC"
            autocomplete="off"
          >
          <datalist id="restartTimezones">
            <option v-for="zone in timezones" :key="zone" :value="zone" />
          </datalist>
          <button
            v-if="browserTimezone && restart.timezone !== browserTimezone"
            type="button"
            class="link-button"
            style="align-self: flex-start;"
            @click="restart.timezone = browserTimezone"
          >
            Использовать ваш пояс: {{ browserTimezone }}
          </button>
        </SettingRow>
      </template>

      <div v-if="restartStatus" class="settings-card-body restart-status">
        <dl class="meta-list">
          <div class="meta-row">
            <dt>Следующая перезагрузка</dt>
            <dd>{{ restartStatus.enabled ? formatRun(restartStatus.next_run) : 'выключена' }}</dd>
          </div>
          <div class="meta-row">
            <dt>Последняя перезагрузка</dt>
            <dd>{{ restartStatus.last_run ? formatRun(restartStatus.last_run) : 'не выполнялась с запуска конфигуратора' }}</dd>
          </div>
        </dl>
        <div v-if="restartStatus.last_error" class="callout is-bad">Последняя перезагрузка завершилась ошибкой: {{ restartStatus.last_error }}</div>
      </div>

      <SaveBar :dirty="restartState.dirty.value" :saving="savingRestart" @reset="resetRestart" />
    </form>
  </div>
</template>
