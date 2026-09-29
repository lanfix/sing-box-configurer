<script setup lang="ts">
// Прокси-группы Clash API: переключение активного узла в selector-ах и замер задержки.
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'

import { get, post } from '../api/client'
import type { ClashProxy } from '../api/types'
import SvgIcon from '../components/SvgIcon.vue'
import FormField from '../components/ui/FormField.vue'
import HelpHint from '../components/ui/HelpHint.vue'
import { icons } from '../icons'
import { showError, showMessage } from '../stores/toast'

// Типы прокси-групп, которые показываются на странице.
const groupTypes = ['Selector', 'URLTest', 'LoadBalance', 'Fallback']

// GLOBAL — служебный selector sing-box: маршрутизация задается правилами, поэтому группа скрыта.
const hiddenGroups = ['GLOBAL']

// defaultTestURL — адрес замера задержки по умолчанию.
const defaultTestURL = 'http://www.gstatic.com/generate_204'

// typeTitles — пояснения к типам групп.
const typeTitles: Record<string, string> = {
  Selector: 'Узел выбирается вручную',
  URLTest: 'Узел выбирается автоматически по задержке',
  Fallback: 'Первый доступный узел',
  LoadBalance: 'Нагрузка распределяется между узлами',
}

const proxies = ref<Record<string, ClashProxy>>({})
const error = ref('')
const loading = ref(false)
const testURL = ref(defaultTestURL)
const search = ref('')

// busy — операции в полете: пока они идут, фоновое обновление не перезаписывает данные.
const busy = ref(0)
const testingGroups = ref<string[]>([])

let timer: ReturnType<typeof setInterval> | null = null

const groups = computed(() => Object.values(proxies.value)
  .filter((proxy) => groupTypes.includes(proxy.type) && !hiddenGroups.includes(proxy.name))
  .sort((a, b) => a.name.localeCompare(b.name)))

// visibleGroups — группы, у которых имя или один из узлов подходит под поиск.
const visibleGroups = computed(() => {
  const query = search.value.trim().toLowerCase()

  if (!query) {
    return groups.value
  }

  return groups.value.filter((group) => group.name.toLowerCase().includes(query) || (group.all ?? []).some((member) => member.toLowerCase().includes(query)))
})

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

// delayClass возвращает класс цвета задержки.
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

// delayText форматирует задержку.
function delayText(delay?: number): string {
  if (delay === undefined) {
    return '—'
  }

  return delay <= 0 ? 'нет ответа' : `${delay} мс`
}

