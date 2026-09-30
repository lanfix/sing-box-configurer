<script setup lang="ts">
// Боковая панель карты: сведения о выбранном узле, строке или связи, живые соединения и разбор трассировки.
import { computed } from 'vue'

import type { TopologyConnection, TopologyGraph, TopologyNode, TopologyRow, TraceResult } from '../../api/types'
import { formatBytesRu, formatRate } from '../../utils/format'
import SvgIcon from '../SvgIcon.vue'
import { icons } from '../../icons'
import { connectionsOf, type MapModel, type Rate } from './model'
import { actionLabels, kindLabels, plural, type Selection } from './nodeData'

const props = defineProps<{
  selection: Selection | null
  trace: TraceResult | null
  graph: TopologyGraph
  model: MapModel
  connections: TopologyConnection[]
  rates: Record<string, Rate>
  live: boolean
  expanded: string[]
  busy: string
}>()

const emit = defineEmits<{
  close: []
  selectProxy: [group: string, member: string]
  testDelay: [tag: string]
  expand: [label: string]
  collapse: [label: string]
  focus: [nodeId: string]
}>()

// maxConnections — сколько соединений показывать в списке.
const maxConnections = 50

const nodesById = computed(() => new Map(props.graph.nodes.map((node) => [node.id, node])))

const mapNode = computed(() => {
  const selection = props.selection

  if (selection?.type !== 'node') {
    return null
  }

  return props.model.nodes.find((item) => item.id === selection.id) ?? null
})

const node = computed(() => mapNode.value?.node ?? null)

const row = computed<{ node: TopologyNode; row: TopologyRow } | null>(() => {
  const selection = props.selection

  if (selection?.type !== 'row') {
    return null
  }

  const owner = nodesById.value.get(selection.node)
  const found = owner?.rows?.find((item) => item.id === selection.row)

  return owner && found ? { node: owner, row: found } : null
})

const edge = computed(() => {
  const selection = props.selection

  return selection?.type === 'edge' ? props.model.edges.find((item) => item.id === selection.id) ?? null : null
})

// members — участники selector-а или urltest-а с задержками.
const members = computed(() => (node.value?.members ?? []).map((tag) => ({
  tag,
  delay: nodesById.value.get(`out:${tag}`)?.delay ?? 0,
  active: tag === node.value?.now,
})))

// Соединения выбранного узла, строки или связи, по убыванию скорости.
const selectedConnections = computed(() => {
  const selection = props.selection
  let list: TopologyConnection[] = []

  switch (selection?.type) {
    case 'node':
      list = connectionsOf(props.connections, props.model, { node: selection.id })
      break

    case 'edge':
      list = connectionsOf(props.connections, props.model, { edge: selection.id })
      break

    case 'row':
      list = selection.node === 'router' ? connectionsOf(props.connections, props.model, { row: selection.row }) : []
      break

    default:
      list = props.connections
  }

  return sortByRate(list)
})

// topHosts — самые быстрые хосты по всем соединениям.
const topHosts = computed(() => {
  const hosts = new Map<string, { host: string; rate: number; conns: number; outbound: string }>()

  for (const conn of props.connections) {
    const host = conn.host || conn.destination
    const rate = rateOf(conn)
    const entry = hosts.get(host) ?? { host, rate: 0, conns: 0, outbound: conn.chain[conn.chain.length - 1] ?? '' }

    entry.rate += rate.up + rate.down
    entry.conns++
    hosts.set(host, entry)
  }

  return [...hosts.values()].sort((a, b) => b.rate - a.rate || b.conns - a.conns).slice(0, 10)
})

// activeTags — outbound-ы, выбранные сейчас в каком-либо selector-е или urltest-е.
const activeTags = computed(() => new Set(props.graph.nodes.map((item) => item.now).filter(Boolean)))

