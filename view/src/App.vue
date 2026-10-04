<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute } from 'vue-router'

import AppSidebar from './components/AppSidebar.vue'
import ConfirmHost from './components/ConfirmHost.vue'
import SvgIcon from './components/SvgIcon.vue'
import { icons } from './icons'
import { auth } from './stores/auth'
import { configChanged, startConfigStatusPolling } from './stores/configStatus'
import { alertLevel, alertsOf, startSubscriptionAlertsPolling } from './stores/subscriptionAlerts'
import { hideToast, toast } from './stores/toast'
import { initUpdates, updateAvailable } from './stores/updates'

const route = useRoute()

// panel — страница панели (с меню); страница входа показывается без него. До первого перехода
// роутера (проверка входа) не показывается ничего.
const panel = computed(() => Boolean(route.name) && route.name !== 'login')

// Фоновые опросы стартуют, когда панель доступна: до входа API отвечает 401.
const unlocked = computed(() => auth.loaded && (!auth.enabled || auth.authenticated))

// menuOpen — меню открыто поверх страницы (на узком экране, где оно скрыто за кнопкой).
const menuOpen = ref(false)

const mainEl = ref<HTMLElement | null>(null)
const contentEl = ref<HTMLElement | null>(null)

// menuAttention — точка на кнопке меню: в нем есть пункт, требующий внимания.
const menuAttention = computed<'' | 'warn' | 'bad'>(() => {
  if (configChanged.value || updateAvailable.value) {
    return 'bad'
  }

  return alertLevel(alertsOf())
})

let pollingStarted = false

watch(unlocked, (value) => {
  if (!value || pollingStarted) {
    return
  }

  pollingStarted = true
  startConfigStatusPolling()
  startSubscriptionAlertsPolling()
  void initUpdates()
}, { immediate: true })

// Переход на другую страницу закрывает меню и открывает новую страницу с начала: область прокрутки
// общая для всех страниц и иначе сохраняла бы положение предыдущей.
watch(() => route.path, () => {
  menuOpen.value = false

  for (const element of [mainEl.value, contentEl.value]) {
    element?.scrollTo({ top: 0 })
  }
})

// onKeydown закрывает открытое меню по Esc.
function onKeydown(event: KeyboardEvent): void {
  if (event.key === 'Escape' && menuOpen.value) {
    menuOpen.value = false
  }
}

onMounted(() => document.addEventListener('keydown', onKeydown))

onBeforeUnmount(() => document.removeEventListener('keydown', onKeydown))
</script>

<template>
  <div v-if="panel" class="app-container" :class="{ 'menu-open': menuOpen }">
    <AppSidebar :open="menuOpen" @close="menuOpen = false" />
    <div class="sidebar-backdrop" aria-hidden="true" @click="menuOpen = false"></div>

    <main ref="mainEl" class="main-content">
      <div class="top-bar">
        <!-- На телефоне строка с заголовком прилипает к верху, а кнопки страницы прокручиваются вместе со страницей. -->
        <div class="top-bar-main">
          <button
            type="button"
            class="icon-btn menu-toggle"
            aria-label="Открыть меню"
            :aria-expanded="menuOpen"
            @click="menuOpen = true"
          >
            <SvgIcon :path="icons.menu" />
            <span v-if="menuAttention" class="menu-toggle-dot" :class="{ 'is-warn': menuAttention === 'warn' }"></span>
          </button>
          <h2 class="page-title">{{ route.meta.title || 'Sing-Box Configurer' }}</h2>
        </div>
        <!-- Страницы выводят сюда свои кнопки через Teleport. -->
        <div id="topbar-actions" class="action-buttons"></div>
      </div>

      <div ref="contentEl" class="content-area">
        <RouterView />
      </div>
    </main>
  </div>

  <RouterView v-else />

  <div class="toast-host" aria-live="polite">
    <Transition name="toast">
      <div v-if="toast" :key="toast.id" class="toast" :class="toast.type" role="status">
        <SvgIcon class="toast-icon" :path="toast.type === 'success' ? icons.check : icons.alert" />
        <span class="toast-text">{{ toast.text }}</span>
        <button type="button" class="icon-btn toast-close" aria-label="Закрыть" @click="hideToast">
          <SvgIcon :path="icons.close" />
        </button>
      </div>
    </Transition>
  </div>

  <ConfirmHost />
</template>
