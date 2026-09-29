<script setup lang="ts">
// Форма DNS-сервера: протокол карточками, популярные серверы в один клик, редкие параметры свернуты.
import { computed, defineAsyncComponent, ref } from 'vue'

import type { DNSServer } from '../../api/types'
import SvgIcon from '../../components/SvgIcon.vue'
import FormField from '../../components/ui/FormField.vue'
import ToggleSwitch from '../../components/ui/ToggleSwitch.vue'
import { icons } from '../../icons'
import { isIP, tagError } from '../../utils/validate'
import {
  needsAddress as typeNeedsAddress,
  parseServerURL,
  providers,
  serverType,
  serverTypes,
  typeGroups,
  usesPath as typeUsesPath,
  usesTLS as typeUsesTLS,
  type DNSProvider,
} from './dnsTypes'

const JsonEditor = defineAsyncComponent(() => import('../../components/JsonEditor.vue'))

const props = defineProps<{
  initial: DNSServer
  editing: boolean
  // Теги других серверов (для domain_resolver) и outbound-ов (для detour).
  servers: string[]
  detours: string[]
  // error — ошибка сохранения от сервера.
  error?: string
  saving?: boolean
}>()

const emit = defineEmits<{
  submit: [server: DNSServer]
  cancel: []
}>()

const server = ref<DNSServer>({ ...props.initial })
const extraText = ref(props.initial.extra ? JSON.stringify(props.initial.extra, null, 2) : '')
const extraError = ref('')

// Тег, описание и SNI, подставленные из популярного сервера: их можно перезаписывать при смене выбора.
const autoTag = ref('')
const autoDescription = ref('')
const autoSNI = ref('')

const needsAddress = computed(() => typeNeedsAddress(server.value.type))
const usesTLS = computed(() => typeUsesTLS(server.value.type))
const usesPath = computed(() => typeUsesPath(server.value.type))
const defaultPort = computed(() => serverType(server.value.type)?.port ?? 0)
const addressIsIP = computed(() => isIP((server.value.server ?? '').trim()))
const resolverOptions = computed(() => props.servers.filter((tag) => tag !== server.value.tag))
const typeProviders = computed(() => providers.filter((provider) => provider.types.includes(server.value.type)))
const activeProvider = computed(() => typeProviders.value.find((provider) => provider.ip === server.value.server))
const tagProblem = computed(() => tagError(server.value.tag))

// showResolver — поле резолвера нужно, только если адрес сервера задан доменом.
const showResolver = computed(() => needsAddress.value && (Boolean(server.value.server) && !addressIsIP.value || Boolean(server.value.domain_resolver)))

// sniWarning — адрес задан IP-адресом, а имя для проверки сертификата не указано.
const sniWarning = computed(() => usesTLS.value && addressIsIP.value && !server.value.tls_server_name && !server.value.tls_insecure)

// advancedOpen — дополнительные параметры раскрыты, если в них что-то задано.
const advancedOpen = computed(() => Boolean(props.initial.detour || props.initial.tls_insecure || props.initial.extra))

// selectType выбирает протокол и обновляет подставленный популярный сервер.
function selectType(value: string): void {
  const provider = activeProvider.value ?? providers.find((item) => item.ip === server.value.server)

  server.value.type = value

  if (provider?.types.includes(value)) {
    applyProvider(provider)
  }
}

// applyProvider заполняет форму адресом популярного сервера.
function applyProvider(provider: DNSProvider): void {
  const type = serverType(server.value.type)

  server.value.server = provider.ip
  server.value.server_port = undefined
  server.value.path = ''
  server.value.tls_server_name = usesTLS.value ? provider.host : ''
  autoSNI.value = server.value.tls_server_name

  if (!props.editing && (!server.value.tag || server.value.tag === autoTag.value)) {
    server.value.tag = type?.short ? `${provider.id}-${type.short}` : provider.id
    autoTag.value = server.value.tag
  }

  if (!server.value.description || server.value.description === autoDescription.value) {
    server.value.description = `${provider.name}, ${type?.subtitle ?? server.value.type}`
    autoDescription.value = server.value.description
  }
}

