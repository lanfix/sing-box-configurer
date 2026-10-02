<script setup lang="ts">
import { defineAsyncComponent, onMounted, ref } from 'vue'

import { get, post } from '../api/client'
import type { ApplyResult, ConfigBackup, ConfigState } from '../api/types'
import ModalDialog from '../components/ModalDialog.vue'
import SvgIcon from '../components/SvgIcon.vue'
import { icons } from '../icons'
import { confirmAction } from '../stores/confirm'
import { configChanged } from '../stores/configStatus'
import { showError, showMessage } from '../stores/toast'
import { formatBytesRu, formatDateTime } from '../utils/format'

const ConfigDiffView = defineAsyncComponent(() => import('../components/ConfigDiffView.vue'))

const state = ref<ConfigState | null>(null)
const loading = ref(false)
const applying = ref(false)
const onlyChanges = ref(true)

// Вывод sing-box check или ошибка запуска после неудачного применения.
const applyError = ref('')

// Резервные копии рабочего конфига: список, выбранная копия и ее содержимое.
const backupsOpen = ref(false)
const backups = ref<ConfigBackup[]>([])
const backupsLoading = ref(false)
const selectedBackup = ref<{ name: string; content: string } | null>(null)
const restoring = ref(false)
const restoreError = ref('')

// openBackups загружает список резервных копий.
async function openBackups(): Promise<void> {
  backupsOpen.value = true
  selectedBackup.value = null
  restoreError.value = ''
  backupsLoading.value = true

  try {
    backups.value = (await get<{ backups: ConfigBackup[] }>('/api/config/backups')).backups ?? []
  } catch (error) {
    showError(error, 'Ошибка загрузки резервных копий')
  } finally {
    backupsLoading.value = false
  }
}

// selectBackup загружает копию для сравнения с рабочим конфигом.
async function selectBackup(backup: ConfigBackup): Promise<void> {
  restoreError.value = ''

  try {
    const data = await get<{ name: string; content: string }>(`/api/config/backups/content?name=${encodeURIComponent(backup.name)}`)

    selectedBackup.value = { name: data.name, content: data.content }
  } catch (error) {
    showError(error)
  }
}

// restoreBackup делает рабочим конфиг из выбранной копии.
async function restoreBackup(): Promise<void> {
  const backup = selectedBackup.value

  if (!backup) {
    return
  }

  const confirmed = await confirmAction({
    title: 'Восстановить конфиг из резервной копии?',
    message: 'Копия проверяется командой sing-box check и становится рабочим конфигом, sing-box перезапускается. '
      + 'Текущий рабочий конфиг сохранится в новую резервную копию. Настройки конфигуратора не меняются, поэтому '
      + 'итоговый конфиг будет отличаться от рабочего, пока вы его не примените.',
    confirmText: 'Восстановить',
    danger: true,
  })

  if (!confirmed) {
    return
  }

  restoring.value = true
  restoreError.value = ''

  try {
    const result = await post<{ message: string }>('/api/config/backups/restore', { name: backup.name })

    showMessage(result.message)
    backupsOpen.value = false
  } catch (error) {
    restoreError.value = error instanceof Error ? error.message : String(error)
  } finally {
    restoring.value = false
    await load()
  }
}

// backupTitle возвращает подпись копии.
function backupTitle(name: string): string {
  const backup = backups.value.find((item) => item.name === name)

  return backup ? formatDateTime(backup.created_at) : name
}

// load рендерит итоговый конфиг и сравнивает его с рабочим.
async function load(): Promise<void> {
  loading.value = true

  try {
    state.value = await get<ConfigState>('/api/config')
    configChanged.value = state.value.changed
  } catch (error) {
    showError(error, 'Ошибка рендера конфига')
  } finally {
    loading.value = false
  }
}

// apply проверяет итоговый конфиг, заменяет им рабочий и перезапускает sing-box.
async function apply(): Promise<void> {
  const confirmed = await confirmAction({
    title: 'Применить конфиг и перезагрузить sing-box?',
    message: 'Итоговый конфиг проверяется командой sing-box check, прежний сохраняется в резервную копию. Открытые соединения будут разорваны.',
    confirmText: 'Применить',
  })

  if (!confirmed) {
    return
  }

  applying.value = true
  applyError.value = ''

  try {
    const result = await post<ApplyResult>('/api/config/apply')
    const warnings = result.warnings?.length ? `\nПредупреждения: ${result.warnings.join('; ')}` : ''

    showMessage(result.message + warnings, warnings ? 'warning' : 'success')
  } catch (error) {
    applyError.value = error instanceof Error ? error.message : String(error)
    showMessage('Конфиг не применен, рабочий конфиг не изменился', 'error')
  } finally {
    applying.value = false
    await load()
  }
}

onMounted(load)
</script>

