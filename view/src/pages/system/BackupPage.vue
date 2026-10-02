<script setup lang="ts">
// Перенос данных: выгрузка app.json и загрузка из файла. Перед загрузкой файл проверяется и показывается его
// содержимое; после загрузки конфигуратор перезапускается.
import { computed, ref } from 'vue'

import { ApiError, postQuiet } from '../../api/client'
import type { AppDataSummary, ImportResult } from '../../api/types'
import SvgIcon from '../../components/SvgIcon.vue'
import ToggleSwitch from '../../components/ui/ToggleSwitch.vue'
import { icons } from '../../icons'
import { confirmAction } from '../../stores/confirm'
import { showError, showMessage } from '../../stores/toast'

// restartTimeout — сколько ждать перезапуска конфигуратора, мс.
const restartTimeout = 90_000

// countLabels — подписи разделов в сводке файла.
const countLabels: Record<string, string> = {
  groups: 'Группы',
  rules: 'Одиночные правила',
  url_sources: 'Источники URL',
  outbounds: 'Outbound-ы',
  urltests: 'URLTest',
  dns_servers: 'DNS-серверы',
  dns_records: 'DNS-записи',
  mixed: 'Mixed-прокси',
  happ: 'Подписки Happ',
  amnezia: 'Конфигурации Amnezia',
}

const exporting = ref(false)
const fileName = ref('')
const content = ref('')
const summary = ref<AppDataSummary | null>(null)
const inspectError = ref('')
const inspecting = ref(false)
const replaceAccess = ref(false)
const importing = ref(false)
const restarting = ref(false)
const restartFailed = ref(false)
const dragging = ref(false)
const fileInput = ref<HTMLInputElement | null>(null)

const counts = computed(() => Object.entries(countLabels)
  .map(([key, label]) => ({ key, label, value: summary.value?.counts[key] ?? 0 })))

const needsMigration = computed(() => summary.value && summary.value.schema_version < summary.value.latest_version)

// exportData скачивает app.json.
async function exportData(): Promise<void> {
  exporting.value = true

  try {
    const response = await fetch('/api/app-data/export')

    if (!response.ok) {
      throw new ApiError(await response.text(), response.status)
    }

    const disposition = response.headers.get('Content-Disposition') ?? ''
    const name = disposition.match(/filename="([^"]+)"/)?.[1] ?? 'sing-box-configurer.json'
    const link = document.createElement('a')

    link.href = URL.createObjectURL(await response.blob())
    link.download = name
    link.click()
    URL.revokeObjectURL(link.href)
  } catch (error) {
    showError(error, 'Не удалось выгрузить данные')
  } finally {
    exporting.value = false
  }
}

// chooseFile читает выбранный файл и проверяет его на сервере.
async function chooseFile(file: File | undefined): Promise<void> {
  if (!file) {
    return
  }

  fileName.value = file.name
  content.value = ''
  summary.value = null
  inspectError.value = ''
  replaceAccess.value = false

  if (file.size > 32 * 1024 * 1024) {
    inspectError.value = 'Файл больше 32 МБ — это не app.json'

    return
  }

  inspecting.value = true

  try {
    content.value = await file.text()

    const data = await postQuiet<{ summary: AppDataSummary }>('/api/app-data/inspect', { content: content.value })

    summary.value = data.summary
  } catch (error) {
    inspectError.value = error instanceof Error ? error.message : String(error)
  } finally {
    inspecting.value = false

    if (fileInput.value) {
      fileInput.value.value = ''
    }
  }
}

// onDrop принимает файл, перетащенный в область загрузки.
function onDrop(event: DragEvent): void {
  dragging.value = false
  void chooseFile(event.dataTransfer?.files?.[0])
}

// clearFile сбрасывает выбранный файл.
function clearFile(): void {
  fileName.value = ''
  content.value = ''
  summary.value = null
  inspectError.value = ''
}

// startedAt возвращает время запуска конфигуратора из /api/health (пусто, если он не отвечает).
async function startedAt(): Promise<string> {
  try {
    const response = await fetch('/api/health', { cache: 'no-store' })

    return response.ok ? ((await response.json()).started_at ?? '') : ''
  } catch {
    return ''
  }
}

// waitRestart ждет, пока конфигуратор запустится заново (изменится время запуска).
async function waitRestart(before: string): Promise<boolean> {
  const deadline = Date.now() + restartTimeout

  while (Date.now() < deadline) {
    await new Promise((resolve) => setTimeout(resolve, 1000))

    const current = await startedAt()

    if (current && current !== before) {
      return true
    }
  }

  return false
}

// importData заменяет данные приложения данными из файла и ждет перезапуска конфигуратора.
async function importData(): Promise<void> {
  const confirmed = await confirmAction({
    title: 'Загрузить данные из файла?',
    message: 'Все текущие настройки будут заменены данными из файла, копия текущих сохранится рядом с резервными '
      + 'копиями конфига. Конфигуратор перезапустится. sing-box продолжит работать с текущим конфигом — '
      + 'новый нужно будет применить на странице «Конфиг».',
    confirmText: 'Загрузить и перезапустить',
    danger: true,
  })

  if (!confirmed) {
    return
  }

  importing.value = true

  try {
    const before = await startedAt()
    const data = await postQuiet<{ result: ImportResult }>('/api/app-data/import', {
      content: content.value,
      replace_access: replaceAccess.value,
    })

    importing.value = false
    restarting.value = true

    if (data.result.migrations.length) {
      showMessage(`Данные обновлены до схемы ${data.result.to_version}`)
    }

    if (await waitRestart(before)) {
      // Полная перезагрузка страницы: все разделы читают новые данные, а при замене входа откроется страница входа.
      window.location.assign('/config')

      return
    }

    restartFailed.value = true
  } catch (error) {
    showError(error, 'Данные не загружены')
  } finally {
    importing.value = false
  }
}

