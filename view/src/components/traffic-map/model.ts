// Модель карты трафика: узлы и связи для отображения (с подписками, свернутыми в кластеры),
// раскладка по колонкам и привязка живых соединений к связям.

import type { TopologyConnection, TopologyEdgeKind, TopologyGraph, TopologyNode, TopologyNodeKind } from '../../api/types'

// Идентификаторы служебных узлов (совпадают с internal/topology).
export const routerId = 'router'
export const dnsRouterId = 'dns'

// clusterMinSize — с какого числа outbound-ов подписка сворачивается в один узел.
const clusterMinSize = 3

// Размеры узлов, по которым считается раскладка (совпадают со стилями узлов).
export const boxWidth = 210
export const boxHeight = 66
export const routerWidth = 290
export const routerHead = 40
export const rowHeight = 30
const routerPad = 8

// Колонки карты: inbound-ы, маршрутизаторы, selector-ы и DNS-серверы, urltest-ы, outbound-ы.
const columnX = [0, 290, 680, 1040, 1330]
const gapY = 14

// dnsGapY — отступ слоя DNS от основных узлов колонки.
const dnsGapY = 60

export type MapNodeKind = TopologyNodeKind | 'cluster'

export interface MapNode {
  id: string
  kind: MapNodeKind
  column: number
  width: number
  height: number
  node?: TopologyNode
  cluster?: { label: string; members: TopologyNode[]; active: string[] }
}

export interface MapEdge {
  id: string
  source: string
  sourceHandle?: string
  target: string
  kind: TopologyEdgeKind
  active: boolean

  // implicit — связь следует из поведения sing-box, а не задана в конфиге (DNS-сервер без detour).
  implicit: boolean

  // originals — связи карты сервера, которые показаны этой связью.
  originals: string[]
}

export interface MapModel {
  nodes: MapNode[]
  edges: MapEdge[]

  // nodeAlias и edgeAlias — где на карте показан узел и связь сервера (с учетом кластеров и скрытых слоев).
  nodeAlias: Record<string, string>
  edgeAlias: Record<string, string>
}

export interface ModelOptions {
  dns: boolean
  expanded: string[]
}

// FlowStats — живой трафик через узел, связь или строку.
export interface FlowStats {
  conns: number
  up: number
  down: number
}

export interface LiveStats {
  nodes: Record<string, FlowStats>
  edges: Record<string, FlowStats>
  rows: Record<string, FlowStats>
}

export interface Rate {
  up: number
  down: number
}

// clusterId возвращает идентификатор узла свернутой подписки.
export function clusterId(label: string): string {
  return `cluster:${label}`
}

// outboundId возвращает идентификатор узла outbound-а.
export function outboundId(tag: string): string {
  return `out:${tag}`
}

// isDNSKind проверяет, что узел этого вида относится к слою DNS.
function isDNSKind(kind: MapNodeKind): boolean {
  return kind === 'dns-router' || kind === 'dns-server'
}

// columnOf возвращает колонку узла.
function columnOf(kind: MapNodeKind): number {
  switch (kind) {
    case 'inbound':
      return 0

    case 'router':
      return 1

    case 'selector':
    case 'dns-router':
      return 2

    case 'urltest':
    case 'dns-server':
      return 3

    default:
      return 4
  }
}

// routerHeight возвращает высоту маршрутизатора с rows строками.
export function routerHeight(rows: number): number {
  return routerHead + rows * rowHeight + routerPad
}

