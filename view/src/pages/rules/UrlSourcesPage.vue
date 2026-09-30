<script setup lang="ts">
import { computed, ref } from 'vue'

import { get, post, postQuiet } from '../../api/client'
import type { OutboundView, URLSource } from '../../api/types'
import GroupBadge from '../../components/GroupBadge.vue'
import ModalDialog from '../../components/ModalDialog.vue'
import OutboundSelect from '../../components/OutboundSelect.vue'
import SvgIcon from '../../components/SvgIcon.vue'
import FormField from '../../components/ui/FormField.vue'
import HelpHint from '../../components/ui/HelpHint.vue'
import IconButton from '../../components/ui/IconButton.vue'
import { groupLabel, useGroups } from '../../composables/useGroups'
import { usePolling } from '../../composables/usePolling'
import { icons } from '../../icons'
import { confirmAction } from '../../stores/confirm'
import { showError, showMessage } from '../../stores/toast'
import { formatAgo, formatDateTime } from '../../utils/format'

// intervals — частота обновления списка, в минутах.
const intervals = [
  { value: 15, label: 'Каждые 15 минут' },
  { value: 30, label: 'Каждые 30 минут' },
  { value: 60, label: 'Каждый час' },
  { value: 180, label: 'Каждые 3 часа' },
  { value: 360, label: 'Каждые 6 часов' },
  { value: 720, label: 'Каждые 12 часов' },
  { value: 1440, label: 'Раз в сутки' },
]

const { groups, loadGroups } = useGroups()

const sources = ref<URLSource[]>([])
const loaded = ref(false)
const applying = ref(false)

// outbounds — outbound-ы для выбора detour (загружаются при открытии формы).
const outbounds = ref<OutboundView[]>([])

// detourOptions — через что можно загружать список: selector-ы групп и outbound-ы, кроме block.
const detourOptions = computed<OutboundView[]>(() => [
  ...groups.value
    .filter((group) => !group.system)
    .map((group) => ({ tag: `select-${group.name}`, type: 'selector', source: 'group' as const, source_name: group.description || group.name })),
  ...outbounds.value.filter((outbound) => outbound.tag !== 'block'),
])

// ID источников, которые сейчас загружаются вручную.
const refreshing = ref<string[]>([])

// Форма источника: id пустой при добавлении (у существующего меняются только группа и описание).
const editor = ref<{
  id: string
  url: string
  group: string
  interval: number
  description: string
  detour: string
  error: string
  saving: boolean
  check: { state: 'idle' | 'checking' | 'ok' | 'bad'; text: string }
} | null>(null)

const viewing = ref<{ source: URLSource; cidrList: string[]; domains: string[]; domainSuffixes: string[] } | null>(null)

// pendingCount — неприменённые изменения источников.
const pendingCount = computed(() => sources.value.filter((source) => !source.applied || source.deleted).length)

// load загружает источники и группы.
async function load(): Promise<void> {
  try {
    const data = await get<{ url_sources: URLSource[] }>('/api/url-sources')

    sources.value = data.url_sources ?? []

    await loadGroups()
  } catch (error) {
    showError(error, 'Ошибка загрузки источников')
  } finally {
    loaded.value = true
  }
}

usePolling(load, 5000)

// intervalLabel возвращает подпись интервала обновления.
function intervalLabel(minutes: number): string {
  const preset = intervals.find((item) => item.value === minutes)

  if (preset) {
    return preset.label.toLowerCase()
  }

  return minutes % 60 === 0 ? `каждые ${minutes / 60} ч` : `каждые ${minutes} мин`
}

// openEditor открывает форму нового или существующего источника.
function openEditor(source?: URLSource): void {
  editor.value = {
    id: source?.id ?? '',
    url: source?.url ?? '',
    group: source?.group ?? groups.value.find((group) => !group.system)?.name ?? groups.value[0]?.name ?? '',
    interval: source?.interval ?? 60,
    description: source?.description ?? '',
    detour: source?.detour ?? '',
    error: '',
    saving: false,
    check: { state: 'idle', text: '' },
  }

  void loadOutbounds()
}

// loadOutbounds загружает outbound-ы для выбора detour.
async function loadOutbounds(): Promise<void> {
  try {
    const data = await get<{ outbounds: OutboundView[] }>('/api/outbounds')

    outbounds.value = data.outbounds ?? []
  } catch (error) {
    showError(error, 'Ошибка загрузки outbound-ов')
  }
}

