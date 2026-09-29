<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'

import { get, post } from '../../api/client'
import type { DNSData, DNSServer, Group, OutboundView } from '../../api/types'
import ModalDialog from '../../components/ModalDialog.vue'
import SvgIcon from '../../components/SvgIcon.vue'
import HelpHint from '../../components/ui/HelpHint.vue'
import IconButton from '../../components/ui/IconButton.vue'
import { icons } from '../../icons'
import { confirmAction } from '../../stores/confirm'
import { showError, showMessage } from '../../stores/toast'
import DnsServerForm from './DnsServerForm.vue'
import { serverType } from './dnsTypes'

const dns = ref<DNSData | null>(null)
const groups = ref<Group[]>([])
const detours = ref<string[]>([])

// Форма сервера: editing — редактирование существующего, иначе добавление.
const editor = ref<{ initial: DNSServer; editing: boolean; error: string; saving: boolean } | null>(null)

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

// load загружает DNS-серверы, группы и возможные detour-ы (outbound-ы и selector-ы групп).
async function load(): Promise<void> {
  try {
    const [data, outbounds, groupsData] = await Promise.all([
      get<DNSData>('/api/dns'),
      get<{ outbounds: OutboundView[] }>('/api/outbounds'),
      get<{ groups: Group[] }>('/api/groups'),
    ])

    dns.value = data
    groups.value = groupsData.groups ?? []
    detours.value = [
      ...outbounds.outbounds.filter((outbound) => outbound.source !== 'builtin' || outbound.tag === 'auto').map((outbound) => outbound.tag),
      ...groups.value.filter((group) => !group.system).map((group) => `select-${group.name}`),
    ]
  } catch (error) {
    showError(error, 'Ошибка загрузки DNS')
  }
}

// openEditor открывает форму нового или существующего сервера.
function openEditor(server?: DNSServer): void {
  editor.value = {
    initial: server ? { ...server } : { ...emptyServer },
    editing: Boolean(server),
    error: '',
    saving: false,
  }
}

// save добавляет или сохраняет сервер. Ошибка показывается прямо в форме.
async function save(server: DNSServer): Promise<void> {
  const current = editor.value

  if (!current) {
    return
  }

  current.saving = true
  current.error = ''

  try {
    await post(current.editing ? '/api/dns/servers/edit' : '/api/dns/servers/add', server)
    showMessage(current.editing ? `DNS-сервер ${server.tag} сохранен` : `DNS-сервер ${server.tag} добавлен`)
    editor.value = null
    await load()
  } catch (error) {
    current.error = error instanceof Error ? error.message : String(error)
  } finally {
    current.saving = false
  }
}

// remove удаляет сервер.
async function remove(server: DNSServer): Promise<void> {
  const confirmed = await confirmAction({
    title: `Удалить DNS-сервер ${server.tag}?`,
    message: 'Сервер пропадет из итогового конфига.',
    confirmText: 'Удалить',
    danger: true,
  })

  if (!confirmed) {
    return
  }

  try {
    await post('/api/dns/servers/delete', { tag: server.tag })
    showMessage(`DNS-сервер ${server.tag} удален`)
    await load()
  } catch (error) {
    showError(error)
  }
}

// address возвращает адрес сервера для таблицы.
function address(server: DNSServer): string {
  if (!server.server) {
    return ''
  }

  const port = server.server_port ? `:${server.server_port}` : ''

  if (server.type === 'https' || server.type === 'h3') {
    return `${server.type}://${server.server}${port}${server.path || '/dns-query'}`
  }

  return `${server.type}://${server.server}${port}`
}

// usage описывает, где используется сервер: такой сервер удалить нельзя.
function usage(server: DNSServer): string[] {
  const result: string[] = []

  if (dns.value?.settings.final === server.tag) {
    result.push('по умолчанию')
  }

  if (dns.value?.settings.default_domain_resolver === server.tag) {
    result.push('резолвер outbound-ов')
  }

  const groupNames = groups.value.filter((group) => group.dns_server === server.tag).map((group) => group.name)

  if (groupNames.length) {
    result.push(groupNames.length === 1 ? `группа ${groupNames[0]}` : `групп: ${groupNames.length}`)
  }

  if (dns.value?.servers.some((item) => item.domain_resolver === server.tag)) {
    result.push('резолвер серверов')
  }

  return result
}

