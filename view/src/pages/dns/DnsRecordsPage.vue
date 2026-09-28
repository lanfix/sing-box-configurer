<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'

import { get, post } from '../../api/client'
import type { DNSRecord } from '../../api/types'
import ModalDialog from '../../components/ModalDialog.vue'
import { showError, showMessage } from '../../stores/toast'
import { parseList } from '../../utils/format'

const records = ref<DNSRecord[]>([])
const loaded = ref(false)

const form = reactive({
  domain: '',
  addresses: '',
  description: '',
})

const editing = ref<DNSRecord | null>(null)
const editForm = reactive({
  domain: '',
  addresses: '',
  description: '',
})

// load загружает DNS-записи.
async function load(): Promise<void> {
  try {
    const data = await get<{ records: DNSRecord[] }>('/api/dns-records')

    records.value = data.records ?? []
  } catch (error) {
    showError(error, 'Ошибка загрузки DNS-записей')
  } finally {
    loaded.value = true
  }
}

// add добавляет запись.
async function add(): Promise<void> {
  try {
    await post('/api/dns-records/add', {
      domain: form.domain,
      addresses: parseList(form.addresses),
      description: form.description,
    })

    showMessage('DNS-запись добавлена')
    form.domain = ''
    form.addresses = ''
    form.description = ''
    await load()
  } catch (error) {
    showError(error)
  }
}

// openEdit открывает редактирование записи.
function openEdit(record: DNSRecord): void {
  editing.value = record
  editForm.domain = record.domain
  editForm.addresses = record.addresses.join(', ')
  editForm.description = record.description
}

// saveEdit сохраняет запись.
async function saveEdit(): Promise<void> {
  if (!editing.value) {
    return
  }

  try {
    await post('/api/dns-records/edit', {
      id: editing.value.id,
      domain: editForm.domain,
      addresses: parseList(editForm.addresses),
      description: editForm.description,
    })

    showMessage('DNS-запись обновлена')
    editing.value = null
    await load()
  } catch (error) {
    showError(error)
  }
}

// remove удаляет запись.
async function remove(record: DNSRecord): Promise<void> {
  if (!confirm(`Удалить DNS-запись ${record.domain}?`)) {
    return
  }

  try {
    await post('/api/dns-records/delete', { id: record.id })
    showMessage('DNS-запись удалена')
    await load()
  } catch (error) {
    showError(error)
  }
}

onMounted(load)
</script>

<template>
  <div class="add-form-card">
    <div class="form-header">Добавить DNS-запись</div>
    <form @submit.prevent="add">
      <div class="form-row">
        <div class="form-group">
          <label class="form-label" for="recordDomain">Домен</label>
          <input id="recordDomain" v-model="form.domain" class="form-input" type="text" placeholder="ha.home.lab" required>
        </div>
        <div class="form-group">
          <label class="form-label" for="recordAddresses">IP-адреса</label>
          <input id="recordAddresses" v-model="form.addresses" class="form-input" type="text" placeholder="192.168.50.8, через запятую или пробел" required>
        </div>
        <div class="form-group" style="flex: 2;">
          <label class="form-label" for="recordDescription">Описание</label>
          <input id="recordDescription" v-model="form.description" class="form-input" type="text" placeholder="Необязательно">
        </div>
      </div>
      <div class="form-actions">
        <button type="submit" class="btn btn-primary">Добавить</button>
      </div>
    </form>
  </div>

  <h3 class="section-title">DNS-записи</h3>
  <p class="section-hint">
    DNS sing-box отвечает на запросы этих доменов указанными адресами (точное совпадение домена). Записи попадают
    в hosts-сервер <code>configurer-hosts</code> и правило в начале <code>dns.rules</code> итогового конфига.
  </p>

  <div class="data-table">
    <table class="table">
      <thead>
        <tr>
          <th>Домен</th>
          <th>IP-адреса</th>
          <th>Описание</th>
          <th style="width: 150px; text-align: center;">Действия</th>
        </tr>
      </thead>
      <tbody>
        <tr v-if="loaded && records.length === 0">
          <td colspan="4" class="empty-state">DNS-записей нет.</td>
        </tr>
        <tr v-for="record in records" :key="record.id">
          <td><strong>{{ record.domain }}</strong></td>
          <td>
            <div v-for="address in record.addresses" :key="address">{{ address }}</div>
          </td>
          <td class="description-cell">{{ record.description }}</td>
          <td class="actions-cell">
            <button class="btn btn-secondary" @click="openEdit(record)">Редактировать</button>
            <button class="btn btn-danger" @click="remove(record)">Удалить</button>
          </td>
        </tr>
      </tbody>
    </table>
  </div>

  <ModalDialog v-if="editing" title="Редактировать DNS-запись" @close="editing = null">
    <form @submit.prevent="saveEdit">
      <div class="form-group">
        <label class="form-label" for="editRecordDomain">Домен</label>
        <input id="editRecordDomain" v-model="editForm.domain" class="form-input" type="text" required>
      </div>
      <div class="form-group">
        <label class="form-label" for="editRecordAddresses">IP-адреса</label>
        <input id="editRecordAddresses" v-model="editForm.addresses" class="form-input" type="text" required>
      </div>
      <div class="form-group">
        <label class="form-label" for="editRecordDescription">Описание</label>
        <input id="editRecordDescription" v-model="editForm.description" class="form-input" type="text">
      </div>
      <div class="form-actions">
        <button type="button" class="btn btn-secondary" @click="editing = null">Отмена</button>
        <button type="submit" class="btn btn-primary">Сохранить</button>
      </div>
    </form>
  </ModalDialog>
</template>
