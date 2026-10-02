<script setup lang="ts">
import { computed, defineAsyncComponent, onMounted, ref } from 'vue'

import { get, post, postQuiet } from '../../api/client'
import type { OutboundSource, OutboundView, SpeedTestResult, SpeedTestSettings, SpeedTestState } from '../../api/types'
import ModalDialog from '../../components/ModalDialog.vue'
import SvgIcon from '../../components/SvgIcon.vue'
import FormField from '../../components/ui/FormField.vue'
import IconButton from '../../components/ui/IconButton.vue'
import { icons } from '../../icons'
import { confirmAction } from '../../stores/confirm'
import { showError, showMessage } from '../../stores/toast'
import { formatAgo, formatBitrate, formatBytesRu } from '../../utils/format'

const JsonEditor = defineAsyncComponent(() => import('../../components/JsonEditor.vue'))

// ServerSource — источники серверов: urltest-ы настраиваются на своей странице, selector-ы групп — на странице групп.
type ServerSource = Exclude<OutboundSource, 'urltest' | 'group'>

const outbounds = ref<OutboundView[]>([])
const loaded = ref(false)
const shareURL = ref('')
const adding = ref(false)
const shareError = ref('')
const sourceFilter = ref<'' | ServerSource>('')
const search = ref('')

// JSON-редактор: id пустой при добавлении.
const editor = ref<{ id: string; tag: string; text: string; error: string; saving: boolean } | null>(null)

const jsonTemplate = JSON.stringify({
  type: 'vless',
  tag: 'my-server',
  server: 'example.com',
  server_port: 443,
}, null, 2)

// sourceLabels — подписи источников outbound-ов.
const sourceLabels: Record<ServerSource, string> = {
  builtin: 'Встроенный',
  manual: 'Вручную',
  happ: 'Happ',
  amnezia: 'Amnezia',
}

// shareSchemes — поддерживаемые схемы share-ссылок.
const shareSchemes = ['vless', 'vmess', 'ss', 'hysteria2', 'hy2', 'trojan', 'wireguard', 'wg']

// Тест скорости: настройки, результаты по тегам, идущий замер и очередь «проверить все».
const speedSettings = ref<SpeedTestSettings | null>(null)
const speedResults = ref<Record<string, SpeedTestResult>>({})
const speedServer = ref('')
const speedRunning = ref('')
const speedQueue = ref<string[]>([])
const speedStopping = ref(false)

// canSpeedTest — через block трафик не идет, замерять нечего.
function canSpeedTest(outbound: OutboundView): boolean {
  return outbound.tag !== 'block'
}

// loadSpeedTest загружает серверы теста и последние результаты.
async function loadSpeedTest(): Promise<void> {
  try {
    const data = await get<SpeedTestState>('/api/speedtest')

    speedSettings.value = data.settings
    speedResults.value = data.results ?? {}
    speedRunning.value = data.running ?? ''
  } catch {
    // Тест скорости — дополнительная функция: страница работает и без него.
  }
}

// runSpeedTest замеряет скорость через outbound tag. Возвращает false, если замер не начался.
async function runSpeedTest(tag: string): Promise<boolean> {
  speedRunning.value = tag

  try {
    const result = await postQuiet<SpeedTestResult>('/api/speedtest/run', { tag, server: speedServer.value })

    speedResults.value = { ...speedResults.value, [tag]: result }

    return true
  } catch (error) {
    showError(error, `Тест скорости ${tag}`)

    return false
  } finally {
    speedRunning.value = ''
  }
}

// runSpeedTestAll замеряет по очереди все показанные outbound-ы.
async function runSpeedTestAll(): Promise<void> {
  const tags = filtered.value.filter(canSpeedTest).map((outbound) => outbound.tag)
  const seconds = tags.length * (speedSettings.value?.duration ?? 10)

  const confirmed = await confirmAction({
    title: `Замерить скорость ${tags.length} outbound-ов?`,
    message: `Замеры идут по очереди и займут не меньше ${Math.ceil(seconds / 60)} мин. Каждый скачивает тестовый файл `
      + `${speedSettings.value?.duration ?? 10} с — на быстром канале это сотни мегабайт трафика на outbound.`,
    confirmText: 'Начать',
  })

  if (!confirmed) {
    return
  }

  speedQueue.value = tags
  speedStopping.value = false

  while (speedQueue.value.length && !speedStopping.value) {
    const [tag, ...rest] = speedQueue.value

    speedQueue.value = rest

    if (!(await runSpeedTest(tag))) {
      break
    }
  }

  speedQueue.value = []
  speedStopping.value = false
}

