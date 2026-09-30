<script setup lang="ts">
// Доступ к панели: вход по логину и паролю. Пока он выключен, панелью может управлять любой,
// у кого есть сетевой доступ к конфигуратору.
import { computed, onMounted, reactive, ref } from 'vue'

import { get, postQuiet } from '../../api/client'
import FormField from '../../components/ui/FormField.vue'
import SaveBar from '../../components/ui/SaveBar.vue'
import SettingRow from '../../components/ui/SettingRow.vue'
import ToggleSwitch from '../../components/ui/ToggleSwitch.vue'
import { useLeaveGuard, useSavedState } from '../../composables/useSavedState'
import { loadAuthStatus } from '../../stores/auth'
import { showError, showMessage } from '../../stores/toast'

interface AuthSettings {
  enabled: boolean
  username: string
}

// minPasswordLength совпадает с проверкой сервера.
const minPasswordLength = 8

const form = reactive({
  enabled: false,
  username: '',
  password: '',
  confirm: '',
  current: '',
})

// saved — сохраненные на сервере настройки: от них зависит, нужен ли текущий пароль.
const saved = ref<AuthSettings>({ enabled: false, username: '' })
const saving = ref(false)
const error = ref('')

const state = useSavedState(form)

useLeaveGuard(() => state.dirty.value)

// needPassword — пароль обязателен: вход включается впервые.
const needPassword = computed(() => form.enabled && !saved.value.enabled)

// validation возвращает текст ошибки формы или пустую строку.
const validation = computed(() => {
  if (!form.enabled) {
    return ''
  }

  if (!form.username.trim()) {
    return 'Задайте логин'
  }

  if (needPassword.value && !form.password) {
    return 'Задайте пароль'
  }

  if (form.password && form.password.length < minPasswordLength) {
    return `Пароль должен быть не короче ${minPasswordLength} символов`
  }

  if (form.password !== form.confirm) {
    return 'Пароли не совпадают'
  }

  return ''
})

// load загружает настройки входа.
async function load(): Promise<void> {
  try {
    const data = await get<AuthSettings>('/api/auth/settings')

    saved.value = data
    Object.assign(form, { enabled: data.enabled, username: data.username, password: '', confirm: '', current: '' })
    state.markSaved()
  } catch (err) {
    showError(err, 'Ошибка загрузки настроек входа')
  }
}

// save сохраняет настройки входа. Сервер выдает новую сессию, поэтому из панели не выбрасывает.
async function save(): Promise<void> {
  error.value = validation.value

  if (error.value) {
    return
  }

  saving.value = true

  try {
    const result = await postQuiet<{ message?: string }>('/api/auth/settings', {
      enabled: form.enabled,
      username: form.username.trim(),
      password: form.password,
      current_password: form.current,
    })

    showMessage(result.message ?? 'Настройки входа сохранены')
    await load()
    await loadAuthStatus()
  } catch (err) {
    error.value = err instanceof Error ? err.message : String(err)
  } finally {
    saving.value = false
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
  <form class="settings-card" autocomplete="off" @submit.prevent="save">
    <div class="settings-card-head">
      <div class="settings-card-title">Доступ к панели</div>
      <p class="settings-card-description">
        Логин и пароль для входа в конфигуратор. Если пароль забыт, выключите вход на сервере командой
        <code>sing-box-configurer auth reset</code> и перезапустите сервис.
      </p>
    </div>

    <SettingRow title="Вход по логину и паролю" class="is-top">
      <template #description>
        Без входа панелью и sing-box может управлять любой, у кого есть сетевой доступ к конфигуратору.
      </template>
      <ToggleSwitch v-model="form.enabled" :label="form.enabled ? 'Включен' : 'Выключен'" />
      <div v-if="!saved.enabled && !form.enabled" class="callout is-warn">Панель открыта без пароля.</div>
    </SettingRow>

    <template v-if="form.enabled">
      <SettingRow title="Логин" input-id="authUsername">
        <input
          id="authUsername"
          v-model="form.username"
          class="form-input"
          type="text"
          autocomplete="username"
          autocapitalize="off"
          spellcheck="false"
          maxlength="64"
        >
      </SettingRow>

      <SettingRow
        :title="saved.enabled ? 'Новый пароль' : 'Пароль'"
        class="is-top"
        :description="saved.enabled ? 'Оставьте пустым, чтобы не менять пароль.' : `Не короче ${minPasswordLength} символов.`"
      >
        <FormField label="Пароль" input-id="authPassword">
          <input id="authPassword" v-model="form.password" class="form-input" type="password" autocomplete="new-password">
        </FormField>
        <FormField label="Повтор пароля" input-id="authConfirm">
          <input id="authConfirm" v-model="form.confirm" class="form-input" type="password" autocomplete="new-password">
        </FormField>
      </SettingRow>
    </template>

    <SettingRow
      v-if="saved.enabled && state.dirty.value"
      title="Текущий пароль"
      input-id="authCurrent"
      description="Нужен, чтобы изменить или выключить вход."
    >
      <input id="authCurrent" v-model="form.current" class="form-input" type="password" autocomplete="current-password">
    </SettingRow>

    <div v-if="error" class="settings-card-body">
      <div class="callout is-bad" role="alert">{{ error }}</div>
    </div>

    <SaveBar :dirty="state.dirty.value" :saving="saving" @reset="reset" />
  </form>
</template>
