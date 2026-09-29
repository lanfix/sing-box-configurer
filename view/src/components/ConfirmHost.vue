<script setup lang="ts">
// Диалог подтверждения, который вызывается через confirmAction().
import { nextTick, ref, watch } from 'vue'

import { confirmRequest, resolveConfirm } from '../stores/confirm'
import ModalDialog from './ModalDialog.vue'

const confirmButton = ref<HTMLButtonElement | null>(null)

// Фокус на кнопке подтверждения: Enter подтверждает, Esc отменяет.
watch(confirmRequest, async (request) => {
  if (request) {
    await nextTick()
    confirmButton.value?.focus()
  }
})
</script>

<template>
  <ModalDialog v-if="confirmRequest" :title="confirmRequest.title" :autofocus="false" @close="resolveConfirm(false)">
    <p v-if="confirmRequest.message" class="confirm-message">{{ confirmRequest.message }}</p>
    <div class="form-actions">
      <button type="button" class="btn btn-secondary" @click="resolveConfirm(false)">Отмена</button>
      <button
        ref="confirmButton"
        type="button"
        class="btn"
        :class="confirmRequest.danger ? 'btn-danger' : 'btn-primary'"
        @click="resolveConfirm(true)"
      >
        {{ confirmRequest.confirmText ?? 'Подтвердить' }}
      </button>
    </div>
  </ModalDialog>
</template>
