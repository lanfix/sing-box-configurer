<script setup lang="ts">
// Активные соединения sing-box: кто и куда подключен, через какую группу и outbound, скорость и трафик.
// Соединения можно закрыть, а домен или IP — добавить в группу правил.
import { computed, ref } from 'vue'

import { get, postQuiet } from '../api/client'
import type { Connection, ConnectionsSnapshot } from '../api/types'
import AddRuleDialog from '../components/AddRuleDialog.vue'
import GroupBadge from '../components/GroupBadge.vue'
import SvgIcon from '../components/SvgIcon.vue'
import FormField from '../components/ui/FormField.vue'
import IconButton from '../components/ui/IconButton.vue'
import { usePolling } from '../composables/usePolling'
import { icons } from '../icons'
import { confirmAction } from '../stores/confirm'
import { showError, showMessage } from '../stores/toast'
import { formatBytesRu, formatDuration, formatRate, splitBytes } from '../utils/format'

type SortKey = 'rate' | 'traffic' | 'start' | 'host'

// pollInterval — период обновления списка, мс.
const pollInterval = 2000

// maxRows — сколько строк показывать: тысячи строк тормозят страницу.
const maxRows = 300

// serviceInbound — служебный inbound конфигуратора (загрузка источников и тест скорости).
const serviceInbound = 'configurer-sources'

const snapshot = ref<ConnectionsSnapshot | null>(null)
const failed = ref('')
const paused = ref(false)
const search = ref('')
const groupFilter = ref('')
const outboundFilter = ref('')
const networkFilter = ref('')
const sortKey = ref<SortKey>('rate')
const now = ref(Date.now())
const addTarget = ref<{ target: string } | null>(null)
const closing = ref(false)

// Прошлые значения трафика соединений для расчета скорости.
let previous = new Map<string, { up: number; down: number; at: number }>()
const rates = ref(new Map<string, { up: number; down: number }>())

// load запрашивает соединения и считает скорость по разнице с прошлым запросом.
async function load(): Promise<void> {
  if (paused.value) {
    return
  }

  try {
    const data = await get<ConnectionsSnapshot>('/api/connections')
    const at = Date.now()
    const nextPrevious = new Map<string, { up: number; down: number; at: number }>()
    const nextRates = new Map<string, { up: number; down: number }>()

    for (const conn of data.connections ?? []) {
      const before = previous.get(conn.id)

      if (before && at > before.at) {
        const seconds = (at - before.at) / 1000

        nextRates.set(conn.id, {
          up: Math.max(0, (conn.upload - before.up) / seconds),
          down: Math.max(0, (conn.download - before.down) / seconds),
        })
      }

      nextPrevious.set(conn.id, { up: conn.upload, down: conn.download, at })
    }

    previous = nextPrevious
    rates.value = nextRates
    snapshot.value = data
    now.value = at
    failed.value = ''
  } catch (error) {
    failed.value = error instanceof Error ? error.message : String(error)
  }
}

usePolling(load, pollInterval)

const connections = computed(() => snapshot.value?.connections ?? [])

// rateOf возвращает скорость соединения (0, пока нет второго замера).
function rateOf(conn: Connection): { up: number; down: number } {
  return rates.value.get(conn.id) ?? { up: 0, down: 0 }
}

const totalRate = computed(() => connections.value.reduce((sum, conn) => {
  const rate = rateOf(conn)

  return { up: sum.up + rate.up, down: sum.down + rate.down }
}, { up: 0, down: 0 }))

const groups = computed(() => [...new Set(connections.value.map((conn) => conn.group).filter(Boolean))].sort() as string[])
const outbounds = computed(() => [...new Set(connections.value.map((conn) => conn.outbound).filter(Boolean))].sort())

// filtered — соединения с учетом фильтров, отсортированные.
const filtered = computed(() => {
  const query = search.value.trim().toLowerCase()

  const list = connections.value.filter((conn) => {
    if (groupFilter.value && conn.group !== groupFilter.value) {
      return false
    }

    if (outboundFilter.value && conn.outbound !== outboundFilter.value) {
      return false
    }

    if (networkFilter.value && conn.network !== networkFilter.value) {
      return false
    }

    if (!query) {
      return true
    }

    return [conn.host, conn.destination, conn.source, conn.rule, conn.outbound, conn.process ?? '', ...conn.chain]
      .some((value) => value.toLowerCase().includes(query))
  })

  const compare: Record<SortKey, (a: Connection, b: Connection) => number> = {
    rate: (a, b) => (rateOf(b).down + rateOf(b).up) - (rateOf(a).down + rateOf(a).up) || (b.download + b.upload) - (a.download + a.upload),
    traffic: (a, b) => (b.download + b.upload) - (a.download + a.upload),
    start: (a, b) => Date.parse(b.start) - Date.parse(a.start),
    host: (a, b) => hostOf(a).localeCompare(hostOf(b)),
  }

  return list.sort(compare[sortKey.value])
})

