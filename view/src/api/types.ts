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
  // detour — outbound sing-box, через который загружается список; пусто — напрямую.
  detour?: string
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
  // warnings — добавленные значения, которые входят в URL-источники групп выше.
  warnings?: { value: string; error: string }[]
}

// RuleCheck — проверка значения нового правила до добавления.
export interface RuleCheck {
  value: string
  // error — добавить нельзя (пересечение с ручным правилом или некорректное значение).
  error?: string
  // warnings — значение перекрыто URL-источником группы выше.
  warnings?: string[]
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

// group — selector группы правил (выбранный в группе outbound).
export type OutboundSource = 'manual' | 'happ' | 'amnezia' | 'builtin' | 'urltest' | 'group'

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
  happ: {
    auto_apply: boolean
  }
  speed_test: SpeedTestSettings
}

export interface SpeedTestServer {
  name: string
  url: string
}

export interface SpeedTestSettings {
  servers: SpeedTestServer[]
  // duration — длительность замера, с.
  duration: number
  streams: number
}

export interface SpeedTestResult {
  tag: string
  server?: string
  server_url?: string
  // download — байт/с.
  download: number
  bytes: number
  duration_ms: number
  latency_ms: number
  attempts: { server: string; error: string }[]
  warning?: string
  error?: string
  tested_at: string
}

export interface SpeedTestState {
  settings: SpeedTestSettings
  running: string
  results: Record<string, SpeedTestResult>
}

// SecuritySettings — защита панели от запросов с чужих сайтов.
export interface SecuritySettings {
  check_host: boolean
  allowed_hosts: string[]
  // extra_hosts — адреса из конфига сервиса, разрешенные всегда.
  extra_hosts: string[]
  current_host: string
  current_allowed: boolean
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
  // platform — способ установки: docker или systemd.
  platform?: string
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

// ===== Карта трафика =====

export type TopologyNodeKind = 'inbound' | 'router' | 'dns-router' | 'selector' | 'urltest' | 'outbound' | 'action' | 'dns-server'

export type TopologyEdgeKind = 'inbound' | 'route' | 'member' | 'dns' | 'detour'

export interface GroupStats {
  domains: number
  suffixes: number
  ips: number
  sources: number
  source_items: number
}

export interface TopologyGroup {
  name: string
  description: string
  dns_server?: string
  stats: GroupStats
}

// TopologyRow — строка маршрутизатора: правило и узел, куда оно направляет трафик.
export interface TopologyRow {
  id: string
  index: number
  label: string
  detail?: string
  action: string
  target?: string
  groups?: string[]
}

export interface TopologyNode {
  id: string
  kind: TopologyNodeKind
  tag: string
  type: string
  label: string
  detail?: string
  rows?: TopologyRow[]
  members?: string[]
  now?: string
  delay?: number
  cluster?: string
  group?: TopologyGroup
  detour?: string
  detour_implicit?: boolean
}

export interface TopologyEdge {
  id: string
  source: string
  source_handle?: string
  target: string
  kind: TopologyEdgeKind
  active: boolean
  implicit?: boolean
}

export interface TopologyGraph {
  nodes: TopologyNode[]
  edges: TopologyEdge[]
  warnings: string[]
}

// TopologyConnection — активное соединение, привязанное к карте.
export interface TopologyConnection {
  id: string
  host: string
  destination: string
  network: string
  source: string
  inbound: string
  row: string
  rule: string
  chain: string[]
  upload: number
  download: number
  start: string
}

export interface RuleMatch {
  type: string
  value: string
  source: 'manual' | 'url'
  source_name?: string
}

export interface TraceStep {
  row: string
  label: string
  action: string
  matched: boolean
  reason: string
  matches?: RuleMatch[]
}

export interface TraceResult {
  query: string
  domain?: string
  ips: string[]
  resolved?: 'hosts' | 'system'
  resolve_error?: string
  inbound: string
  dns?: {
    row: string
    label: string
    action: string
    server?: string
    reason: string
    matches?: RuleMatch[]
    chain: string[]
    implicit?: boolean
  }
  steps: TraceStep[]
  row: string
  action: string
  chain: string[]
  outbound?: string
  nodes: string[]
  edges: string[]
  dns_nodes: string[]
  dns_edges: string[]
  notes: string[]
}

// Connection — активное соединение sing-box (страница «Соединения»).
export interface Connection {
  id: string
  host: string
  destination: string
  network: string
  source: string
  inbound: string
  rule: string
  // group — группа правил, в selector которой ушло соединение.
  group?: string
  chain: string[]
  outbound: string
  process?: string
  upload: number
  download: number
  start: string
}

export interface ConnectionsSnapshot {
  connections: Connection[]
  download_total: number
  upload_total: number
  memory: number
}

// ConfigBackup — резервная копия рабочего конфига sing-box.
export interface ConfigBackup {
  name: string
  created_at: string
  size: number
}

export type LogSource = 'sing-box' | 'configurer'

export interface LogsResponse {
  source: LogSource
  platform: string
  text: string
}

// AppDataSummary — содержимое файла импорта app.json.
export interface AppDataSummary {
  schema_version: number
  latest_version: number
  counts: Record<string, number>
  has_auth: boolean
  username?: string
  allowed_hosts: string[]
}

export interface ImportResult {
  from_version: number
  to_version: number
  migrations: string[]
  backup: string
}
