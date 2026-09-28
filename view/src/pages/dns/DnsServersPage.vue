<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'

import { get, post } from '../../api/client'
import type { DNSData, DNSServer, Group, OutboundView } from '../../api/types'
import ModalDialog from '../../components/ModalDialog.vue'
import { showError, showMessage } from '../../stores/toast'
import DnsServerForm from './DnsServerForm.vue'

const dns = ref<DNSData | null>(null)
const detours = ref<string[]>([])
const editing = ref<DNSServer | null>(null)

// formKey пересоздает форму добавления после успешного добавления.
const formKey = ref(0)

const emptyServer: DNSServer = {
  tag: '',
  type: 'https',
  server: '',
  server_port: undefined,
  path: '',
  tls_server_name: '',
  tls_insecure: false,
  detour: '',
  domain_resolver: '',
  description: '',
  extra: null,
}

const serverTags = computed(() => dns.value?.servers.map((server) => server.tag) ?? [])

// load загружает DNS-серверы и возможные detour-ы (outbound-ы и selector-ы групп).
async function load(): Promise<void> {
  try {
    const [data, outbounds, groups] = await Promise.all([
      get<DNSData>('/api/dns'),
      get<{ outbounds: OutboundView[] }>('/api/outbounds'),
      get<{ groups: Group[] }>('/api/groups'),
    ])

    dns.value = data
    detours.value = [
      ...outbounds.outbounds.filter((outbound) => outbound.source !== 'builtin' || outbound.tag === 'auto').map((outbound) => outbound.tag),
      ...groups.groups.filter((group) => !group.system).map((group) => `select-${group.name}`),
    ]
  } catch (error) {
    showError(error, 'Ошибка загрузки DNS')
  }
}

// add добавляет сервер.
async function add(server: DNSServer): Promise<void> {
  try {
    await post('/api/dns/servers/add', server)
    showMessage(`DNS-сервер ${server.tag} добавлен`)
    formKey.value++
    await load()
  } catch (error) {
    showError(error)
  }
}

// save сохраняет изменения сервера.
async function save(server: DNSServer): Promise<void> {
  try {
    await post('/api/dns/servers/edit', server)
    showMessage(`DNS-сервер ${server.tag} обновлен`)
    editing.value = null
    await load()
  } catch (error) {
    showError(error)
  }
}

// remove удаляет сервер.
async function remove(server: DNSServer): Promise<void> {
  if (!confirm(`Удалить DNS-сервер ${server.tag}?`)) {
    return
  }

  try {
    await post('/api/dns/servers/delete', { tag: server.tag })
    showMessage('DNS-сервер удален')
    await load()
  } catch (error) {
    showError(error)
  }
}

// address возвращает адрес сервера для таблицы.
function address(server: DNSServer): string {
  if (!server.server) {
    return '—'
  }

  const port = server.server_port ? `:${server.server_port}` : ''
  const path = server.path ?? ''

  if (server.type === 'https' || server.type === 'h3') {
    return `${server.type}://${server.server}${port}${path || '/dns-query'}`
  }

  return `${server.type}://${server.server}${port}`
}

// usage описывает, где используется сервер.
function usage(server: DNSServer): string[] {
  const result: string[] = []

  if (dns.value?.settings.final === server.tag) {
    result.push('final')
  }

  if (dns.value?.settings.default_domain_resolver === server.tag) {
    result.push('резолвер по умолчанию')
  }

  return result
}

onMounted(load)
</script>

<template>
  <div class="add-form-card">
    <div class="form-header">Добавить DNS-сервер</div>
    <DnsServerForm
      :key="formKey"
      :initial="emptyServer"
      :editing="false"
      :servers="serverTags"
      :detours="detours"
      @submit="add"
    />
  </div>

  <h3 class="section-title">DNS-серверы</h3>
  <p class="section-hint">
    Серверы попадают в <code>dns.servers</code> итогового конфига. Сервер, указанный в группе, резолвит ее домены,
    остальные запросы уходят на сервер <code>final</code> (задается в настройках DNS). Сервер, который используется
    группами или настройками, удалить нельзя.
  </p>

  <div class="data-table">
    <table class="table">
      <thead>
        <tr>
          <th>Тег</th>
          <th>Тип</th>
          <th>Адрес</th>
          <th>Параметры</th>
          <th>Описание</th>
          <th style="width: 150px; text-align: center;">Действия</th>
        </tr>
      </thead>
      <tbody>
        <tr v-if="dns && dns.servers.length === 0">
          <td colspan="6" class="empty-state">DNS-серверов нет.</td>
        </tr>
        <tr v-for="server in dns?.servers ?? []" :key="server.tag">
          <td>
            <strong>{{ server.tag }}</strong>
            <span v-for="item in usage(server)" :key="item" class="badge badge-group" style="margin-left: 8px;">{{ item }}</span>
          </td>
          <td><span class="badge badge-domain">{{ server.type }}</span></td>
          <td><code>{{ address(server) }}</code></td>
          <td class="description-cell">
            <div v-if="server.tls_server_name">SNI: {{ server.tls_server_name }}</div>
            <div v-if="server.tls_insecure">без проверки сертификата</div>
            <div v-if="server.detour">через {{ server.detour }}</div>
            <div v-if="server.domain_resolver">резолвер: {{ server.domain_resolver }}</div>
            <div v-if="server.extra"><code>{{ JSON.stringify(server.extra) }}</code></div>
          </td>
          <td class="description-cell">{{ server.description }}</td>
          <td class="actions-cell">
            <button class="btn btn-secondary" @click="editing = server">Редактировать</button>
            <button class="btn btn-danger" @click="remove(server)">Удалить</button>
          </td>
        </tr>
      </tbody>
    </table>
  </div>

  <ModalDialog v-if="editing" :title="`DNS-сервер ${editing.tag}`" wide @close="editing = null">
    <DnsServerForm
      :initial="editing"
      :editing="true"
      :servers="serverTags"
      :detours="detours"
      @submit="save"
      @cancel="editing = null"
    />
  </ModalDialog>
</template>
