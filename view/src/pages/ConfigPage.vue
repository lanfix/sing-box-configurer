<script setup lang="ts">
import { defineAsyncComponent, onMounted, ref } from 'vue'

import { get, post } from '../api/client'
import type { ApplyResult, ConfigState } from '../api/types'
import { confirmAction } from '../stores/confirm'
import { configChanged } from '../stores/configStatus'
import { showError, showMessage } from '../stores/toast'

const ConfigDiffView = defineAsyncComponent(() => import('../components/ConfigDiffView.vue'))

const state = ref<ConfigState | null>(null)
const loading = ref(false)
const applying = ref(false)
const onlyChanges = ref(true)

// Вывод sing-box check или ошибка запуска после неудачного применения.
const applyError = ref('')

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
</template>
