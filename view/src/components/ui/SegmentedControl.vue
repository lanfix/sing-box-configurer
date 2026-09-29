<script setup lang="ts" generic="T extends string">
// Выбор одного варианта из нескольких кнопками-сегментами.
const model = defineModel<T>({ required: true })

defineProps<{
  options: { value: T; label: string; title?: string }[]
  disabled?: boolean
  ariaLabel?: string
}>()
</script>

<template>
  <div class="segmented" role="radiogroup" :aria-label="ariaLabel">
    <button
      v-for="option in options"
      :key="option.value"
      type="button"
      role="radio"
      class="segmented-item"
      :class="{ active: model === option.value }"
      :aria-checked="model === option.value"
      :title="option.title"
      :disabled="disabled"
      @click="model = option.value"
    >
      {{ option.label }}
    </button>
  </div>
</template>
