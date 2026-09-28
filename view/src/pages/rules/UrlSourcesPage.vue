<script setup lang="ts">
import { computed, reactive, ref } from 'vue'

import { get, post } from '../../api/client'
import type { URLSource } from '../../api/types'
import GroupBadge from '../../components/GroupBadge.vue'
import ModalDialog from '../../components/ModalDialog.vue'
import { groupLabel, useGroups } from '../../composables/useGroups'
import { usePolling } from '../../composables/usePolling'
import { showError, showMessage } from '../../stores/toast'
import { formatDateTime } from '../../utils/format'

const { groups, loadGroups } = useGroups()

const sources = ref<URLSource[]>([])
const loaded = ref(false)

// ID источников, которые сейчас загружаются вручную.
const refreshing = ref<string[]>([])

const form = reactive({
  group: '',
  url: '',
  interval: 60,
  description: '',
})

const editing = ref<URLSource | null>(null)
const editForm = reactive({
  group: '',
  description: '',
})

const viewing = ref<{ cidrList: string[]; domains: string[]; domainSuffixes: string[] } | null>(null)

// pendingCount — неприменённые изменения источников.
const pendingCount = computed(() => sources.value.filter((source) => !source.applied || source.deleted).length)

// load загружает источники и группы.
async function load(): Promise<void> {
  try {
    const data = await get<{ url_sources: URLSource[] }>('/api/url-sources')

    sources.value = data.url_sources ?? []

    await loadGroups()

    if (!form.group && groups.value.length) {
      form.group = groups.value.find((group) => !group.system)?.name ?? groups.value[0].name
    }
  } catch (error) {
    showError(error, 'Ошибка загрузки источников')
  } finally {
    loaded.value = true
  }
}

usePolling(load, 5000)

// validate проверяет, что URL отдает список правил.
async function validate(): Promise<void> {
  if (!form.url) {
    showMessage('Введите URL', 'error')

    return
  }

  try {
    const result = await post<{ valid: boolean; error: string; count: number }>('/api/url-sources/validate', { url: form.url })

    if (result.valid) {
      showMessage(`URL валидный, найдено правил: ${result.count}`)
    } else {
      showMessage(`Ошибка валидации: ${result.error}`, 'error')
    }
  } catch (error) {
    showError(error)
  }
}

// add добавляет источник.
async function add(): Promise<void> {
  try {
    await post('/api/url-sources/add', form)
    showMessage('Источник добавлен')
    form.url = ''
    form.interval = 60
    form.description = ''
    await load()
  } catch (error) {
    showError(error)
  }
}

// refresh загружает список источника сейчас.
async function refresh(source: URLSource): Promise<void> {
  refreshing.value = [...refreshing.value, source.id]

  try {
    await post('/api/url-sources/refresh', { id: source.id })
    showMessage('Источник загружен')
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
    viewing.value = await get(`/api/url-sources/rules?id=${encodeURIComponent(source.id)}`)
  } catch (error) {
    showError(error)
  }
}

