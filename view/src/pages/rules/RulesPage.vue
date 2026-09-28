<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'

import { get, post } from '../../api/client'
import type { BulkAddResult, Rule } from '../../api/types'
import GroupBadge from '../../components/GroupBadge.vue'
import ModalDialog from '../../components/ModalDialog.vue'
import { groupLabel, useGroups } from '../../composables/useGroups'
import { showError, showMessage } from '../../stores/toast'
import { ruleTypeLabel } from '../../utils/format'

const { groups, loadGroups } = useGroups()

const rules = ref<Rule[]>([])
const pendingCount = ref(0)
const loaded = ref(false)

const form = reactive({
  group: '',
  type: 'domain',
  bulk: false,
  value: '',
  values: '',
  description: '',
})

const filter = reactive({
  group: '',
  search: '',
})

const editing = ref<Rule | null>(null)
const editForm = reactive({
  group: '',
  description: '',
})

// filteredRules — правила с учетом фильтра по группе и поиска.
const filteredRules = computed(() => {
  const search = filter.search.trim().toLowerCase()

  return rules.value.filter((rule) => {
    if (filter.group && rule.group !== filter.group) {
      return false
    }

    return !search || rule.value.toLowerCase().includes(search) || rule.description.toLowerCase().includes(search)
  })
})

// load загружает правила и группы.
async function load(): Promise<void> {
  try {
    const data = await get<{ rules: Rule[]; pending_count: number }>('/api/rules')

    rules.value = data.rules ?? []
    pendingCount.value = data.pending_count ?? 0

    await loadGroups()

    if (!form.group && groups.value.length) {
      form.group = groups.value.find((group) => !group.system)?.name ?? groups.value[0].name
    }
  } catch (error) {
    showError(error, 'Ошибка загрузки правил')
  } finally {
    loaded.value = true
  }
}

// add добавляет одно правило или несколько (по одному на строку).
async function add(): Promise<void> {
  try {
    if (form.bulk) {
      const result = await post<BulkAddResult>('/api/rules/add-bulk', {
        type: form.type,
        values: form.values,
        description: form.description,
        group: form.group,
      })

      let message = `Добавлено: ${result.success} из ${result.total}`

      if (result.failed > 0) {
        const details = (result.failed_values ?? []).slice(0, 3).map((failure) => `${failure.value}: ${failure.error}`).join('\n')

        message += ` (ошибок: ${result.failed})\n${details}${result.failed > 3 ? `\n... и ещё ${result.failed - 3}` : ''}`
      }

      showMessage(message, result.failed > 0 ? 'warning' : 'success')
      form.values = ''
    } else {
      await post('/api/rules/add', {
        type: form.type,
        value: form.value,
        description: form.description,
        group: form.group,
      })

      showMessage('Правило добавлено')
      form.value = ''
    }

    form.description = ''
    await load()
  } catch (error) {
    showError(error)
  }
}

// remove помечает правило на удаление.
async function remove(rule: Rule): Promise<void> {
  if (!confirm(`Удалить правило ${rule.value}?`)) {
    return
  }

  try {
    await post('/api/rules/delete', { id: rule.id })
    showMessage('Правило помечено на удаление')
    await load()
  } catch (error) {
    showError(error)
  }
}

// openEdit открывает редактирование правила.
function openEdit(rule: Rule): void {
  editing.value = rule
  editForm.group = rule.group
  editForm.description = rule.description
}

// saveEdit сохраняет группу и описание правила.
async function saveEdit(): Promise<void> {
  if (!editing.value) {
    return
  }

  try {
    await post('/api/rules/edit', { id: editing.value.id, ...editForm })
    showMessage('Правило обновлено')
    editing.value = null
    await load()
  } catch (error) {
    showError(error)
  }
}

// apply применяет изменения: sing-box подхватит rule-set-ы без перезапуска.
async function apply(): Promise<void> {
  try {
    await post('/api/apply')
    showMessage('Правила применены, sing-box подхватит их без перезапуска')
    await load()
  } catch (error) {
    showError(error)
  }
}

// statusClass и statusText описывают состояние правила.
function statusClass(rule: Rule): string {
  if (rule.deleted) {
    return 'status-deleted'
  }

  return rule.applied ? 'status-applied' : 'status-pending'
}

function statusText(rule: Rule): string {
  if (rule.deleted) {
    return 'К удалению'
  }

  return rule.applied ? 'Применено' : 'Ожидает'
}

onMounted(load)
</script>