onMounted(load)
</script>

<template>
  <Teleport to="#topbar-actions" defer>
    <button class="btn btn-primary" @click="openEditor()">
      <SvgIcon class="btn-icon" :path="icons.plus" />
      Добавить сервер
    </button>
  </Teleport>

  <p class="page-intro">
    Серверы, которые sing-box использует для DNS-запросов. Какой сервер отвечает по умолчанию, задается в
    <RouterLink :to="{ name: 'dns-settings' }">настройках DNS</RouterLink>, а для доменов отдельной
    <RouterLink :to="{ name: 'groups' }">группы</RouterLink> можно выбрать свой.
  </p>

  <HelpHint>
    <p>
      Запрос к домену из группы с собственным DNS-сервером уходит на этот сервер, все остальные — на сервер по
      умолчанию. Серверы попадают в <code>dns.servers</code> итогового конфига.
    </p>
    <p>
      Шифрованные протоколы (DoH, DoT, DoH3, DoQ) скрывают запросы от провайдера. Если адрес сервера задан доменом,
      sing-box сначала узнает его IP через другой сервер, поэтому надежнее указывать IP-адрес и имя из сертификата.
    </p>
    <p>Сервер, который используется группами, настройками или другими серверами, удалить нельзя.</p>
  </HelpHint>

  <div class="data-table">
    <table class="table">
      <thead>
        <tr>
          <th>Сервер</th>
          <th>Протокол</th>
          <th>Адрес</th>
          <th>Маршрут</th>
          <th class="col-actions"></th>
        </tr>
      </thead>
      <tbody>
        <tr v-if="dns && dns.servers.length === 0">
          <td colspan="5" class="empty-state">DNS-серверов нет. Добавьте первый кнопкой «Добавить сервер».</td>
        </tr>
        <tr v-for="server in dns?.servers ?? []" :key="server.tag">
          <td>
            <span class="cell-main">{{ server.tag }}</span>
            <span v-if="usage(server).length" class="inline-badges">
              <span v-for="item in usage(server)" :key="item" class="badge badge-group">{{ item }}</span>
            </span>
            <div v-if="server.description" class="cell-sub">{{ server.description }}</div>
          </td>
          <td>
            <span class="badge badge-domain">{{ serverType(server.type)?.title ?? server.type }}</span>
            <div class="cell-sub">{{ serverType(server.type)?.subtitle }}</div>
          </td>
          <td>
            <span v-if="address(server)" class="cell-mono">{{ address(server) }}</span>
            <span v-else class="muted">—</span>
            <div v-if="server.tls_server_name" class="cell-sub">SNI: {{ server.tls_server_name }}</div>
            <div v-if="server.tls_insecure" class="cell-sub" style="color: #d29922;">сертификат не проверяется</div>
          </td>
          <td class="description-cell">
            <div>{{ server.detour ? `через ${server.detour}` : 'напрямую' }}</div>
            <div v-if="server.domain_resolver" class="cell-sub">адрес узнается через {{ server.domain_resolver }}</div>
            <div v-if="server.extra" class="cell-sub" :title="JSON.stringify(server.extra, null, 2)">+ JSON-параметры</div>
          </td>
          <td class="actions-cell">
            <div class="row-actions">
              <IconButton icon="edit" title="Изменить" @click="openEditor(server)" />
              <IconButton
                icon="trash"
                danger
                :title="usage(server).length ? `Нельзя удалить: используется (${usage(server).join(', ')})` : 'Удалить'"
                :disabled="usage(server).length > 0"
                @click="remove(server)"
              />
            </div>
          </td>
        </tr>
      </tbody>
    </table>
  </div>

  <ModalDialog
    v-if="editor"
    :title="editor.editing ? `DNS-сервер ${editor.initial.tag}` : 'Новый DNS-сервер'"
    :subtitle="editor.editing ? undefined : 'Выберите протокол и популярный сервер или укажите свой адрес.'"
    :autofocus="false"
    wide
    @close="editor = null"
  >
    <DnsServerForm
      :initial="editor.initial"
      :editing="editor.editing"
      :servers="serverTags"
      :detours="detours"
      :error="editor.error"
      :saving="editor.saving"
      @submit="save"
      @cancel="editor = null"
    />
  </ModalDialog>
</template>
