<script setup lang="ts">
// Карта трафика рабочего конфига sing-box: inbound-ы → правила → группы → urltest-ы → outbound-ы.
// Живой режим привязывает соединения Clash API к связям, поиск показывает путь домена или IP.
import { Background } from '@vue-flow/background'
import { Controls } from '@vue-flow/controls'
import { VueFlow, useVueFlow, type Edge, type EdgeMouseEvent, type Node, type NodeDragEvent, type NodeMouseEvent } from '@vue-flow/core'
import { MiniMap } from '@vue-flow/minimap'
import { computed, nextTick, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'

import '@vue-flow/core/dist/style.css'
import '@vue-flow/controls/dist/style.css'
import '@vue-flow/minimap/dist/style.css'
import './traffic-map.css'

import { get, post } from '../../api/client'
import type { TopologyConnection, TopologyGraph, TraceResult } from '../../api/types'
import { icons } from '../../icons'
import { showError } from '../../stores/toast'
import { formatRate } from '../../utils/format'
import SvgIcon from '../SvgIcon.vue'
import BoxNode from './BoxNode.vue'
import { aggregate, boxHeight, boxWidth, buildModel, layout, type LiveStats, type MapModel, type Rate } from './model'
import type { NodeState, NodeViewData, Selection } from './nodeData'
import RouterNode from './RouterNode.vue'
import TrafficPanel from './TrafficPanel.vue'

// Интервалы опроса: карта — для активных узлов и задержек, соединения — для живого режима.
const graphInterval = 5000
const liveInterval = 2000

const prefsKey = 'traffic-map-prefs'
const positionsKey = 'traffic-map-positions'

// Панель поверх карты на узком экране (совпадает с traffic-map.css).
const narrowQuery = '(max-width: 1200px)'
const panelWidth = 330

interface Prefs {
  live: boolean
  dns: boolean
  allEdges: boolean
  tall: boolean
  expanded: string[]
}

const flowId = 'traffic-map'
const { setNodes, updateNodeData, fitView, findNode, setCenter, setViewport, getNodes, onNodesInitialized } = useVueFlow(flowId)

const stage = ref<HTMLElement | null>(null)
const graph = ref<TopologyGraph | null>(null)
const loadError = ref('')
const connections = ref<TopologyConnection[]>([])
const rates = ref<Record<string, Rate>>({})
const liveError = ref('')
const selection = ref<Selection | null>(null)
const busy = ref('')

const query = ref('')
const traceInbound = ref('')
const trace = ref<TraceResult | null>(null)
const tracing = ref(false)
const traceError = ref('')

const prefs = reactive<Prefs>(readStorage<Prefs>(prefsKey, {
  live: true,
  dns: false,
  allEdges: false,
  tall: false,
  expanded: [],
}))

// saved — позиции узлов, перетащенных вручную.
let saved: Record<string, { x: number; y: number }> = readStorage(positionsKey, {})
const layoutVersion = ref(0)
let fitted = false

// Предыдущий снимок счетчиков соединений: по разнице считается скорость.
let previous = new Map<string, { up: number; down: number; at: number }>()

let graphTimer: ReturnType<typeof setInterval> | null = null
let liveTimer: ReturnType<typeof setInterval> | null = null

// readStorage читает значение из localStorage (в приватном режиме хранилище может быть недоступно).
function readStorage<T>(key: string, fallback: T): T {
  try {
    const raw = localStorage.getItem(key)

    return raw ? { ...fallback, ...JSON.parse(raw) } : fallback
  } catch {
    return fallback
  }
}

// writeStorage сохраняет значение в localStorage.
function writeStorage(key: string, value: unknown): void {
  try {
    localStorage.setItem(key, JSON.stringify(value))
  } catch {
    // Хранилище недоступно: настройки живут до перезагрузки страницы.
  }
}

watch(prefs, () => writeStorage(prefsKey, prefs), { deep: true })

const model = computed<MapModel | null>(() => (graph.value ? buildModel(graph.value, { dns: prefs.dns, expanded: prefs.expanded }) : null))

const inbounds = computed(() => graph.value?.nodes.filter((node) => node.kind === 'inbound') ?? [])

const liveStats = computed<LiveStats | null>(() => (prefs.live && model.value ? aggregate(connections.value, rates.value, model.value) : null))

const totalRate = computed(() => Object.values(rates.value).reduce((sum, rate) => sum + rate.up + rate.down, 0))

// Путь трассировки в идентификаторах отображаемых узлов и связей.
const tracePath = computed(() => {
  if (!trace.value || !model.value) {
    return null
  }

  const aliasNodes = model.value.nodeAlias
  const aliasEdges = model.value.edgeAlias

  // Путь DNS-запроса подсвечивается вместе со слоем DNS: без него outbound-ы DNS-сервера выглядели бы
  // частью пути соединения.
  const nodes = prefs.dns ? [...trace.value.nodes, ...trace.value.dns_nodes] : trace.value.nodes
  const edges = prefs.dns ? [...trace.value.edges, ...trace.value.dns_edges] : trace.value.edges

  return {
    nodes: new Set(nodes.map((id) => aliasNodes[id]).filter(Boolean)),
    edges: new Set(edges.map((id) => aliasEdges[id]).filter(Boolean)),
    rows: [trace.value.row, prefs.dns ? trace.value.dns?.row ?? '' : ''].filter(Boolean),
  }
})

const selectedNodeId = computed(() => {
  const current = selection.value

  if (current?.type === 'node') {
    return current.id
  }

  return current?.type === 'row' ? current.node : ''
})

// nodeState возвращает подсветку узла по трассировке.
function nodeState(id: string): NodeState {
  if (!tracePath.value) {
    return ''
  }

  return tracePath.value.nodes.has(id) ? 'path' : 'dim'
}

// onRow выбирает строку маршрутизатора.
function onRow(nodeId: string, rowId: string): void {
  selection.value = { type: 'row', node: nodeId, row: rowId }
}

// nodeData — данные компонентов узлов, пересчитываются при каждом обновлении.
const nodeData = computed<Record<string, NodeViewData>>(() => {
  const result: Record<string, NodeViewData> = {}
  const current = selection.value

  for (const item of model.value?.nodes ?? []) {
    result[item.id] = {
      item,
      state: nodeState(item.id),
      live: liveStats.value?.nodes[item.id],
      rows: item.node?.rows ? liveStats.value?.rows : undefined,
      pathRows: tracePath.value?.rows,
      selectedRow: current?.type === 'row' && current.node === item.id ? current.row : undefined,
      onRow,
    }
  }

  return result
})

// Структура карты: при ее изменении узлы расставляются заново.
const structureKey = computed(() => `${layoutVersion.value}#${(model.value?.nodes ?? []).map((item) => `${item.id}:${item.height}`).join('|')}`)

watch(structureKey, () => {
  if (!model.value) {
    return
  }

  const auto = layout(model.value)

  const nodes: Node<NodeViewData>[] = model.value.nodes.map((item) => ({
    id: item.id,
    type: item.node?.rows ? 'router' : 'box',
    position: saved[item.id] ?? auto[item.id] ?? { x: 0, y: 0 },
    data: nodeData.value[item.id],
    connectable: false,
  }))

  setNodes(nodes)
})

// Вид подгоняется под карту, когда узлы впервые измерены: до этого их размеры неизвестны.
onNodesInitialized(() => {
  if (fitted) {
    return
  }

  fitted = true
  void fitView({ padding: 0.08 })
})

watch(nodeData, (data) => {
  for (const [id, value] of Object.entries(data)) {
    if (findNode(id)) {
      updateNodeData(id, value, { replace: true })
    }
  }
})

// edgeWidth возвращает толщину связи по скорости трафика.
function edgeWidth(rate: number): number {
  return Math.min(7, 2.2 + Math.log10(1 + rate / 1024) * 1.4)
}

const flowEdges = computed<Edge[]>(() => {
  const current = model.value

  if (!current) {
    return []
  }

  const path = tracePath.value
  const focus = selectedNodeId.value
  const selectedEdge = selection.value?.type === 'edge' ? selection.value.id : ''

  return current.edges
    .filter((edge) => edge.active || prefs.allEdges || edge.source === focus || edge.target === focus || path?.edges.has(edge.id))
    .map((edge) => {
      const live = liveStats.value?.edges[edge.id]
      const rate = live ? live.up + live.down : 0
      const onPath = path?.edges.has(edge.id) ?? false
      const classes = [`k-${edge.kind}`, edge.active ? 'is-active' : 'is-idle', edge.implicit ? 'is-implicit' : '']

      if (path) {
        classes.push(onPath ? 'is-path' : 'is-dim')
      }

      if (live?.conns) {
        classes.push('is-live')
      }

      if (edge.id === selectedEdge || (focus && (edge.source === focus || edge.target === focus))) {
        classes.push('is-focus')
      }

      let width = edge.active ? 1.8 : 1.1

      if (live?.conns) {
        width = edgeWidth(rate)
      }

      if (onPath) {
        width = Math.max(width, 3)
      }

      return {
        id: edge.id,
        source: edge.source,
        target: edge.target,
        sourceHandle: edge.sourceHandle,
        type: 'default',
        animated: !!live?.conns && !path,
        class: classes.join(' '),
        style: { strokeWidth: width },
        label: live?.conns && rate >= 1024 ? formatRate(rate) : undefined,
        labelBgPadding: [4, 2] as [number, number],
        labelBgBorderRadius: 4,
        interactionWidth: 14,
      }
    })
})

// loadGraph загружает карту. silent — фоновое обновление без сообщений об ошибках.
async function loadGraph(silent = false): Promise<void> {
  try {
    graph.value = await get<TopologyGraph>('/api/topology')
    loadError.value = ''
  } catch (error) {
    if (!silent || !graph.value) {
      loadError.value = error instanceof Error ? error.message : String(error)
    }
  }
}

// loadConnections загружает соединения и считает их скорость по разнице с прошлым снимком.
async function loadConnections(): Promise<void> {
  try {
    const data = await get<{ connections: TopologyConnection[] }>('/api/topology/connections')
    const now = Date.now()
    const next = new Map<string, { up: number; down: number; at: number }>()
    const nextRates: Record<string, Rate> = {}

    for (const conn of data.connections ?? []) {
      const before = previous.get(conn.id)

      if (before) {
        const seconds = Math.max((now - before.at) / 1000, 0.5)

        nextRates[conn.id] = {
          up: Math.max(0, (conn.upload - before.up) / seconds),
          down: Math.max(0, (conn.download - before.down) / seconds),
        }
      }

      next.set(conn.id, { up: conn.upload, down: conn.download, at: now })
    }

    previous = next
    rates.value = nextRates
    connections.value = data.connections ?? []
    liveError.value = ''
  } catch (error) {
    liveError.value = error instanceof Error ? error.message : String(error)
  }
}

// startLive включает или выключает опрос соединений.
function startLive(enabled: boolean): void {
  if (liveTimer) {
    clearInterval(liveTimer)
    liveTimer = null
  }

  previous = new Map()
  rates.value = {}
  connections.value = []

  if (!enabled) {
    return
  }

  void loadConnections()
  liveTimer = setInterval(() => void loadConnections(), liveInterval)
}

watch(() => prefs.live, startLive)

onMounted(() => {
  void loadGraph()
  graphTimer = setInterval(() => void loadGraph(true), graphInterval)
  startLive(prefs.live)
})

onBeforeUnmount(() => {
  if (graphTimer) {
    clearInterval(graphTimer)
  }

  if (liveTimer) {
    clearInterval(liveTimer)
  }
})

// runTrace ищет путь соединения к домену или IP.
async function runTrace(): Promise<void> {
  const value = query.value.trim()

  if (!value) {
    traceError.value = 'Введите домен или IP-адрес'

    return
  }

  tracing.value = true
  traceError.value = ''

  try {
    const params = new URLSearchParams({ query: value })

    if (traceInbound.value) {
      params.set('inbound', traceInbound.value)
    }

    trace.value = await get<TraceResult>(`/api/topology/trace?${params}`)
    selection.value = { type: 'trace' }

    await nextTick()
    await fitPath()
  } catch (error) {
    traceError.value = error instanceof Error ? error.message : String(error)
  } finally {
    tracing.value = false
  }
}

// fitPath подгоняет вид под путь трассировки. На узком экране панель лежит поверх карты справа,
// поэтому путь вписывается в оставшуюся слева часть.
async function fitPath(): Promise<void> {
  const flow = stage.value?.querySelector('.tm-flow')
  const ids = tracePath.value?.nodes ?? new Set<string>()
  const nodes = getNodes.value.filter((node) => ids.has(node.id))

  if (!flow || nodes.length === 0) {
    return
  }

  const left = Math.min(...nodes.map((node) => node.position.x))
  const top = Math.min(...nodes.map((node) => node.position.y))
  const right = Math.max(...nodes.map((node) => node.position.x + (node.dimensions.width || boxWidth)))
  const bottom = Math.max(...nodes.map((node) => node.position.y + (node.dimensions.height || boxHeight)))
  const overlay = window.matchMedia(narrowQuery).matches ? panelWidth + 20 : 0
  const width = Math.max(flow.clientWidth - overlay, 200)
  const height = flow.clientHeight
  const zoom = Math.min((width * 0.9) / (right - left), (height * 0.9) / (bottom - top), 1.2)

  await setViewport({
    x: (width - (right - left) * zoom) / 2 - left * zoom,
    y: (height - (bottom - top) * zoom) / 2 - top * zoom,
    zoom,
  }, { duration: 400 })
}

// clearTrace снимает подсветку трассировки.
function clearTrace(): void {
  trace.value = null
  traceError.value = ''

  if (selection.value?.type === 'trace') {
    selection.value = null
  }
}

function onNodeClick(event: NodeMouseEvent): void {
  selection.value = { type: 'node', id: event.node.id }
}

// onNodeDoubleClick раскрывает свернутую подписку.
function onNodeDoubleClick(event: NodeMouseEvent): void {
  const cluster = model.value?.nodes.find((item) => item.id === event.node.id)?.cluster

  if (cluster) {
    expand(cluster.label)
  }
}

function onEdgeClick(event: EdgeMouseEvent): void {
  selection.value = { type: 'edge', id: event.edge.id }
}

function onPaneClick(): void {
  selection.value = trace.value ? { type: 'trace' } : null
}

// onDragStop запоминает положение перетащенных узлов.
function onDragStop(event: NodeDragEvent): void {
  for (const node of event.nodes) {
    saved[node.id] = { x: Math.round(node.position.x), y: Math.round(node.position.y) }
  }

  writeStorage(positionsKey, saved)
}

// resetLayout возвращает автоматическую раскладку.
function resetLayout(): void {
  saved = {}
  writeStorage(positionsKey, saved)
  layoutVersion.value++
  void nextTick(() => setTimeout(() => fitView({ padding: 0.12, duration: 300 }), 50))
}

// expand раскрывает подписку.
function expand(label: string): void {
  if (!prefs.expanded.includes(label)) {
    prefs.expanded.push(label)
  }

  selection.value = null
}

// collapse сворачивает подписку.
function collapse(label: string): void {
  prefs.expanded = prefs.expanded.filter((item) => item !== label)
  selection.value = null
}

// focusNode выбирает узел и центрирует на нем карту.
function focusNode(id: string): void {
  const target = model.value?.nodeAlias[id] ?? id
  const node = findNode(target)

  if (!node) {
    return
  }

  selection.value = { type: 'node', id: target }
  void setCenter(node.position.x + (node.dimensions.width || 200) / 2, node.position.y + (node.dimensions.height || 60) / 2, { zoom: 1, duration: 400 })
}

// selectProxy переключает участника selector-а.
async function selectProxy(group: string, member: string): Promise<void> {
  busy.value = group

  try {
    await post('/api/clash/proxies/select', { group, name: member })
    await loadGraph(true)
  } catch (error) {
    showError(error, 'Не удалось переключить')
  } finally {
    busy.value = ''
  }
}

// testDelay замеряет задержку участников группы.
async function testDelay(tag: string): Promise<void> {
  busy.value = tag

  try {
    await get(`/api/clash/group/delay?group=${encodeURIComponent(tag)}`)
    await loadGraph(true)
  } catch (error) {
    showError(error, 'Замер не удался')
  } finally {
    busy.value = ''
  }
}

// minimapColor возвращает цвет узла на миникарте.
function minimapColor(node: Node): string {
  const kind = (node.data as NodeViewData | undefined)?.item.kind ?? ''

  const colors: Record<string, string> = {
    inbound: '#3fb950',
    router: '#8b949e',
    'dns-router': '#39c5cf',
    selector: '#a371f7',
    urltest: '#d2a8ff',
    outbound: '#58a6ff',
    cluster: '#58a6ff',
    action: '#f85149',
    'dns-server': '#39c5cf',
  }

  return colors[kind] ?? '#8b949e'
}
</script>

<template>
  <div class="add-form-card tm-card" :class="{ 'is-tall': prefs.tall }">
    <div class="tm-toolbar">
      <form class="tm-search" role="search" @submit.prevent="runTrace">
        <div class="tm-search-field" :class="{ 'has-error': traceError }">
          <SvgIcon class="tm-search-icon" :path="icons.search" />
          <input
            v-model="query"
            class="form-input"
            type="text"
            placeholder="youtube.com или 1.1.1.1"
            aria-label="Домен или IP для поиска пути"
            spellcheck="false"
            autocomplete="off"
            @input="traceError = ''"
          />
        </div>
        <select v-model="traceInbound" class="form-select tm-inbound" aria-label="Inbound, через который приходит соединение">
          <option value="">Inbound: по умолчанию</option>
          <option v-for="inbound in inbounds" :key="inbound.id" :value="inbound.tag">{{ inbound.tag }}</option>
        </select>
        <button type="submit" class="btn btn-primary" :disabled="tracing">{{ tracing ? 'Поиск…' : 'Найти путь' }}</button>
        <button v-if="trace" type="button" class="btn btn-ghost" @click="clearTrace">Сбросить</button>
      </form>

      <div class="tm-toggles">
        <button type="button" class="tm-chip" :class="{ 'is-on': prefs.live }" :aria-pressed="prefs.live" @click="prefs.live = !prefs.live">
          <span class="tm-chip-dot" :class="{ 'is-bad': prefs.live && liveError }"></span>Живой режим
          <span v-if="prefs.live && !liveError" class="tm-chip-value">{{ formatRate(totalRate) }}</span>
        </button>
        <button type="button" class="tm-chip" :class="{ 'is-on': prefs.dns }" :aria-pressed="prefs.dns" @click="prefs.dns = !prefs.dns">DNS</button>
        <button type="button" class="tm-chip" :class="{ 'is-on': prefs.allEdges }" :aria-pressed="prefs.allEdges" @click="prefs.allEdges = !prefs.allEdges">Все связи</button>
        <button type="button" class="btn btn-ghost btn-sm" title="Вернуть автоматическую раскладку" @click="resetLayout">Раскладка</button>
        <button type="button" class="btn btn-ghost btn-sm" @click="prefs.tall = !prefs.tall">{{ prefs.tall ? 'Свернуть' : 'Развернуть' }}</button>
      </div>
    </div>

    <div v-if="traceError" class="field-error tm-trace-error">{{ traceError }}</div>

    <div ref="stage" class="tm-stage">
      <div v-if="loadError && !graph" class="tm-empty">
        <div>Карта недоступна</div>
        <div class="card-hint">{{ loadError }}</div>
        <button type="button" class="btn btn-secondary btn-sm" @click="loadGraph()">Повторить</button>
      </div>

      <VueFlow
        v-else
        :id="flowId"
        class="tm-flow"
        :edges="flowEdges"
        :min-zoom="0.15"
        :max-zoom="2"
        :nodes-connectable="false"
        :edges-updatable="false"
        :elevate-edges-on-select="false"
        :zoom-on-double-click="false"
        :delete-key-code="null"
        @node-click="onNodeClick"
        @node-double-click="onNodeDoubleClick"
        @edge-click="onEdgeClick"
        @pane-click="onPaneClick"
        @node-drag-stop="onDragStop"
      >
        <template #node-box="props">
          <BoxNode v-bind="props" />
        </template>
        <template #node-router="props">
          <RouterNode v-bind="props" />
        </template>

        <Background pattern-color="#21262d" :gap="22" />
        <MiniMap position="bottom-left" pannable zoomable :node-color="minimapColor" mask-color="rgba(13, 17, 23, 0.7)" :width="170" :height="110" />
        <Controls :show-interactive="false" position="top-left" />
      </VueFlow>

      <TrafficPanel
        v-if="graph && model"
        :selection="selection"
        :trace="trace"
        :graph="graph"
        :model="model"
        :connections="connections"
        :rates="rates"
        :live="prefs.live"
        :expanded="prefs.expanded"
        :busy="busy"
        @close="selection = trace && selection?.type !== 'trace' ? { type: 'trace' } : null"
        @select-proxy="selectProxy"
        @test-delay="testDelay"
        @expand="expand"
        @collapse="collapse"
        @focus="focusNode"
      />
      <!-- Место под панель занято сразу: иначе карта сузится уже после вписывания. -->
      <div v-else class="tm-panel is-idle"></div>
    </div>
  </div>
</template>