// streamsWord склоняет слово «поток».
function streamsWord(count: number): string {
  if (count % 10 === 1 && count % 100 !== 11) {
    return 'поток'
  }

  return count % 10 >= 2 && count % 10 <= 4 && (count % 100 < 12 || count % 100 > 14) ? 'потока' : 'потоков'
}

// speedLevel возвращает класс цвета скорости.
function speedLevel(result: SpeedTestResult): string {
  const mbps = (result.download * 8) / 1e6

  if (mbps >= 50) {
    return 'd-good'
  }

  if (mbps >= 15) {
    return 'd-ok'
  }

  return mbps >= 5 ? 'd-slow' : 'd-bad'
}

// speedTitle возвращает подробности замера для подсказки.
function speedTitle(result: SpeedTestResult): string {
  const lines: string[] = []

  if (result.server) {
    lines.push(`Сервер: ${result.server}`)
    lines.push(`Скачано ${formatBytesRu(result.bytes)} за ${(result.duration_ms / 1000).toFixed(1)} с`)
    lines.push(`Отклик (до первого байта): ${result.latency_ms} мс`)
  }

  for (const attempt of result.attempts ?? []) {
    lines.push(`${attempt.server}: ${attempt.error}`)
  }

  lines.push(`Замер: ${formatAgo(result.tested_at)}`)

  return lines.join('\n')
}

