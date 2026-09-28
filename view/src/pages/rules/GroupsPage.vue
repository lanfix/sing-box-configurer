<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'

import { get, post } from '../../api/client'
import type { DNSData, Group, OutboundView } from '../../api/types'
import ModalDialog from '../../components/ModalDialog.vue'
import { useGroups } from '../../composables/useGroups'
import { showError, showMessage } from '../../stores/toast'
import { formatDateTime } from '../../utils/format'

const { groups, loadGroups } = useGroups()

const outboundTags = ref<string[]>([])
const dnsServers = ref<string[]>([])

const form = reactive({
  name: '',
  description: '',
  default_outbound: 'auto',
  dns_server: '',
})

const editing = ref<Group | null>(null)
const editForm = reactive({
  description: '',
  default_outbound: '',
  dns_server: '',
})

// userGroups и systemGroups — пользовательские и системные группы.
const systemGroups = computed(() => groups.value.filter((group) => group.system))
const userGroups = computed(() => groups.value.filter((group) => !group.system))

// load загружает группы, outbound-ы и DNS-серверы для выпадающих списков.
async function load(): Promise<void> {
  try {
    const [outbounds, dns] = await Promise.all([
      get<{ outbounds: OutboundView[] }>('/api/outbounds'),
      get<DNSData>('/api/dns'),
      loadGroups(),
    ])

    outboundTags.value = outbounds.outbounds.map((outbound) => outbound.tag)
    dnsServers.value = dns.servers.map((server) => server.tag)
  } catch (error) {
    showError(error, 'Ошибка загрузки групп')
  }
}

// add добавляет группу.
async function add(): Promise<void> {
  try {
    await post('/api/groups/add', form)
    showMessage(`Группа ${form.name} добавлена`)
    form.name = ''
    form.description = ''
    await load()
  } catch (error) {
    showError(error)
  }
}

// openEdit открывает редактирование группы.
function openEdit(group: Group): void {
  editing.value = group
  editForm.description = group.description
  editForm.default_outbound = group.default_outbound || 'direct'
  editForm.dns_server = group.dns_server ?? ''
}

// saveEdit сохраняет группу.
async function saveEdit(): Promise<void> {
  if (!editing.value) {
    return
  }

  try {
    await post('/api/groups/edit', { name: editing.value.name, ...editForm })
    showMessage('Группа обновлена')
    editing.value = null
    await load()
  } catch (error) {
    showError(error)
  }
}

// remove удаляет пустую группу.
async function remove(group: Group): Promise<void> {
  if (!confirm(`Удалить группу ${group.name}?`)) {
    return
  }

  try {
    await post('/api/groups/delete', { name: group.name })
    showMessage('Группа удалена')
    await load()
  } catch (error) {
    showError(error)
  }
}

onMounted(load)
</script>

