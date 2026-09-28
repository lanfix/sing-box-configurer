<script setup lang="ts">
// Прокси-группы Clash API: переключение активного узла в selector-ах и замер задержки.
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'

import { get, post } from '../../api/client'
import type { ClashProxy } from '../../api/types'
import SvgIcon from '../../components/SvgIcon.vue'
import { icons } from '../../icons'
import { showError, showMessage } from '../../stores/toast'

// Типы прокси-групп, которые показываются на странице.
const groupTypes = ['Selector', 'URLTest', 'LoadBalance', 'Fallback']

// GLOBAL — служебный selector sing-box: маршрутизация задается правилами, поэтому группа скрыта.
const hiddenGroups = ['GLOBAL']

const proxies = ref<Record<string, ClashProxy>>({})
const error = ref('')
const loading = ref(false)
const testURL = ref('http://www.gstatic.com/generate_204')

// busy — операции в полете: пока они идут, фоновое обновление не перезаписывает данные.
const busy = ref(0)
const testingGroups = ref<string[]>([])

let timer: ReturnType<typeof setInterval> | null = null

const groups = computed(() => Object.values(proxies.value)
  .filter((proxy) => groupTypes.includes(proxy.type) && !hiddenGroups.includes(proxy.name))
  .sort((a, b) => a.name.localeCompare(b.name)))

// merge сохраняет уже замеренные задержки, если sing-box вернул пустую историю.
function merge(fresh: Record<string, ClashProxy>): void {
  for (const proxy of Object.values(fresh)) {
    const cached = proxies.value[proxy.name]

    if (!proxy.history?.length && cached?.history?.length) {
      proxy.history = cached.history
    }
  }

  proxies.value = fresh
}

// load загружает прокси. silent — фоновое обновление без индикатора.
async function load(silent = false): Promise<void> {
  if (silent && busy.value > 0) {
    return
  }

  if (!silent) {
    loading.value = true
  }

  try {
    const data = await get<{ proxies: Record<string, ClashProxy> }>('/api/clash/proxies')

    if (!silent || busy.value === 0) {
      merge(data.proxies ?? {})
    }

    error.value = ''
  } catch (err) {
    if (!silent) {
      error.value = err instanceof Error ? err.message : String(err)
    }
  } finally {
    loading.value = false
  }
}

// lastDelay возвращает последнюю замеренную задержку прокси.
function lastDelay(name?: string): number | undefined {
  const history = name ? proxies.value[name]?.history : undefined

  return history?.length ? history[history.length - 1].delay : undefined
}

// delayClass и delayText форматируют задержку.
function delayClass(delay?: number): string {
  if (delay === undefined) {
    return ''
  }

  if (delay <= 0 || delay >= 800) {
    return 'd-bad'
  }

  if (delay < 200) {
    return 'd-good'
  }

  return delay < 400 ? 'd-ok' : 'd-slow'
}

function delayText(delay?: number): string {
  if (delay === undefined) {
    return '—'
  }

  return delay <= 0 ? 'timeout' : `${delay} ms`
}

// select переключает активный узел selector-группы.
async function select(group: ClashProxy, name: string): Promise<void> {
  if (group.type !== 'Selector') {
    return
  }

  try {
    await post('/api/clash/proxies/select', { group: group.name, name })
    group.now = name
    showMessage(`${group.name}: выбран ${name}`)
  } catch (err) {
    showError(err, 'Ошибка переключения')
  }
}

// applyDelays записывает результаты замера в историю прокси.
function applyDelays(delays: Record<string, number>): void {
  for (const [name, delay] of Object.entries(delays)) {
    const proxy = proxies.value[name] ?? (proxies.value[name] = { name, type: '', history: [] })

    proxy.history = [...(proxy.history ?? []), { delay }]
  }
}

// testGroup замеряет задержку всех узлов группы.
async function testGroup(name: string): Promise<number> {
  const data = await get<{ delays: Record<string, number> }>(`/api/clash/group/delay?group=${encodeURIComponent(name)}&url=${encodeURIComponent(testURL.value.trim() || 'http://www.gstatic.com/generate_204')}`)

  applyDelays(data.delays ?? {})

  return Object.keys(data.delays ?? {}).length
}

