// Типы ответов API конфигуратора.

export interface Rule {
  id: string
  type: 'domain' | 'domain_suffix' | 'ip' | 'cidr'
  value: string
  description: string
  group: string
  applied: boolean
  deleted: boolean
  created_at: string
}

export interface URLSource {
  id: string
  url: string
  description: string
  group: string
  interval: number
  last_update: string
  last_status: string
  last_error?: string
  items_count: number
  applied: boolean
  deleted: boolean
  created_at: string
}

export interface Group {
  name: string
  description: string
  default_outbound?: string
  dns_server?: string
  created_at: string
  system?: boolean
}

export interface BulkAddResult {
  success: number
  failed: number
  total: number
  failed_values?: { value: string; error: string }[]
}

export interface DNSServer {
  tag: string
  type: string
  server?: string
  server_port?: number
  path?: string
  tls_server_name?: string
  tls_insecure?: boolean
  detour?: string
  domain_resolver?: string
  description?: string
  extra?: Record<string, unknown> | null
}

export interface DNSSettings {
  final: string
  strategy?: string
  default_domain_resolver?: string
  cache_capacity?: number
  optimistic?: boolean
  timeout?: string
  extra?: Record<string, unknown> | null
}

export interface DNSData {
  servers: DNSServer[]
  settings: DNSSettings
  rules: Record<string, unknown>[]
}

export interface DNSRecord {
  id: string
  domain: string
  addresses: string[]
  description: string
  created_at: string
}

export type OutboundSource = 'manual' | 'happ' | 'amnezia' | 'builtin' | 'urltest'

export interface OutboundView {
  id?: string
  tag: string
  type: string
  server?: string
  port?: number
  source: OutboundSource
  source_name?: string
  profile_id?: string
  config?: Record<string, unknown>
}

// URLTestSourceKind — источник участников urltest-а.
export type URLTestSourceKind = 'all' | 'manual' | 'happ' | 'amnezia'

export interface URLTestSource {
  kind: URLTestSourceKind
  profile_id?: string
}

export interface URLTest {
  id: string
  tag: string
  description: string
  sources: URLTestSource[]
  include_regexp?: string
  exclude_regexp?: string
  tags: string[]
  exclude_tags: string[]
  url?: string
  interval?: string
  tolerance?: number
  interrupt_exist_connections: boolean
  created_at?: string
}

export type URLTestMemberState = 'included' | 'excluded_tag' | 'excluded_regexp' | 'missing'

export interface URLTestMember {
  tag: string
  state: URLTestMemberState
}

export interface URLTestView extends URLTest {
  members: URLTestMember[]
  error?: string
}

// URLTestCandidate — outbound, который может войти в urltest.
export interface URLTestCandidate {
  tag: string
  type: string
  source: 'manual' | 'happ' | 'amnezia'
  profile_id?: string
}

export interface SubscriptionProfileRef {
  source: 'happ' | 'amnezia'
  id: string
  name: string
}

export interface URLTestsData {
  urltests: URLTestView[]
  candidates: URLTestCandidate[]
  profiles: SubscriptionProfileRef[]
}

export interface MixedUser {
  username: string
  password: string
}

export interface MixedInbound {
  tag: string
  listen: string
  listen_port: number
  users: MixedUser[]
  extra?: Record<string, unknown> | null
}

export interface Settings {
  log_level: string
  clash_api: {
    secret: string
    allow_origins: string[]
  }
}

export interface ConfigState {
  rendered: string
  actual: string
  changed: boolean
  warnings: string[]
  actual_error?: string
}

export interface ApplyResult {
  success: boolean
  message: string
  warnings: string[]
  backup?: string
}

export interface HappServer {
  tag: string
  name: string
  type: string
  protocol: string
  address: string
  port: number
  outbound: Record<string, unknown>
}

export interface HappProfileInfo {
  title?: string
  upload?: number
  download?: number
  total?: number
  expire?: string
  announce?: string
  support_url?: string
  web_page_url?: string
  update_interval?: number
}

export interface HappProfile {
  id: string
  name: string
  url: string
  info?: HappProfileInfo
  servers: HappServer[]
  warnings?: string[]
  last_update: string
  last_error?: string
  created_at: string
}

export interface AmneziaItem {
  tag: string
  name: string
  protocol: string
  server: string
  config: Record<string, unknown>
  requires_awg: boolean
}

export interface AmneziaCountry {
  code: string
  name: string
}

export interface AmneziaPremium {
  server_country_code?: string
  server_country_name?: string
  available_countries?: AmneziaCountry[]
  subscription_end?: string
  config_expires_at?: string
}

export interface AmneziaProfile {
  id: string
  name: string
  description: string
  server: string
  items: AmneziaItem[]
  warnings?: string[]
  premium?: AmneziaPremium
  last_update: string
  last_error?: string
  requires_awg: boolean
}

export interface AWGSupport {
  supported: boolean
  version: string
  error?: string
}

export interface TrafficSample {
  up: number
  down: number
}

export interface Overview {
  downloadTotal: number
  uploadTotal: number
  memory: number
  connections: { total: number; tcp: number; udp: number }
  rules: number
  version: string
  traffic: TrafficSample[]
  trafficStreamOk: boolean
}

export interface ClashProxy {
  type: string
  name: string
  now?: string
  all?: string[]
  history?: { time?: string; delay: number }[]
  udp?: boolean
}

export interface Release {
  version: string
  changelog?: string
}

export interface UpdateCheck {
  current_version: string
  latest_version: string
  available: Release[]
  checked_at?: string
  error?: string
  // unsupported — почему обновление через интерфейс недоступно (например, локальная сборка).
  unsupported?: string
}

export interface UpdateStep {
  time: string
  level: string
  step: string
  message: string
  error?: string
}

export interface UpdateStatus {
  exists: boolean
  running: boolean
  update_id?: string
  target?: string
  result?: string
  from_version?: string
  to_version?: string
  error?: string
  steps?: UpdateStep[]
  started_at?: string
  finished_at?: string
}