// onAddressChange разбирает вставленный адрес целиком (https://dns.google/dns-query) и убирает
// значения, подставленные популярным сервером, если адрес заменили на другой.
function onAddressChange(): void {
  const parsed = parseServerURL(server.value.server ?? '')

  if (parsed) {
    server.value.type = parsed.type
    server.value.server = parsed.server
    server.value.server_port = parsed.port

    if (typeUsesPath(parsed.type)) {
      server.value.path = parsed.path && parsed.path !== '/dns-query' ? parsed.path : ''
    }
  } else {
    server.value.server = (server.value.server ?? '').trim()
  }

  if (activeProvider.value) {
    return
  }

  if (autoSNI.value && server.value.tls_server_name === autoSNI.value) {
    server.value.tls_server_name = ''
    autoSNI.value = ''
  }

  if (autoTag.value && server.value.tag === autoTag.value) {
    server.value.tag = ''
    autoTag.value = ''
  }

  if (autoDescription.value && server.value.description === autoDescription.value) {
    server.value.description = ''
    autoDescription.value = ''
  }
}

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
    tag: server.value.tag.trim(),
    type: server.value.type,
    detour: server.value.detour,
    domain_resolver: '',
    description: server.value.description?.trim(),
    extra,
  }

  if (needsAddress.value) {
    result.server = server.value.server?.trim()
    result.server_port = server.value.server_port || 0
    result.domain_resolver = showResolver.value ? server.value.domain_resolver : ''
  }

  if (usesPath.value) {
    result.path = server.value.path
  }

  if (usesTLS.value) {
    result.tls_server_name = server.value.tls_server_name?.trim()
    result.tls_insecure = server.value.tls_insecure
  }

  emit('submit', result)
}
</script>

