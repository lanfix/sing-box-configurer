<script setup lang="ts">
defineProps<{
  title: string
  wide?: boolean
}>()

const emit = defineEmits<{
  close: []
}>()
</script>

<template>
  <Teleport to="body">
    <div class="modal-overlay" @click.self="emit('close')">
      <div class="modal" :class="{ 'modal-wide': wide }">
        <h3>{{ title }}</h3>
        <div class="modal-body-scroll">
          <slot />
        </div>
      </div>
    </div>
  </Teleport>
</template>
