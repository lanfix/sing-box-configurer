<script setup lang="ts">
// Обзор: суммарный трафик, подключения, график скорости за 60 секунд и потребление памяти.
// Цвета серий заданы в main.css (--series-down / --series-up); обе серии на одной оси Y (B/s).
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue'

import { get } from '../api/client'
import type { Overview, TrafficSample } from '../api/types'
import { usePolling } from '../composables/usePolling'
import { monotonePath, niceMax } from '../utils/chart'
import { formatRate, splitBytes } from '../utils/format'

const trafficCapacity = 60
const memoryCapacity = 60

const overview = ref<Overview | null>(null)
const failed = ref(false)
const traffic = ref<TrafficSample[]>([])
const memory = ref<number[]>([])
const memoryPeak = ref(0)
const tableOpen = ref(false)

// Позиция курсора над графиком: окно сдвигается каждую секунду, и крестовина остается под указателем.
const hoverX = ref<number | null>(null)

const chartWrap = ref<HTMLElement | null>(null)
const memoryWrap = ref<HTMLElement | null>(null)

const size = reactive({
  chartWidth: 0,
  chartHeight: 0,
  memoryWidth: 0,
  memoryHeight: 0,
})

// load запрашивает сводку Clash API.
async function load(): Promise<void> {
  try {
    const data = await get<Overview>('/api/clash/overview')

    overview.value = data
    failed.value = false
    traffic.value = Array.isArray(data.traffic) ? data.traffic : []

    const current = Number(data.memory) || 0

    memory.value = [...memory.value, current].slice(-memoryCapacity)
    memoryPeak.value = Math.max(memoryPeak.value, current)
  } catch {
    // Секундный опрос: не показываем сообщений, статус виден в плитке, график остается прежним.
    failed.value = true
  }
}

usePolling(load, 1000)

// measure обновляет размеры графиков при изменении окна.
function measure(): void {
  size.chartWidth = chartWrap.value?.clientWidth ?? 0
  size.chartHeight = chartWrap.value?.clientHeight ?? 0
  size.memoryWidth = memoryWrap.value?.clientWidth ?? 0
  size.memoryHeight = memoryWrap.value?.clientHeight ?? 0
}

onMounted(() => {
  measure()
  window.addEventListener('resize', measure)
})

onBeforeUnmount(() => window.removeEventListener('resize', measure))

const downTotal = computed(() => splitBytes(overview.value?.downloadTotal ?? 0))
const upTotal = computed(() => splitBytes(overview.value?.uploadTotal ?? 0))
const memoryNow = computed(() => splitBytes(overview.value?.memory ?? 0))
const memoryPeakText = computed(() => {
  const parts = splitBytes(memoryPeak.value)

  return `${parts.value} ${parts.unit}`
})

const latest = computed(() => traffic.value[traffic.value.length - 1])

const peakRate = computed(() => {
  if (traffic.value.length === 0) {
    return ''
  }

  const down = Math.max(0, ...traffic.value.map((sample) => sample.down))
  const up = Math.max(0, ...traffic.value.map((sample) => sample.up))

  return `Пик за окно: ${formatRate(down)} ↓ · ${formatRate(up)} ↑`
})

const streamDot = computed(() => {
  if (failed.value) {
    return 'is-bad'
  }

  return overview.value?.trafficStreamOk ? 'is-good' : 'is-warn'
})

// chart — геометрия и пути графика скорости.
const chart = computed(() => {
  const width = size.chartWidth
  const height = size.chartHeight
  const padL = 58
  const padR = 14
  const padT = 12
  const padB = 22
  const plotW = Math.max(0, width - padL - padR)
  const plotH = Math.max(0, height - padT - padB)
  const samples = traffic.value
  const n = samples.length
  const max = niceMax(Math.max(0, ...samples.map((sample) => Math.max(sample.down, sample.up))))
  const stepX = plotW / (trafficCapacity - 1)
  const xs = samples.map((_, i) => padL + plotW - (n - 1 - i) * stepX)
  const yAt = (value: number) => padT + plotH * (1 - Math.min(value, max) / max)
  const baseline = padT + plotH

  const ticks = [0, 1, 2, 3, 4].map((i) => ({
    y: padT + (plotH * i) / 4,
    label: formatRate(max * (1 - i / 4)),
  }))

  const series = (['up', 'down'] as const).map((name) => {
    const points = samples.map((sample, i) => ({ x: xs[i], y: yAt(sample[name]) }))
    const line = monotonePath(points)
    const area = n > 0 ? `${line}L${xs[n - 1].toFixed(2)},${baseline}L${xs[0].toFixed(2)},${baseline}Z` : ''

    return { name, line, area }
  })

  let hoverIndex: number | null = null

  if (hoverX.value !== null && xs.length > 0) {
    let best = Infinity

    xs.forEach((x, i) => {
      const distance = Math.abs(x - (hoverX.value as number))

      if (distance < best) {
        best = distance
        hoverIndex = i
      }
    })
  }

  return { width, height, padL, padT, plotW, plotH, xs, yAt, ticks, series, hoverIndex: hoverIndex as number | null }
})

