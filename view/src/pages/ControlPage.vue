<script setup lang="ts">
import { ref } from 'vue'

import { post } from '../api/client'
import SvgIcon from '../components/SvgIcon.vue'
import { icons } from '../icons'
import { showError, showMessage } from '../stores/toast'
import ClashPanel from './control/ClashPanel.vue'
import RestartPanel from './control/RestartPanel.vue'
import SettingsPanel from './control/SettingsPanel.vue'
import UpdatePanel from './control/UpdatePanel.vue'

const restarting = ref(false)

// restart перезапускает sing-box с рабочим конфигом.
async function restart(): Promise<void> {
  if (!confirm('Перезагрузить sing-box с текущим рабочим конфигом?\n\nТекущие соединения будут разорваны.')) {
    return
  }

  restarting.value = true

  try {
    const result = await post('/api/control/reload')

    showMessage(result.message ?? 'sing-box перезапущен')
  } catch (error) {
    showError(error)
  } finally {
    restarting.value = false
  }
}
</script>

<template>
  <Teleport to="#topbar-actions" defer>
    <button class="btn btn-warning" :disabled="restarting" @click="restart">
      <SvgIcon class="btn-icon" :path="icons.restart" />
      {{ restarting ? 'Перезагрузка...' : 'Перезагрузить sing-box' }}
    </button>
  </Teleport>

  <UpdatePanel />
  <SettingsPanel />
  <RestartPanel />
  <ClashPanel />
</template>
