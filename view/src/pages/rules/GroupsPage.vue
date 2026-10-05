<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'

import { get, post } from '../../api/client'
import type { DNSData, DNSServer, Group, OutboundView } from '../../api/types'
import ModalDialog from '../../components/ModalDialog.vue'
import OutboundSelect from '../../components/OutboundSelect.vue'
import SvgIcon from '../../components/SvgIcon.vue'
import FormField from '../../components/ui/FormField.vue'
import HelpHint from '../../components/ui/HelpHint.vue'
import IconButton from '../../components/ui/IconButton.vue'
import { useGroups } from '../../composables/useGroups'
import { icons } from '../../icons'
import { confirmAction } from '../../stores/confirm'
import { showError, showMessage } from '../../stores/toast'
import { formatDateTime } from '../../utils/format'

const { groups, loadGroups } = useGroups()

const outbounds = ref<OutboundView[]>([])
const dnsServers = ref<DNSServer[]>([])

// Форма группы: editing — имя редактируемой группы, пустое при добавлении.
const editor = ref<{
  editing: string
  name: string
  description: string
  default_outbound: string
  dns_server: string
  error: string
  saving: boolean
} | null>(null)

// userGroups и systemGroups — пользовательские и системные группы.
const systemGroups = computed(() => groups.value.filter((group) => group.system))
const userGroups = computed(() => groups.value.filter((group) => !group.system))
const outboundTags = computed(() => outbounds.value.map((outbound) => outbound.tag))

// nameProblem — имя новой группы содержит недопустимые символы.
const nameProblem = computed(() => {
  const name = editor.value?.name ?? ''

  if (!name || /^[a-zA-Z0-9_-]+$/.test(name)) {
    return ''
  }

  return 'Только латинские буквы, цифры, дефис и подчеркивание'
})

// systemTitles — пояснения к системным группам.
const systemTitles: Record<string, string> = {
  block: 'Соединения с этими адресами отклоняются, домены не резолвятся. Срабатывает раньше всех групп, в том числе bypass.',
  bypass: 'Трафик идет мимо sing-box напрямую: IP исключаются из туннеля, домены — по ответам DNS. Адреса из block сюда не попадают.',
}

// load загружает группы, outbound-ы и DNS-серверы для выпадающих списков.
async function load(): Promise<void> {
  try {
    const [outboundsData, dns] = await Promise.all([
      get<{ outbounds: OutboundView[] }>('/api/outbounds'),
      get<DNSData>('/api/dns'),
      loadGroups(),
    ])

    outbounds.value = outboundsData.outbounds ?? []
    dnsServers.value = dns.servers
  } catch (error) {
    showError(error, 'Ошибка загрузки групп')
  }
}

// openEditor открывает форму новой или существующей группы.
function openEditor(group?: Group): void {
  // urltest auto можно удалить: тогда новая группа по умолчанию идет напрямую.
  const defaultOutbound = outboundTags.value.includes('auto') ? 'auto' : 'direct'

  editor.value = {
    editing: group?.name ?? '',
    name: group?.name ?? '',
    description: group?.description ?? '',
    default_outbound: group ? group.default_outbound || 'direct' : defaultOutbound,
    dns_server: group?.dns_server ?? '',
    error: '',
    saving: false,
  }
}

// save добавляет или сохраняет группу. Ошибка показывается прямо в форме.
async function save(): Promise<void> {
  const current = editor.value

  if (!current) {
    return
  }

  current.saving = true
  current.error = ''

  const body = {
    name: current.name.trim(),
    description: current.description.trim(),
    default_outbound: current.default_outbound,
    dns_server: current.dns_server,
  }

  try {
    await post(current.editing ? '/api/groups/edit' : '/api/groups/add', body)
    showMessage(current.editing ? `Группа ${body.name} сохранена` : `Группа ${body.name} добавлена`)
    editor.value = null
    await load()
  } catch (error) {
    current.error = error instanceof Error ? error.message : String(error)
  } finally {
    current.saving = false
  }
}

// remove удаляет пустую группу.
async function remove(group: Group): Promise<void> {
  const confirmed = await confirmAction({
    title: `Удалить группу ${group.name}?`,
    message: 'Удалить можно только пустую группу — без правил и источников URL.',
    confirmText: 'Удалить',
    danger: true,
  })

  if (!confirmed) {
    return
  }

  try {
    await post('/api/groups/delete', { name: group.name })
    showMessage(`Группа ${group.name} удалена`)
    await load()
  } catch (error) {
    showError(error)
  }
}

// dnsLabel возвращает подпись DNS-сервера группы.
function dnsLabel(tag: string): string {
  const server = dnsServers.value.find((item) => item.tag === tag)

  return server?.description ? `${tag} — ${server.description}` : tag
}

onMounted(load)
</script>

