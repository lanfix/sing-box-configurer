<script setup lang="ts">
import { computed, defineAsyncComponent, onMounted, ref } from 'vue'

import { get, post } from '../../api/client'
import type { OutboundSource, OutboundView } from '../../api/types'
import ModalDialog from '../../components/ModalDialog.vue'
import SvgIcon from '../../components/SvgIcon.vue'
import FormField from '../../components/ui/FormField.vue'
import IconButton from '../../components/ui/IconButton.vue'
import { icons } from '../../icons'
import { confirmAction } from '../../stores/confirm'
import { showError, showMessage } from '../../stores/toast'

const JsonEditor = defineAsyncComponent(() => import('../../components/JsonEditor.vue'))

// ServerSource — источники серверов: urltest-ы настраиваются на своей странице.
type ServerSource = Exclude<OutboundSource, 'urltest'>

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
const shareSchemes = ['vless', 'hysteria2', 'hy2', 'trojan', 'wireguard', 'wg']

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

onMounted(load)
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
            placeholder="vless://... или hysteria2://..."
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
          Поддерживаются <code>vless://</code>, <code>hysteria2://</code>, <code>trojan://</code> и
          <code>wireguard://</code>. Другие серверы можно
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
    <span class="table-count">{{ filtered.length }} из {{ outbounds.length }}</span>
  </div>

  <div class="data-table">
    <table class="table">
      <thead>
        <tr>
          <th>Тег</th>
          <th>Протокол</th>
          <th>Адрес</th>
          <th>Источник</th>
          <th class="col-actions"></th>
        </tr>
      </thead>
      <tbody>
        <tr v-if="loaded && outbounds.length === 0">
          <td colspan="5" class="empty-state">Outbound-ов нет.</td>
        </tr>
        <tr v-else-if="loaded && filtered.length === 0">
          <td colspan="5" class="empty-state">Ничего не найдено.</td>
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