// remove помечает источник на удаление.
async function remove(source: URLSource): Promise<void> {
  if (!confirm('Удалить этот источник?')) {
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

// openEdit открывает редактирование источника.
function openEdit(source: URLSource): void {
  editing.value = source
  editForm.group = source.group
  editForm.description = source.description
}

// saveEdit сохраняет источник.
async function saveEdit(): Promise<void> {
  if (!editing.value) {
    return
  }

  try {
    await post('/api/url-sources/edit', { id: editing.value.id, ...editForm })
    showMessage('Источник обновлен')
    editing.value = null
    await load()
  } catch (error) {
    showError(error)
  }
}

// apply применяет изменения источников.
async function apply(): Promise<void> {
  try {
    await post('/api/url-sources/apply')
    showMessage('Источники применены')
    await load()
  } catch (error) {
    showError(error)
  }
}

// loadStatus описывает результат последней загрузки источника.
function loadStatus(source: URLSource): { cls: string; icon: string; text: string } {
  if (source.last_status === 'success') {
    return { cls: 'status-success', icon: '🟢', text: 'Успех' }
  }

  if (source.last_status === 'error') {
    return { cls: 'status-error', icon: '🔴', text: 'Ошибка' }
  }

  return { cls: 'status-pending', icon: '🟡', text: 'Ожидает' }
}
</script>

<template>
  <Teleport to="#topbar-actions" defer>
    <button class="btn btn-warning" :disabled="pendingCount === 0" @click="apply">
      Применить источники
      <span v-if="pendingCount > 0" class="pending-badge">{{ pendingCount }}</span>
    </button>
  </Teleport>

  <div class="add-form-card">
    <div class="form-header">Добавить источник URL</div>
    <form @submit.prevent="add">
      <div class="form-row">
        <div class="form-group">
          <label class="form-label" for="sourceGroup">Группа</label>
          <select id="sourceGroup" v-model="form.group" class="form-select" required>
            <option v-for="group in groups" :key="group.name" :value="group.name">{{ groupLabel(group) }}</option>
          </select>
        </div>
        <div class="form-group" style="flex: 2;">
          <label class="form-label" for="sourceURL">URL</label>
          <input id="sourceURL" v-model="form.url" class="form-input" type="url" placeholder="https://example.com/ips.txt" required>
        </div>
        <div class="form-group">
          <label class="form-label" for="sourceInterval">Интервал (мин)</label>
          <input id="sourceInterval" v-model.number="form.interval" class="form-input" type="number" min="1" required>
        </div>
      </div>
      <div class="form-row">
        <div class="form-group">
          <label class="form-label" for="sourceDescription">Описание</label>
          <textarea id="sourceDescription" v-model="form.description" class="form-textarea" placeholder="Описание источника"></textarea>
        </div>
      </div>
      <div class="form-actions">
        <button type="button" class="btn btn-secondary" @click="validate">Проверить</button>
        <button type="submit" class="btn btn-primary">Добавить</button>
      </div>
    </form>
  </div>

  <div class="data-table">
    <table class="table">
      <thead>
        <tr>
          <th>Статус</th>
          <th>Группа</th>
          <th>URL</th>
          <th>Описание</th>
          <th>Интервал</th>
          <th>Обновлено</th>
          <th>Элементов</th>
          <th style="width: 150px; text-align: center;">Действия</th>
        </tr>
      </thead>
      <tbody>
        <tr v-if="loaded && sources.length === 0">
          <td colspan="8" class="empty-state">Источников нет. Добавьте первый источник выше.</td>
        </tr>
        <tr v-for="source in sources" :key="source.id">
          <td>
            <span class="status-badge" :class="source.deleted ? 'status-deleted' : (source.applied ? 'status-applied' : 'status-pending')">
              {{ source.deleted ? 'К удалению' : (source.applied ? 'Применено' : 'Ожидает') }}
            </span>
            <div class="status-indicator" :class="loadStatus(source).cls">
              <span class="status-icon">{{ loadStatus(source).icon }}</span>
              <span class="status-text">{{ loadStatus(source).text }}</span>
            </div>
            <div v-if="source.last_error" class="source-error" :title="source.last_error">{{ source.last_error }}</div>
          </td>
          <td><GroupBadge :group="source.group" /></td>
          <td class="url-cell" :title="source.url">{{ source.url }}</td>
          <td class="description-cell">{{ source.description }}</td>
          <td>{{ source.interval }} мин</td>
          <td class="date-cell">{{ formatDateTime(source.last_update) }}</td>
          <td>{{ source.items_count || 0 }}</td>
          <td class="actions-cell">
            <button
              class="btn btn-secondary"
              title="Загрузить список сейчас, не дожидаясь интервала"
              :disabled="!source.applied || source.deleted || refreshing.includes(source.id)"
              @click="refresh(source)"
            >
              {{ refreshing.includes(source.id) ? 'Загрузка...' : 'Обновить' }}
            </button>
            <button class="btn btn-info" :disabled="!source.applied" @click="view(source)">Посмотреть</button>
            <button class="btn btn-secondary" :disabled="source.deleted" @click="openEdit(source)">Редактировать</button>
            <button class="btn btn-danger" :disabled="source.deleted" @click="remove(source)">{{ source.deleted ? 'Удалено' : 'Удалить' }}</button>
          </td>
        </tr>
      </tbody>
    </table>
  </div>

  <ModalDialog v-if="editing" title="Редактировать источник" @close="editing = null">
    <form @submit.prevent="saveEdit">
      <div class="form-group">
        <label class="form-label">URL</label>
        <input class="form-input" type="text" :value="editing.url" disabled>
      </div>
      <div class="form-group">
        <label class="form-label" for="editSourceGroup">Группа</label>
        <select id="editSourceGroup" v-model="editForm.group" class="form-select">
          <option v-for="group in groups" :key="group.name" :value="group.name">{{ groupLabel(group) }}</option>
        </select>
      </div>
      <div class="form-group">
        <label class="form-label" for="editSourceDescription">Описание</label>
        <textarea id="editSourceDescription" v-model="editForm.description" class="form-textarea"></textarea>
      </div>
      <div class="form-actions">
        <button type="button" class="btn btn-secondary" @click="editing = null">Отмена</button>
        <button type="submit" class="btn btn-primary">Сохранить</button>
      </div>
    </form>
  </ModalDialog>

  <ModalDialog v-if="viewing" title="Загруженные правила" wide @close="viewing = null">
    <div v-if="viewing.cidrList.length" class="modal-section">
      <h5>Префиксы CIDR ({{ viewing.cidrList.length }})</h5>
      <pre>{{ viewing.cidrList.join('\n') }}</pre>
    </div>
    <div v-if="viewing.domains.length" class="modal-section">
      <h5>Конкретные домены ({{ viewing.domains.length }})</h5>
      <pre>{{ viewing.domains.join('\n') }}</pre>
    </div>
    <div v-if="viewing.domainSuffixes.length" class="modal-section">
      <h5>Суффиксы доменов ({{ viewing.domainSuffixes.length }})</h5>
      <pre>{{ viewing.domainSuffixes.join('\n') }}</pre>
    </div>
    <div v-if="!viewing.cidrList.length && !viewing.domains.length && !viewing.domainSuffixes.length" class="empty-state">
      Нет загруженных правил.
    </div>
    <button class="btn btn-primary modal-close" @click="viewing = null">Закрыть</button>
  </ModalDialog>
</template>
