<script setup lang="ts">
// Журналы sing-box и конфигуратора: логи контейнеров в docker, journald в systemd.
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'

import { get } from '../../api/client'
import type { LogSource, LogsResponse, Settings } from '../../api/types'
import SvgIcon from '../../components/SvgIcon.vue'
import FormField from '../../components/ui/FormField.vue'
import SegmentedControl from '../../components/ui/SegmentedControl.vue'
import ToggleSwitch from '../../components/ui/ToggleSwitch.vue'
import { icons } from '../../icons'
import { showError, showMessage } from '../../stores/toast'

type Level = 'error' | 'warn' | 'info' | 'debug'
type LevelFilter = 'all' | 'warn' | 'error'

// refreshInterval — период автообновления, мс.
const refreshInterval = 3000

const sources: { value: LogSource; label: string }[] = [
  { value: 'sing-box', label: 'sing-box' },
  { value: 'configurer', label: 'Конфигуратор' },
]

const levelFilters: { value: LevelFilter; label: string }[] = [
  { value: 'all', label: 'Все' },
  { value: 'warn', label: 'Предупреждения и ошибки' },
  { value: 'error', label: 'Только ошибки' },
]

const lineOptions = [200, 500, 1000, 2000, 5000]

const source = ref<LogSource>('sing-box')
const lines = ref(500)
const levelFilter = ref<LevelFilter>('all')
const search = ref('')
const autoRefresh = ref(true)
const follow = ref(true)
const text = ref('')
const platform = ref('')
const error = ref('')
const loading = ref(false)
const logLevel = ref('')
const viewer = ref<HTMLElement | null>(null)

let timer: ReturnType<typeof setInterval> | null = null

