<script setup lang="ts">
import { onMounted } from 'vue'
import { useRoute } from 'vue-router'

import AppSidebar from './components/AppSidebar.vue'
import { startConfigStatusPolling } from './stores/configStatus'
import { startSubscriptionAlertsPolling } from './stores/subscriptionAlerts'
import { toast } from './stores/toast'
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
        <div v-if="toast" class="message show" :class="toast.type">{{ toast.text }}</div>

        <RouterView />
      </div>
    </main>
  </div>
</template>
