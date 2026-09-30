<script setup lang="ts">
// Узел карты: inbound, группа, urltest, outbound, действие, DNS-сервер или свернутая подписка.
import { Handle, Position, type NodeProps } from '@vue-flow/core'
import { computed } from 'vue'

import { formatRate } from '../../utils/format'
import { kindLabels, plural, type NodeViewData } from './nodeData'

const props = defineProps<NodeProps<NodeViewData>>()

const item = computed(() => props.data.item)
const node = computed(() => item.value.node)

// Узлы, из которых выходят связи: у outbound-ов и действий их нет.
const hasSource = computed(() => ['inbound', 'selector', 'urltest', 'dns-server'].includes(item.value.kind))

const kindLabel = computed(() => {
  if (item.value.kind === 'outbound') {
    return node.value?.type ?? kindLabels.outbound
  }

  return kindLabels[item.value.kind] ?? item.value.kind
})

const title = computed(() => item.value.cluster?.label ?? node.value?.label ?? item.value.id)

const subtitle = computed(() => {
  if (item.value.cluster) {
    const { active, members } = item.value.cluster
    const count = plural(members.length, 'сервер', 'сервера', 'серверов')

    return active.length > 0 ? `→ ${active.join(', ')} · ${count}` : count
  }

  if ((item.value.kind === 'selector' || item.value.kind === 'urltest') && node.value?.now) {
    return `→ ${node.value.now}`
  }

  const detail = node.value?.detail ?? ''
  const address = detail.includes(' · ') ? detail.slice(detail.indexOf(' · ') + 3) : detail

  // У DNS-сервера — через какой outbound уходят запросы.
  if (item.value.kind === 'dns-server') {
    const egress = node.value?.detour_implicit ? 'напрямую' : node.value?.detour

    return egress ? `${address} → ${egress}` : `${address} · без соединений`
  }

  return address
})

const delayClass = computed(() => {
  const delay = node.value?.delay ?? 0

  if (delay <= 0) {
    return ''
  }

  if (delay < 200) {
    return 'd-good'
  }

  return delay < 400 ? 'd-ok' : 'd-slow'
})

const liveText = computed(() => {
  const live = props.data.live

  if (!live || live.conns === 0) {
    return ''
  }

  return `${live.conns} conn · ${formatRate(live.up + live.down)}`
})

// variant — оттенок узла: встроенные direct/block и действия выделяются отдельно.
const variant = computed(() => {
  const type = node.value?.type

  if (item.value.kind === 'action' || type === 'block') {
    return type === 'bypass' ? 'v-bypass' : 'v-block'
  }

  return type === 'direct' ? 'v-direct' : ''
})
</script>

<template>
  <div
    class="tm-node"
    :class="[`k-${item.kind}`, variant, data.state ? `is-${data.state}` : '', { 'is-selected': selected, 'is-live': !!liveText }]"
  >
    <Handle v-if="item.kind !== 'inbound'" type="target" :position="Position.Left" :connectable="false" />

    <div class="tm-node-head">
      <span class="tm-node-kind">{{ kindLabel }}</span>
      <span v-if="delayClass" class="tm-node-delay" :class="delayClass">{{ node?.delay }} ms</span>
    </div>
    <div class="tm-node-title" :title="title">{{ title }}</div>
    <div class="tm-node-sub" :title="node?.detail">
      <span v-if="liveText" class="tm-node-live">{{ liveText }}</span>
      <span v-else>{{ subtitle }}</span>
    </div>

    <Handle v-if="hasSource" type="source" :position="Position.Right" :connectable="false" />
  </div>
</template>
