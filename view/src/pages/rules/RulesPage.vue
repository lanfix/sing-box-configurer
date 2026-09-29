<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'

import { get, post } from '../../api/client'
import type { BulkAddResult, Rule } from '../../api/types'
import GroupBadge from '../../components/GroupBadge.vue'
import ModalDialog from '../../components/ModalDialog.vue'
import SvgIcon from '../../components/SvgIcon.vue'
import FormField from '../../components/ui/FormField.vue'
import IconButton from '../../components/ui/IconButton.vue'
import SegmentedControl from '../../components/ui/SegmentedControl.vue'
import { groupLabel, useGroups } from '../../composables/useGroups'
import { icons } from '../../icons'
import { confirmAction } from '../../stores/confirm'
import { showError, showMessage } from '../../stores/toast'
import { ruleTypeLabel } from '../../utils/format'
import { isCIDR, isDomain, isIP } from '../../utils/validate'

type RuleType = Rule['type']

// ruleTypes — типы правил с пояснением и примером.
const ruleTypes: { value: RuleType; label: string; hint: string; placeholder: string }[] = [
  { value: 'domain_suffix', label: 'Домен и поддомены', hint: 'Домен и все его поддомены: example.com, www.example.com, api.example.com.', placeholder: 'example.com' },
  { value: 'domain', label: 'Точный домен', hint: 'Только этот домен: www.example.com, но не example.com и не api.www.example.com.', placeholder: 'www.example.com' },
  { value: 'ip', label: 'IP-адрес', hint: 'Один адрес IPv4 или IPv6.', placeholder: '203.0.113.10' },
  { value: 'cidr', label: 'Подсеть', hint: 'Диапазон адресов в формате CIDR.', placeholder: '203.0.113.0/24' },
]

const { groups, loadGroups } = useGroups()

const rules = ref<Rule[]>([])
const pendingCount = ref(0)
const loaded = ref(false)
const adding = ref(false)
const applying = ref(false)

const form = reactive({
  group: '',
  type: 'domain_suffix' as RuleType,
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

const currentType = computed(() => ruleTypes.find((type) => type.value === form.type) ?? ruleTypes[0])

// detectType определяет вид значения: домен, IP или подсеть.
function detectType(value: string): 'domain' | 'ip' | 'cidr' | '' {
  if (isCIDR(value)) {
    return 'cidr'
  }

  if (isIP(value)) {
    return 'ip'
  }

  return isDomain(value) ? 'domain' : ''
}

// matchesType проверяет, что значение подходит под выбранный тип.
function matchesType(value: string, type: RuleType): boolean {
  const detected = detectType(value)

  return type === 'domain_suffix' ? detected === 'domain' : detected === type
}

// entered — введенные значения (одно или по строкам).
const entered = computed(() => (form.bulk ? form.values.split('\n') : [form.value]).map((value) => value.trim()).filter(Boolean))

// suggestion — значение не подходит под тип, но похоже на другой: предлагаем переключить.
const suggestion = computed(() => {
  if (entered.value.length === 0) {
    return null
  }

  const mismatched = entered.value.filter((value) => !matchesType(value, form.type))

  if (mismatched.length === 0) {
    return null
  }

  const detected = detectType(mismatched[0])
  const target = detected === 'domain' ? 'domain_suffix' : detected
  const suggested = target ? ruleTypes.find((type) => type.value === target) : undefined

  return {
    text: form.bulk ? `Не похоже на «${currentType.value.label.toLowerCase()}»: ${mismatched.length} из ${entered.value.length}` : `Не похоже на «${currentType.value.label.toLowerCase()}»`,
    type: suggested && entered.value.every((value) => matchesType(value, suggested.value)) ? suggested : undefined,
  }
})

// applySuggestion переключает тип правила на предложенный.
function applySuggestion(): void {
  if (suggestion.value?.type) {
    form.type = suggestion.value.type.value
  }
}

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
  adding.value = true

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

        message += ` (ошибок: ${result.failed})\n${details}${result.failed > 3 ? `\n... и еще ${result.failed - 3}` : ''}`
      }

      showMessage(message, result.failed > 0 ? 'warning' : 'success')
      form.values = ''
    } else {
      await post('/api/rules/add', {
        type: form.type,
        value: form.value.trim(),
        description: form.description,
        group: form.group,
      })

      showMessage(`Правило ${form.value.trim()} добавлено. Примените правила, чтобы оно заработало.`)
      form.value = ''
    }

    form.description = ''
    await load()
  } catch (error) {
    showError(error)
  } finally {
    adding.value = false
  }
}

