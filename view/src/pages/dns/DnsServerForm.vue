<script setup lang="ts">
// Форма DNS-сервера: поля зависят от типа сервера.
import { computed, defineAsyncComponent, ref, watch } from 'vue'

import type { DNSServer } from '../../api/types'

const JsonEditor = defineAsyncComponent(() => import('../../components/JsonEditor.vue'))

const props = defineProps<{
  initial: DNSServer
  editing: boolean
  // Теги других серверов (для domain_resolver) и outbound-ов (для detour).
  servers: string[]
  detours: string[]
}>()

const emit = defineEmits<{
  submit: [server: DNSServer]
  cancel: []
}>()

// serverTypes — поддерживаемые типы серверов.
const serverTypes = [
  { value: 'udp', label: 'UDP — обычный DNS' },
  { value: 'tcp', label: 'TCP — обычный DNS' },
  { value: 'tls', label: 'TLS — DNS over TLS (DoT)' },
  { value: 'https', label: 'HTTPS — DNS over HTTPS (DoH)' },
  { value: 'h3', label: 'HTTP/3 — DNS over HTTP/3' },
  { value: 'quic', label: 'QUIC — DNS over QUIC (DoQ)' },
  { value: 'local', label: 'Local — системный резолвер' },
  { value: 'dhcp', label: 'DHCP — DNS от DHCP' },
]

// defaultPorts — порты по умолчанию для подсказки.
const defaultPorts: Record<string, number> = {
  udp: 53,
  tcp: 53,
  tls: 853,
  https: 443,
  h3: 443,
  quic: 853,
}

const server = ref<DNSServer>({ ...props.initial })
const extraText = ref(props.initial.extra ? JSON.stringify(props.initial.extra, null, 2) : '')
const extraError = ref('')

const needsAddress = computed(() => server.value.type !== 'local' && server.value.type !== 'dhcp')
const usesTLS = computed(() => ['tls', 'https', 'h3', 'quic'].includes(server.value.type))
const usesPath = computed(() => server.value.type === 'https' || server.value.type === 'h3')
const resolverOptions = computed(() => props.servers.filter((tag) => tag !== server.value.tag))

watch(() => props.initial, (value) => {
  server.value = { ...value }
  extraText.value = value.extra ? JSON.stringify(value.extra, null, 2) : ''
})

// submit проверяет дополнительные параметры и отправляет сервер.
function submit(): void {
  extraError.value = ''

  let extra: Record<string, unknown> | null = null

  if (extraText.value.trim()) {
    try {
      const parsed = JSON.parse(extraText.value)

      if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
        throw new Error('нужен JSON-объект')
      }

      extra = parsed
    } catch (error) {
      extraError.value = `Дополнительные параметры: ${error instanceof Error ? error.message : error}`

      return
    }
  }

  const result: DNSServer = {
    tag: server.value.tag,
    type: server.value.type,
    detour: server.value.detour,
    domain_resolver: server.value.domain_resolver,
    description: server.value.description,
    extra,
  }

  if (needsAddress.value) {
    result.server = server.value.server
    result.server_port = server.value.server_port || 0
  }

  if (usesPath.value) {
    result.path = server.value.path
  }

  if (usesTLS.value) {
    result.tls_server_name = server.value.tls_server_name
    result.tls_insecure = server.value.tls_insecure
  }

  emit('submit', result)
}
</script>

<template>
  <form @submit.prevent="submit">
    <div class="form-row">
      <div class="form-group">
        <label class="form-label" for="dnsTag">Тег</label>
        <input
          id="dnsTag"
          v-model="server.tag"
          class="form-input"
          type="text"
          placeholder="cloudflare"
          required
          pattern="[A-Za-z0-9_.\-]+"
          :disabled="editing"
        >
      </div>
      <div class="form-group" style="flex: 2;">
        <label class="form-label" for="dnsType">Тип</label>
        <select id="dnsType" v-model="server.type" class="form-select">
          <option v-for="item in serverTypes" :key="item.value" :value="item.value">{{ item.label }}</option>
        </select>
      </div>
    </div>

    <div v-if="needsAddress" class="form-row">
      <div class="form-group" style="flex: 2;">
        <label class="form-label" for="dnsServer">Адрес сервера</label>
        <input id="dnsServer" v-model="server.server" class="form-input" type="text" placeholder="1.1.1.1 или dns.example.com" required>
      </div>
      <div class="form-group">
        <label class="form-label" for="dnsPort">Порт</label>
        <input
          id="dnsPort"
          v-model.number="server.server_port"
          class="form-input"
          type="number"
          min="0"
          max="65535"
          :placeholder="`${defaultPorts[server.type] ?? ''} (по умолчанию)`"
        >
      </div>
      <div v-if="usesPath" class="form-group">
        <label class="form-label" for="dnsPath">Путь</label>
        <input id="dnsPath" v-model="server.path" class="form-input" type="text" placeholder="/dns-query (по умолчанию)">
      </div>
    </div>

    <div v-if="usesTLS" class="form-row">
      <div class="form-group" style="flex: 2;">
        <label class="form-label" for="dnsSNI">TLS server name (SNI)</label>
        <input id="dnsSNI" v-model="server.tls_server_name" class="form-input" type="text" placeholder="cloudflare-dns.com">
        <p class="form-help">Нужен, если адрес сервера задан IP-адресом.</p>
      </div>
      <div class="form-group">
        <label class="form-label">Проверка сертификата</label>
        <label class="check-label">
          <input v-model="server.tls_insecure" type="checkbox">
          <span>Не проверять (insecure)</span>
        </label>
      </div>
    </div>

    <div class="form-row">
      <div class="form-group">
        <label class="form-label" for="dnsDetour">Через outbound (detour)</label>
        <select id="dnsDetour" v-model="server.detour" class="form-select">
          <option value="">— напрямую</option>
          <option v-if="server.detour && !detours.includes(server.detour)" :value="server.detour">{{ server.detour }} (не найден)</option>
          <option v-for="tag in detours" :key="tag" :value="tag">{{ tag }}</option>
        </select>
      </div>
      <div v-if="needsAddress" class="form-group">
        <label class="form-label" for="dnsResolver">Резолвер адреса (domain_resolver)</label>
        <select id="dnsResolver" v-model="server.domain_resolver" class="form-select">
          <option value="">— по умолчанию</option>
          <option v-for="tag in resolverOptions" :key="tag" :value="tag">{{ tag }}</option>
        </select>
      </div>
    </div>

    <div class="form-group">
      <label class="form-label" for="dnsDescription">Описание</label>
      <input id="dnsDescription" v-model="server.description" class="form-input" type="text" placeholder="Необязательно">
    </div>

    <div class="form-group">
      <label class="form-label">Дополнительные параметры (JSON, необязательно)</label>
      <JsonEditor v-model="extraText" height="120px" :invalid="Boolean(extraError)" />
      <p class="form-help">Поля дописываются в объект сервера как есть, например <code>{"neighbor_domain": [".lan"]}</code>.</p>
      <p v-if="extraError" class="code-error">{{ extraError }}</p>
    </div>

    <div class="form-actions">
      <button v-if="editing" type="button" class="btn btn-secondary" @click="emit('cancel')">Отмена</button>
      <button type="submit" class="btn btn-primary">{{ editing ? 'Сохранить' : 'Добавить' }}</button>
    </div>
  </form>
</template>
