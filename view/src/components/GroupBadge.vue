<script setup lang="ts">
defineProps<{
  group: string
}>()
</script>

<template>
  <span v-if="group === 'bypass'" class="badge badge-bypass" title="sing-box не перехватывает этот трафик">мимо туннеля</span>
  <span v-else-if="group === 'block'" class="badge badge-system" title="Соединения отклоняются">block</span>
  <span v-else class="badge badge-group">{{ group }}</span>
</template>
