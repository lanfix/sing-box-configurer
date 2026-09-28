<script setup lang="ts">
import { onMounted, ref } from 'vue'

import { get, post } from '../../api/client'
import type { Settings } from '../../api/types'
import { showError, showMessage } from '../../stores/toast'

const logLevels = ['trace', 'debug', 'info', 'warn', 'error', 'fatal', 'panic']

const settings = ref<Settings | null>(null)
const logLevel = ref('warn')
const origins = ref('')
const showSecret = ref(false)

// load загружает общие настройки.
async function load(): Promise<void> {
  try {
    settings.value = await get<Settings>('/api/settings')
    logLevel.value = settings.value.log_level
    origins.value = settings.value.clash_api.allow_origins.join('\n')
  } catch (error) {
    showError(error, 'Ошибка загрузки настроек')
  }
}

// save сохраняет уровень логов и CORS-origin-ы.
async function save(): Promise<void> {
  try {
    await post('/api/settings', {
      log_level: logLevel.value,
      allow_origins: origins.value.split('\n').map((origin) => origin.trim()).filter(Boolean),
    })

    showMessage('Настройки сохранены, они вступят в силу после применения конфига')
    await load()
  } catch (error) {
    showError(error)
  }
}

// regenerate создает новый секрет Clash API.
async function regenerate(): Promise<void> {
  if (!confirm('Создать новый токен Clash API?\n\nСтарый перестанет работать после применения конфига: внешние панели (yacd и т.п.) нужно будет перенастроить.')) {
    return
  }

  try {
    const result = await post('/api/settings/regenerate-secret')

    showMessage(result.message ?? 'Токен обновлен')
    await load()
  } catch (error) {
    showError(error)
  }
}

// copySecret копирует токен Clash API.
async function copySecret(): Promise<void> {
  try {
    await navigator.clipboard.writeText(settings.value?.clash_api.secret ?? '')
    showMessage('Токен скопирован')
  } catch {
    showMessage('Не удалось скопировать токен', 'error')
  }
}

onMounted(load)
</script>

<template>
  <div class="add-form-card">
    <div class="form-header" style="margin-bottom: 4px;">Общие настройки sing-box</div>
    <p class="card-hint" style="margin: 0 0 16px;">Изменения попадают в итоговый конфиг и вступают в силу после его применения.</p>

    <form @submit.prevent="save">
      <div class="form-row">
        <div class="form-group" style="flex: 0 1 240px;">
          <label class="form-label" for="logLevel">Уровень логов</label>
          <select id="logLevel" v-model="logLevel" class="form-select">
            <option v-for="level in logLevels" :key="level" :value="level">{{ level }}</option>
          </select>
        </div>
      </div>

      <div class="form-group">
        <label class="form-label">Токен Clash API</label>
        <div class="secret-row">
          <code>{{ showSecret ? settings?.clash_api.secret : '••••••••••••••••••••••••••••••••' }}</code>
          <button type="button" class="btn btn-secondary btn-sm" @click="showSecret = !showSecret">{{ showSecret ? 'Скрыть' : 'Показать' }}</button>
          <button type="button" class="btn btn-secondary btn-sm" @click="copySecret">Копировать</button>
          <button type="button" class="btn btn-warning btn-sm" @click="regenerate">Новый токен</button>
        </div>
        <p class="form-help">
          Clash API слушает <code>0.0.0.0:9090</code> и требует заголовок <code>Authorization: Bearer &lt;токен&gt;</code>.
          Конфигуратор берет токен из рабочего конфига sing-box сам.
        </p>
      </div>

      <div class="form-group">
        <label class="form-label" for="allowOrigins">CORS: разрешенные origin-ы (по одному на строку)</label>
        <textarea id="allowOrigins" v-model="origins" class="form-textarea" rows="3" placeholder="*&#10;http://yacd.example"></textarea>
        <p class="form-help">Origin-ы внешних панелей, которым браузер разрешит запросы к Clash API. <code>*</code> — любые.</p>
      </div>

      <div class="form-actions">
        <button type="submit" class="btn btn-primary">Сохранить</button>
      </div>
    </form>
  </div>
</template>
