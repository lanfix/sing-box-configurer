<script setup lang="ts">
// Безопасность: вход в панель по логину и паролю, адреса, по которым открывается панель (защита от
// DNS rebinding), и встроенная защита от запросов с чужих сайтов.
import { computed, onMounted, reactive, ref } from 'vue'

import { get, postQuiet } from '../../api/client'
import type { SecuritySettings } from '../../api/types'
import ChipsInput from '../../components/ui/ChipsInput.vue'
import SaveBar from '../../components/ui/SaveBar.vue'
import SettingRow from '../../components/ui/SettingRow.vue'
import ToggleSwitch from '../../components/ui/ToggleSwitch.vue'
import { useLeaveGuard, useSavedState } from '../../composables/useSavedState'
import { showError, showMessage } from '../../stores/toast'
import { isDomain, isIP } from '../../utils/validate'
import PanelAccessCard from './PanelAccessCard.vue'

const form = reactive({
  check_host: true,
  allowed_hosts: [] as string[],
})

const info = ref<SecuritySettings | null>(null)
const saving = ref(false)
const error = ref('')

const state = useSavedState(form)

useLeaveGuard(() => state.dirty.value)

// currentIsName — панель открыта по доменному имени, а не по IP-адресу или localhost.
const currentIsName = computed(() => {
  const host = info.value?.current_host ?? ''

  return host !== '' && host !== 'localhost' && !host.endsWith('.localhost') && !isIP(host)
})

// currentListed — доменное имя текущего адреса есть в списке формы.
const currentListed = computed(() => {
  const host = info.value?.current_host ?? ''

  return form.allowed_hosts.some((allowed) => allowed === host || (allowed.startsWith('*.') && host.endsWith(allowed.slice(1))))
})

// validateHost проверяет доменное имя или шаблон *.домен.
function validateHost(value: string): string {
  const name = value.toLowerCase().replace(/^\*\./, '')

  if (value.includes('/') || !isDomain(name) || isIP(name)) {
    return 'Укажите доменное имя без http:// и пути, например router.lan или *.example.com'
  }

  return ''
}

// load загружает настройки защиты.
async function load(): Promise<void> {
  try {
    const data = await get<SecuritySettings>('/api/security')

    info.value = data
    form.check_host = data.check_host
    form.allowed_hosts = [...data.allowed_hosts]
    state.markSaved()
  } catch (err) {
    showError(err, 'Ошибка загрузки настроек безопасности')
  }
}

// save сохраняет настройки защиты. Сервер не даст сохранить список, без которого текущий адрес не откроется.
async function save(): Promise<void> {
  error.value = ''
  saving.value = true

  try {
    const result = await postQuiet<{ message?: string }>('/api/security', form)

    showMessage(result.message ?? 'Настройки безопасности сохранены')
    await load()
  } catch (err) {
    error.value = err instanceof Error ? err.message : String(err)
  } finally {
    saving.value = false
  }
}

// addCurrent добавляет в список доменное имя, по которому открыта панель.
function addCurrent(): void {
  const host = info.value?.current_host

  if (host && !form.allowed_hosts.includes(host)) {
    form.allowed_hosts = [...form.allowed_hosts, host]
  }
}

// reset отменяет изменения.
function reset(): void {
  state.reset()
  error.value = ''
}

onMounted(load)
</script>

<template>
  <div class="page-narrow">
    <PanelAccessCard />

    <form class="settings-card" @submit.prevent="save">
      <div class="settings-card-head">
        <div class="settings-card-title">Адрес панели</div>
        <p class="settings-card-description">
          Защита от DNS rebinding: чужой сайт может направить свой домен на адрес панели, и браузер посчитает
          запросы к ней своими. Поэтому панель отвечает, только если ее открыли по IP-адресу, <code>localhost</code>
          или разрешенному доменному имени. Действует сразу после сохранения.
        </p>
      </div>

      <SettingRow title="Проверять адрес" class="is-top">
        <template #description>
          Если панель перестала открываться по домену, выключите проверку на сервере командой
          <code>sing-box-configurer security reset</code> и перезапустите сервис.
        </template>
        <ToggleSwitch v-model="form.check_host" :label="form.check_host ? 'Включено' : 'Выключено'" />
        <div v-if="!form.check_host" class="callout is-warn">Панель отвечает на любое доменное имя: защита от DNS rebinding выключена.</div>
      </SettingRow>

      <SettingRow v-if="form.check_host" title="Разрешенные домены" input-id="allowedHosts" class="is-top">
        <template #description>
          Доменные имена, по которым вы открываете панель (например, через reverse proxy).
          <code>*.example.com</code> разрешает все поддомены. IP-адреса и <code>localhost</code> разрешены всегда.
        </template>
        <ChipsInput v-model="form.allowed_hosts" input-id="allowedHosts" placeholder="router.lan" :validate="validateHost" />
        <span v-if="info?.extra_hosts.length" class="field-hint">
          Также всегда разрешены адреса из конфига сервиса: <code>{{ info.extra_hosts.join(', ') }}</code>.
        </span>
        <button v-if="currentIsName && !currentListed" type="button" class="link-button" style="align-self: flex-start;" @click="addCurrent">
          Добавить текущий адрес: {{ info?.current_host }}
        </button>
      </SettingRow>

      <div v-if="error" class="settings-card-body">
        <div class="callout is-bad" role="alert">{{ error }}</div>
      </div>

      <SaveBar :dirty="state.dirty.value" :saving="saving" @reset="reset" />
    </form>

    <section class="settings-card">
      <div class="settings-card-head">
        <div class="settings-card-title">Запросы с других сайтов</div>
        <p class="settings-card-description">Работает всегда и не настраивается.</p>
      </div>

      <div class="settings-card-body">
        <ul class="security-list">
          <li>Запросы к API со страниц других сайтов (в том числе с соседних портов того же адреса) отклоняются: чужая страница не может ни прочитать данные панели, ни изменить настройки (CSRF).</li>
          <li>Файлы интерфейса нельзя подключить на чужой странице, а саму панель — встроить во фрейм.</li>
          <li>Интерфейс выполняет только собственные скрипты (Content Security Policy).</li>
          <li>Открыть панель по ссылке с другого сайта можно: страницу открывает сам пользователь.</li>
        </ul>
      </div>
    </section>
  </div>
</template>