// hoverSample — измерение под курсором.
const hoverSample = computed(() => (chart.value.hoverIndex === null ? null : traffic.value[chart.value.hoverIndex]))

const tooltipStyle = computed(() => {
  const index = chart.value.hoverIndex

  if (index === null) {
    return {}
  }

  const x = chart.value.xs[index]
  const flip = x > chart.value.width / 2

  return {
    left: `${flip ? x - 12 : x + 12}px`,
    transform: flip ? 'translateX(-100%)' : 'none',
  }
})

const tooltipTime = computed(() => {
  const index = chart.value.hoverIndex

  if (index === null) {
    return ''
  }

  const secondsAgo = traffic.value.length - 1 - index

  return secondsAgo === 0 ? 'сейчас' : `${secondsAgo} с назад`
})

// memoryChart — пути спарклайна памяти. Шкала берется вокруг наблюдаемого диапазона с запасом:
// шкала от нуля исказила бы почти плоский ряд.
const memoryChart = computed(() => {
  const width = size.memoryWidth
  const height = size.memoryHeight
  const values = memory.value
  const n = values.length

  if (n < 2 || width <= 0 || height <= 0) {
    return { line: '', area: '' }
  }

  const padT = 6
  const plotH = height - padT - 2
  const stepX = width / (memoryCapacity - 1)
  const min = Math.min(...values)
  const max = Math.max(...values)
  const span = Math.max(max - min, max * 0.05, 1)
  const top = max + span * 0.25
  const bottom = Math.max(0, min - span * 0.25)

  const points = values.map((value, i) => ({
    x: width - (n - 1 - i) * stepX,
    y: padT + plotH * (1 - (value - bottom) / (top - bottom)),
  }))

  const line = monotonePath(points)

  return {
    line,
    area: `${line}L${points[n - 1].x.toFixed(2)},${padT + plotH}L${points[0].x.toFixed(2)},${padT + plotH}Z`,
  }
})

// tableRows — табличный двойник графика (новые измерения сверху).
const tableRows = computed(() => traffic.value.map((sample, i) => {
  const secondsAgo = traffic.value.length - 1 - i

  return { when: secondsAgo === 0 ? 'сейчас' : `−${secondsAgo} с`, down: formatRate(sample.down), up: formatRate(sample.up) }
}).reverse())

// onPointer запоминает положение курсора над графиком.
function onPointer(event: PointerEvent): void {
  const rect = chartWrap.value?.getBoundingClientRect()

  if (rect) {
    hoverX.value = event.clientX - rect.left
  }
}
</script>

