<script setup lang="ts">
// Добавление домена или IP в группу правил прямо из соединения или поиска пути: значение проверяется
// на пересечения до добавления, правила можно сразу применить.
import { computed, onMounted, ref } from 'vue'

import { post } from '../api/client'
import type { Rule } from '../api/types'
import { groupLabel, useGroups } from '../composables/useGroups'
import { useRuleCheck } from '../composables/useRuleCheck'
import { showError, showMessage } from '../stores/toast'
import { isIP } from '../utils/validate'
import ModalDialog from './ModalDialog.vue'
import FormField from './ui/FormField.vue'
import SegmentedControl from './ui/SegmentedControl.vue'
import ToggleSwitch from './ui/ToggleSwitch.vue'

const props = defineProps<{
  // target — домен или IP-адрес (IPv6 — без скобок и порта).
  target: string
  // context — откуда добавляется правило, для описания по умолчанию.
  context?: string
}>()

const emit = defineEmits<{
  close: []
  added: []
}>()

const { groups, loadGroups } = useGroups()

const isAddress = isIP(props.target)
const candidates = isAddress ? [props.target] : domainCandidates(props.target)

const type = ref<Rule['type']>(isAddress ? 'ip' : 'domain_suffix')
const value = ref(isAddress ? props.target : defaultCandidate(candidates))
const group = ref('')
const description = ref(props.context ?? '')
const applyNow = ref(true)
const saving = ref(false)

const typeOptions = computed<{ value: Rule['type']; label: string }[]>(() => isAddress
  ? [{ value: 'ip', label: 'IP-адрес' }]
  : [
      { value: 'domain_suffix', label: 'Домен и поддомены' },
      { value: 'domain', label: 'Точный домен' },
    ])

const values = computed(() => (value.value.trim() ? [value.value.trim()] : []))
const { checks, pendingCount, checking } = useRuleCheck(type, values, group)
const check = computed(() => checks.value[0])

// typeHint — что попадет под правило.
const typeHint = computed(() => {
  const current = value.value.trim() || 'example.com'

  if (type.value === 'domain_suffix') {
    return `Сработает для ${current} и всех его поддоменов.`
  }

  return type.value === 'domain' ? `Сработает только для ${current}.` : 'Сработает для соединений с этим адресом.'
})

// domainCandidates возвращает домен и его родительские домены до второго уровня.
function domainCandidates(host: string): string[] {
  const labels = host.toLowerCase().replace(/\.$/, '').split('.')
  const result: string[] = []

  for (let i = 0; i <= labels.length - 2; i++) {
    result.push(labels.slice(i).join('.'))
  }

  return result.length ? result : [host]
}

// defaultCandidate выбирает домен по умолчанию: домен второго уровня, а для зон вида co.uk или com.ru — третьего.
function defaultCandidate(list: string[]): string {
  const base = list[list.length - 1]
  const labels = base.split('.')

  if (list.length > 1 && labels.length === 2 && labels[0].length <= 3 && labels[1].length === 2) {
    return list[list.length - 2]
  }

  return base
}

// submit добавляет правило и, если выбрано, применяет правила.
async function submit(): Promise<void> {
  saving.value = true

  try {
    const result = await post<{ warnings?: string[] }>('/api/rules/add', {
      type: type.value,
      value: value.value.trim(),
      description: description.value,
      group: group.value,
    })

    let message = `Правило ${value.value.trim()} добавлено в группу ${group.value}`

    if (applyNow.value) {
      await post('/api/apply')
      message += ' и применено — sing-box подхватит его без перезапуска'
    } else {
      message += '. Примените правила, чтобы оно заработало'
    }

    const warnings = result.warnings ?? []

    showMessage(warnings.length ? `${message}.\n${warnings.join('\n')}` : message, warnings.length ? 'warning' : 'success')
    emit('added')
    emit('close')
  } catch (error) {
    showError(error)
  } finally {
    saving.value = false
  }
}

onMounted(async () => {
  try {
    await loadGroups()
    group.value = groups.value.find((item) => !item.system)?.name ?? groups.value[0]?.name ?? ''
  } catch (error) {
    showError(error, 'Ошибка загрузки групп')
  }
})
</script>

<template>
  <ModalDialog title="Добавить в группу" :subtitle="target" @close="emit('close')">
    <form class="form-stack" @submit.prevent="submit">
      <FormField v-if="!isAddress" label="Что добавить">
        <SegmentedControl v-model="type" :options="typeOptions" aria-label="Тип правила" />
      </FormField>

      <FormField label="Значение" input-id="addRuleValue" :error="check?.error">
        <input
          id="addRuleValue"
          v-model="value"
          class="form-input is-mono"
          type="text"
          autocomplete="off"
          spellcheck="false"
          required
        >
        <template #hint>{{ typeHint }}</template>
      </FormField>

      <div v-if="candidates.length > 1" class="preset-list">
        <span class="preset-label">Уровень домена:</span>
        <button
          v-for="candidate in candidates"
          :key="candidate"
          type="button"
          class="preset-chip"
          :class="{ active: candidate === value }"
          @click="value = candidate"
        >{{ candidate }}</button>
      </div>

      <FormField label="Группа" input-id="addRuleGroup">
        <select id="addRuleGroup" v-model="group" class="form-select" required>
          <option v-for="item in groups" :key="item.name" :value="item.name">{{ groupLabel(item) }}</option>
        </select>
      </FormField>

      <div v-if="check?.warnings?.length" class="callout is-warn">
        <div v-for="warning in check.warnings" :key="warning">{{ warning }}</div>
      </div>

      <FormField label="Описание" input-id="addRuleDescription" optional>
        <input id="addRuleDescription" v-model="description" class="form-input" type="text" placeholder="Для чего это правило">
      </FormField>

      <ToggleSwitch
        v-model="applyNow"
        label="Сразу применить правила"
        :description="applyNow && pendingCount > 0
          ? `Вместе с ним применятся и другие неприменённые изменения правил: ${pendingCount}.`
          : 'sing-box подхватит правило без перезапуска.'"
      />

      <div class="form-actions">
        <button type="button" class="btn btn-secondary" @click="emit('close')">Отмена</button>
        <button type="submit" class="btn btn-primary" :disabled="saving || checking || Boolean(check?.error) || !group">
          {{ saving ? 'Добавление...' : applyNow ? 'Добавить и применить' : 'Добавить' }}
        </button>
      </div>
    </form>
  </ModalDialog>
</template>