// ansiRe — escape-последовательности цветов терминала.
// eslint-disable-next-line no-control-regex
const ansiRe = /\x1b\[[0-9;]*m/g

// levelOf определяет уровень строки: sing-box пишет уровень словом (INFO, WARN, ERROR), конфигуратор —
// в тексте сообщения (Warning:, Error, failed).
function levelOf(line: string): Level {
  const token = line.match(/\b(TRACE|DEBUG|INFO|WARN|ERROR|FATAL|PANIC)\b/)?.[1]

  if (token) {
    const levels: Record<string, Level> = { TRACE: 'debug', DEBUG: 'debug', INFO: 'info', WARN: 'warn', ERROR: 'error', FATAL: 'error', PANIC: 'error' }

    return levels[token]
  }

  if (/\bwarning\b/i.test(line)) {
    return 'warn'
  }

  return /\b(error|failed|fatal|panic)\b/i.test(line) ? 'error' : 'info'
}

const parsed = computed(() => text.value
  .replace(ansiRe, '')
  .split('\n')
  .filter((line) => line.trim() !== '')
  .map((line, index) => ({ index, line, level: levelOf(line) })))

const visible = computed(() => {
  const query = search.value.trim().toLowerCase()

  return parsed.value.filter((item) => {
    if (levelFilter.value === 'error' && item.level !== 'error') {
      return false
    }

    if (levelFilter.value === 'warn' && item.level !== 'error' && item.level !== 'warn') {
      return false
    }

    return !query || item.line.toLowerCase().includes(query)
  })
})

const counts = computed(() => ({
  error: parsed.value.filter((item) => item.level === 'error').length,
  warn: parsed.value.filter((item) => item.level === 'warn').length,
}))

// sourceHint — откуда читается журнал.
const sourceHint = computed(() => {
  if (platform.value === 'docker') {
    return source.value === 'sing-box' ? 'Логи контейнера sing-box (docker logs).' : 'Логи контейнера конфигуратора (docker logs).'
  }

  if (platform.value === 'systemd') {
    return source.value === 'sing-box' ? 'Журнал службы sing-box (journalctl).' : 'Журнал службы конфигуратора (journalctl).'
  }

  return ''
})

// load читает журнал. При автообновлении ошибки показываются в странице, а не всплывающими сообщениями.
async function load(): Promise<void> {
  if (loading.value) {
    return
  }

  loading.value = true

  try {
    const data = await get<LogsResponse>(`/api/logs?source=${encodeURIComponent(source.value)}&lines=${lines.value}`)
    const atBottom = isAtBottom()

    text.value = data.text ?? ''
    platform.value = data.platform ?? ''
    error.value = ''

    if (follow.value && atBottom) {
      await nextTick()
      scrollToBottom()
    }
  } catch (err) {
    error.value = err instanceof Error ? err.message : String(err)
  } finally {
    loading.value = false
  }
}

// isAtBottom проверяет, что журнал прокручен до конца: тогда новые строки прокручиваются следом.
function isAtBottom(): boolean {
  const element = viewer.value

  return !element || element.scrollHeight - element.scrollTop - element.clientHeight < 40
}

// scrollToBottom прокручивает журнал к последним строкам.
function scrollToBottom(): void {
  if (viewer.value) {
    viewer.value.scrollTop = viewer.value.scrollHeight
  }
}

// download сохраняет показанные строки в файл.
function download(): void {
  const content = visible.value.map((item) => item.line).join('\n') + '\n'
  const link = document.createElement('a')
  const stamp = new Date().toISOString().replace(/[:.]/g, '-').slice(0, 19)

  link.href = URL.createObjectURL(new Blob([content], { type: 'text/plain;charset=utf-8' }))
  link.download = `${source.value}-${stamp}.log`
  link.click()
  URL.revokeObjectURL(link.href)
}

// copy копирует показанные строки в буфер обмена.
async function copy(): Promise<void> {
  try {
    await navigator.clipboard.writeText(visible.value.map((item) => item.line).join('\n'))
    showMessage(`Скопировано строк: ${visible.value.length}`)
  } catch (err) {
    showError(err, 'Не удалось скопировать')
  }
}

// restartTimer перезапускает автообновление.
function restartTimer(): void {
  if (timer) {
    clearInterval(timer)
    timer = null
  }

  if (autoRefresh.value) {
    timer = setInterval(() => void load(), refreshInterval)
  }
}

watch([source, lines], async () => {
  text.value = ''
  await load()
  await nextTick()
  scrollToBottom()
})

watch(autoRefresh, restartTimer)

watch(follow, async (value) => {
  if (value) {
    await nextTick()
    scrollToBottom()
  }
})

onMounted(async () => {
  restartTimer()
  await load()
  await nextTick()
  scrollToBottom()

  try {
    logLevel.value = (await get<Settings>('/api/settings')).log_level
  } catch {
    // Уровень логов — только подсказка.
  }
})

onBeforeUnmount(() => {
  if (timer) {
    clearInterval(timer)
  }
})
</script>

<template>
  <Teleport to="#topbar-actions" defer>
    <button class="btn btn-secondary" :disabled="loading" @click="load">
      <SvgIcon class="btn-icon" :path="icons.refresh" />
      Обновить
    </button>
  </Teleport>

  <div class="add-form-card logs-controls">
    <div class="logs-controls-row">
      <FormField label="Журнал">
        <SegmentedControl v-model="source" :options="sources" aria-label="Журнал" />
      </FormField>
      <FormField label="Уровень">
        <SegmentedControl v-model="levelFilter" :options="levelFilters" aria-label="Уровень" />
      </FormField>
      <FormField label="Строк" input-id="logLines">
        <select id="logLines" v-model.number="lines" class="form-select">
          <option v-for="option in lineOptions" :key="option" :value="option">последние {{ option }}</option>
        </select>
      </FormField>
      <FormField label="Поиск" input-id="logSearch" class="logs-search">
        <div class="search-input">
          <SvgIcon :path="icons.search" />
          <input id="logSearch" v-model="search" class="form-input" type="search" placeholder="Текст в строке">
        </div>
      </FormField>
    </div>
    <div class="logs-controls-row">
      <ToggleSwitch v-model="autoRefresh" :label="`Обновлять каждые ${refreshInterval / 1000} с`" />
      <ToggleSwitch v-model="follow" label="Прокручивать к новым строкам" />
      <span class="logs-spacer"></span>
      <button type="button" class="btn btn-secondary btn-sm" :disabled="visible.length === 0" @click="copy">
        <SvgIcon class="btn-icon" :path="icons.copy" />
        Копировать
      </button>
      <button type="button" class="btn btn-secondary btn-sm" :disabled="visible.length === 0" @click="download">
        <SvgIcon class="btn-icon" :path="icons.download" />
        Скачать
      </button>
    </div>
    <p class="card-hint logs-hint">
      {{ sourceHint }}
      <template v-if="source === 'sing-box' && logLevel">
        Уровень логов sing-box — <code>{{ logLevel }}</code>, его можно изменить в
        <RouterLink :to="{ name: 'system-settings' }">настройках</RouterLink>.
      </template>
    </p>
  </div>

  <div v-if="error" class="callout is-bad" style="margin-bottom: 12px;">{{ error }}</div>

  <div class="logs-meta">
    <span>Показано {{ visible.length }} из {{ parsed.length }}</span>
    <span v-if="counts.error" class="logs-count is-error">ошибок: {{ counts.error }}</span>
    <span v-if="counts.warn" class="logs-count is-warn">предупреждений: {{ counts.warn }}</span>
  </div>

  <div ref="viewer" class="logs-viewer" :class="{ 'is-loading': loading && !text }">
    <div v-if="!loading && parsed.length === 0 && !error" class="logs-empty">Журнал пуст.</div>
    <div v-else-if="parsed.length > 0 && visible.length === 0" class="logs-empty">Под фильтр не попала ни одна строка.</div>
    <div v-for="item in visible" :key="item.index" class="logs-line" :class="`is-${item.level}`">{{ item.line }}</div>
  </div>
</template>