<template>
  <form class="form-stack" @submit.prevent="submit">
    <div class="field">
      <span class="field-label">Протокол</span>
      <div class="choice-groups">
        <div v-for="group in typeGroups" :key="group.value">
          <div class="choice-group-label">{{ group.title }}</div>
          <div class="choice-grid">
            <button
              v-for="item in serverTypes.filter((type) => type.group === group.value)"
              :key="item.value"
              type="button"
              class="choice-card"
              :class="{ active: server.type === item.value }"
              :aria-pressed="server.type === item.value"
              @click="selectType(item.value)"
            >
              <span class="choice-card-title">{{ item.title }}</span>
              <span class="choice-card-sub">{{ item.subtitle }}{{ item.port ? `, порт ${item.port}` : '' }}</span>
            </button>
          </div>
        </div>
      </div>
    </div>

    <template v-if="needsAddress">
      <div v-if="typeProviders.length" class="preset-list">
        <span class="preset-label">Популярные:</span>
        <button
          v-for="provider in typeProviders"
          :key="provider.id"
          type="button"
          class="preset-chip"
          :class="{ active: activeProvider?.id === provider.id }"
          @click="applyProvider(provider)"
        >
          {{ provider.name }}
        </button>
      </div>

      <div class="form-grid dns-address-grid">
        <FormField label="Адрес сервера" input-id="dnsServer" class="dns-address">
          <input
            id="dnsServer"
            v-model="server.server"
            class="form-input"
            type="text"
            placeholder="1.1.1.1 или dns.example.com"
            autocomplete="off"
            spellcheck="false"
            required
            @change="onAddressChange"
          >
          <template #hint>IP-адрес или домен. Можно вставить ссылку целиком, например <code>https://dns.google/dns-query</code>.</template>
        </FormField>
        <FormField label="Порт" input-id="dnsPort" optional>
          <input
            id="dnsPort"
            v-model.number="server.server_port"
            class="form-input"
            type="number"
            min="1"
            max="65535"
            :placeholder="String(defaultPort)"
          >
        </FormField>
        <FormField v-if="usesPath" label="Путь" input-id="dnsPath" optional class="span-2" hint="Меняется редко: почти все серверы отвечают на /dns-query.">
          <input id="dnsPath" v-model="server.path" class="form-input" type="text" placeholder="/dns-query" spellcheck="false">
        </FormField>
      </div>

      <FormField
        v-if="usesTLS"
        label="Имя сервера в сертификате (SNI)"
        input-id="dnsSNI"
        :optional="!addressIsIP"
      >
        <input id="dnsSNI" v-model="server.tls_server_name" class="form-input" type="text" :placeholder="activeProvider?.host ?? 'dns.example.com'" spellcheck="false">
        <template #hint>
          <span v-if="sniWarning" class="field-warning">
            Адрес задан IP-адресом — укажите домен из сертификата сервера, иначе проверка сертификата не пройдет.
          </span>
          <template v-else>По умолчанию берется адрес сервера. Нужно, если адрес задан IP-адресом.</template>
        </template>
      </FormField>

      <FormField v-if="showResolver" label="Через какой сервер узнать IP адреса" input-id="dnsResolver">
        <select id="dnsResolver" v-model="server.domain_resolver" class="form-select">
          <option value="">Резолвер outbound-ов из настроек DNS</option>
          <option v-for="tag in resolverOptions" :key="tag" :value="tag">{{ tag }}</option>
        </select>
        <template #hint>Адрес сервера — домен, поэтому сначала sing-box узнает его IP. Выберите сервер, заданный IP-адресом.</template>
      </FormField>
    </template>

    <div v-else class="callout">
      <template v-if="server.type === 'local'">Запросы уходят системному резолверу хоста, на котором работает sing-box (обычно — <code>/etc/resolv.conf</code>).</template>
      <template v-else>Используется DNS-сервер, который роутер выдает по DHCP. Интерфейс можно указать в дополнительных параметрах: <code>{"interface": "eth0"}</code>.</template>
    </div>

    <div class="form-grid">
      <FormField label="Тег" input-id="dnsTag" :error="tagProblem">
        <input
          id="dnsTag"
          v-model="server.tag"
          class="form-input"
          type="text"
          placeholder="cloudflare-doh"
          required
          pattern="[A-Za-z0-9_.\-]+"
          autocomplete="off"
          spellcheck="false"
          :disabled="editing"
          @input="autoTag = ''"
        >
        <template #hint>{{ editing ? 'Не меняется: на тег ссылаются группы и настройки.' : 'Короткое имя латиницей, по нему сервер выбирается в группах и настройках.' }}</template>
      </FormField>
      <FormField label="Описание" input-id="dnsDescription" optional>
        <input id="dnsDescription" v-model="server.description" class="form-input" type="text" placeholder="Для чего этот сервер" @input="autoDescription = ''">
      </FormField>
    </div>

    <details class="disclosure" :open="advancedOpen">
      <summary>
        <SvgIcon :path="icons.chevron" />
        Дополнительно
        <span class="disclosure-note">— маршрут запросов, проверка сертификата, JSON</span>
      </summary>
      <div class="disclosure-body form-stack">
        <FormField label="Отправлять запросы" input-id="dnsDetour">
          <select id="dnsDetour" v-model="server.detour" class="form-select">
            <option value="">Напрямую</option>
            <option v-if="server.detour && !detours.includes(server.detour)" :value="server.detour">{{ server.detour }} (не найден)</option>
            <option v-for="tag in detours" :key="tag" :value="tag">Через {{ tag }}</option>
          </select>
          <template #hint>Через outbound или selector группы — например, чтобы до сервера не дотянулась блокировка провайдера.</template>
        </FormField>

        <ToggleSwitch
          v-if="usesTLS"
          :model-value="Boolean(server.tls_insecure)"
          label="Не проверять сертификат"
          description="Небезопасно: включайте только для своего сервера с самоподписанным сертификатом."
          @update:model-value="server.tls_insecure = $event"
        />

        <FormField label="Дополнительные параметры (JSON)" optional :error="extraError">
          <JsonEditor v-model="extraText" height="120px" :invalid="Boolean(extraError)" />
          <template #hint>Поля дописываются в объект сервера как есть, например <code>{"neighbor_domain": [".lan"]}</code>.</template>
        </FormField>
      </div>
    </details>

    <div v-if="error" class="form-error">{{ error }}</div>

    <div class="form-actions">
      <button type="button" class="btn btn-secondary" @click="emit('cancel')">Отмена</button>
      <button type="submit" class="btn btn-primary" :disabled="saving || Boolean(tagProblem)">
        {{ saving ? 'Сохранение...' : (editing ? 'Сохранить' : 'Добавить сервер') }}
      </button>
    </div>
  </form>
</template>
