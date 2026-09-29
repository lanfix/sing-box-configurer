<script setup lang="ts">
import { onMounted } from 'vue'
import { useRoute } from 'vue-router'

import AppSidebar from './components/AppSidebar.vue'
import ConfirmHost from './components/ConfirmHost.vue'
import SvgIcon from './components/SvgIcon.vue'
import { icons } from './icons'
import { startConfigStatusPolling } from './stores/configStatus'
import { startSubscriptionAlertsPolling } from './stores/subscriptionAlerts'
import { hideToast, toast } from './stores/toast'
import { initUpdates } from './stores/updates'

const route = useRoute()

onMounted(() => {
  startConfigStatusPolling()
  startSubscriptionAlertsPolling()
  void initUpdates()
})
</script>

<template>
  <div class="app-container">
    <AppSidebar />

    <main class="main-content">
      <div class="top-bar">
        <h2 class="page-title">{{ route.meta.title || 'Sing-Box Configurer' }}</h2>
        <!-- Страницы выводят сюда свои кнопки через Teleport. -->
        <div id="topbar-actions" class="action-buttons"></div>
      </div>

      <div class="content-area">
        <RouterView />
      </div>
    </main>

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
  </div>
</template>
