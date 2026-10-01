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
import { auth, loadAuthStatus } from '../../stores/auth'
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

// changingPassword — показаны поля нового пароля (вход уже включен, пароль задан).
const changingPassword = ref(false)

const state = useSavedState(form)

useLeaveGuard(() => state.dirty.value)

// needPassword — пароль обязателен: вход включается впервые.
const needPassword = computed(() => form.enabled && !saved.value.enabled)

// showPassword — показаны поля пароля.
const showPassword = computed(() => form.enabled && (needPassword.value || changingPassword.value))

// needCurrent — для сохранения изменений нужен текущий пароль.
const needCurrent = computed(() => saved.value.enabled && state.dirty.value)

// credentialsTitle — заголовок блока полей: при выключении входа в нем остается только текущий пароль.
const credentialsTitle = computed(() => (form.enabled ? 'Логин и пароль' : 'Подтверждение'))

// credentialsDescription — пояснение к блоку полей.
const credentialsDescription = computed(() => {
  if (!form.enabled) {
    return 'Чтобы выключить вход, введите текущий пароль.'
  }

  if (!saved.value.enabled) {
    return `Пароль — не короче ${minPasswordLength} символов. После сохранения вы останетесь в панели, а в других браузерах понадобится вход.`
  }

  return 'Пароль хранится только в виде хэша, поэтому не показывается. Изменения подтверждаются текущим паролем.'
})

// validation возвращает текст ошибки формы или пустую строку.
const validation = computed(() => {
  if (needCurrent.value && !form.current) {
    return 'Введите текущий пароль'
  }

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
    changingPassword.value = false
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

// cancelPasswordChange скрывает поля нового пароля и очищает их.
function cancelPasswordChange(): void {
  changingPassword.value = false
  form.password = ''
  form.confirm = ''
}

// reset отменяет изменения.
function reset(): void {
  state.reset()
  changingPassword.value = false
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
      <div v-if="saved.enabled && form.enabled" class="callout is-good">
        Вход включен: логин <strong>{{ saved.username }}</strong>, пароль задан.
        <template v-if="auth.authenticated && auth.username"> Вы вошли как {{ auth.username }}.</template>
      </div>
      <div v-else-if="saved.enabled" class="callout is-warn">После сохранения вход выключится, и панель будет открыта без пароля.</div>
      <div v-else-if="!form.enabled" class="callout is-warn">Панель открыта без пароля.</div>
    </SettingRow>

    <SettingRow v-if="form.enabled || needCurrent" :title="credentialsTitle" :description="credentialsDescription" class="is-top">
      <div class="credentials-fields">
        <FormField v-if="form.enabled" label="Логин" input-id="authUsername">
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
        </FormField>

        <template v-if="showPassword">
          <FormField :label="saved.enabled ? 'Новый пароль' : 'Пароль'" input-id="authPassword">
            <input id="authPassword" v-model="form.password" class="form-input" type="password" autocomplete="new-password">
          </FormField>
          <FormField label="Повтор пароля" input-id="authConfirm">
            <input id="authConfirm" v-model="form.confirm" class="form-input" type="password" autocomplete="new-password">
          </FormField>
          <button v-if="changingPassword" type="button" class="link-button" @click="cancelPasswordChange">Не менять пароль</button>
        </template>
        <button v-else-if="form.enabled" type="button" class="link-button" @click="changingPassword = true">Сменить пароль</button>

        <FormField v-if="needCurrent" label="Текущий пароль" input-id="authCurrent">
          <input id="authCurrent" v-model="form.current" class="form-input" type="password" autocomplete="current-password">
        </FormField>
      </div>
    </SettingRow>

    <div v-if="error" class="settings-card-body">
      <div class="callout is-bad" role="alert">{{ error }}</div>
    </div>

    <SaveBar :dirty="state.dirty.value" :saving="saving" @reset="reset" />
  </form>
</template>
