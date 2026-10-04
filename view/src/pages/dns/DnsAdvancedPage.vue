<script setup lang="ts">
// Расширенные настройки DNS в JSON: пользовательские правила и дополнительные поля секции dns.
import { defineAsyncComponent, onMounted, reactive, ref } from 'vue'

import { get, post } from '../../api/client'
import type { DNSData } from '../../api/types'
import HelpHint from '../../components/ui/HelpHint.vue'
import SaveBar from '../../components/ui/SaveBar.vue'
import { useLeaveGuard, useSavedState } from '../../composables/useSavedState'
import { showError, showMessage } from '../../stores/toast'

const JsonEditor = defineAsyncComponent(() => import('../../components/JsonEditor.vue'))

// reservedKeys — поля секции dns, которые конфигуратор заполняет сам.
const reservedKeys = ['servers', 'rules', 'final', 'strategy', 'cache_capacity', 'optimistic', 'timeout']

// rulesExample — пример правила для пустого списка.
const rulesExample = JSON.stringify([
  {
    domain_suffix: ['corp.example'],
    server: 'local',
  },
], null, 2)

// extraExample — пример дополнительных полей.
const extraExample = JSON.stringify({ reverse_mapping: true }, null, 2)

const form = reactive({ rules: '[]', extra: '' })
const servers = ref<string[]>([])
const loaded = ref(false)
const saving = ref(false)
const rulesError = ref('')
const extraError = ref('')
const saveError = ref('')

const { dirty, markSaved, reset } = useSavedState(form)

useLeaveGuard(() => dirty.value)

// load загружает правила и дополнительные поля.
async function load(): Promise<void> {
  try {
    const data = await get<DNSData>('/api/dns')

    servers.value = data.servers.map((server) => server.tag)
    form.rules = JSON.stringify(data.rules ?? [], null, 2)
    form.extra = data.settings.extra ? JSON.stringify(data.settings.extra, null, 2) : ''
    markSaved()
  } catch (err) {
    showError(err, 'Ошибка загрузки настроек DNS')
  } finally {
    loaded.value = true
  }
}

// parseRules разбирает правила: нужен массив объектов.
function parseRules(): Record<string, unknown>[] | null {
  try {
    const rules = JSON.parse(form.rules.trim() || '[]')

    if (!Array.isArray(rules) || rules.some((rule) => !rule || typeof rule !== 'object' || Array.isArray(rule))) {
      throw new Error('нужен JSON-массив объектов: [ {...}, {...} ]')
    }

    return rules
  } catch (err) {
    rulesError.value = err instanceof Error ? err.message : String(err)

    return null
  }
}

// parseExtra разбирает дополнительные поля: нужен объект без зарезервированных полей.
function parseExtra(): Record<string, unknown> | null | undefined {
  if (!form.extra.trim()) {
    return null
  }

  try {
    const extra = JSON.parse(form.extra)

    if (!extra || typeof extra !== 'object' || Array.isArray(extra)) {
      throw new Error('нужен JSON-объект: { "поле": значение }')
    }

    const reserved = reservedKeys.filter((key) => key in extra)

    if (reserved.length) {
      throw new Error(`поля ${reserved.join(', ')} задаются конфигуратором`)
    }

    return extra
  } catch (err) {
    extraError.value = err instanceof Error ? err.message : String(err)

    return undefined
  }
}

// save проверяет оба JSON и сохраняет их вместе.
async function save(): Promise<void> {
  rulesError.value = ''
  extraError.value = ''
  saveError.value = ''

  const rules = parseRules()
  const extra = parseExtra()

  if (rules === null || extra === undefined) {
    return
  }

  saving.value = true

  try {
    await post('/api/dns/advanced', { rules, extra })
    showMessage('Расширенные настройки DNS сохранены')
    await load()
  } catch (err) {
    saveError.value = err instanceof Error ? err.message : String(err)
  } finally {
    saving.value = false
  }
}

onMounted(load)
</script>

<template>
  <p class="page-intro">
    Для того, чего нет в формах. Обычно здесь ничего задавать не нужно: серверы, DNS-записи, домены групп и
    <RouterLink :to="{ name: 'dns-settings' }">настройки</RouterLink> конфигуратор переносит в конфиг sing-box сам.
  </p>

  <form class="page-narrow" @submit.prevent="save">
    <div class="settings-card">
      <div class="settings-card-head">
        <div class="settings-card-title">Правила</div>
        <p class="settings-card-description">
          Массив в формате <code>dns.rules</code> sing-box — например, отправить домены <code>*.corp.example</code> на
          внутренний DNS-сервер. Правила идут после системных: DNS-записей, фильтра HTTPS-записей и правил групп.
        </p>
      </div>
      <div class="settings-card-body">
        <HelpHint title="Ограничения и доступные серверы">
          <p>
            Нельзя использовать legacy address filter — <code>ip_cidr</code> или <code>ip_is_private</code> без
            <code>match_response</code>: с ними sing-box не запустится.
          </p>
          <p v-if="servers.length">
            Серверы для поля <code>server</code>:
            <code v-for="tag in servers" :key="tag" style="margin-right: 4px;">{{ tag }}</code>
          </p>
        </HelpHint>
        <JsonEditor v-if="loaded" v-model="form.rules" height="280px" :invalid="Boolean(rulesError)" />
        <p v-if="rulesError" class="field-error" style="margin-top: 8px;">{{ rulesError }}</p>
        <p v-else-if="loaded && form.rules.trim() === '[]'" class="field-hint" style="margin-top: 8px;">
          Правил нет.
          <button type="button" class="link-button" @click="form.rules = rulesExample">Вставить пример</button>
        </p>
      </div>
    </div>

    <div class="settings-card">
      <div class="settings-card-head">
        <div class="settings-card-title">Дополнительные поля секции dns</div>
        <p class="settings-card-description">
          JSON-объект, поля которого дописываются в секцию <code>dns</code> как есть, — например,
          <code>reverse_mapping</code>, <code>independent_cache</code> или <code>client_subnet</code>. Поля
          <template v-for="(key, index) in reservedKeys" :key="key"><code>{{ key }}</code>{{ index < reservedKeys.length - 1 ? ', ' : '' }}</template>
          задает конфигуратор —
          их здесь указывать нельзя.
        </p>
      </div>
      <div class="settings-card-body">
        <JsonEditor v-if="loaded" v-model="form.extra" height="140px" :invalid="Boolean(extraError)" />
        <p v-if="extraError" class="field-error" style="margin-top: 8px;">{{ extraError }}</p>
        <p v-else-if="loaded && !form.extra.trim()" class="field-hint" style="margin-top: 8px;">
          Полей нет.
          <button type="button" class="link-button" @click="form.extra = extraExample">Вставить пример</button>
        </p>
      </div>
    </div>

    <div v-if="saveError" class="form-error" style="margin-bottom: 16px;">{{ saveError }}</div>

    <SaveBar v-if="loaded" class="is-floating" :dirty="dirty" :saving="saving" @reset="reset" />
  </form>
</template>