<template>
  <div class="stat-row">
    <div class="stat-tile">
      <div class="stat-label">Всего загружено</div>
      <div class="stat-value">{{ overview ? downTotal.value : '—' }}<span class="stat-unit">{{ overview ? downTotal.unit : '' }}</span></div>
      <div class="stat-foot"><span class="legend-key" data-series="down"></span>С запуска ядра</div>
    </div>
    <div class="stat-tile">
      <div class="stat-label">Всего отдано</div>
      <div class="stat-value">{{ overview ? upTotal.value : '—' }}<span class="stat-unit">{{ overview ? upTotal.unit : '' }}</span></div>
      <div class="stat-foot"><span class="legend-key" data-series="up"></span>С запуска ядра</div>
    </div>
    <div class="stat-tile">
      <div class="stat-label">Активные подключения</div>
      <div class="stat-value">{{ overview?.connections.total ?? '—' }}<span class="stat-unit">conn</span></div>
      <div class="stat-foot">
        <span class="stat-dot" :class="streamDot"></span>
        <span v-if="failed">Clash API недоступен</span>
        <span v-else>TCP {{ overview?.connections.tcp ?? 0 }} · UDP {{ overview?.connections.udp ?? 0 }}</span>
      </div>
    </div>
  </div>

  <div class="overview-grid">
    <div class="add-form-card">
      <div class="chart-head">
        <div>
          <div class="form-header" style="margin-bottom: 2px;">Трафик в реальном времени</div>
          <p class="card-hint" style="margin: 0;">Последние 60 секунд</p>
        </div>
        <div class="chart-legend">
          <div class="legend-item">
            <span class="legend-key" data-series="down"></span>
            <span class="legend-name">Загрузка</span>
            <span class="legend-value">{{ latest ? formatRate(latest.down) : '—' }}</span>
          </div>
          <div class="legend-item">
            <span class="legend-key" data-series="up"></span>
            <span class="legend-name">Отдача</span>
            <span class="legend-value">{{ latest ? formatRate(latest.up) : '—' }}</span>
          </div>
        </div>
      </div>

      <div ref="chartWrap" class="chart-wrap" @pointermove="onPointer" @pointerleave="hoverX = null">
        <svg
          v-if="chart.width > 0 && chart.height > 0"
          class="chart-svg"
          role="img"
          aria-label="График скорости загрузки и отдачи за последние 60 секунд"
          :width="chart.width"
          :height="chart.height"
          :viewBox="`0 0 ${chart.width} ${chart.height}`"
        >
          <template v-for="tick in chart.ticks" :key="tick.y">
            <line class="chart-grid" :x1="chart.padL" :y1="tick.y" :x2="chart.padL + chart.plotW" :y2="tick.y" />
            <text class="chart-tick" :x="chart.padL - 8" :y="tick.y + 3.5" text-anchor="end">{{ tick.label }}</text>
          </template>

          <template v-if="traffic.length > 0">
            <text class="chart-tick" :x="chart.padL" :y="chart.padT + chart.plotH + 15" text-anchor="start">−60 с</text>
            <text class="chart-tick" :x="chart.padL + chart.plotW / 2" :y="chart.padT + chart.plotH + 15" text-anchor="middle">−30 с</text>
            <text class="chart-tick" :x="chart.padL + chart.plotW" :y="chart.padT + chart.plotH + 15" text-anchor="end">сейчас</text>
          </template>

          <template v-for="series in chart.series" :key="series.name">
            <path class="chart-area" :data-series="series.name" :d="series.area" />
            <path class="chart-line" :data-series="series.name" :d="series.line" />
          </template>

          <template v-if="hoverSample && chart.hoverIndex !== null">
            <line
              class="chart-crosshair"
              :x1="chart.xs[chart.hoverIndex]"
              :y1="chart.padT"
              :x2="chart.xs[chart.hoverIndex]"
              :y2="chart.padT + chart.plotH"
            />
            <circle class="chart-dot" data-series="up" :cx="chart.xs[chart.hoverIndex]" :cy="chart.yAt(hoverSample.up)" r="4" />
            <circle class="chart-dot" data-series="down" :cx="chart.xs[chart.hoverIndex]" :cy="chart.yAt(hoverSample.down)" r="4" />
          </template>

          <text
            v-if="traffic.length === 0"
            class="chart-empty"
            :x="chart.padL + chart.plotW / 2"
            :y="chart.padT + chart.plotH / 2"
            text-anchor="middle"
          >Ожидание данных от Clash API…</text>
        </svg>

        <div v-if="hoverSample" class="chart-tooltip" :style="tooltipStyle">
          <div class="tt-time">{{ tooltipTime }}</div>
          <div class="tt-row"><span class="tt-key" data-series="down"></span><span class="tt-value">{{ formatRate(hoverSample.down) }}</span><span class="tt-name">Загрузка</span></div>
          <div class="tt-row"><span class="tt-key" data-series="up"></span><span class="tt-value">{{ formatRate(hoverSample.up) }}</span><span class="tt-name">Отдача</span></div>
        </div>
      </div>

      <div class="chart-foot">
        <button class="btn btn-secondary btn-sm" @click="tableOpen = !tableOpen">{{ tableOpen ? 'Скрыть таблицу' : 'Показать таблицу' }}</button>
        <span class="card-hint">{{ peakRate }}</span>
      </div>

      <div v-if="tableOpen" class="chart-table">
        <div v-if="tableRows.length === 0" class="empty-state">Нет данных.</div>
        <table v-else class="table">
          <thead><tr><th>Время</th><th>Загрузка</th><th>Отдача</th></tr></thead>
          <tbody>
            <tr v-for="row in tableRows" :key="row.when"><td>{{ row.when }}</td><td>{{ row.down }}</td><td>{{ row.up }}</td></tr>
          </tbody>
        </table>
      </div>
    </div>

    <div class="add-form-card">
      <div class="form-header" style="margin-bottom: 2px;">Использование памяти</div>
      <p class="card-hint" style="margin: 0 0 16px;">Текущий процесс sing-box</p>
      <div class="stat-value stat-value-lg">{{ overview ? memoryNow.value : '—' }}<span class="stat-unit">{{ overview ? memoryNow.unit : '' }}</span></div>
      <div ref="memoryWrap" class="mem-spark-wrap">
        <svg
          v-if="memoryChart.line"
          class="chart-svg"
          role="img"
          aria-label="График потребления памяти"
          :width="size.memoryWidth"
          :height="size.memoryHeight"
          :viewBox="`0 0 ${size.memoryWidth} ${size.memoryHeight}`"
        >
          <path class="chart-area" data-series="down" :d="memoryChart.area" />
          <path class="chart-line" data-series="down" :d="memoryChart.line" />
        </svg>
      </div>
      <dl class="meta-list">
        <div class="meta-row"><dt>Пик</dt><dd>{{ memoryPeak ? memoryPeakText : '—' }}</dd></div>
        <div class="meta-row"><dt>Правил</dt><dd>{{ overview?.rules ?? '—' }}</dd></div>
        <div class="meta-row"><dt>Ядро</dt><dd>{{ overview?.version || '—' }}</dd></div>
      </dl>
    </div>
  </div>
</template>
