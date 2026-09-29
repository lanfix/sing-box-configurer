<script setup lang="ts">
// Переключатель «вкл/выкл» с подписью и пояснением.
const model = defineModel<boolean>({ required: true })

defineProps<{
  label?: string
  description?: string
  disabled?: boolean
}>()
</script>

<template>
  <label class="toggle" :class="{ 'is-disabled': disabled }">
    <input v-model="model" type="checkbox" role="switch" :disabled="disabled">
    <span class="toggle-track"><span class="toggle-thumb"></span></span>
    <span v-if="label || description || $slots.default" class="toggle-text">
      <span v-if="label" class="toggle-label">{{ label }}</span>
      <span v-if="description || $slots.default" class="toggle-description"><slot>{{ description }}</slot></span>
    </span>
  </label>
</template>