// shareProblem — ссылка не похожа на поддерживаемую share-ссылку.
const shareProblem = computed(() => {
  const scheme = shareURL.value.trim().match(/^([a-z0-9]+):\/\//i)?.[1]?.toLowerCase()

  if (!shareURL.value.trim() || !scheme || shareSchemes.includes(scheme)) {
    return ''
  }

  return `Ссылки ${scheme}:// не поддерживаются — добавьте такой сервер JSON-ом`
})

const filtered = computed(() => {
  const query = search.value.trim().toLowerCase()

  return outbounds.value.filter((outbound) => {
    if (sourceFilter.value && outbound.source !== sourceFilter.value) {
      return false
    }

    return !query || outbound.tag.toLowerCase().includes(query) || (outbound.server ?? '').toLowerCase().includes(query)
  })
})

// load загружает outbound-ы всех источников.
async function load(): Promise<void> {
  try {
    const data = await get<{ outbounds: OutboundView[] }>('/api/outbounds')

    outbounds.value = (data.outbounds ?? []).filter((outbound) => outbound.source !== 'urltest')
  } catch (error) {
    showError(error, 'Ошибка загрузки outbound-ов')
  } finally {
    loaded.value = true
  }
}

// addShare добавляет outbound из share-ссылки.
async function addShare(): Promise<void> {
  adding.value = true
  shareError.value = ''

  try {
    const result = await post('/api/outbounds/add', { shareUrl: shareURL.value.trim() })

    showMessage(result.message ?? 'Outbound добавлен')
    shareURL.value = ''
    await load()
  } catch (error) {
    shareError.value = error instanceof Error ? error.message : String(error)
  } finally {
    adding.value = false
  }
}

// openEditor открывает JSON-редактор для нового или существующего outbound-а.
function openEditor(outbound?: OutboundView): void {
  editor.value = {
    id: outbound?.id ?? '',
    tag: outbound?.tag ?? '',
    text: outbound?.config ? JSON.stringify(outbound.config, null, 2) : jsonTemplate,
    error: '',
    saving: false,
  }
}

// saveEditor сохраняет outbound из JSON-редактора.
async function saveEditor(): Promise<void> {
  const current = editor.value

  if (!current) {
    return
  }

  let config: unknown

  try {
    config = JSON.parse(current.text)

    if (!config || typeof config !== 'object' || Array.isArray(config)) {
      throw new Error('нужен JSON-объект outbound-а')
    }
  } catch (error) {
    current.error = error instanceof Error ? error.message : String(error)

    return
  }

  current.saving = true
  current.error = ''

  try {
    if (current.id) {
      await post('/api/outbounds/edit', { id: current.id, config })
    } else {
      await post('/api/outbounds/add-json', { config })
    }

    showMessage('Outbound сохранен')
    editor.value = null
    await load()
  } catch (error) {
    current.error = error instanceof Error ? error.message : String(error)
  } finally {
    current.saving = false
  }
}

// remove удаляет outbound, добавленный вручную.
async function remove(outbound: OutboundView): Promise<void> {
  const confirmed = await confirmAction({
    title: `Удалить outbound ${outbound.tag}?`,
    message: 'Группы, у которых он выбран по умолчанию, после применения конфига получат block.',
    confirmText: 'Удалить',
    danger: true,
  })

  if (!confirmed) {
    return
  }

  try {
    await post('/api/outbounds/delete', { id: outbound.id })
    showMessage('Outbound удален')
    await load()
  } catch (error) {
    showError(error)
  }
}

onMounted(() => {
  void load()
  void loadSpeedTest()
})
</script>

<template>
  <Teleport to="#topbar-actions" defer>
    <button class="btn btn-secondary" @click="openEditor()">
      <SvgIcon class="btn-icon" :path="icons.plus" />
      Добавить JSON-ом
    </button>
  </Teleport>

  <p class="page-intro">
    Серверы, через которые может идти трафик: добавленные вручную и полученные из подписок. Все они доступны в
    selector-ах групп, а в автовыбор попадают по правилам раздела
    <RouterLink :to="{ name: 'urltests' }">URLTest</RouterLink>.
  </p>

  <div class="add-form-card">
    <form @submit.prevent="addShare">
      <FormField label="Добавить по share-ссылке" input-id="shareURL" :error="shareError || shareProblem">
        <div class="input-group">
          <input
            id="shareURL"
            v-model="shareURL"
            class="form-input is-mono"
            type="text"
            placeholder="vless://..., ss://... или vmess://..."
            autocomplete="off"
            spellcheck="false"
            required
            @input="shareError = ''"
          >
          <button type="submit" class="btn btn-primary" :disabled="adding || Boolean(shareProblem)">
            {{ adding ? 'Добавление...' : 'Добавить' }}
          </button>
        </div>
        <template #hint>
          Поддерживаются <code>vless://</code>, <code>vmess://</code>, <code>ss://</code>, <code>hysteria2://</code>,
          <code>trojan://</code> и <code>wireguard://</code>. Другие серверы можно
          <button type="button" class="link-button" @click="openEditor()">добавить JSON-ом</button>.
        </template>
      </FormField>
    </form>
  </div>

  <div class="table-toolbar">
    <FormField label="Поиск" input-id="outboundSearch">
      <div class="search-input">
        <SvgIcon :path="icons.search" />
        <input id="outboundSearch" v-model="search" class="form-input" type="search" placeholder="Тег или адрес">
      </div>
    </FormField>
    <FormField label="Источник" input-id="sourceFilter">
      <select id="sourceFilter" v-model="sourceFilter" class="form-select">
        <option value="">Все</option>
        <option v-for="(label, source) in sourceLabels" :key="source" :value="source">{{ label }}</option>
      </select>
    </FormField>
    <FormField v-if="speedSettings" label="Сервер теста скорости" input-id="speedServer">
      <select id="speedServer" v-model="speedServer" class="form-select" :disabled="Boolean(speedRunning)">
        <option value="">Первый доступный</option>
        <option v-for="server in speedSettings.servers" :key="server.url" :value="server.url">{{ server.name }}</option>
      </select>
    </FormField>
    <span class="table-count">
      {{ filtered.length }} из {{ outbounds.length }}
      <template v-if="speedSettings">
        <button
          v-if="speedQueue.length === 0"
          type="button"
          class="btn btn-secondary btn-sm"
          :disabled="Boolean(speedRunning) || filtered.length === 0"
          title="Замерить скорость всех показанных outbound-ов по очереди"
          @click="runSpeedTestAll"
        >
          <SvgIcon class="btn-icon" :path="icons.speedometer" />
          Замерить все
        </button>
        <button v-else type="button" class="btn btn-secondary btn-sm" :disabled="speedStopping" @click="speedStopping = true">
          {{ speedStopping ? 'Остановка...' : `Остановить (осталось ${speedQueue.length})` }}
        </button>
      </template>
    </span>
  </div>

  <p v-if="speedSettings" class="card-hint speed-hint">
    Тест скорости скачивает файл через outbound {{ speedSettings.duration }} с в {{ speedSettings.streams }}
    {{ streamsWord(speedSettings.streams) }}. Если тестовый сервер недоступен через outbound (заблокирован
    в стране роутера или выхода), берется следующий по списку. Серверы настраиваются в
    <RouterLink :to="{ name: 'system-settings' }">настройках</RouterLink>.
  </p>

  <div class="data-table">
    <table class="table">
      <thead>
        <tr>
          <th>Тег</th>
          <th>Протокол</th>
          <th>Адрес</th>
          <th>Источник</th>
          <th v-if="speedSettings">Скорость</th>
          <th class="col-actions"></th>
        </tr>
      </thead>
      <tbody>
        <tr v-if="loaded && outbounds.length === 0">
          <td :colspan="speedSettings ? 6 : 5" class="empty-state">Outbound-ов нет.</td>
        </tr>
        <tr v-else-if="loaded && filtered.length === 0">
          <td :colspan="speedSettings ? 6 : 5" class="empty-state">Ничего не найдено.</td>
        </tr>
        <tr v-for="outbound in filtered" :key="`${outbound.source}:${outbound.tag}`">
          <td><span class="cell-main">{{ outbound.tag }}</span></td>
          <td><span class="badge badge-domain">{{ outbound.type }}</span></td>
          <td>
            <span v-if="outbound.server" class="cell-mono">{{ outbound.server }}{{ outbound.port ? `:${outbound.port}` : '' }}</span>
            <span v-else class="muted">—</span>
          </td>
          <td>
            <span class="badge badge-source">{{ sourceLabels[outbound.source as ServerSource] }}</span>
            <div v-if="outbound.source_name" class="cell-sub">{{ outbound.source_name }}</div>
          </td>
          <td v-if="speedSettings" class="speed-cell">
            <div v-if="canSpeedTest(outbound)" class="speed-result">
              <IconButton
                icon="speedometer"
                :title="speedRunning === outbound.tag ? 'Идет замер...' : 'Замерить скорость'"
                :disabled="Boolean(speedRunning) || speedQueue.length > 0"
                :class="{ 'is-spinning': speedRunning === outbound.tag }"
                @click="runSpeedTest(outbound.tag)"
              />
              <span v-if="speedRunning === outbound.tag" class="muted">Замер...</span>
              <template v-else-if="speedResults[outbound.tag]">
                <span
                  v-if="!speedResults[outbound.tag].error"
                  class="speed-value"
                  :title="speedTitle(speedResults[outbound.tag])"
                >
                  <span class="proxy-delay" :class="speedLevel(speedResults[outbound.tag])">{{ formatBitrate(speedResults[outbound.tag].download) }}</span>
                  <span class="cell-sub">
                    {{ speedResults[outbound.tag].latency_ms }} мс · {{ speedResults[outbound.tag].server }}
                    <span v-if="speedResults[outbound.tag].warning" class="field-warning" :title="speedResults[outbound.tag].warning"> · мало данных</span>
                  </span>
                </span>
                <span v-else class="speed-error" :title="`${speedResults[outbound.tag].error}\n\n${speedTitle(speedResults[outbound.tag])}`">
                  <RouterLink v-if="speedResults[outbound.tag].error?.includes('примените конфиг')" :to="{ name: 'config' }">Примените конфиг</RouterLink>
                  <template v-else>{{ speedResults[outbound.tag].error }}</template>
                </span>
              </template>
            </div>
          </td>
          <td class="actions-cell">
            <div v-if="outbound.source === 'manual'" class="row-actions">
              <IconButton icon="edit" title="Изменить JSON" @click="openEditor(outbound)" />
              <IconButton icon="trash" title="Удалить" danger @click="remove(outbound)" />
            </div>
            <span
              v-else
              class="muted cell-sub"
              :title="outbound.source === 'builtin' ? 'Встроенный outbound' : 'Управляется подпиской'"
            >{{ outbound.source === 'builtin' ? '' : 'из подписки' }}</span>
          </td>
        </tr>
      </tbody>
    </table>
  </div>

  <ModalDialog
    v-if="editor"
    :title="editor.id ? `Outbound ${editor.tag}` : 'Новый outbound'"
    subtitle="Объект outbound-а sing-box. WireGuard (&quot;type&quot;: &quot;wireguard&quot;) попадет в секцию endpoints."
    :autofocus="false"
    wide
    @close="editor = null"
  >
    <form @submit.prevent="saveEditor">
      <JsonEditor v-model="editor.text" height="360px" :invalid="Boolean(editor.error)" />
      <div v-if="editor.error" class="form-error" style="margin-top: 12px;">{{ editor.error }}</div>
      <div class="form-actions">
        <button type="button" class="btn btn-secondary" @click="editor = null">Отмена</button>
        <button type="submit" class="btn btn-primary" :disabled="editor.saving">{{ editor.saving ? 'Сохранение...' : 'Сохранить' }}</button>
      </div>
    </form>
  </ModalDialog>
</template>