// validate проверяет, что URL отдает список правил. Результат показывается под полем.
async function validate(): Promise<void> {
  const current = editor.value

  if (!current?.url.trim()) {
    return
  }

  current.check = { state: 'checking', text: 'Загружаю список...' }

  try {
    const result = await postQuiet<{ valid: boolean; error: string; count: number }>('/api/url-sources/validate', {
      url: current.url.trim(),
      detour: current.detour,
    })

    current.check = result.valid
      ? { state: 'ok', text: `Список загружен, записей: ${result.count}` }
      : { state: 'bad', text: result.error || 'Список не распознан' }
  } catch (error) {
    current.check = { state: 'bad', text: error instanceof Error ? error.message : String(error) }
  }
}

// save добавляет источник или сохраняет группу и описание существующего.
async function save(): Promise<void> {
  const current = editor.value

  if (!current) {
    return
  }

  current.saving = true
  current.error = ''

  try {
    if (current.id) {
      await post('/api/url-sources/edit', {
        id: current.id,
        group: current.group,
        description: current.description.trim(),
        detour: current.detour,
      })
      showMessage('Источник сохранен')
    } else {
      await post('/api/url-sources/add', {
        url: current.url.trim(),
        group: current.group,
        interval: current.interval,
        description: current.description.trim(),
        detour: current.detour,
      })

      showMessage('Источник добавлен. Примените источники, чтобы список загрузился.')
    }

    editor.value = null
    await load()
  } catch (error) {
    current.error = error instanceof Error ? error.message : String(error)
  } finally {
    current.saving = false
  }
}

// refresh загружает список источника сейчас.
async function refresh(source: URLSource): Promise<void> {
  refreshing.value = [...refreshing.value, source.id]

  try {
    await post('/api/url-sources/refresh', { id: source.id })
    showMessage('Список источника загружен')
  } catch (error) {
    showError(error, 'Ошибка загрузки источника')
  } finally {
    refreshing.value = refreshing.value.filter((id) => id !== source.id)
    await load()
  }
}

// view показывает загруженные правила источника.
async function view(source: URLSource): Promise<void> {
  try {
    const data = await get<{ cidrList: string[]; domains: string[]; domainSuffixes: string[] }>(`/api/url-sources/rules?id=${encodeURIComponent(source.id)}`)

    viewing.value = { source, ...data }
  } catch (error) {
    showError(error)
  }
}

// remove помечает источник на удаление.
async function remove(source: URLSource): Promise<void> {
  const confirmed = await confirmAction({
    title: 'Удалить источник?',
    message: `${source.url}\n\nАдреса из этого списка перестанут действовать после применения источников.`,
    confirmText: 'Удалить',
    danger: true,
  })

  if (!confirmed) {
    return
  }

  try {
    await post('/api/url-sources/delete', { id: source.id })
    showMessage('Источник помечен на удаление')
    await load()
  } catch (error) {
    showError(error)
  }
}

// apply применяет изменения источников.
async function apply(): Promise<void> {
  applying.value = true

  try {
    await post('/api/url-sources/apply')
    showMessage('Источники применены')
    await load()
  } catch (error) {
    showError(error)
  } finally {
    applying.value = false
  }
}

// loadStatus описывает результат последней загрузки источника.
function loadStatus(source: URLSource): { dot: string; text: string } {
  if (source.last_status === 'success') {
    return { dot: 'is-good', text: `${source.items_count || 0} записей · ${formatAgo(source.last_update)}` }
  }

  if (source.last_status === 'error') {
    return { dot: 'is-bad', text: 'Ошибка загрузки' }
  }

  return { dot: '', text: 'Еще не загружался' }
}

// applyStatus описывает, применен ли источник.
function applyStatus(source: URLSource): { cls: string; text: string } {
  if (source.deleted) {
    return { cls: 'status-deleted', text: 'К удалению' }
  }

  return source.applied ? { cls: 'status-applied', text: 'Работает' } : { cls: 'status-pending', text: 'Не применен' }
}
</script>