// testOne замеряет задержку одной группы по кнопке.
async function testOne(group: ClashProxy): Promise<void> {
  busy.value++
  testingGroups.value = [...testingGroups.value, group.name]

  try {
    const count = await testGroup(group.name)

    showMessage(`${group.name}: проверено узлов — ${count}`)
  } catch (err) {
    showError(err, `Ошибка замера задержки (${group.name})`)
  } finally {
    busy.value--
    testingGroups.value = testingGroups.value.filter((name) => name !== group.name)
  }
}

// testAll замеряет задержку всех групп по очереди.
async function testAll(): Promise<void> {
  busy.value++
  loading.value = true

  let ok = 0
  const failed: string[] = []

  for (const group of groups.value) {
    try {
      await testGroup(group.name)
      ok++
    } catch {
      failed.push(group.name)
    }
  }

  busy.value--
  loading.value = false

  if (failed.length === 0) {
    showMessage(`Задержка замерена: групп — ${ok}`)
  } else {
    showMessage(`Замер завершён: успешно ${ok}, с ошибкой ${failed.join(', ')}`, 'error')
  }
}

onMounted(() => {
  void load()
  timer = setInterval(() => void load(true), 5000)
})

onBeforeUnmount(() => {
  if (timer) {
    clearInterval(timer)
  }
})
</script>

<template>
  <div class="add-form-card has-progress" :class="{ 'is-loading': loading }">
    <div class="progress-line"></div>
    <div class="form-header" style="margin-bottom: 4px;">Прокси-группы</div>
    <p class="card-hint" style="margin: 0 0 16px;">
      Переключение активного узла в selector-группах и замер задержки. Данные из Clash API sing-box обновляются автоматически.
    </p>
    <div class="clash-controls">
      <div class="form-group" style="flex: 1;">
        <label class="form-label" for="clashTestURL">URL для проверки задержки</label>
        <input id="clashTestURL" v-model="testURL" class="form-input" type="text">
      </div>
      <button class="btn btn-secondary" @click="load()">
        <SvgIcon class="btn-icon" :path="icons.refresh" />
        Обновить
      </button>
      <button class="btn btn-primary" :disabled="groups.length === 0 || loading" @click="testAll">
        <SvgIcon class="btn-icon" :path="icons.speedometer" />
        Проверить все
      </button>
    </div>
  </div>

  <div class="clash-groups">
    <div v-if="error" class="empty-state">Не удалось получить данные Clash API: {{ error }}</div>
    <div v-else-if="groups.length === 0 && !loading" class="empty-state">Прокси-группы не найдены.</div>

    <div v-for="group in groups" :key="group.name" class="clash-group has-progress" :class="{ 'is-loading': testingGroups.includes(group.name) }">
      <div class="progress-line"></div>
      <div class="clash-group-head">
        <div class="clash-group-meta">
          <div class="clash-group-title">
            <strong>{{ group.name }}</strong>
            <span class="clash-type">{{ group.type }}</span>
          </div>
          <div class="clash-group-now">Активен: {{ group.now || '—' }}</div>
        </div>
        <div class="clash-group-actions">
          <span class="proxy-delay" :class="delayClass(lastDelay(group.now))">{{ delayText(lastDelay(group.now)) }}</span>
          <button class="btn btn-secondary btn-sm" :disabled="testingGroups.includes(group.name)" @click="testOne(group)">
            <SvgIcon class="btn-icon" :path="icons.speedometer" />
            Проверить
          </button>
        </div>
      </div>
      <div class="proxy-grid">
        <div
          v-for="member in group.all ?? []"
          :key="member"
          class="proxy-node"
          :class="{ active: member === group.now, selectable: group.type === 'Selector', readonly: group.type !== 'Selector' }"
          @click="select(group, member)"
        >
          <div class="proxy-node-row">
            <span class="proxy-node-name" :title="member">{{ member }}</span>
            <span v-if="proxies[member]?.udp" class="proxy-udp">UDP</span>
          </div>
          <div class="proxy-node-row">
            <span class="proxy-node-type">{{ proxies[member]?.type || '—' }}</span>
            <span class="proxy-delay" :class="delayClass(lastDelay(member))">{{ delayText(lastDelay(member)) }}</span>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>
