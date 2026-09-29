<script setup lang="ts">
// Подвал формы настроек: показывает несохраненные изменения, кнопки «Отменить» и «Сохранить» (submit формы).
defineProps<{
  dirty: boolean
  saving?: boolean
  saveText?: string
}>()

const emit = defineEmits<{
  reset: []
}>()
</script>

<template>
  <div class="save-bar" :class="{ 'is-dirty': dirty }">
    <span v-if="dirty" class="save-bar-status"><span class="stat-dot is-warn"></span>Есть несохраненные изменения</span>
    <span v-else class="save-bar-status is-clean">Изменений нет</span>
    <span class="save-bar-spacer"></span>
    <button v-if="dirty" type="button" class="btn btn-secondary" :disabled="saving" @click="emit('reset')">Отменить</button>
    <button type="submit" class="btn btn-primary" :disabled="!dirty || saving">{{ saving ? 'Сохранение...' : (saveText ?? 'Сохранить') }}</button>
  </div>
</template>