// reloadPage перезагружает страницу.
function reloadPage(): void {
  window.location.reload()
}
</script>

<template>
  <div class="page-narrow">
    <div class="settings-card">
      <div class="settings-card-head">
        <div class="settings-card-title">Выгрузка</div>
        <p class="settings-card-description">
          Все данные конфигуратора одним файлом <code>app.json</code>: группы и правила, источники URL, DNS,
          outbound-ы, URLTest, inbound-ы, подписки и настройки. Файл подойдет для резервной копии и для переноса
          на другой роутер.
        </p>
      </div>
      <div class="settings-card-body is-stack">
        <div class="callout is-warn">
          В файле есть секреты: хэш пароля панели, ссылки подписок, ключи серверов и токен Clash API.
          Храните его так же надежно, как пароль.
        </div>
        <div class="form-actions" style="margin-top: 0;">
          <button type="button" class="btn btn-primary" :disabled="exporting" @click="exportData">
            <SvgIcon class="btn-icon" :path="icons.download" />
            {{ exporting ? 'Выгрузка...' : 'Скачать app.json' }}
          </button>
        </div>
      </div>
    </div>

    <div class="settings-card">
      <div class="settings-card-head">
        <div class="settings-card-title">Загрузка</div>
        <p class="settings-card-description">
          Данные из файла заменяют текущие. Файл прежней версии конфигуратора приводится к текущему формату,
          файл более новой версии загрузить нельзя — сначала обновите конфигуратор.
        </p>
      </div>

      <div class="settings-card-body is-stack">
        <label
          class="drop-zone"
          :class="{ 'is-dragging': dragging, 'has-file': Boolean(fileName) }"
          @dragover.prevent="dragging = true"
          @dragleave.prevent="dragging = false"
          @drop.prevent="onDrop"
        >
          <input
            ref="fileInput"
            type="file"
            accept=".json,application/json"
            class="drop-zone-input"
            @change="chooseFile(($event.target as HTMLInputElement).files?.[0])"
          >
          <SvgIcon class="drop-zone-icon" :path="icons.upload" />
          <span v-if="inspecting">Проверка файла...</span>
          <span v-else-if="fileName" class="drop-zone-file">{{ fileName }}</span>
          <span v-else>Выберите файл <code>app.json</code> или перетащите его сюда</span>
        </label>

        <div v-if="inspectError" class="callout is-bad">
          {{ inspectError }}
          <template v-if="summary">
            <br>Схема данных файла: {{ summary.schema_version }}, эта версия конфигуратора поддерживает {{ summary.latest_version }}.
          </template>
        </div>

        <template v-if="summary && !inspectError">
          <div class="import-summary">
            <div v-for="item in counts" :key="item.key" class="import-summary-item" :class="{ 'is-empty': item.value === 0 }">
              <span class="import-summary-value">{{ item.value }}</span>
              <span class="import-summary-label">{{ item.label }}</span>
            </div>
          </div>

          <div v-if="needsMigration" class="callout">
            Файл создан прежней версией конфигуратора (схема данных {{ summary.schema_version }}) — при загрузке он будет
            приведен к текущему формату (схема {{ summary.latest_version }}).
          </div>

          <ToggleSwitch v-model="replaceAccess" label="Заменить доступ к панели данными из файла">
            <template v-if="replaceAccess">
              Вход в панель и разрешенные доменные имена возьмутся из файла:
              <strong>{{ summary.has_auth ? `вход по логину ${summary.username}` : 'вход выключен' }}</strong><template v-if="summary.allowed_hosts.length">,
                домены {{ summary.allowed_hosts.join(', ') }}</template>.
              После перезапуска войдите с логином и паролем из файла.
            </template>
            <template v-else>
              Текущий логин, пароль и разрешенные доменные имена панели останутся как есть. В файле:
              {{ summary.has_auth ? `вход по логину ${summary.username}` : 'вход выключен' }}.
            </template>
          </ToggleSwitch>

          <div v-if="replaceAccess && !summary.has_auth" class="callout is-warn">
            В файле вход в панель выключен: после загрузки панелью сможет управлять любой, у кого есть доступ к порту.
          </div>

          <div class="form-actions" style="margin-top: 0;">
            <button type="button" class="btn btn-secondary" :disabled="importing" @click="clearFile">Отмена</button>
            <button type="button" class="btn btn-danger" :disabled="importing || restarting" @click="importData">
              <SvgIcon class="btn-icon" :path="icons.upload" />
              {{ importing ? 'Загрузка...' : 'Загрузить и перезапустить' }}
            </button>
          </div>
        </template>
      </div>
    </div>
  </div>

  <Teleport to="body">
    <div v-if="restarting" class="modal-overlay restart-overlay">
      <div class="restart-card">
        <template v-if="!restartFailed">
          <div class="restart-spinner"></div>
          <div class="restart-title">Конфигуратор перезапускается</div>
          <p class="restart-text">Данные загружены. Страница обновится, как только конфигуратор запустится.</p>
        </template>
        <template v-else>
          <div class="restart-title">Конфигуратор не ответил</div>
          <p class="restart-text">
            Данные загружены, но конфигуратор не запустился за {{ restartTimeout / 1000 }} секунд. Проверьте его
            журнал на сервере и перезапустите службу или контейнер вручную.
          </p>
          <button type="button" class="btn btn-primary" @click="reloadPage">Обновить страницу</button>
        </template>
      </div>
    </div>
  </Teleport>
</template>