const visible = computed(() => filtered.value.slice(0, maxRows))
const filtersActive = computed(() => Boolean(search.value.trim() || groupFilter.value || outboundFilter.value || networkFilter.value))
const downTotal = computed(() => splitBytes(snapshot.value?.download_total ?? 0))
const upTotal = computed(() => splitBytes(snapshot.value?.upload_total ?? 0))

// hostOf возвращает домен соединения или адрес назначения.
function hostOf(conn: Connection): string {
  return conn.host || conn.destination
}

// ruleTarget возвращает домен или IP соединения для правила (без порта и скобок IPv6).
function ruleTarget(conn: Connection): string {
  if (conn.host) {
    return conn.host
  }

  const address = conn.destination.replace(/:\d+$/, '')

  return address.replace(/^\[(.*)]$/, '$1')
}

// age возвращает длительность соединения.
function age(conn: Connection): string {
  const start = Date.parse(conn.start)

  return Number.isNaN(start) ? '—' : formatDuration(now.value - start)
}

// closeConnections закрывает соединения из списка или все.
async function closeConnections(list: Connection[] | 'all'): Promise<void> {
  const all = list === 'all'
  const count = all ? connections.value.length : list.length

  if (count === 0) {
    return
  }

  if (all || count > 1) {
    const confirmed = await confirmAction({
      title: all ? 'Закрыть все соединения?' : `Закрыть соединения: ${count}?`,
      message: 'Приложения переподключатся заново — уже по текущим правилам и выбранным outbound-ам.',
      confirmText: 'Закрыть',
      danger: true,
    })

    if (!confirmed) {
      return
    }
  }

  closing.value = true

  try {
    const result = await postQuiet<{ message?: string }>('/api/connections/close', all ? { all: true } : { ids: list.map((conn) => conn.id) })

    showMessage(result.message ?? 'Соединения закрыты')
    await load()
  } catch (error) {
    showError(error)
  } finally {
    closing.value = false
  }
}

// resetFilters сбрасывает фильтры.
function resetFilters(): void {
  search.value = ''
  groupFilter.value = ''
  outboundFilter.value = ''
  networkFilter.value = ''
}
</script>