<template>
  <Teleport to="#topbar-actions" defer>
    <button class="btn btn-primary" @click="openEditor()">
      <SvgIcon class="btn-icon" :path="icons.plus" />
      Добавить группу
    </button>
  </Teleport>

  <p class="page-intro">
    Группа объединяет домены и IP-адреса, трафик к которым идет одним путем: через выбранный outbound и, при
    желании, со своим DNS-сервером. Адреса добавляются в группу на страницах
    <RouterLink :to="{ name: 'rules' }">одиночных правил</RouterLink> и
    <RouterLink :to="{ name: 'url-sources' }">источников URL</RouterLink>.
  </p>

  <HelpHint>
    <p>
      Для каждой группы в конфиге sing-box создаются два rule-set-а (домены и IP) и selector
      <code>select-&lt;группа&gt;</code>, через который идет ее трафик. Outbound по умолчанию — тот, что выбран в
      selector-е после применения конфига; переключить его на лету можно на странице
      <RouterLink :to="{ name: 'proxies' }">«Прокси»</RouterLink>.
    </p>
    <p>
      Если у группы задан DNS-сервер, ее домены резолвятся через него — например, через DNS внутри VPN, чтобы получить
      адреса, актуальные для VPN-сервера. Удалить можно только пустую группу.
    </p>
  </HelpHint>

  <div class="data-table">
    <table class="table table-cards">
      <thead>
        <tr>
          <th>Группа</th>
          <th>Трафик идет через</th>
          <th>DNS-сервер</th>
          <th>Создана</th>
          <th class="col-actions"></th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="group in systemGroups" :key="group.name">
          <td>
            <span class="cell-main">{{ group.name }}</span>
            <span class="inline-badges"><span class="badge badge-system">системная</span></span>
            <div class="cell-sub">{{ systemTitles[group.name] ?? group.description }}</div>
          </td>
          <td class="muted" data-label="Трафик">{{ group.name === 'block' ? 'отклоняется' : 'мимо туннеля' }}</td>
          <td class="muted mobile-hidden">—</td>
          <td class="muted mobile-hidden">—</td>
          <td class="actions-cell mobile-hidden"></td>
        </tr>
        <tr v-for="group in userGroups" :key="group.name">
          <td>
            <span class="cell-main">{{ group.name }}</span>
            <div v-if="group.description" class="cell-sub">{{ group.description }}</div>
          </td>
          <td data-label="Трафик">
            {{ group.default_outbound || 'direct' }}
            <span
              v-if="outboundTags.length && !outboundTags.includes(group.default_outbound || 'direct')"
              class="status-badge status-error"
              style="margin-left: 6px;"
              title="Outbound не найден: по умолчанию будет выбран block"
            >не найден</span>
          </td>
          <td data-label="DNS-сервер">
            <span v-if="group.dns_server">{{ group.dns_server }}</span>
            <span v-else class="muted">по умолчанию</span>
          </td>
          <td class="date-cell" data-label="Создана">{{ formatDateTime(group.created_at) }}</td>
          <td class="actions-cell">
            <div class="row-actions">
              <IconButton icon="edit" title="Изменить" @click="openEditor(group)" />
              <IconButton icon="trash" title="Удалить" danger @click="remove(group)" />
            </div>
          </td>
        </tr>
        <tr v-if="groups.length && userGroups.length === 0">
          <td colspan="5" class="empty-state">Своих групп пока нет. Создайте первую кнопкой «Добавить группу».</td>
        </tr>
      </tbody>
    </table>
  </div>

  <ModalDialog v-if="editor" :title="editor.editing ? `Группа ${editor.editing}` : 'Новая группа'" @close="editor = null">
    <form class="form-stack" @submit.prevent="save">
      <FormField
        v-if="!editor.editing"
        label="Имя"
        input-id="groupName"
        :error="nameProblem"
        hint="Латиница, цифры, дефис и подчеркивание. Имя не меняется после создания."
      >
        <input
          id="groupName"
          v-model="editor.name"
          class="form-input"
          type="text"
          placeholder="work"
          required
          autocomplete="off"
          spellcheck="false"
        >
      </FormField>

      <FormField label="Описание" input-id="groupDescription" optional>
        <input id="groupDescription" v-model="editor.description" class="form-input" type="text" placeholder="Например, рабочие сервисы">
      </FormField>

      <FormField label="Трафик идет через" input-id="groupOutbound">
        <OutboundSelect v-model="editor.default_outbound" :outbounds="outbounds" input-id="groupOutbound" />
        <template #hint>Выбирается после применения конфига. На лету переключается на странице «Прокси».</template>
      </FormField>

      <FormField label="DNS-сервер для доменов группы" input-id="groupDNS">
        <select id="groupDNS" v-model="editor.dns_server" class="form-select">
          <option value="">Сервер по умолчанию (из настроек DNS)</option>
          <option v-for="server in dnsServers" :key="server.tag" :value="server.tag">{{ dnsLabel(server.tag) }}</option>
        </select>
      </FormField>

      <div v-if="editor.error" class="form-error">{{ editor.error }}</div>

      <div class="form-actions">
        <button type="button" class="btn btn-secondary" @click="editor = null">Отмена</button>
        <button type="submit" class="btn btn-primary" :disabled="editor.saving || Boolean(nameProblem)">
          {{ editor.saving ? 'Сохранение...' : (editor.editing ? 'Сохранить' : 'Добавить группу') }}
        </button>
      </div>
    </form>
  </ModalDialog>
</template>