<template>
  <Teleport to="#topbar-actions" defer>
    <button class="btn btn-warning" :disabled="pendingCount === 0" @click="apply">
      Применить правила
      <span v-if="pendingCount > 0" class="pending-badge">{{ pendingCount }}</span>
    </button>
  </Teleport>

  <div class="add-form-card">
    <div class="form-header">Добавить правило</div>
    <form @submit.prevent="add">
      <div class="form-row">
        <div class="form-group">
          <label class="form-label" for="ruleGroup">Группа</label>
          <select id="ruleGroup" v-model="form.group" class="form-select" required>
            <option v-for="group in groups" :key="group.name" :value="group.name">{{ groupLabel(group) }}</option>
          </select>
        </div>
        <div class="form-group">
          <label class="form-label" for="ruleType">Тип</label>
          <select id="ruleType" v-model="form.type" class="form-select">
            <option value="domain">Домен</option>
            <option value="domain_suffix">Суффикс домена</option>
            <option value="ip">IP адрес</option>
            <option value="cidr">CIDR</option>
          </select>
        </div>
        <div class="form-group">
          <label class="form-label">Режим добавления</label>
          <label class="check-label">
            <input v-model="form.bulk" type="checkbox">
            <span>Массовое добавление</span>
          </label>
        </div>
      </div>

      <div v-if="!form.bulk" class="form-row">
        <div class="form-group" style="flex: 1;">
          <label class="form-label" for="ruleValue">Значение</label>
          <input id="ruleValue" v-model="form.value" class="form-input" type="text" placeholder="example.com или 192.168.1.0/24" required>
        </div>
      </div>
      <div v-else class="form-row">
        <div class="form-group" style="flex: 1;">
          <label class="form-label" for="ruleValues">Значения (по одному на строку)</label>
          <textarea id="ruleValues" v-model="form.values" class="form-textarea" rows="8" placeholder="example.com&#10;google.com" required></textarea>
        </div>
      </div>

      <div class="form-row">
        <div class="form-group">
          <label class="form-label" for="ruleDescription">Описание (необязательно)</label>
          <textarea id="ruleDescription" v-model="form.description" class="form-textarea" placeholder="Краткое описание правила"></textarea>
        </div>
      </div>

      <div class="form-actions">
        <button type="submit" class="btn btn-primary">Добавить</button>
      </div>
    </form>
  </div>

  <div class="table-toolbar">
    <div class="form-group">
      <label class="form-label" for="filterGroup">Группа</label>
      <select id="filterGroup" v-model="filter.group" class="form-select">
        <option value="">Все группы</option>
        <option v-for="group in groups" :key="group.name" :value="group.name">{{ groupLabel(group) }}</option>
      </select>
    </div>
    <div class="form-group">
      <label class="form-label" for="filterSearch">Поиск</label>
      <input id="filterSearch" v-model="filter.search" class="form-input" type="search" placeholder="Значение или описание">
    </div>
    <span class="table-count">{{ filteredRules.length }} из {{ rules.length }}</span>
  </div>

  <div class="data-table">
    <table class="table">
      <thead>
        <tr>
          <th>Статус</th>
          <th>Группа</th>
          <th>Тип</th>
          <th>Значение</th>
          <th>Описание</th>
          <th style="width: 100px; text-align: center;">Действия</th>
        </tr>
      </thead>
      <tbody>
        <tr v-if="loaded && filteredRules.length === 0">
          <td colspan="6" class="empty-state">Правил нет.</td>
        </tr>
        <tr v-for="rule in filteredRules" :key="rule.id">
          <td><span class="status-badge" :class="statusClass(rule)">{{ statusText(rule) }}</span></td>
          <td><GroupBadge :group="rule.group" /></td>
          <td><span class="badge" :class="`badge-${rule.type}`">{{ ruleTypeLabel(rule.type) }}</span></td>
          <td><code>{{ rule.value }}</code></td>
          <td class="description-cell">{{ rule.description }}</td>
          <td class="actions-cell">
            <button class="btn btn-secondary" :disabled="rule.deleted" @click="openEdit(rule)">Редактировать</button>
            <button class="btn btn-danger" :disabled="rule.deleted" @click="remove(rule)">{{ rule.deleted ? 'Удалено' : 'Удалить' }}</button>
          </td>
        </tr>
      </tbody>
    </table>
  </div>

  <ModalDialog v-if="editing" title="Редактировать правило" @close="editing = null">
    <form @submit.prevent="saveEdit">
      <div class="form-group">
        <label class="form-label">Значение</label>
        <input class="form-input" type="text" :value="`${ruleTypeLabel(editing.type)}: ${editing.value}`" disabled>
      </div>
      <div class="form-group">
        <label class="form-label" for="editRuleGroup">Группа</label>
        <select id="editRuleGroup" v-model="editForm.group" class="form-select">
          <option v-for="group in groups" :key="group.name" :value="group.name">{{ groupLabel(group) }}</option>
        </select>
      </div>
      <div class="form-group">
        <label class="form-label" for="editRuleDescription">Описание</label>
        <textarea id="editRuleDescription" v-model="editForm.description" class="form-textarea"></textarea>
      </div>
      <div class="form-actions">
        <button type="button" class="btn btn-secondary" @click="editing = null">Отмена</button>
        <button type="submit" class="btn btn-primary">Сохранить</button>
      </div>
    </form>
  </ModalDialog>
</template>
