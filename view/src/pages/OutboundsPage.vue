<script setup lang="ts">
import { computed, defineAsyncComponent, onMounted, ref } from 'vue'

import { get, post } from '../api/client'
import type { OutboundSource, OutboundView } from '../api/types'
import ModalDialog from '../components/ModalDialog.vue'
import { showError, showMessage } from '../stores/toast'

const JsonEditor = defineAsyncComponent(() => import('../components/JsonEditor.vue'))

const outbounds = ref<OutboundView[]>([])
const loaded = ref(false)
const shareURL = ref('')
const sourceFilter = ref<'' | OutboundSource>('')

// JSON-редактор: id пустой при добавлении.
const editor = ref<{ id: string; text: string; error: string } | null>(null)

const jsonTemplate = JSON.stringify({
  type: 'vless',
  tag: 'my-server',
  server: 'example.com',
  server_port: 443,
}, null, 2)

// sourceLabels — подписи источников outbound-ов.
const sourceLabels: Record<OutboundSource, string> = {
  builtin: 'Встроенный',
  manual: 'Вручную',
  happ: 'Happ',
  amnezia: 'Amnezia',
}

const filtered = computed(() => outbounds.value.filter((outbound) => !sourceFilter.value || outbound.source === sourceFilter.value))

// load загружает outbound-ы всех источников.
async function load(): Promise<void> {
  try {
    const data = await get<{ outbounds: OutboundView[] }>('/api/outbounds')

    outbounds.value = data.outbounds ?? []
  } catch (error) {
    showError(error, 'Ошибка загрузки outbound-ов')
  } finally {
    loaded.value = true
  }
}

// addShare добавляет outbound из share-ссылки.
async function addShare(): Promise<void> {
  try {
    const result = await post('/api/outbounds/add', { shareUrl: shareURL.value.trim() })

    showMessage(result.message ?? 'Outbound добавлен')
    shareURL.value = ''
    await load()
  } catch (error) {
    showError(error)
  }
}

// openEditor открывает JSON-редактор для нового или существующего outbound-а.
function openEditor(outbound?: OutboundView): void {
  editor.value = {
    id: outbound?.id ?? '',
    text: outbound?.config ? JSON.stringify(outbound.config, null, 2) : jsonTemplate,
    error: '',
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
  }
}

// remove удаляет outbound, добавленный вручную.
async function remove(outbound: OutboundView): Promise<void> {
  if (!confirm(`Удалить outbound ${outbound.tag}?`)) {
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
  <div class="add-form-card">
    <div class="form-header">Добавить outbound</div>
    <p class="card-hint" style="margin-bottom: 16px;">
      Share-ссылки vless://, hysteria2://, trojan://, wireguard:// разворачиваются в объект sing-box. Любой другой
      outbound можно добавить JSON-ом. Все outbound-ы, кроме selector-ов, попадают во встроенный urltest
      <code>auto</code> и в selector-ы групп.
    </p>
    <form @submit.prevent="addShare">
      <div class="form-row">
        <div class="form-group">
          <label class="form-label" for="shareURL">Share-ссылка</label>
          <input id="shareURL" v-model="shareURL" class="form-input" type="text" placeholder="vless://... или hysteria2://..." required>
        </div>
      </div>
      <div class="form-actions">
        <button type="button" class="btn btn-secondary" @click="openEditor()">Добавить JSON</button>
        <button type="submit" class="btn btn-primary">Добавить</button>
      </div>
    </form>
  </div>

  <div class="table-toolbar">
    <div class="form-group">
      <label class="form-label" for="sourceFilter">Источник</label>
      <select id="sourceFilter" v-model="sourceFilter" class="form-select">
        <option value="">Все</option>
        <option v-for="(label, source) in sourceLabels" :key="source" :value="source">{{ label }}</option>
      </select>
    </div>
    <span class="table-count">{{ filtered.length }} из {{ outbounds.length }}</span>
  </div>

  <div class="data-table">
    <table class="table">
      <thead>
        <tr>
          <th>Тег</th>
          <th>Тип</th>
          <th>Сервер</th>
          <th>Порт</th>
          <th>Источник</th>
          <th style="width: 150px; text-align: center;">Действия</th>
        </tr>
      </thead>
      <tbody>
        <tr v-if="loaded && filtered.length === 0">
          <td colspan="6" class="empty-state">Outbound-ов нет.</td>
        </tr>
        <tr v-for="outbound in filtered" :key="`${outbound.source}:${outbound.tag}`">
          <td><strong>{{ outbound.tag }}</strong></td>
          <td><span class="badge badge-domain">{{ outbound.type }}</span></td>
          <td>{{ outbound.server || '—' }}</td>
          <td>{{ outbound.port || '—' }}</td>
          <td>
            <span class="badge badge-source">{{ sourceLabels[outbound.source] }}</span>
            <span v-if="outbound.source_name" class="muted" style="margin-left: 6px;">{{ outbound.source_name }}</span>
          </td>
          <td class="actions-cell">
            <template v-if="outbound.source === 'manual'">
              <button class="btn btn-secondary" @click="openEditor(outbound)">Редактировать</button>
              <button class="btn btn-danger" @click="remove(outbound)">Удалить</button>
            </template>
            <span v-else class="muted">—</span>
          </td>
        </tr>
      </tbody>
    </table>
  </div>

  <ModalDialog v-if="editor" :title="editor.id ? 'Редактировать outbound' : 'Добавить outbound'" wide @close="editor = null">
    <p class="card-hint" style="margin-bottom: 12px;">
      Объект outbound-а sing-box. WireGuard (<code>"type": "wireguard"</code>) попадет в секцию endpoints.
    </p>
    <JsonEditor v-model="editor.text" height="360px" :invalid="Boolean(editor.error)" />
    <p v-if="editor.error" class="code-error">{{ editor.error }}</p>
    <div class="form-actions" style="margin-top: 16px;">
      <button type="button" class="btn btn-secondary" @click="editor = null">Отмена</button>
      <button type="button" class="btn btn-primary" @click="saveEditor">Сохранить</button>
    </div>
  </ModalDialog>
</template>