// remove помечает правило на удаление.
async function remove(rule: Rule): Promise<void> {
  const confirmed = await confirmAction({
    title: `Удалить правило ${rule.value}?`,
    message: 'Правило будет удалено после применения правил.',
    confirmText: 'Удалить',
    danger: true,
  })

  if (!confirmed) {
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
  applying.value = true

  try {
    await post('/api/apply')
    showMessage('Правила применены, sing-box подхватит их без перезапуска')
    await load()
  } catch (error) {
    showError(error)
  } finally {
    applying.value = false
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

  return rule.applied ? 'Работает' : 'Не применено'
}

onMounted(load)
</script>

<template>
  <Teleport to="#topbar-actions" defer>
    <button class="btn btn-warning" :disabled="pendingCount === 0 || applying" @click="apply">
      {{ applying ? 'Применение...' : 'Применить правила' }}
      <span v-if="pendingCount > 0" class="pending-badge">{{ pendingCount }}</span>
    </button>
  </Teleport>

  <div class="add-form-card">
    <div class="form-header">Добавить правило</div>
    <form class="form-stack" @submit.prevent="add">
      <div class="rule-add-top">
        <FormField label="Группа" input-id="ruleGroup" class="rule-add-group">
          <select id="ruleGroup" v-model="form.group" class="form-select" required>
            <option v-for="group in groups" :key="group.name" :value="group.name">{{ groupLabel(group) }}</option>
          </select>
        </FormField>
        <FormField label="Что добавить">
          <SegmentedControl v-model="form.type" :options="ruleTypes" aria-label="Тип правила" />
        </FormField>
      </div>

      <FormField :label="form.bulk ? 'Значения — по одному на строку' : 'Значение'" :input-id="form.bulk ? 'ruleValues' : 'ruleValue'">
        <input
          v-if="!form.bulk"
          id="ruleValue"
          v-model="form.value"
          class="form-input is-mono"
          type="text"
          :placeholder="currentType.placeholder"
          autocomplete="off"
          spellcheck="false"
          required
        >
        <textarea
          v-else
          id="ruleValues"
          v-model="form.values"
          class="form-textarea is-mono"
          rows="8"
          :placeholder="`${currentType.placeholder}\n...`"
          spellcheck="false"
          required
        ></textarea>
        <template #hint>
          <span v-if="suggestion" class="field-warning">
            {{ suggestion.text }}.
            <button v-if="suggestion.type" type="button" class="link-button" @click="applySuggestion">
              Выбрать «{{ suggestion.type.label }}»
            </button>
          </span>
          <template v-else>{{ currentType.hint }}</template>
          <button type="button" class="link-button rule-bulk-toggle" @click="form.bulk = !form.bulk">
            {{ form.bulk ? 'Добавить одно значение' : 'Добавить списком' }}
          </button>
        </template>
      </FormField>

      <FormField label="Описание" input-id="ruleDescription" optional>
        <input id="ruleDescription" v-model="form.description" class="form-input" type="text" placeholder="Для чего это правило">
      </FormField>

      <div class="form-actions" style="margin-top: 0;">
        <button type="submit" class="btn btn-primary" :disabled="adding">
          <SvgIcon class="btn-icon" :path="icons.plus" />
          {{ adding ? 'Добавление...' : (form.bulk ? `Добавить (${entered.length})` : 'Добавить') }}
        </button>
      </div>
    </form>
  </div>

  <div v-if="pendingCount > 0" class="pending-bar">
    <span class="pending-bar-text">
      Не применено изменений: {{ pendingCount }}
      <small>Новые и удаленные правила начнут действовать после применения. sing-box подхватит их без перезапуска.</small>
    </span>
    <button class="btn btn-warning btn-sm" :disabled="applying" @click="apply">Применить</button>
  </div>

  <div class="table-toolbar">
    <FormField label="Группа" input-id="filterGroup">
      <select id="filterGroup" v-model="filter.group" class="form-select">
        <option value="">Все группы</option>
        <option v-for="group in groups" :key="group.name" :value="group.name">{{ groupLabel(group) }}</option>
      </select>
    </FormField>
    <FormField label="Поиск" input-id="filterSearch">
      <div class="search-input">
        <SvgIcon :path="icons.search" />
        <input id="filterSearch" v-model="filter.search" class="form-input" type="search" placeholder="Значение или описание">
      </div>
    </FormField>
    <span class="table-count">{{ filteredRules.length }} из {{ rules.length }}</span>
  </div>

  <div class="data-table">
    <table class="table">
      <thead>
        <tr>
          <th>Значение</th>
          <th>Тип</th>
          <th>Группа</th>
          <th>Статус</th>
          <th class="col-actions"></th>
        </tr>
      </thead>
      <tbody>
        <tr v-if="loaded && rules.length === 0">
          <td colspan="5" class="empty-state">Правил пока нет. Добавьте первое в форме выше.</td>
        </tr>
        <tr v-else-if="loaded && filteredRules.length === 0">
          <td colspan="5" class="empty-state">Ничего не найдено.</td>
        </tr>
        <tr v-for="rule in filteredRules" :key="rule.id" :class="{ 'is-deleted': rule.deleted }">
          <td>
            <span class="cell-mono">{{ rule.value }}</span>
            <div v-if="rule.description" class="cell-sub">{{ rule.description }}</div>
          </td>
          <td><span class="badge" :class="`badge-${rule.type}`">{{ ruleTypeLabel(rule.type) }}</span></td>
          <td><GroupBadge :group="rule.group" /></td>
          <td><span class="status-badge" :class="statusClass(rule)">{{ statusText(rule) }}</span></td>
          <td class="actions-cell">
            <div class="row-actions">
              <IconButton icon="edit" title="Изменить" :disabled="rule.deleted" @click="openEdit(rule)" />
              <IconButton icon="trash" :title="rule.deleted ? 'Уже помечено на удаление' : 'Удалить'" danger :disabled="rule.deleted" @click="remove(rule)" />
            </div>
          </td>
        </tr>
      </tbody>
    </table>
  </div>

  <ModalDialog v-if="editing" :title="`Правило ${editing.value}`" :subtitle="ruleTypeLabel(editing.type)" @close="editing = null">
    <form class="form-stack" @submit.prevent="saveEdit">
      <FormField label="Группа" input-id="editRuleGroup">
        <select id="editRuleGroup" v-model="editForm.group" class="form-select">
          <option v-for="group in groups" :key="group.name" :value="group.name">{{ groupLabel(group) }}</option>
        </select>
      </FormField>
      <FormField label="Описание" input-id="editRuleDescription" optional>
        <input id="editRuleDescription" v-model="editForm.description" class="form-input" type="text">
      </FormField>
      <div class="form-actions">
        <button type="button" class="btn btn-secondary" @click="editing = null">Отмена</button>
        <button type="submit" class="btn btn-primary">Сохранить</button>
      </div>
    </form>
  </ModalDialog>
</template>
