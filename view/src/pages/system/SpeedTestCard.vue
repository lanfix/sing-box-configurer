<script setup lang="ts">
// Настройки теста скорости outbound-ов: тестовые серверы по порядку, длительность замера и число потоков.
import { onMounted, reactive, ref } from 'vue'

import { get, post } from '../../api/client'
import type { SpeedTestServer, SpeedTestState } from '../../api/types'
import SvgIcon from '../../components/SvgIcon.vue'
import IconButton from '../../components/ui/IconButton.vue'
import SaveBar from '../../components/ui/SaveBar.vue'
import SettingRow from '../../components/ui/SettingRow.vue'
import { useLeaveGuard, useSavedState } from '../../composables/useSavedState'
import { icons } from '../../icons'
import { confirmAction } from '../../stores/confirm'
import { showError, showMessage } from '../../stores/toast'

const form = reactive({
  servers: [] as SpeedTestServer[],
  duration: 10,
  streams: 4,
})

const saving = ref(false)
const state = useSavedState(form)

useLeaveGuard(() => state.dirty.value)

// load загружает настройки теста скорости.
async function load(): Promise<void> {
  try {
    const data = await get<SpeedTestState>('/api/speedtest')

    form.servers = data.settings.servers.map((server) => ({ ...server }))
    form.duration = data.settings.duration
    form.streams = data.settings.streams
    state.markSaved()
  } catch (error) {
    showError(error, 'Ошибка загрузки настроек теста скорости')
  }
}

// save сохраняет настройки: они действуют сразу.
async function save(): Promise<void> {
  saving.value = true

  try {
    await post('/api/speedtest/settings', {
      servers: form.servers.filter((server) => server.url.trim() !== ''),
      duration: Number(form.duration),
      streams: Number(form.streams),
    })
    showMessage('Настройки теста скорости сохранены')
    await load()
  } catch (error) {
    showError(error)
  } finally {
    saving.value = false
  }
}

// resetDefaults возвращает серверы и параметры по умолчанию.
async function resetDefaults(): Promise<void> {
  const confirmed = await confirmAction({
    title: 'Вернуть настройки теста скорости по умолчанию?',
    message: 'Список серверов, длительность и число потоков заменятся стандартными.',
    confirmText: 'Вернуть',
  })

  if (!confirmed) {
    return
  }

  try {
    await post('/api/speedtest/settings/reset')
    showMessage('Настройки теста скорости сброшены')
    await load()
  } catch (error) {
    showError(error)
  }
}

// move меняет место сервера в списке: серверы пробуются по порядку.
function move(index: number, offset: number): void {
  const target = index + offset

  if (target < 0 || target >= form.servers.length) {
    return
  }

  const [server] = form.servers.splice(index, 1)

  form.servers.splice(target, 0, server)
}

onMounted(load)
</script>

<template>
  <form class="settings-card" @submit.prevent="save">
    <div class="settings-card-head">
      <div class="settings-card-title">Тест скорости outbound-ов</div>
      <p class="settings-card-description">
        Замер скачивает файл с тестового сервера через outbound. Серверы пробуются по порядку: если сервер не ответил
        через outbound за 10 секунд (например, заблокирован в стране роутера или выхода), берется следующий.
        Настройки действуют сразу.
      </p>
    </div>

    <SettingRow title="Тестовые серверы" class="is-top is-wide">
      <template #description>
        Ссылка на большой файл (100 МБ и больше): он скачивается заново, если закончится раньше времени.
        Сервер, доступный и напрямую, и через VPN, лучше поставить первым.
      </template>
      <div class="list-editor">
        <div v-for="(server, index) in form.servers" :key="index" class="list-editor-row speed-server-row">
          <input v-model="server.name" class="form-input speed-server-name" type="text" placeholder="Название" aria-label="Название сервера">
          <input v-model="server.url" class="form-input is-mono" type="url" placeholder="https://example.com/100MB.bin" aria-label="Ссылка на файл" required>
          <IconButton icon="chevron" title="Выше" class="speed-move-up" :disabled="index === 0" @click="move(index, -1)" />
          <IconButton icon="chevron" title="Ниже" class="speed-move-down" :disabled="index === form.servers.length - 1" @click="move(index, 1)" />
          <IconButton icon="trash" title="Удалить" danger :disabled="form.servers.length === 1" @click="form.servers.splice(index, 1)" />
        </div>
        <div class="speed-server-actions">
          <button type="button" class="btn btn-secondary btn-sm" :disabled="form.servers.length >= 20" @click="form.servers.push({ name: '', url: '' })">
            <SvgIcon class="btn-icon" :path="icons.plus" />
            Добавить сервер
          </button>
          <button type="button" class="link-button" @click="resetDefaults">Вернуть по умолчанию</button>
        </div>
      </div>
    </SettingRow>

    <SettingRow title="Длительность замера" input-id="speedDuration" description="От 3 до 60 секунд. Дольше — точнее, но больше трафика.">
      <div class="input-group">
        <input id="speedDuration" v-model.number="form.duration" class="form-input" type="number" min="3" max="60" required>
        <span class="input-addon">с</span>
      </div>
    </SettingRow>

    <SettingRow title="Параллельных загрузок" input-id="speedStreams" description="От 1 до 16. Одно соединение часто не загружает быстрый канал полностью.">
      <input id="speedStreams" v-model.number="form.streams" class="form-input" type="number" min="1" max="16" required>
    </SettingRow>

    <SaveBar :dirty="state.dirty.value" :saving="saving" @reset="state.reset" />
  </form>
</template>