// buildModel собирает узлы и связи для отображения.
export function buildModel(graph: TopologyGraph, options: ModelOptions): MapModel {
  const nodeAlias: Record<string, string> = {}
  const edgeAlias: Record<string, string> = {}
  const nodes: MapNode[] = []
  const clusters = new Map<string, TopologyNode[]>()

  for (const node of graph.nodes) {
    if (node.kind === 'outbound' && node.cluster) {
      clusters.set(node.cluster, [...(clusters.get(node.cluster) ?? []), node])
    }
  }

  const collapsed = new Set([...clusters.entries()]
    .filter(([label, members]) => members.length >= clusterMinSize && !options.expanded.includes(label))
    .map(([label]) => label))

  for (const node of graph.nodes) {
    if (!options.dns && isDNSKind(node.kind)) {
      continue
    }

    if (node.kind === 'outbound' && node.cluster && collapsed.has(node.cluster)) {
      nodeAlias[node.id] = clusterId(node.cluster)

      continue
    }

    nodeAlias[node.id] = node.id

    const height = node.rows ? routerHeight(node.rows.length) : boxHeight

    nodes.push({
      id: node.id,
      kind: node.kind,
      column: columnOf(node.kind),
      width: node.rows ? routerWidth : boxWidth,
      height,
      node,
    })
  }

  const clusterNodes = new Map<string, MapNode>()

  for (const label of collapsed) {
    const node: MapNode = {
      id: clusterId(label),
      kind: 'cluster',
      column: 4,
      width: boxWidth,
      height: boxHeight,
      cluster: { label, members: clusters.get(label) ?? [], active: [] },
    }

    clusterNodes.set(node.id, node)
    nodes.push(node)
  }

  const merged = new Map<string, MapEdge>()
  const tags = new Map(graph.nodes.map((node) => [node.id, node.tag]))

  for (const edge of graph.edges) {
    const source = nodeAlias[edge.source]
    const target = nodeAlias[edge.target]

    if (!source || !target || source === target) {
      continue
    }

    // Участник подписки, выбранный сейчас в группе, показывается на узле кластера.
    const cluster = clusterNodes.get(target)?.cluster
    const member = tags.get(edge.target)

    if (cluster && edge.active && member && !cluster.active.includes(member)) {
      cluster.active.push(member)
    }

    const key = `${source}|${edge.source_handle ?? ''}|${target}`
    const existing = merged.get(key)

    if (existing) {
      existing.active = existing.active || edge.active
      existing.implicit = existing.implicit && !!edge.implicit
      existing.originals.push(edge.id)
      edgeAlias[edge.id] = existing.id

      continue
    }

    const id = source === edge.source && target === edge.target ? edge.id : `m:${key}`

    merged.set(key, {
      id,
      source,
      sourceHandle: edge.source_handle || undefined,
      target,
      kind: edge.kind,
      active: edge.active,
      implicit: !!edge.implicit,
      originals: [edge.id],
    })

    edgeAlias[edge.id] = id
  }

  return { nodes, edges: [...merged.values()], nodeAlias, edgeAlias }
}

