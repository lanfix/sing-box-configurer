<script setup lang="ts">
// Строка страницы настроек: название и пояснение слева, элемент управления справа.
defineProps<{
  title: string
  description?: string
  inputId?: string
}>()
</script>

<template>
  <div class="setting-row">
    <div class="setting-text">
      <label v-if="inputId" class="setting-title" :for="inputId">{{ title }}</label>
      <div v-else class="setting-title">{{ title }}</div>
      <p v-if="description || $slots.description" class="setting-description">
        <slot name="description">{{ description }}</slot>
      </p>
    </div>
    <div class="setting-control">
      <slot />
    </div>
  </div>
</template>
