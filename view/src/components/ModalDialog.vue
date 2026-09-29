<script lang="ts">
// openModals — открытые окна по порядку: Esc закрывает только верхнее (например, подтверждение поверх формы).
const openModals: symbol[] = []
</script>

<script setup lang="ts">
// Модальное окно: заголовок с кнопкой закрытия, прокручиваемое содержимое. Закрывается по Esc и клику мимо окна.
import { nextTick, onBeforeUnmount, onMounted, ref } from 'vue'

import { icons } from '../icons'
import SvgIcon from './SvgIcon.vue'

const props = defineProps<{
  title: string
  subtitle?: string
  wide?: boolean
  // autofocus — поставить курсор в первое поле ввода при открытии.
  autofocus?: boolean
}>()

const emit = defineEmits<{
  close: []
}>()

const body = ref<HTMLElement | null>(null)
const id = Symbol('modal')

// onKeydown закрывает окно по Esc, если оно верхнее.
function onKeydown(event: KeyboardEvent): void {
  if (event.key === 'Escape' && openModals[openModals.length - 1] === id) {
    event.stopImmediatePropagation()
    emit('close')
  }
}

onMounted(async () => {
  openModals.push(id)
  document.addEventListener('keydown', onKeydown)

  if (props.autofocus !== false) {
    await nextTick()

    const field = body.value?.querySelector<HTMLElement>('input:not([disabled]):not([type=checkbox]):not([type=radio]), textarea:not([disabled]), select:not([disabled])')

    field?.focus()
  }
})

onBeforeUnmount(() => {
  openModals.splice(openModals.indexOf(id), 1)
  document.removeEventListener('keydown', onKeydown)
})
</script>

<template>
  <Teleport to="body">
    <div class="modal-overlay" @mousedown.self="emit('close')">
      <div class="modal" :class="{ 'modal-wide': wide }" role="dialog" aria-modal="true" :aria-label="title">
        <div class="modal-header">
          <div class="modal-heading">
            <h3 class="modal-title">{{ title }}</h3>
            <p v-if="subtitle" class="modal-subtitle">{{ subtitle }}</p>
          </div>
          <button type="button" class="icon-btn" title="Закрыть (Esc)" aria-label="Закрыть" @click="emit('close')">
            <SvgIcon :path="icons.close" />
          </button>
        </div>
        <div ref="body" class="modal-body-scroll">
          <slot />
        </div>
      </div>
    </div>
  </Teleport>
</template>
