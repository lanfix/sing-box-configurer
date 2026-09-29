<script setup lang="ts">
// Квадратная кнопка с иконкой. title обязателен: он же подсказка и подпись для экранных дикторов.
import { icons } from '../../icons'
import SvgIcon from '../SvgIcon.vue'

defineProps<{
  icon: keyof typeof icons
  title: string
  danger?: boolean
  disabled?: boolean
}>()
</script>

<template>
  <button type="button" class="icon-btn" :class="{ 'is-danger': danger }" :title="title" :aria-label="title" :disabled="disabled">
    <SvgIcon :path="icons[icon]" />
  </button>
</template>
