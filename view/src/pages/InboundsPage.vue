<script setup lang="ts">
import { onMounted, ref } from 'vue'

import { get, post } from '../api/client'
import type { MixedInbound } from '../api/types'
import ModalDialog from '../components/ModalDialog.vue'
import { showError, showMessage } from '../stores/toast'

const mixed = ref<MixedInbound[]>([])
const loaded = ref(false)

// Редактор mixed-inbound-а: editing — тег редактируемого или пустая строка для нового.
const editor = ref<{ editing: string; value: MixedInbound } | null>(null)

// load загружает mixed-inbound-ы.
async function load(): Promise<void> {
  try {
    const data = await get<{ mixed: MixedInbound[] }>('/api/inbounds')

    mixed.value = data.mixed ?? []
  } catch (error) {
    showError(error, 'Ошибка загрузки inbound-ов')
  } finally {
    loaded.value = true
  }
}

// openEditor открывает форму нового или существующего inbound-а.
function openEditor(inbound?: MixedInbound): void {
  editor.value = {
    editing: inbound?.tag ?? '',
    value: inbound
      ? { ...inbound, users: inbound.users.map((user) => ({ ...user })) }
      : { tag: 'mixed-proxy', listen: '0.0.0.0', listen_port: 1080, users: [], extra: null },
  }
}

// save сохраняет inbound.
async function save(): Promise<void> {
  if (!editor.value) {
    return
  }

  try {
    await post(editor.value.editing ? '/api/inbounds/mixed/edit' : '/api/inbounds/mixed/add', editor.value.value)
    showMessage('Inbound сохранен')
    editor.value = null
    await load()
  } catch (error) {
    showError(error)
  }
}

// remove удаляет inbound.
async function remove(inbound: MixedInbound): Promise<void> {
  if (!confirm(`Удалить inbound ${inbound.tag}?`)) {
    return
  }

  try {
    await post('/api/inbounds/mixed/delete', { tag: inbound.tag })
    showMessage('Inbound удален')
    await load()
  } catch (error) {
    showError(error)
  }
}

onMounted(load)
</script>

<template>
  <Teleport to="#topbar-actions" defer>
    <button class="btn btn-primary" @click="openEditor()">Добавить mixed-прокси</button>
  </Teleport>

  <h3 class="section-title">Встроенные inbound-ы</h3>
  <p class="section-hint">Всегда присутствуют в итоговом конфиге.</p>

  <div class="data-table">
    <table class="table">
      <thead>
        <tr>
          <th>Тег</th>
          <th>Тип</th>
          <th>Параметры</th>
        </tr>
      </thead>
      <tbody>
        <tr>
          <td><strong>tun-in</strong></td>
          <td><span class="badge badge-domain">tun</span></td>
          <td class="description-cell">
            tun0, 198.18.0.1/30, auto_route и auto_redirect. Исключения группы bypass отсекаются в nftables
            (<code>route_exclude_address_set</code>).
          </td>
        </tr>
        <tr>
          <td><strong>dns-in</strong></td>
          <td><span class="badge badge-domain">direct</span></td>
          <td class="description-cell">0.0.0.0:53 — DNS-сервер для устройств сети, запросы перехватываются hijack-dns.</td>
        </tr>
      </tbody>
    </table>
  </div>

  <h3 class="section-title" style="margin-top: 24px;">Mixed-прокси</h3>
  <p class="section-hint">
    HTTP и SOCKS5 на одном порту. Домены, полученные через прокси, резолвятся до маршрутизации (правило
    <code>resolve</code>), поэтому для них работают и IP-правила групп. Без пользователей прокси открыт для всех,
    кто может подключиться к порту.
  </p>

  <div class="data-table">
    <table class="table">
      <thead>
        <tr>
          <th>Тег</th>
          <th>Адрес</th>
          <th>Пользователи</th>
          <th style="width: 150px; text-align: center;">Действия</th>
        </tr>
      </thead>
      <tbody>
        <tr v-if="loaded && mixed.length === 0">
          <td colspan="4" class="empty-state">Mixed-прокси нет.</td>
        </tr>
        <tr v-for="inbound in mixed" :key="inbound.tag">
          <td><strong>{{ inbound.tag }}</strong></td>
          <td><code>{{ inbound.listen }}:{{ inbound.listen_port }}</code></td>
          <td>
            <span v-if="inbound.users.length">{{ inbound.users.map((user) => user.username).join(', ') }}</span>
            <span v-else class="status-badge status-pending">без авторизации</span>
          </td>
          <td class="actions-cell">
            <button class="btn btn-secondary" @click="openEditor(inbound)">Редактировать</button>
            <button class="btn btn-danger" @click="remove(inbound)">Удалить</button>
          </td>
        </tr>
      </tbody>
    </table>
  </div>

  <ModalDialog v-if="editor" :title="editor.editing ? `Inbound ${editor.editing}` : 'Новый mixed-прокси'" @close="editor = null">
    <form @submit.prevent="save">
      <div class="form-group">
        <label class="form-label" for="inboundTag">Тег</label>
        <input id="inboundTag" v-model="editor.value.tag" class="form-input" type="text" required :disabled="Boolean(editor.editing)">
      </div>
      <div class="form-row">
        <div class="form-group">
          <label class="form-label" for="inboundListen">Адрес</label>
          <input id="inboundListen" v-model="editor.value.listen" class="form-input" type="text" placeholder="0.0.0.0">
        </div>
        <div class="form-group">
          <label class="form-label" for="inboundPort">Порт</label>
          <input id="inboundPort" v-model.number="editor.value.listen_port" class="form-input" type="number" min="1" max="65535" required>
        </div>
      </div>
      <div class="form-group">
        <label class="form-label">Пользователи</label>
        <div class="list-editor">
          <div v-for="(user, index) in editor.value.users" :key="index" class="list-editor-row">
            <input v-model="user.username" class="form-input" type="text" placeholder="Имя" required>
            <input v-model="user.password" class="form-input" type="text" placeholder="Пароль" required>
            <button type="button" class="btn btn-danger btn-sm" @click="editor.value.users.splice(index, 1)">Удалить</button>
          </div>
          <div>
            <button type="button" class="btn btn-secondary btn-sm" @click="editor.value.users.push({ username: '', password: '' })">
              Добавить пользователя
            </button>
          </div>
        </div>
      </div>
      <div class="form-actions">
        <button type="button" class="btn btn-secondary" @click="editor = null">Отмена</button>
        <button type="submit" class="btn btn-primary">Сохранить</button>
      </div>
    </form>
  </ModalDialog>
</template>
