// Типы DNS-серверов sing-box и популярные публичные серверы для быстрого заполнения формы.

export type DNSTypeGroup = 'plain' | 'secure' | 'system'

export interface DNSServerType {
  value: string
  group: DNSTypeGroup
  title: string
  subtitle: string
  // port — порт по умолчанию, 0 — адрес не нужен.
  port: number
  // short — суффикс тега для публичных серверов.
  short: string
}

export const serverTypes: DNSServerType[] = [
  { value: 'udp', group: 'plain', title: 'UDP', subtitle: 'Обычный DNS', port: 53, short: '' },
  { value: 'tcp', group: 'plain', title: 'TCP', subtitle: 'Обычный DNS по TCP', port: 53, short: 'tcp' },
  { value: 'https', group: 'secure', title: 'DoH', subtitle: 'DNS over HTTPS', port: 443, short: 'doh' },
  { value: 'tls', group: 'secure', title: 'DoT', subtitle: 'DNS over TLS', port: 853, short: 'dot' },
  { value: 'h3', group: 'secure', title: 'DoH3', subtitle: 'DNS over HTTP/3', port: 443, short: 'doh3' },
  { value: 'quic', group: 'secure', title: 'DoQ', subtitle: 'DNS over QUIC', port: 853, short: 'doq' },
  { value: 'local', group: 'system', title: 'Системный', subtitle: 'Резолвер хоста', port: 0, short: '' },
  { value: 'dhcp', group: 'system', title: 'DHCP', subtitle: 'DNS, выданный роутером', port: 0, short: '' },
]

export const typeGroups: { value: DNSTypeGroup; title: string }[] = [
  { value: 'secure', title: 'Шифрованный — провайдер не видит запросы' },
  { value: 'plain', title: 'Без шифрования' },
  { value: 'system', title: 'Системный' },
]

export interface DNSProvider {
  id: string
  name: string
  ip: string
  // host — имя из сертификата сервера для шифрованных протоколов.
  host: string
  types: string[]
}

export const providers: DNSProvider[] = [
  { id: 'cloudflare', name: 'Cloudflare', ip: '1.1.1.1', host: 'cloudflare-dns.com', types: ['udp', 'tcp', 'tls', 'https', 'h3'] },
  { id: 'google', name: 'Google', ip: '8.8.8.8', host: 'dns.google', types: ['udp', 'tcp', 'tls', 'https'] },
  { id: 'quad9', name: 'Quad9', ip: '9.9.9.9', host: 'dns.quad9.net', types: ['udp', 'tcp', 'tls', 'https'] },
  { id: 'adguard', name: 'AdGuard', ip: '94.140.14.14', host: 'dns.adguard-dns.com', types: ['udp', 'tcp', 'tls', 'https', 'h3', 'quic'] },
  { id: 'yandex', name: 'Яндекс', ip: '77.88.8.8', host: 'common.dot.dns.yandex.net', types: ['udp', 'tcp', 'tls', 'https'] },
]

// serverType возвращает описание типа сервера.
export function serverType(value: string): DNSServerType | undefined {
  return serverTypes.find((item) => item.value === value)
}

// usesTLS проверяет, что тип сервера работает поверх TLS.
export function usesTLS(type: string): boolean {
  return ['tls', 'https', 'h3', 'quic'].includes(type)
}

// usesPath проверяет, что у типа сервера есть путь запроса.
export function usesPath(type: string): boolean {
  return type === 'https' || type === 'h3'
}

// needsAddress проверяет, что для типа сервера нужен адрес.
export function needsAddress(type: string): boolean {
  return type !== 'local' && type !== 'dhcp'
}

// schemeTypes — схемы адресов, которые можно вставить в поле адреса целиком.
const schemeTypes: Record<string, string> = {
  udp: 'udp',
  tcp: 'tcp',
  tls: 'tls',
  https: 'https',
  h3: 'h3',
  quic: 'quic',
}

// parseServerURL разбирает адрес вида https://dns.google/dns-query или tls://1.1.1.1:853.
export function parseServerURL(value: string): { type: string; server: string; port?: number; path?: string } | null {
  const match = value.trim().match(/^([a-z0-9]+):\/\/(\[[^\]]+\]|[^/:]+)(?::(\d+))?(\/.*)?$/i)

  if (!match || !schemeTypes[match[1].toLowerCase()]) {
    return null
  }

  return {
    type: schemeTypes[match[1].toLowerCase()],
    server: match[2].replace(/^\[|\]$/g, ''),
    port: match[3] ? Number(match[3]) : undefined,
    path: match[4],
  }
}
