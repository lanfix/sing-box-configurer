<script setup lang="ts">
import { defineAsyncComponent, onMounted, reactive, ref } from 'vue'

import { get, post } from '../../api/client'
import type { DNSData } from '../../api/types'
import { showError, showMessage } from '../../stores/toast'

const JsonEditor = defineAsyncComponent(() => import('../../components/JsonEditor.vue'))

const servers = ref<string[]>([])

const settings = reactive({
  final: '',
  strategy: '',
  default_domain_resolver: '',
  cache_capacity: 0,
  optimistic: false,
  timeout: '',
})

const settingsExtra = ref('')
const settingsError = ref('')

const rulesText = ref('[]')
const rulesError = ref('')

// strategies — стратегии разрешения адресов sing-box.
const strategies = [
  { value: '', label: '— по умолчанию' },
  { value: 'prefer_ipv4', label: 'prefer_ipv4' },
  { value: 'prefer_ipv6', label: 'prefer_ipv6' },
  { value: 'ipv4_only', label: 'ipv4_only' },
  { value: 'ipv6_only', label: 'ipv6_only' },
]

// load загружает настройки DNS.
async function load(): Promise<void> {
  try {
    const data = await get<DNSData>('/api/dns')

    servers.value = data.servers.map((server) => server.tag)

    settings.final = data.settings.final ?? ''
    settings.strategy = data.settings.strategy ?? ''
    settings.default_domain_resolver = data.settings.default_domain_resolver ?? ''
    settings.cache_capacity = data.settings.cache_capacity ?? 0
    settings.optimistic = data.settings.optimistic ?? false
    settings.timeout = data.settings.timeout ?? ''
    settingsExtra.value = data.settings.extra ? JSON.stringify(data.settings.extra, null, 2) : ''
    rulesText.value = JSON.stringify(data.rules ?? [], null, 2)
  } catch (error) {
    showError(error, 'Ошибка загрузки настроек DNS')
  }
}

// saveSettings сохраняет общие параметры DNS.
async function saveSettings(): Promise<void> {
  settingsError.value = ''

  let extra: Record<string, unknown> | null = null

  if (settingsExtra.value.trim()) {
    try {
      extra = JSON.parse(settingsExtra.value)

      if (!extra || typeof extra !== 'object' || Array.isArray(extra)) {
        throw new Error('нужен JSON-объект')
      }
    } catch (error) {
      settingsError.value = `Дополнительные параметры: ${error instanceof Error ? error.message : error}`

      return
    }
  }

  try {
    await post('/api/dns/settings', { ...settings, extra })
    showMessage('Настройки DNS сохранены')
    await load()
  } catch (error) {
    showError(error)
  }
}

// saveRules сохраняет пользовательские DNS-правила.
async function saveRules(): Promise<void> {
  rulesError.value = ''

  let rules: unknown

  try {
    rules = JSON.parse(rulesText.value || '[]')

    if (!Array.isArray(rules) || rules.some((rule) => !rule || typeof rule !== 'object' || Array.isArray(rule))) {
      throw new Error('нужен JSON-массив объектов')
    }
  } catch (error) {
    rulesError.value = error instanceof Error ? error.message : String(error)

    return
  }

  try {
    await post('/api/dns/rules', { rules })
    showMessage('DNS-правила сохранены')
    await load()
  } catch (error) {
    showError(error)
  }
}

onMounted(load)
</script>

<template>
  <div class="add-form-card">
    <div class="form-header">Общие параметры</div>
    <form @submit.prevent="saveSettings">
      <div class="form-row">
        <div class="form-group">
          <label class="form-label" for="dnsFinal">Сервер по умолчанию (final)</label>
          <select id="dnsFinal" v-model="settings.final" class="form-select">
            <option value="">— первый сервер</option>
            <option v-for="tag in servers" :key="tag" :value="tag">{{ tag }}</option>
          </select>
        </div>
        <div class="form-group">
          <label class="form-label" for="dnsResolver">Резолвер доменов outbound-ов</label>
          <select id="dnsResolver" v-model="settings.default_domain_resolver" class="form-select">
            <option value="">— не задан</option>
            <option v-for="tag in servers" :key="tag" :value="tag">{{ tag }}</option>
          </select>
          <p class="form-help"><code>route.default_domain_resolver</code>: резолвит адреса серверов outbound-ов.</p>
        </div>
        <div class="form-group">
          <label class="form-label" for="dnsStrategy">Стратегия</label>
          <select id="dnsStrategy" v-model="settings.strategy" class="form-select">
            <option v-for="item in strategies" :key="item.value" :value="item.value">{{ item.label }}</option>
          </select>
        </div>
      </div>
      <div class="form-row">
        <div class="form-group">
          <label class="form-label" for="dnsCache">Размер кэша (cache_capacity)</label>
          <input id="dnsCache" v-model.number="settings.cache_capacity" class="form-input" type="number" min="0" placeholder="0 — по умолчанию">
        </div>
        <div class="form-group">
          <label class="form-label" for="dnsTimeout">Таймаут</label>
          <input id="dnsTimeout" v-model="settings.timeout" class="form-input" type="text" placeholder="5s">
        </div>
        <div class="form-group">
          <label class="form-label">Кэш</label>
          <label class="check-label">
            <input v-model="settings.optimistic" type="checkbox">
            <span>Оптимистичный кэш (optimistic)</span>
          </label>
        </div>
      </div>
      <div class="form-group">
        <label class="form-label">Дополнительные параметры секции dns (JSON, необязательно)</label>
        <JsonEditor v-model="settingsExtra" height="100px" :invalid="Boolean(settingsError)" />
        <p v-if="settingsError" class="code-error">{{ settingsError }}</p>
      </div>
      <div class="form-actions">
        <button type="submit" class="btn btn-primary">Сохранить</button>
      </div>
    </form>
  </div>

  <div class="add-form-card">
    <div class="form-header">Пользовательские DNS-правила</div>
    <p class="card-hint" style="margin-bottom: 12px;">
      JSON-массив правил sing-box. В итоговом конфиге они идут после системных правил: DNS-записей, фильтра
      HTTPS-записей и правил групп. Правила не должны использовать legacy address filter
      (<code>ip_cidr</code>/<code>ip_is_private</code> без <code>match_response</code>), иначе sing-box не запустится.
    </p>
    <JsonEditor v-model="rulesText" height="320px" :invalid="Boolean(rulesError)" />
    <p v-if="rulesError" class="code-error">{{ rulesError }}</p>
    <div class="form-actions" style="margin-top: 12px;">
      <button class="btn btn-primary" @click="saveRules">Сохранить правила</button>
    </div>
  </div>
</template>