const groupStats = computed(() => {
  const stats = node.value?.group?.stats

  if (!stats) {
    return []
  }

  return [
    { label: 'Домены', value: stats.domains },
    { label: 'Суффиксы', value: stats.suffixes },
    { label: 'IP и подсети', value: stats.ips },
    { label: 'URL-источники', value: stats.sources ? `${stats.sources} (${stats.source_items})` : 0 },
  ]
})

// rowGroups — группы строки маршрутизатора с их узлами.
const rowGroups = computed(() => (row.value?.row.groups ?? []).map((name) => {
  const selector = props.graph.nodes.find((item) => item.kind === 'selector' && item.label === name)

  return { name, selector, stats: selector?.group?.stats }
}))

// rateOf возвращает текущую скорость соединения.
function rateOf(conn: TopologyConnection): Rate {
  return props.rates[conn.id] ?? { up: 0, down: 0 }
}

// sortByRate сортирует соединения по скорости и обрезает список.
function sortByRate(list: TopologyConnection[]): TopologyConnection[] {
  return [...list]
    .sort((a, b) => {
      const rateA = rateOf(a)
      const rateB = rateOf(b)

      return rateB.up + rateB.down - (rateA.up + rateA.down) || b.download - a.download
    })
    .slice(0, maxConnections)
}

// nodeTitle возвращает подпись узла по идентификатору карты сервера.
function nodeTitle(id?: string): string {
  if (!id) {
    return '—'
  }

  const found = nodesById.value.get(id)

  if (found) {
    return found.label
  }

  return id.replace(/^(out|in|dns|cluster):/, '')
}

// delayClass возвращает класс цвета задержки.
function delayClass(delay: number): string {
  if (delay <= 0) {
    return ''
  }

  if (delay < 200) {
    return 'd-good'
  }

  return delay < 400 ? 'd-ok' : 'd-slow'
}

// matchSource возвращает подпись источника совпавшего правила.
function matchSource(source: string, name?: string): string {
  return source === 'manual' ? 'ручное правило' : `источник «${name}»`
}

const title = computed(() => {
  switch (props.selection?.type) {
    case 'node':
      return mapNode.value?.cluster?.label ?? node.value?.label ?? ''

    case 'row':
      return row.value?.row.label ?? ''

    case 'edge':
      return edge.value ? `${nodeTitle(edge.value.source)} → ${nodeTitle(edge.value.target)}` : ''

    case 'trace':
      return props.trace?.query ?? ''

    default:
      return 'Карта трафика'
  }
})

const kicker = computed(() => {
  switch (props.selection?.type) {
    case 'node':
      return mapNode.value ? (node.value?.kind === 'outbound' ? node.value.type : kindLabels[mapNode.value.kind]) : ''

    case 'row':
      return row.value ? `${row.value.node.label} · правило ${row.value.row.index < 0 ? 'final' : row.value.row.index + 1}` : ''

    case 'edge':
      return 'Связь'

    case 'trace':
      return 'Путь соединения'

    default:
      return 'Подсказка'
  }
})
</script>

