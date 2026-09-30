<script setup lang="ts">
// Маршрутизатор (route или dns): правила по порядку, из каждой строки выходит связь к ее узлу.
import { Handle, Position, type NodeProps } from '@vue-flow/core'
import { computed } from 'vue'

import type { TopologyRow } from '../../api/types'
import { actionLabels, type NodeViewData } from './nodeData'

const props = defineProps<NodeProps<NodeViewData>>()

const node = computed(() => props.data.item.node)
const rows = computed(() => node.value?.rows ?? [])
const isDNS = computed(() => props.data.item.kind === 'dns-router')

// rowClass возвращает классы строки: служебная, на пути трассировки, выбранная, с трафиком.
function rowClass(row: TopologyRow): Record<string, boolean> {
  const pathRows = props.data.pathRows ?? []

  return {
    'is-service': !row.target,
    'is-final': row.index < 0,
    'is-path': pathRows.includes(row.id),
    'is-dim': pathRows.length > 0 && !pathRows.includes(row.id),
    'is-selected': props.data.selectedRow === row.id,
    'is-live': (props.data.rows?.[row.id]?.conns ?? 0) > 0,
  }
}

// rowBadge возвращает подпись справа: число соединений или действие.
function rowBadge(row: TopologyRow): string {
  const conns = props.data.rows?.[row.id]?.conns ?? 0

  if (conns > 0) {
    return `${conns}`
  }

  return row.action === 'route' ? '' : (actionLabels[row.action] ?? row.action)
}

// selectRow сообщает карте о выборе строки.
function selectRow(row: TopologyRow): void {
  props.data.onRow?.(props.id, row.id)
}
</script>

<template>
  <div class="tm-router" :class="[{ 'is-dns': isDNS, 'is-selected': selected }, data.state ? `is-${data.state}` : '']">
    <Handle type="target" :position="Position.Left" :connectable="false" class="tm-router-in" />

    <div class="tm-router-head">
      <span class="tm-router-title">{{ node?.label }}</span>
      <span class="tm-router-sub">{{ isDNS ? 'DNS-правила по порядку' : 'правила по порядку' }}</span>
    </div>

    <div
      v-for="row in rows"
      :key="row.id"
      class="tm-row"
      :class="rowClass(row)"
      :title="[row.label, row.detail].filter(Boolean).join('\n')"
      @click.stop="selectRow(row)"
    >
      <span class="tm-row-index">{{ row.index < 0 ? '∗' : row.index + 1 }}</span>
      <span class="tm-row-label">{{ row.label }}</span>
      <span v-if="rowBadge(row)" class="tm-row-badge">{{ rowBadge(row) }}</span>
      <Handle v-if="row.target" :id="row.id" type="source" :position="Position.Right" :connectable="false" />
    </div>
  </div>
</template>
