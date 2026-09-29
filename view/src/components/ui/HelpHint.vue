<script setup lang="ts">
// Сворачиваемое пояснение «Как это работает»: подробности не занимают место, пока не нужны.
import { icons } from '../../icons'
import SvgIcon from '../SvgIcon.vue'

withDefaults(defineProps<{
  title?: string
}>(), {
  title: 'Как это работает',
})
</script>

<template>
  <details class="help-hint">
    <summary>
      <SvgIcon :path="icons.info" />
      <span>{{ title }}</span>
    </summary>
    <div class="help-hint-body">
      <slot />
    </div>
  </details>
</template>