// layout раскладывает узлы по колонкам. В каждой колонке узел стремится к средней высоте связей,
// которые в него входят (активные связи весят больше), а пересечения узлов раздвигаются вниз.
export function layout(model: MapModel): Record<string, { x: number; y: number }> {
  const positions: Record<string, { x: number; y: number }> = {}
  const byId = new Map(model.nodes.map((node) => [node.id, node]))
  const incoming = new Map<string, MapEdge[]>()

  for (const edge of model.edges) {
    incoming.set(edge.target, [...(incoming.get(edge.target) ?? []), edge])
  }

  // anchorY — высота, на которой связь выходит из узла-источника.
  const anchorY = (edge: MapEdge): number | null => {
    const source = byId.get(edge.source)
    const position = positions[edge.source]

    if (!source || !position) {
      return null
    }

    const rows = source.node?.rows

    if (edge.sourceHandle && rows) {
      const index = rows.findIndex((row) => row.id === edge.sourceHandle)

      if (index >= 0) {
        return position.y + routerHead + index * rowHeight + rowHeight / 2
      }
    }

    return position.y + source.height / 2
  }

  // Маршрутизатор — опора раскладки.
  const router = byId.get(routerId)

  if (router) {
    positions[router.id] = { x: columnX[1], y: 0 }
  }

  let y = 0

  for (const node of model.nodes.filter((item) => item.column === 0)) {
    positions[node.id] = { x: columnX[0], y }
    y += node.height + gapY
  }

  for (const column of [2, 3, 4]) {
    const items = model.nodes.filter((node) => node.column === column).map((node, order) => {
      let sum = 0
      let weight = 0

      for (const edge of incoming.get(node.id) ?? []) {
        const anchor = anchorY(edge)

        if (anchor === null) {
          continue
        }

        const edgeWeight = edge.active ? 6 : 1

        sum += anchor * edgeWeight
        weight += edgeWeight
      }

      const desired = weight > 0 ? sum / weight - node.height / 2 : Number.POSITIVE_INFINITY

      return { node, desired, order }
    })

    // Слой DNS идет в колонке после основных узлов.
    items.sort((a, b) => Number(isDNSKind(a.node.kind)) - Number(isDNSKind(b.node.kind)) || a.desired - b.desired || a.order - b.order)

    let bottom = Number.NEGATIVE_INFINITY
    let previousDNS = false

    for (const item of items) {
      const gap = isDNSKind(item.node.kind) && bottom > Number.NEGATIVE_INFINITY && !previousDNS ? dnsGapY : gapY
      const top = Number.isFinite(item.desired) ? Math.max(item.desired, bottom + gap) : Math.max(bottom + gap, 0)

      positions[item.node.id] = { x: columnX[column], y: Math.round(top) }
      bottom = top + item.node.height
      previousDNS = isDNSKind(item.node.kind)
    }
  }

  return positions
}

// connectionPath возвращает связи и узлы карты сервера, через которые проходит соединение.
export function connectionPath(conn: TopologyConnection): { edges: string[]; nodes: string[] } {
  const edges = [`e:in:${conn.inbound}`]
  const nodes = [`in:${conn.inbound}`, routerId]

  if (conn.row) {
    edges.push(`e:router:${conn.row}`)
  }

  conn.chain.forEach((tag, i) => {
    nodes.push(outboundId(tag))

    if (i > 0) {
      edges.push(`e:${outboundId(conn.chain[i - 1])}>${outboundId(tag)}`)
    }
  })

  return { edges, nodes }
}

// emptyStats возвращает нулевую статистику.
function emptyStats(): FlowStats {
  return { conns: 0, up: 0, down: 0 }
}

// addStats прибавляет соединение со скоростью rate к статистике key.
function addStats(target: Record<string, FlowStats>, key: string | undefined, rate: Rate): void {
  if (!key) {
    return
  }

  const stats = target[key] ?? (target[key] = emptyStats())

  stats.conns++
  stats.up += rate.up
  stats.down += rate.down
}

// aggregate считает соединения и скорость по узлам, связям и строкам маршрутизатора карты.
export function aggregate(connections: TopologyConnection[], rates: Record<string, Rate>, model: MapModel): LiveStats {
  const stats: LiveStats = { nodes: {}, edges: {}, rows: {} }

  for (const conn of connections) {
    const rate = rates[conn.id] ?? { up: 0, down: 0 }
    const path = connectionPath(conn)

    for (const id of new Set(path.edges.map((edge) => model.edgeAlias[edge]))) {
      addStats(stats.edges, id, rate)
    }

    for (const id of new Set(path.nodes.map((node) => model.nodeAlias[node]))) {
      addStats(stats.nodes, id, rate)
    }

    addStats(stats.rows, conn.row || undefined, rate)
  }

  return stats
}

// connectionsOf возвращает соединения, которые проходят через узел, связь или строку карты.
export function connectionsOf(
  connections: TopologyConnection[],
  model: MapModel,
  target: { node?: string; edge?: string; row?: string },
): TopologyConnection[] {
  return connections.filter((conn) => {
    if (target.row) {
      return conn.row === target.row
    }

    const path = connectionPath(conn)

    if (target.edge) {
      return path.edges.some((edge) => model.edgeAlias[edge] === target.edge)
    }

    return path.nodes.some((node) => model.nodeAlias[node] === target.node)
  })
}