<template>
  <Teleport to="#topbar-actions" defer>
    <button class="btn btn-secondary" :title="paused ? 'Продолжить обновление' : 'Остановить обновление, чтобы рассмотреть список'" @click="paused = !paused">
      <SvgIcon class="btn-icon" :path="paused ? icons.play : icons.pause" />
      {{ paused ? 'Продолжить' : 'Пауза' }}
    </button>
    <button
      class="btn btn-danger"
      :disabled="closing || (filtersActive ? filtered.length === 0 : connections.length === 0)"
      @click="closeConnections(filtersActive ? filtered : 'all')"
    >
      {{ filtersActive ? `Закрыть найденные (${filtered.length})` : 'Закрыть все' }}
    </button>
  </Teleport>

  <div class="stat-row">
    <div class="stat-tile">
      <div class="stat-label">Активные соединения</div>
      <div class="stat-value">{{ snapshot ? connections.length : '—' }}</div>
      <div class="stat-foot">
        <span class="stat-dot" :class="failed ? 'is-bad' : paused ? 'is-warn' : 'is-good'"></span>
        <span v-if="failed">Clash API недоступен</span>
        <span v-else-if="paused">Обновление на паузе</span>
        <span v-else>Обновляется каждые {{ pollInterval / 1000 }} с</span>
      </div>
    </div>
    <div class="stat-tile">
      <div class="stat-label">Скорость сейчас</div>
      <div class="stat-value conn-rate-value">↓ {{ formatRate(totalRate.down) }}</div>
      <div class="stat-foot">↑ {{ formatRate(totalRate.up) }}</div>
    </div>
    <div class="stat-tile">
      <div class="stat-label">Всего с запуска ядра</div>
      <div class="stat-value">{{ snapshot ? downTotal.value : '—' }}<span class="stat-unit">{{ snapshot ? downTotal.unit : '' }}</span></div>
      <div class="stat-foot">отдано {{ snapshot ? `${upTotal.value} ${upTotal.unit}` : '—' }}</div>
    </div>
  </div>

  <div v-if="failed" class="callout is-bad" style="margin-bottom: 16px;">Не удалось получить соединения: {{ failed }}</div>

  <div class="table-toolbar">
    <FormField label="Поиск" input-id="connSearch">
      <div class="search-input">
        <SvgIcon :path="icons.search" />
        <input id="connSearch" v-model="search" class="form-input" type="search" placeholder="Домен, IP, устройство, outbound">
      </div>
    </FormField>
    <FormField label="Группа" input-id="connGroup">
      <select id="connGroup" v-model="groupFilter" class="form-select">
        <option value="">Все</option>
        <option v-for="group in groups" :key="group" :value="group">{{ group }}</option>
      </select>
    </FormField>
    <FormField label="Outbound" input-id="connOutbound">
      <select id="connOutbound" v-model="outboundFilter" class="form-select">
        <option value="">Все</option>
        <option v-for="tag in outbounds" :key="tag" :value="tag">{{ tag }}</option>
      </select>
    </FormField>
    <FormField label="Сеть" input-id="connNetwork">
      <select id="connNetwork" v-model="networkFilter" class="form-select">
        <option value="">TCP и UDP</option>
        <option value="tcp">TCP</option>
        <option value="udp">UDP</option>
      </select>
    </FormField>
    <FormField label="Сортировка" input-id="connSort">
      <select id="connSort" v-model="sortKey" class="form-select">
        <option value="rate">По скорости</option>
        <option value="traffic">По трафику</option>
        <option value="start">Сначала новые</option>
        <option value="host">По хосту</option>
      </select>
    </FormField>
    <span class="table-count">
      {{ filtered.length }} из {{ connections.length }}
      <button v-if="filtersActive" type="button" class="link-button" @click="resetFilters">сбросить</button>
    </span>
  </div>

  <div class="data-table">
    <table class="table table-cards conn-table">
      <thead>
        <tr>
          <th>Хост</th>
          <th>Источник</th>
          <th>Маршрут</th>
          <th class="col-num">Скорость</th>
          <th class="col-num">Трафик</th>
          <th class="col-num">Время</th>
          <th class="col-actions"></th>
        </tr>
      </thead>
      <tbody>
        <tr v-if="snapshot && connections.length === 0">
          <td colspan="7" class="empty-state">Активных соединений нет.</td>
        </tr>
        <tr v-else-if="snapshot && filtered.length === 0">
          <td colspan="7" class="empty-state">Ничего не найдено.</td>
        </tr>
        <tr v-else-if="!snapshot && !failed">
          <td colspan="7" class="empty-state">Загрузка...</td>
        </tr>
        <tr v-for="conn in visible" :key="conn.id">
          <td class="conn-host-cell">
            <span class="cell-mono conn-host" :title="hostOf(conn)">{{ hostOf(conn) || '—' }}</span>
            <div class="cell-sub">
              <span class="conn-network">{{ conn.network }}</span>
              <template v-if="conn.host && conn.destination"> · {{ conn.destination }}</template>
            </div>
          </td>
          <td data-label="Источник">
            <span class="cell-mono">{{ conn.source || '—' }}</span>
            <div class="cell-sub">
              <template v-if="conn.inbound === serviceInbound">служебное: конфигуратор</template>
              <template v-else>{{ conn.inbound }}</template>
              <template v-if="conn.process"> · {{ conn.process }}</template>
            </div>
          </td>
          <td class="conn-route" data-label="Маршрут">
            <GroupBadge v-if="conn.group" :group="conn.group" />
            <span class="conn-chain" :title="conn.rule">
              {{ conn.chain.length ? conn.chain.join(' → ') : '—' }}
            </span>
          </td>
          <td class="col-num" data-label="Скорость">
            <div>↓ {{ formatRate(rateOf(conn).down) }}</div>
            <div class="cell-sub">↑ {{ formatRate(rateOf(conn).up) }}</div>
          </td>
          <td class="col-num" data-label="Трафик">
            <div>↓ {{ formatBytesRu(conn.download) }}</div>
            <div class="cell-sub">↑ {{ formatBytesRu(conn.upload) }}</div>
          </td>
          <td class="col-num" data-label="Время">{{ age(conn) }}</td>
          <td class="actions-cell">
            <div class="row-actions">
              <IconButton
                icon="plus"
                :title="`Добавить ${ruleTarget(conn)} в группу правил`"
                :disabled="!ruleTarget(conn) || conn.inbound === serviceInbound"
                @click="addTarget = { target: ruleTarget(conn) }"
              />
              <IconButton icon="close" title="Закрыть соединение" danger :disabled="closing" @click="closeConnections([conn])" />
            </div>
          </td>
        </tr>
      </tbody>
    </table>
  </div>

  <p v-if="filtered.length > maxRows" class="card-hint" style="margin-top: 8px;">
    Показаны первые {{ maxRows }} соединений из {{ filtered.length }} — уточните поиск или фильтры.
  </p>

  <AddRuleDialog v-if="addTarget" :target="addTarget.target" context="Добавлено из соединений" @close="addTarget = null" />
</template>
