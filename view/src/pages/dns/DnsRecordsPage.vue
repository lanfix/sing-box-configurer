<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'

import { get, post } from '../../api/client'
import type { DNSRecord } from '../../api/types'
import ModalDialog from '../../components/ModalDialog.vue'
import SvgIcon from '../../components/SvgIcon.vue'
import ChipsInput from '../../components/ui/ChipsInput.vue'
import FormField from '../../components/ui/FormField.vue'
import HelpHint from '../../components/ui/HelpHint.vue'
import IconButton from '../../components/ui/IconButton.vue'
import { icons } from '../../icons'
import { confirmAction } from '../../stores/confirm'
import { showError, showMessage } from '../../stores/toast'
import { ipError, isDomain } from '../../utils/validate'

const records = ref<DNSRecord[]>([])
const loaded = ref(false)
const search = ref('')

// Форма записи: id пустой при добавлении.
const editor = ref<{ id: string; domain: string; addresses: string[]; description: string; error: string; saving: boolean } | null>(null)

// filtered — записи с учетом поиска по домену, адресу и описанию.
const filtered = computed(() => {
  const query = search.value.trim().toLowerCase()

  if (!query) {
    return records.value
  }

  return records.value.filter((record) => record.domain.toLowerCase().includes(query)
    || record.addresses.some((address) => address.includes(query))
    || record.description.toLowerCase().includes(query))
})

// domainProblem — домен записи введен с ошибкой.
const domainProblem = computed(() => {
  const domain = editor.value?.domain.trim() ?? ''

  if (!domain || isDomain(domain)) {
    return ''
  }

  return domain.includes('://') || domain.includes('/') ? 'Нужен только домен, без схемы и пути' : 'Не похоже на домен'
})

// addressesProblem — среди адресов есть некорректные или список пуст после попытки сохранить.
const addressesProblem = computed(() => {
  const invalid = editor.value?.addresses.filter((address) => ipError(address)) ?? []

  return invalid.length ? `Не похоже на IP-адрес: ${invalid.join(', ')}` : ''
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

// openEditor открывает форму новой или существующей записи.
function openEditor(record?: DNSRecord): void {
  editor.value = {
    id: record?.id ?? '',
    domain: record?.domain ?? '',
    addresses: [...(record?.addresses ?? [])],
    description: record?.description ?? '',
    error: '',
    saving: false,
  }
}

// save добавляет или сохраняет запись. Ошибка показывается прямо в форме.
async function save(): Promise<void> {
  const current = editor.value

  if (!current) {
    return
  }

  if (current.addresses.length === 0) {
    current.error = 'Укажите хотя бы один IP-адрес'

    return
  }

  current.saving = true
  current.error = ''

  try {
    await post(current.id ? '/api/dns-records/edit' : '/api/dns-records/add', {
      id: current.id || undefined,
      domain: current.domain.trim(),
      addresses: current.addresses,
      description: current.description.trim(),
    })

    showMessage(current.id ? 'DNS-запись сохранена' : 'DNS-запись добавлена')
    editor.value = null
    await load()
  } catch (error) {
    current.error = error instanceof Error ? error.message : String(error)
  } finally {
    current.saving = false
  }
}

// remove удаляет запись.
async function remove(record: DNSRecord): Promise<void> {
  const confirmed = await confirmAction({
    title: `Удалить DNS-запись ${record.domain}?`,
    message: 'sing-box перестанет отвечать на этот домен заданными адресами.',
    confirmText: 'Удалить',
    danger: true,
  })

  if (!confirmed) {
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
  <Teleport to="#topbar-actions" defer>
    <button class="btn btn-primary" @click="openEditor()">
      <SvgIcon class="btn-icon" :path="icons.plus" />
      Добавить запись
    </button>
  </Teleport>

  <p class="page-intro">
    Свои ответы на DNS-запросы — как файл hosts, но для всех устройств сети. Например, <code>nas.home</code> →
    <code>192.168.1.10</code>.
  </p>

  <HelpHint>
    <p>
      Запись срабатывает только на точное совпадение домена, поддомены не затрагиваются. Записи собираются в
      hosts-сервер <code>configurer-hosts</code>, а правило для них ставится первым в <code>dns.rules</code>,
      поэтому они важнее всех остальных правил и групп.
    </p>
  </HelpHint>

  <div v-if="records.length > 5" class="table-toolbar">
    <div class="field">
      <div class="search-input">
        <SvgIcon :path="icons.search" />
        <input v-model="search" class="form-input" type="search" placeholder="Домен, адрес или описание" aria-label="Поиск">
      </div>
    </div>
    <span class="table-count">{{ filtered.length }} из {{ records.length }}</span>
  </div>

  <div class="data-table">
    <table class="table table-cards">
      <thead>
        <tr>
          <th>Домен</th>
          <th>Отвечает адресами</th>
          <th>Описание</th>
          <th class="col-actions"></th>
        </tr>
      </thead>
      <tbody>
        <tr v-if="loaded && records.length === 0">
          <td colspan="4" class="empty-state">DNS-записей нет. Добавьте первую кнопкой «Добавить запись».</td>
        </tr>
        <tr v-else-if="loaded && filtered.length === 0">
          <td colspan="4" class="empty-state">Ничего не найдено.</td>
        </tr>
        <tr v-for="record in filtered" :key="record.id">
          <td><span class="cell-main">{{ record.domain }}</span></td>
          <td data-label="Адреса">
            <div class="value-list">
              <code v-for="address in record.addresses" :key="address">{{ address }}</code>
            </div>
          </td>
          <td class="description-cell" :class="{ 'mobile-hidden': !record.description }">{{ record.description }}</td>
          <td class="actions-cell">
            <div class="row-actions">
              <IconButton icon="edit" title="Изменить" @click="openEditor(record)" />
              <IconButton icon="trash" title="Удалить" danger @click="remove(record)" />
            </div>
          </td>
        </tr>
      </tbody>
    </table>
  </div>

  <ModalDialog v-if="editor" :title="editor.id ? `DNS-запись ${editor.domain}` : 'Новая DNS-запись'" @close="editor = null">
    <form class="form-stack" @submit.prevent="save">
      <FormField label="Домен" input-id="recordDomain" :error="domainProblem" hint="Точное имя: поддомены не затрагиваются.">
        <input
          id="recordDomain"
          v-model="editor.domain"
          class="form-input"
          type="text"
          placeholder="nas.home"
          autocomplete="off"
          spellcheck="false"
          required
        >
      </FormField>

      <FormField
        label="IP-адреса"
        input-id="recordAddresses"
        :error="addressesProblem"
        hint="Введите адрес и нажмите Enter. Можно несколько, в том числе IPv6."
      >
        <ChipsInput v-model="editor.addresses" input-id="recordAddresses" placeholder="192.168.1.10" :validate="ipError" />
      </FormField>

      <FormField label="Описание" input-id="recordDescription" optional>
        <input id="recordDescription" v-model="editor.description" class="form-input" type="text" placeholder="Например, домашний NAS">
      </FormField>

      <div v-if="editor.error" class="form-error">{{ editor.error }}</div>

      <div class="form-actions">
        <button type="button" class="btn btn-secondary" @click="editor = null">Отмена</button>
        <button type="submit" class="btn btn-primary" :disabled="editor.saving || Boolean(domainProblem || addressesProblem)">
          {{ editor.saving ? 'Сохранение...' : (editor.id ? 'Сохранить' : 'Добавить') }}
        </button>
      </div>
    </form>
  </ModalDialog>
</template>