// select переключает активный узел selector-группы.
async function select(group: ClashProxy, name: string): Promise<void> {
  if (group.type !== 'Selector' || group.now === name) {
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
  const url = testURL.value.trim() || defaultTestURL
  const data = await get<{ delays: Record<string, number> }>(`/api/clash/group/delay?group=${encodeURIComponent(name)}&url=${encodeURIComponent(url)}`)

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
    testingGroups.value = [...testingGroups.value, group.name]

    try {
      await testGroup(group.name)
      ok++
    } catch {
      failed.push(group.name)
    } finally {
      testingGroups.value = testingGroups.value.filter((name) => name !== group.name)
    }
  }

  busy.value--
  loading.value = false

  if (failed.length === 0) {
    showMessage(`Задержка замерена: групп — ${ok}`)
  } else {
    showMessage(`Замер завершен: успешно ${ok}, с ошибкой ${failed.join(', ')}`, 'error')
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
  <Teleport to="#topbar-actions" defer>
    <button class="btn btn-secondary" :disabled="loading" @click="load()">
      <SvgIcon class="btn-icon" :path="icons.refresh" />
      Обновить
    </button>
    <button class="btn btn-primary" :disabled="groups.length === 0 || loading" @click="testAll">
      <SvgIcon class="btn-icon" :path="icons.speedometer" />
      Проверить все
    </button>
  </Teleport>

  <p class="page-intro">
    Активные узлы групп в работающем sing-box. В группах с ручным выбором узел переключается кликом — сразу, без
    применения конфига. Узел по умолчанию после перезапуска задается в настройках
    <RouterLink :to="{ name: 'groups' }">групп</RouterLink>.
  </p>

  <HelpHint title="Цвета задержки">
    <p>
      <span class="proxy-delay d-good">до 200 мс</span> — отлично,
      <span class="proxy-delay d-ok">до 400 мс</span> — нормально,
      <span class="proxy-delay d-slow">до 800 мс</span> — медленно,
      <span class="proxy-delay d-bad">больше 800 мс или нет ответа</span> — проблема.
      Задержка замеряется запросом к адресу проверки через каждый узел.
    </p>
  </HelpHint>

  <div class="table-toolbar">
    <FormField label="Поиск" input-id="proxySearch">
      <div class="search-input">
        <SvgIcon :path="icons.search" />
        <input id="proxySearch" v-model="search" class="form-input" type="search" placeholder="Группа или узел">
      </div>
    </FormField>
    <FormField label="Адрес для замера задержки" input-id="clashTestURL" class="toolbar-wide">
      <input id="clashTestURL" v-model="testURL" class="form-input" type="url" :placeholder="defaultTestURL">
    </FormField>
    <span class="table-count">Групп: {{ visibleGroups.length }}</span>
  </div>

  <div class="clash-groups has-progress" :class="{ 'is-loading': loading }">
    <div class="progress-line"></div>
    <div v-if="error" class="callout is-bad">Не удалось получить данные Clash API: {{ error }}</div>
    <div v-else-if="groups.length === 0 && !loading" class="empty-state">Прокси-группы не найдены. Возможно, sing-box не запущен.</div>
    <div v-else-if="visibleGroups.length === 0" class="empty-state">Ничего не найдено.</div>

    <div
      v-for="group in visibleGroups"
      :key="group.name"
      class="clash-group has-progress"
      :class="{ 'is-loading': testingGroups.includes(group.name) }"
    >
      <div class="progress-line"></div>
      <div class="clash-group-head">
        <div class="clash-group-meta">
          <div class="clash-group-title">
            <strong>{{ group.name }}</strong>
            <span class="clash-type" :title="typeTitles[group.type]">{{ group.type }}</span>
          </div>
          <div class="clash-group-now">
            {{ group.type === 'Selector' ? 'Выбран' : 'Сейчас используется' }}: {{ group.now || '—' }}
            <span v-if="group.type !== 'Selector'" class="muted"> · {{ typeTitles[group.type] }}</span>
          </div>
        </div>
        <div class="clash-group-actions">
          <span class="proxy-delay" :class="delayClass(lastDelay(group.now))">{{ delayText(lastDelay(group.now)) }}</span>
          <button class="btn btn-secondary btn-sm" :disabled="testingGroups.includes(group.name)" @click="testOne(group)">
            <SvgIcon class="btn-icon" :path="icons.speedometer" />
            {{ testingGroups.includes(group.name) ? 'Проверка...' : 'Проверить' }}
          </button>
        </div>
      </div>
      <div class="proxy-grid">
        <component
          :is="group.type === 'Selector' ? 'button' : 'div'"
          v-for="member in group.all ?? []"
          :key="member"
          :type="group.type === 'Selector' ? 'button' : undefined"
          class="proxy-node"
          :class="{
            active: member === group.now,
            selectable: group.type === 'Selector',
            readonly: group.type !== 'Selector',
            'is-match': search.trim() && member.toLowerCase().includes(search.trim().toLowerCase()),
          }"
          :title="group.type === 'Selector' ? `Выбрать ${member}` : member"
          @click="select(group, member)"
        >
          <div class="proxy-node-row">
            <span class="proxy-node-name">{{ member }}</span>
            <span v-if="proxies[member]?.udp" class="proxy-udp" title="Поддерживает UDP">UDP</span>
          </div>
          <div class="proxy-node-row">
            <span class="proxy-node-type">{{ proxies[member]?.type || '—' }}</span>
            <span class="proxy-delay" :class="delayClass(lastDelay(member))">{{ delayText(lastDelay(member)) }}</span>
          </div>
        </component>
      </div>
    </div>
  </div>
</template>
