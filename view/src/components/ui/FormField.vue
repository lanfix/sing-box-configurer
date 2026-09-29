<script setup lang="ts">
// Поле формы: подпись с пометкой «необязательно», элемент ввода, подсказка или ошибка под ним.
defineProps<{
  label: string
  inputId?: string
  hint?: string
  error?: string
  optional?: boolean
}>()
</script>

<template>
  <div class="field" :class="{ 'has-error': Boolean(error) }">
    <label class="field-label" :for="inputId">
      {{ label }}
      <span v-if="optional" class="field-optional">необязательно</span>
    </label>
    <slot />
    <p v-if="error" class="field-error">{{ error }}</p>
    <p v-else-if="hint || $slots.hint" class="field-hint"><slot name="hint">{{ hint }}</slot></p>
  </div>
</template>