<template>
  <Teleport to="#topbar-actions" defer>
    <button class="btn btn-secondary" :disabled="applying" title="Вернуть рабочий конфиг из резервной копии" @click="openBackups">
      <SvgIcon class="btn-icon" :path="icons.history" />
      Резервные копии
    </button>
    <button class="btn btn-secondary" :disabled="loading || applying" @click="load">Обновить</button>
    <button
      class="btn btn-warning"
      :disabled="!state?.changed || applying || loading"
      :title="state?.changed ? 'Проверить, применить и перезагрузить sing-box' : 'Итоговый конфиг совпадает с рабочим'"
      @click="apply"
    >
      {{ applying ? 'Применение...' : 'Применить и перезагрузить sing-box' }}
    </button>
  </Teleport>

  <div class="add-form-card has-progress" :class="{ 'is-loading': loading || applying }">
    <div class="progress-line"></div>
    <div class="form-header" style="margin-bottom: 8px;">Итоговый конфиг sing-box</div>
    <p class="card-hint" style="margin-bottom: 12px;">
      Конфиг собирается из групп, правил, DNS, outbound-ов, inbound-ов и подписок. Рабочий конфиг sing-box меняется
      только при применении: итоговый конфиг проверяется командой <code>sing-box check</code>, прежний сохраняется
      в резервную копию, и если sing-box не запустится, он будет восстановлен.
    </p>

    <div v-if="state" class="config-status-line">
      <template v-if="state.changed">
        <span class="stat-dot is-warn"></span>
        <span>Итоговый конфиг отличается от рабочего — для применения требуется перезагрузка sing-box.</span>
      </template>
      <template v-else>
        <span class="stat-dot is-good"></span>
        <span>Рабочий конфиг совпадает с итоговым, перезагрузка не требуется.</span>
      </template>
    </div>

    <div v-if="state?.actual_error" class="happ-note is-warn" style="margin-top: 12px;">
      Рабочий конфиг: {{ state.actual_error }}
    </div>

    <div v-if="state?.warnings?.length" class="config-warnings">
      <div v-for="warning in state.warnings" :key="warning" class="happ-note">{{ warning }}</div>
    </div>

    <pre v-if="applyError" class="check-output">{{ applyError }}</pre>

    <div class="config-toolbar">
      <label class="check-label">
        <input v-model="onlyChanges" type="checkbox">
        <span>Показывать только изменения</span>
      </label>
      <div class="config-legend">
        <span class="legend-added">Добавлено в итоговом</span>
        <span class="legend-removed">Удалено из рабочего</span>
      </div>
    </div>
  </div>

  <ConfigDiffView
    v-if="state"
    :original="state.actual"
    :modified="state.rendered"
    :only-changes="onlyChanges && state.changed"
  />
  <div v-else-if="loading" class="empty-state">Рендер конфига...</div>

  <ModalDialog
    v-if="backupsOpen"
    title="Резервные копии рабочего конфига"
    subtitle="Копия снимается перед каждым применением. Хранятся 10 последних."
    :autofocus="false"
    wide
    @close="backupsOpen = false"
  >
    <p class="card-hint" style="margin-top: 0;">
      Восстановление возвращает sing-box к прежнему рабочему конфигу — например, если новый конфиг работает
      неправильно. Настройки конфигуратора при этом не меняются.
    </p>

    <div v-if="backupsLoading" class="empty-state">Загрузка...</div>
    <div v-else-if="backups.length === 0" class="empty-state">Резервных копий пока нет: они появятся после первого применения конфига.</div>
    <ul v-else class="backup-list">
      <li v-for="backup in backups" :key="backup.name">
        <button
          type="button"
          class="backup-item"
          :class="{ active: selectedBackup?.name === backup.name }"
          @click="selectBackup(backup)"
        >
          <span class="backup-item-title">{{ formatDateTime(backup.created_at) }}</span>
          <span class="backup-item-sub">{{ formatBytesRu(backup.size) }}</span>
        </button>
      </li>
    </ul>

    <template v-if="selectedBackup && state">
      <div class="backup-diff-head">
        <div class="form-header" style="margin: 0;">Копия от {{ backupTitle(selectedBackup.name) }}</div>
        <div class="config-legend">
          <span class="legend-added">Есть в копии</span>
          <span class="legend-removed">Есть только в рабочем</span>
        </div>
      </div>
      <div v-if="selectedBackup.content === state.actual" class="callout is-good">Копия совпадает с рабочим конфигом.</div>
      <ConfigDiffView v-else :original="state.actual" :modified="selectedBackup.content" :only-changes="true" />
      <pre v-if="restoreError" class="check-output">{{ restoreError }}</pre>
      <div class="form-actions">
        <button type="button" class="btn btn-secondary" @click="backupsOpen = false">Закрыть</button>
        <button
          type="button"
          class="btn btn-warning"
          :disabled="restoring || selectedBackup.content === state.actual"
          @click="restoreBackup"
        >
          {{ restoring ? 'Восстановление...' : 'Восстановить и перезагрузить sing-box' }}
        </button>
      </div>
    </template>
  </ModalDialog>
</template>