<template>
  <Teleport to="#topbar-actions" defer>
    <button class="btn btn-warning" :disabled="pendingCount === 0 || applying" @click="apply">
      {{ applying ? 'Применение...' : 'Применить источники' }}
      <span v-if="pendingCount > 0" class="pending-badge">{{ pendingCount }}</span>
    </button>
    <button class="btn btn-primary" @click="openEditor()">
      <SvgIcon class="btn-icon" :path="icons.plus" />
      Добавить источник
    </button>
  </Teleport>

  <p class="page-intro">
    Списки доменов и подсетей по ссылке, например готовые списки сервисов на GitHub. Конфигуратор сам скачивает
    их с заданной частотой и добавляет в выбранную группу.
  </p>

  <HelpHint>
    <p>
      Подходит текстовый файл, одна запись на строку: домен (действует вместе с поддоменами), <code>full:домен</code>
      (только точный домен), IP-адрес или подсеть CIDR. Пустые строки и комментарии с <code>#</code> или
      <code>//</code> пропускаются. Кнопка «Проверить» в форме покажет, сколько записей удалось распознать.
    </p>
    <p>Новый или удаленный источник начинает действовать после применения источников.</p>
    <p>
      Загруженные списки сохраняются на диске: после перезапуска конфигуратор сразу отдает их sing-box, даже если
      источник пока недоступен. Если список не загрузился, следующая попытка будет через минуту, затем реже.
    </p>
  </HelpHint>

  <div v-if="pendingCount > 0" class="pending-bar">
    <span class="pending-bar-text">
      Не применено изменений: {{ pendingCount }}
      <small>Новые источники загрузятся, а удаленные исчезнут из конфига после применения.</small>
    </span>
    <button class="btn btn-warning btn-sm" :disabled="applying" @click="apply">Применить</button>
  </div>

  <div class="data-table">
    <table class="table">
      <thead>
        <tr>
          <th>Источник</th>
          <th>Группа</th>
          <th>Последняя загрузка</th>
          <th>Обновление</th>
          <th>Статус</th>
          <th class="col-actions"></th>
        </tr>
      </thead>
      <tbody>
        <tr v-if="loaded && sources.length === 0">
          <td colspan="6" class="empty-state">Источников нет. Добавьте первый кнопкой «Добавить источник».</td>
        </tr>
        <tr v-for="source in sources" :key="source.id">
          <td class="url-cell" :title="source.url">
            <span class="cell-mono">{{ source.url }}</span>
            <div v-if="source.description || source.detour" class="cell-sub">
              {{ source.description }}<template v-if="source.description && source.detour"> · </template>
              <template v-if="source.detour">загрузка через {{ source.detour }}</template>
            </div>
          </td>
          <td><GroupBadge :group="source.group" /></td>
          <td>
            <div class="inline-status is-muted" :title="formatDateTime(source.last_update)">
              <span class="stat-dot" :class="loadStatus(source).dot"></span>
              <span>{{ loadStatus(source).text }}</span>
            </div>
            <div v-if="source.last_error" class="source-error" :title="source.last_error">{{ source.last_error }}</div>
          </td>
          <td class="date-cell">{{ intervalLabel(source.interval) }}</td>
          <td><span class="status-badge" :class="applyStatus(source).cls">{{ applyStatus(source).text }}</span></td>
          <td class="actions-cell">
            <div class="row-actions">
              <IconButton
                icon="refresh"
                :title="refreshing.includes(source.id) ? 'Загрузка...' : 'Загрузить сейчас, не дожидаясь расписания'"
                :disabled="!source.applied || source.deleted || refreshing.includes(source.id)"
                @click="refresh(source)"
              />
              <IconButton icon="eye" title="Посмотреть загруженные записи" :disabled="!source.applied" @click="view(source)" />
              <IconButton icon="edit" title="Изменить" :disabled="source.deleted" @click="openEditor(source)" />
              <IconButton icon="trash" :title="source.deleted ? 'Уже помечен на удаление' : 'Удалить'" danger :disabled="source.deleted" @click="remove(source)" />
            </div>
          </td>
        </tr>
      </tbody>
    </table>
  </div>

  <ModalDialog v-if="editor" :title="editor.id ? 'Источник URL' : 'Новый источник URL'" @close="editor = null">
    <form class="form-stack" @submit.prevent="save">
      <FormField label="Ссылка на список" input-id="sourceURL" :hint="editor.id ? 'Ссылка не меняется: чтобы сменить ее, добавьте новый источник.' : undefined">
        <div class="input-group">
          <input
            id="sourceURL"
            v-model="editor.url"
            class="form-input"
            type="url"
            placeholder="https://example.com/list.txt"
            required
            :disabled="Boolean(editor.id)"
            @input="editor.check = { state: 'idle', text: '' }"
          >
          <button
            v-if="!editor.id"
            type="button"
            class="btn btn-secondary"
            :disabled="!editor.url.trim() || editor.check.state === 'checking'"
            @click="validate"
          >
            Проверить
          </button>
        </div>
        <div
          v-if="editor.check.state !== 'idle'"
          class="inline-status"
          :class="{ 'is-good': editor.check.state === 'ok', 'is-bad': editor.check.state === 'bad', 'is-muted': editor.check.state === 'checking' }"
        >
          <SvgIcon v-if="editor.check.state !== 'checking'" :path="editor.check.state === 'ok' ? icons.check : icons.alert" />
          <span>{{ editor.check.text }}</span>
        </div>
      </FormField>

      <div class="form-grid">
        <FormField label="Группа" input-id="sourceGroup" hint="Куда добавить адреса из списка.">
          <select id="sourceGroup" v-model="editor.group" class="form-select" required>
            <option v-for="group in groups" :key="group.name" :value="group.name">{{ groupLabel(group) }}</option>
          </select>
        </FormField>
        <FormField label="Обновлять" input-id="sourceInterval" :hint="editor.id ? 'Частота задается при добавлении.' : undefined">
          <select id="sourceInterval" v-model.number="editor.interval" class="form-select" :disabled="Boolean(editor.id)">
            <option v-if="!intervals.some((item) => item.value === editor!.interval)" :value="editor.interval">{{ intervalLabel(editor.interval) }}</option>
            <option v-for="item in intervals" :key="item.value" :value="item.value">{{ item.label }}</option>
          </select>
        </FormField>
      </div>

      <FormField label="Загружать через" input-id="sourceDetour">
        <OutboundSelect
          v-model="editor.detour"
          :outbounds="detourOptions"
          input-id="sourceDetour"
          empty-label="Напрямую из конфигуратора"
          @update:model-value="editor.check = { state: 'idle', text: '' }"
        />
        <template #hint>
          Для списков на заблокированных сайтах выберите VPN: список загрузится через этот outbound sing-box.
          Начнет работать после <RouterLink :to="{ name: 'config' }">применения конфига</RouterLink>, пока sing-box
          не запущен — действует последний загруженный список.
        </template>
      </FormField>

      <FormField label="Описание" input-id="sourceDescription" optional>
        <input id="sourceDescription" v-model="editor.description" class="form-input" type="text" placeholder="Например, сервисы Google">
      </FormField>

      <div v-if="editor.error" class="form-error">{{ editor.error }}</div>

      <div class="form-actions">
        <button type="button" class="btn btn-secondary" @click="editor = null">Отмена</button>
        <button type="submit" class="btn btn-primary" :disabled="editor.saving">
          {{ editor.saving ? 'Сохранение...' : (editor.id ? 'Сохранить' : 'Добавить источник') }}
        </button>
      </div>
    </form>
  </ModalDialog>

  <ModalDialog v-if="viewing" title="Загруженные записи" :subtitle="viewing.source.url" wide @close="viewing = null">
    <div v-if="viewing.domainSuffixes.length" class="modal-section">
      <h5>Домены с поддоменами ({{ viewing.domainSuffixes.length }})</h5>
      <pre>{{ viewing.domainSuffixes.join('\n') }}</pre>
    </div>
    <div v-if="viewing.domains.length" class="modal-section">
      <h5>Точные домены ({{ viewing.domains.length }})</h5>
      <pre>{{ viewing.domains.join('\n') }}</pre>
    </div>
    <div v-if="viewing.cidrList.length" class="modal-section">
      <h5>Подсети и IP-адреса ({{ viewing.cidrList.length }})</h5>
      <pre>{{ viewing.cidrList.join('\n') }}</pre>
    </div>
    <div v-if="!viewing.cidrList.length && !viewing.domains.length && !viewing.domainSuffixes.length" class="empty-state">
      Записей нет: список пуст или еще не загружался.
    </div>
  </ModalDialog>
</template>
