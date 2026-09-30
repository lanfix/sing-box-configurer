<script setup lang="ts">
// Выбор outbound-а из списка, сгруппированного по источникам: встроенные, URLTest, вручную, подписки.
import { computed } from 'vue'

import type { OutboundSource, OutboundView } from '../api/types'

const model = defineModel<string>({ required: true })

const props = defineProps<{
  outbounds: OutboundView[]
  inputId?: string
  // emptyLabel — подпись пустого значения; без нее пустое значение не предлагается.
  emptyLabel?: string
}>()

// sourceTitles — заголовки групп списка в порядке показа.
const sourceTitles: [OutboundSource, string][] = [
  ['group', 'Группы (outbound, выбранный в группе)'],
  ['builtin', 'Встроенные'],
  ['urltest', 'Автовыбор самого быстрого (URLTest)'],
  ['manual', 'Добавленные вручную'],
  ['happ', 'Подписки Happ'],
  ['amnezia', 'Amnezia'],
]

const sections = computed(() => sourceTitles
  .map(([source, title]) => ({ source, title, items: props.outbounds.filter((outbound) => outbound.source === source) }))
  .filter((section) => section.items.length > 0))

// missing — выбранный outbound пропал из списка (например, удален urltest).
const missing = computed(() => Boolean(model.value) && !props.outbounds.some((outbound) => outbound.tag === model.value))

// label возвращает подпись outbound-а.
function label(outbound: OutboundView): string {
  if (outbound.source === 'builtin' && outbound.source_name) {
    return `${outbound.tag} — ${outbound.source_name.toLowerCase()}`
  }

  return outbound.source_name && outbound.source !== 'manual' ? `${outbound.tag} (${outbound.source_name})` : outbound.tag
}
</script>

<template>
  <select :id="inputId" v-model="model" class="form-select">
    <option v-if="emptyLabel !== undefined" value="">{{ emptyLabel }}</option>
    <option v-if="missing" :value="model">{{ model }} — не найден</option>
    <optgroup v-for="section in sections" :key="section.source" :label="section.title">
      <option v-for="outbound in section.items" :key="outbound.tag" :value="outbound.tag">{{ label(outbound) }}</option>
    </optgroup>
  </select>
</template>
