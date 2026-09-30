<script setup lang="ts">
// Страница входа в панель: открывается, когда вход по логину и паролю включен, а сессии нет.
import { reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'

import FormField from '../components/ui/FormField.vue'
import { login, safeRedirect } from '../stores/auth'

const route = useRoute()
const router = useRouter()

const form = reactive({
  username: '',
  password: '',
})

const error = ref('')
const submitting = ref(false)

// submit проверяет логин и пароль и возвращает на страницу, с которой пришли.
async function submit(): Promise<void> {
  submitting.value = true
  error.value = ''

  try {
    await login(form.username, form.password)
    await router.replace(safeRedirect(route.query.redirect))
  } catch (err) {
    error.value = err instanceof Error ? err.message : String(err)
    form.password = ''
  } finally {
    submitting.value = false
  }
}
</script>

<template>
  <div class="login-page">
    <form class="login-card" @submit.prevent="submit">
      <div class="login-brand">
        <img class="logo" src="/favicon.svg" alt="">
        <div class="logo-text">
          <span class="title">Sing-Box</span>
          <span class="subtitle">Configurer</span>
        </div>
      </div>

      <h1 class="login-title">Вход в панель</h1>

      <div class="form-stack">
        <FormField label="Логин" input-id="loginUsername">
          <input
            id="loginUsername"
            v-model="form.username"
            class="form-input"
            type="text"
            autocomplete="username"
            autocapitalize="off"
            spellcheck="false"
            required
            autofocus
          >
        </FormField>

        <FormField label="Пароль" input-id="loginPassword">
          <input
            id="loginPassword"
            v-model="form.password"
            class="form-input"
            type="password"
            autocomplete="current-password"
            required
          >
        </FormField>

        <div v-if="error" class="callout is-bad" role="alert">{{ error }}</div>

        <button type="submit" class="btn btn-primary login-submit" :disabled="submitting">
          {{ submitting ? 'Вход...' : 'Войти' }}
        </button>
      </div>

      <p class="login-hint">
        Забыли пароль? Выключите вход на сервере командой <code>sing-box-configurer auth reset</code> и перезапустите сервис.
      </p>
    </form>
  </div>
</template>