<template>
  <div class="add-form-card">
    <div class="form-header">Добавить группу</div>
    <form @submit.prevent="add">
      <div class="form-row">
        <div class="form-group">
          <label class="form-label" for="groupName">Имя группы</label>
          <input
            id="groupName"
            v-model="form.name"
            class="form-input"
            type="text"
            placeholder="work, personal..."
            required
            pattern="[a-zA-Z0-9_\-]+"
            title="Только латинские буквы, цифры, дефис и подчеркивание"
          >
        </div>
        <div class="form-group" style="flex: 2;">
          <label class="form-label" for="groupDescription">Описание</label>
          <input id="groupDescription" v-model="form.description" class="form-input" type="text" placeholder="Описание группы">
        </div>
        <div class="form-group">
          <label class="form-label" for="groupOutbound">Outbound по умолчанию</label>
          <select id="groupOutbound" v-model="form.default_outbound" class="form-select">
            <option v-for="tag in outboundTags" :key="tag" :value="tag">{{ tag }}</option>
          </select>
        </div>
        <div class="form-group">
          <label class="form-label" for="groupDNS">DNS-сервер</label>
          <select id="groupDNS" v-model="form.dns_server" class="form-select">
            <option value="">— через dns.final</option>
            <option v-for="tag in dnsServers" :key="tag" :value="tag">{{ tag }}</option>
          </select>
        </div>
      </div>
      <div class="form-actions">
        <button type="submit" class="btn btn-primary">Добавить</button>
      </div>
    </form>
  </div>

  <h3 class="section-title">Группы</h3>
  <p class="section-hint">
    Каждая группа создает в конфиге sing-box два rule-set-а (домены и IP) и selector <code>select-&lt;группа&gt;</code>,
    через который идет ее трафик. Если у группы задан DNS-сервер, домены группы резолвятся через него.
    Системные группы: <strong>block</strong> — соединения отклоняются, <strong>bypass</strong> — трафик идет мимо туннеля
    sing-box (IP исключаются в nftables, домены — по DNS-ответам sing-box). Удалить можно только пустую группу.
  </p>

  <div class="data-table">
    <table class="table">
      <thead>
        <tr>
          <th>Имя</th>
          <th>Описание</th>
          <th>Outbound по умолчанию</th>
          <th>DNS-сервер</th>
          <th>Создана</th>
          <th style="width: 150px; text-align: center;">Действия</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="group in systemGroups" :key="group.name">
          <td>
            <strong>{{ group.name }}</strong>
            <span class="badge badge-system" style="margin-left: 10px;">системная</span>
          </td>
          <td class="description-cell">{{ group.description }}</td>
          <td class="muted">—</td>
          <td class="muted">—</td>
          <td class="muted">—</td>
          <td class="actions-cell muted">Не изменяется</td>
        </tr>
        <tr v-for="group in userGroups" :key="group.name">
          <td><strong>{{ group.name }}</strong></td>
          <td class="description-cell">{{ group.description }}</td>
          <td>
            {{ group.default_outbound || 'direct' }}
            <span
              v-if="outboundTags.length && !outboundTags.includes(group.default_outbound || 'direct')"
              class="status-badge status-error"
              style="margin-left: 6px;"
              title="Outbound не найден: по умолчанию будет выбран block"
            >не найден</span>
          </td>
          <td>
            <span v-if="group.dns_server">{{ group.dns_server }}</span>
            <span v-else class="muted">dns.final</span>
          </td>
          <td class="date-cell">{{ formatDateTime(group.created_at) }}</td>
          <td class="actions-cell">
            <button class="btn btn-secondary" @click="openEdit(group)">Редактировать</button>
            <button class="btn btn-danger" @click="remove(group)">Удалить</button>
          </td>
        </tr>
        <tr v-if="groups.length && userGroups.length === 0">
          <td colspan="6" class="empty-state">Пользовательских групп нет.</td>
        </tr>
      </tbody>
    </table>
  </div>

  <ModalDialog v-if="editing" :title="`Редактировать группу ${editing.name}`" @close="editing = null">
    <form @submit.prevent="saveEdit">
      <div class="form-group">
        <label class="form-label" for="editGroupDescription">Описание</label>
        <textarea id="editGroupDescription" v-model="editForm.description" class="form-textarea"></textarea>
      </div>
      <div class="form-group">
        <label class="form-label" for="editGroupOutbound">Outbound по умолчанию</label>
        <select id="editGroupOutbound" v-model="editForm.default_outbound" class="form-select">
          <option v-if="!outboundTags.includes(editForm.default_outbound)" :value="editForm.default_outbound">
            {{ editForm.default_outbound }} (не найден)
          </option>
          <option v-for="tag in outboundTags" :key="tag" :value="tag">{{ tag }}</option>
        </select>
      </div>
      <div class="form-group">
        <label class="form-label" for="editGroupDNS">DNS-сервер</label>
        <select id="editGroupDNS" v-model="editForm.dns_server" class="form-select">
          <option value="">— через dns.final</option>
          <option v-for="tag in dnsServers" :key="tag" :value="tag">{{ tag }}</option>
        </select>
      </div>
      <div class="form-actions">
        <button type="button" class="btn btn-secondary" @click="editing = null">Отмена</button>
        <button type="submit" class="btn btn-primary">Сохранить</button>
      </div>
    </form>
  </ModalDialog>
</template>