<template>
  <aside class="tm-panel" :class="{ 'is-idle': !selection }" aria-label="Сведения о выбранном элементе карты">
    <header class="tm-panel-head">
      <div class="tm-panel-heading">
        <div class="tm-panel-kicker">{{ kicker }}</div>
        <div class="tm-panel-title">{{ title }}</div>
      </div>
      <button v-if="selection" type="button" class="icon-btn" aria-label="Закрыть" @click="emit('close')">
        <SvgIcon :path="icons.close" />
      </button>
    </header>

    <div class="tm-panel-body">
      <!-- Трассировка -->
      <template v-if="selection?.type === 'trace' && trace">
        <dl class="tm-facts">
          <div v-if="trace.domain"><dt>Домен</dt><dd>{{ trace.domain }}</dd></div>
          <div>
            <dt>IP</dt>
            <dd>
              <template v-if="trace.ips.length">{{ trace.ips.slice(0, 4).join(', ') }}<template v-if="trace.ips.length > 4"> и еще {{ trace.ips.length - 4 }}</template></template>
              <template v-else>—</template>
              <span v-if="trace.resolved" class="tm-muted"> ({{ trace.resolved === 'hosts' ? 'DNS-записи' : 'системный резолвер' }})</span>
            </dd>
          </div>
          <div><dt>Inbound</dt><dd>{{ trace.inbound }}</dd></div>
          <div v-if="trace.dns">
            <dt>DNS</dt>
            <dd>
              {{ trace.dns.server || actionLabels[trace.dns.action] || trace.dns.action }}
              <span class="tm-muted">· {{ trace.dns.label }}</span>
            </dd>
          </div>
          <div v-if="trace.dns?.server">
            <dt>DNS-запрос</dt>
            <dd>
              <template v-if="trace.dns.chain.length">
                {{ trace.dns.chain.map((tag) => nodeTitle(`out:${tag}`)).join(' → ') }}
                <span v-if="trace.dns.implicit" class="tm-muted">(detour не задан — напрямую)</span>
              </template>
              <template v-else>без соединений</template>
            </dd>
          </div>
        </dl>

        <div class="tm-result" :class="`a-${trace.action}`">
          <div class="tm-result-label">Итог</div>
          <div v-if="trace.chain.length" class="tm-chain">
            <template v-for="(tag, i) in trace.chain" :key="tag">
              <span v-if="i > 0" class="tm-chain-arrow">→</span>
              <button type="button" class="tm-chain-item" @click="emit('focus', `out:${tag}`)">{{ nodeTitle(`out:${tag}`) }}</button>
            </template>
          </div>
          <div v-else class="tm-chain">{{ actionLabels[trace.action] ?? trace.action }}</div>
        </div>

        <div class="tm-section-title">Правила маршрутизации</div>
        <ol class="tm-steps">
          <li v-for="step in trace.steps" :key="step.row" :class="{ 'is-match': step.row === trace.row, 'is-pass': step.matched && step.row !== trace.row }">
            <div class="tm-step-head">
              <span class="tm-step-mark">{{ step.row === trace.row ? '●' : step.matched ? '↓' : '○' }}</span>
              <span class="tm-step-label">{{ step.label }}</span>
              <span class="tm-step-action">{{ actionLabels[step.action] ?? step.action }}</span>
            </div>
            <div class="tm-step-reason">{{ step.reason }}</div>
            <ul v-if="step.matches?.length" class="tm-matches">
              <li v-for="match in step.matches" :key="`${match.type}:${match.value}:${match.source_name}`">
                <code>{{ match.type }} {{ match.value }}</code> <span class="tm-muted">— {{ matchSource(match.source, match.source_name) }}</span>
              </li>
            </ul>
          </li>
        </ol>

        <ul v-if="trace.notes.length" class="tm-notes">
          <li v-for="note in trace.notes" :key="note">{{ note }}</li>
        </ul>
      </template>

      <!-- Строка маршрутизатора -->
      <template v-else-if="row">
        <dl class="tm-facts">
          <div><dt>Действие</dt><dd>{{ actionLabels[row.row.action] ?? row.row.action }}</dd></div>
          <div v-if="row.row.target"><dt>Куда</dt><dd><button type="button" class="tm-link" @click="emit('focus', row.row.target!)">{{ nodeTitle(row.row.target) }}</button></dd></div>
          <div v-if="row.row.detail"><dt>Условия</dt><dd>{{ row.row.detail }}</dd></div>
        </dl>
        <p v-if="!row.row.target" class="tm-hint">Служебное правило: меняет параметры соединения и передает его следующим правилам.</p>
        <p v-if="row.row.index < 0" class="tm-hint">Сюда попадает трафик, для которого не подошло ни одно правило выше.</p>

        <div v-for="group in rowGroups" :key="group.name" class="tm-group-card">
          <div class="tm-group-name">{{ group.name }}</div>
          <div v-if="group.stats" class="tm-muted">
            {{ plural(group.stats.domains + group.stats.suffixes, 'домен', 'домена', 'доменов') }} · {{ group.stats.ips }} IP
            <template v-if="group.stats.sources"> · {{ plural(group.stats.sources, 'источник', 'источника', 'источников') }}</template>
          </div>
          <RouterLink class="tm-link" :to="{ name: 'groups' }">Открыть группы</RouterLink>
        </div>
      </template>

      <!-- Узел -->
      <template v-else-if="mapNode">
        <dl class="tm-facts">
          <div v-if="node?.detail"><dt>Тип</dt><dd>{{ node.detail }}</dd></div>
          <div v-if="node?.cluster"><dt>Подписка</dt><dd>{{ node.cluster }}</dd></div>
          <div v-if="node?.delay"><dt>Задержка</dt><dd :class="delayClass(node.delay)">{{ node.delay }} ms</dd></div>
          <div v-if="node?.group?.description"><dt>Описание</dt><dd>{{ node.group.description }}</dd></div>
          <div v-if="node?.group?.dns_server"><dt>DNS-сервер</dt><dd>{{ node.group.dns_server }}</dd></div>
          <div v-if="node?.kind === 'dns-server'">
            <dt>Выход</dt>
            <dd>
              <button v-if="node.detour" type="button" class="tm-link" @click="emit('focus', `out:${node.detour}`)">{{ nodeTitle(`out:${node.detour}`) }}</button>
              <template v-else>соединений не создает</template>
            </dd>
          </div>
        </dl>
        <p v-if="node?.detour_implicit" class="tm-hint">
          detour не задан: sing-box соединяется с сервером напрямую, мимо правил маршрутизации — как direct.
          Чтобы запросы шли через прокси, укажите detour в настройках DNS-сервера.
        </p>

        <div v-if="groupStats.length" class="tm-stats">
          <div v-for="stat in groupStats" :key="stat.label" class="tm-stat">
            <div class="tm-stat-value">{{ stat.value }}</div>
            <div class="tm-stat-label">{{ stat.label }}</div>
          </div>
        </div>

        <template v-if="members.length">
          <div class="tm-section-title">
            <span>{{ node?.kind === 'selector' ? 'Выбор узла' : 'Участники' }}</span>
            <button
              v-if="node?.kind === 'urltest'"
              type="button"
              class="btn btn-secondary btn-sm"
              :disabled="busy === node.tag"
              @click="emit('testDelay', node.tag)"
            >{{ busy === node.tag ? 'Замер…' : 'Проверить задержку' }}</button>
          </div>
          <div class="tm-members" role="radiogroup" :aria-label="`Узлы ${node?.label}`">
            <button
              v-for="member in members"
              :key="member.tag"
              type="button"
              class="tm-member"
              :class="{ 'is-active': member.active }"
              :role="node?.kind === 'selector' ? 'radio' : undefined"
              :aria-checked="node?.kind === 'selector' ? member.active : undefined"
              :disabled="node?.kind !== 'selector' || busy === node?.tag"
              @click="emit('selectProxy', node!.tag, member.tag)"
            >
              <span class="tm-member-dot"></span>
              <span class="tm-member-name">{{ member.tag }}</span>
              <span v-if="member.delay" class="tm-member-delay" :class="delayClass(member.delay)">{{ member.delay }} ms</span>
            </button>
          </div>
          <p v-if="node?.kind === 'urltest'" class="tm-hint">URLTest выбирает узел сам — по наименьшей задержке.</p>
        </template>

        <template v-if="mapNode.cluster">
          <div class="tm-section-title">
            <span>Серверы подписки</span>
            <button type="button" class="btn btn-secondary btn-sm" @click="emit('expand', mapNode.cluster.label)">Раскрыть</button>
          </div>
          <ul class="tm-plain-list">
            <li v-for="member in mapNode.cluster.members" :key="member.id" :class="{ 'is-active': activeTags.has(member.tag) }">
              <span>{{ member.label }}</span>
              <span v-if="member.delay" :class="delayClass(member.delay)">{{ member.delay }} ms</span>
            </li>
          </ul>
        </template>

        <button
          v-if="node?.cluster && expanded.includes(node.cluster)"
          type="button"
          class="btn btn-secondary btn-sm tm-block-btn"
          @click="emit('collapse', node.cluster)"
        >Свернуть подписку</button>
      </template>

      <!-- Связь -->
      <template v-else-if="edge">
        <dl class="tm-facts">
          <div><dt>Откуда</dt><dd><button type="button" class="tm-link" @click="emit('focus', edge.source)">{{ nodeTitle(edge.source) }}</button></dd></div>
          <div><dt>Куда</dt><dd><button type="button" class="tm-link" @click="emit('focus', edge.target)">{{ nodeTitle(edge.target) }}</button></dd></div>
          <div><dt>Состояние</dt><dd>{{ edge.active ? 'активна' : 'запасной вариант' }}</dd></div>
        </dl>
      </template>

      <!-- Подсказка -->
      <template v-else>
        <p class="tm-hint">Трафик идет слева направо: inbound → правила маршрутизации по порядку → группа → URLTest → outbound. Толстые линии — активный выбор, остальные варианты видны при выборе узла или с включенным «Все связи».</p>
        <ul class="tm-legend">
          <li><span class="tm-swatch k-inbound"></span>Inbound</li>
          <li><span class="tm-swatch k-router"></span>Правила</li>
          <li><span class="tm-swatch k-selector"></span>Группа (selector)</li>
          <li><span class="tm-swatch k-urltest"></span>URLTest</li>
          <li><span class="tm-swatch k-outbound"></span>Outbound</li>
          <li><span class="tm-swatch k-block"></span>Блокировка</li>
          <li><span class="tm-swatch k-dns"></span>DNS</li>
        </ul>

        <ul v-if="graph.warnings.length" class="tm-notes">
          <li v-for="warning in graph.warnings" :key="warning">{{ warning }}</li>
        </ul>

        <template v-if="live">
          <div class="tm-section-title">Самые активные хосты</div>
          <p v-if="topHosts.length === 0" class="tm-hint">Нет активных соединений.</p>
          <ul v-else class="tm-conns">
            <li v-for="host in topHosts" :key="host.host" class="tm-conn">
              <div class="tm-conn-main">
                <span class="tm-conn-host" :title="host.host">{{ host.host || '—' }}</span>
                <span class="tm-conn-rate">{{ formatRate(host.rate) }}</span>
              </div>
              <div class="tm-conn-sub">{{ plural(host.conns, 'соединение', 'соединения', 'соединений') }} · {{ host.outbound }}</div>
            </li>
          </ul>
        </template>
      </template>

      <!-- Живые соединения выбранного элемента -->
      <template v-if="live && selection && selection.type !== 'trace'">
        <div class="tm-section-title">Соединения <span class="tm-muted">{{ selectedConnections.length }}</span></div>
        <p v-if="selectedConnections.length === 0" class="tm-hint">Сейчас через этот элемент соединений нет.</p>
        <ul v-else class="tm-conns">
          <li v-for="conn in selectedConnections" :key="conn.id" class="tm-conn">
            <div class="tm-conn-main">
              <span class="tm-conn-host" :title="conn.host || conn.destination">{{ conn.host || conn.destination }}</span>
              <span class="tm-conn-rate">{{ formatRate(rateOf(conn).down + rateOf(conn).up) }}</span>
            </div>
            <div class="tm-conn-sub">
              {{ conn.network }} · {{ conn.inbound }} → {{ conn.chain[conn.chain.length - 1] ?? '—' }}
              · ↓ {{ formatBytesRu(conn.download) }} ↑ {{ formatBytesRu(conn.upload) }}
            </div>
          </li>
        </ul>
      </template>
    </div>
  </aside>
</template>
