// Проверки значений форм на стороне браузера: сервер проверяет их еще раз.

const ipv4Re = /^(25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)(\.(25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)){3}$/
const domainRe = /^(?=.{1,253}$)([a-z0-9_]([a-z0-9_-]{0,61}[a-z0-9_])?\.)*[a-z0-9_]([a-z0-9_-]{0,61}[a-z0-9_])?$/i

// isIPv4 проверяет IPv4-адрес.
export function isIPv4(value: string): boolean {
  return ipv4Re.test(value)
}

// isIPv6 проверяет IPv6-адрес (упрощенно: шестнадцатеричные группы и одно сокращение ::).
export function isIPv6(value: string): boolean {
  if (!value.includes(':') || (value.match(/::/g) ?? []).length > 1) {
    return false
  }

  const groups = value.split(':')

  return groups.length <= 8 && groups.every((group) => /^[0-9a-f]{0,4}$/i.test(group))
}

// isIP проверяет IPv4- или IPv6-адрес.
export function isIP(value: string): boolean {
  return isIPv4(value) || isIPv6(value)
}

// isCIDR проверяет подсеть вида 10.0.0.0/8 или fd00::/8.
export function isCIDR(value: string): boolean {
  const [address, prefix, extra] = value.split('/')

  if (extra !== undefined || prefix === undefined || !/^\d{1,3}$/.test(prefix)) {
    return false
  }

  return (isIPv4(address) && Number(prefix) <= 32) || (isIPv6(address) && Number(prefix) <= 128)
}

// isDomain проверяет доменное имя.
export function isDomain(value: string): boolean {
  return domainRe.test(value.replace(/\.$/, ''))
}

// ipError возвращает ошибку для значения, которое должно быть IP-адресом.
export function ipError(value: string): string {
  return isIP(value) ? '' : 'Не похоже на IP-адрес'
}

// tagError проверяет тег: латиница, цифры, точка, дефис и подчеркивание.
export function tagError(value: string): string {
  if (!value) {
    return ''
  }

  return /^[A-Za-z0-9_.-]+$/.test(value) ? '' : 'Только латинские буквы, цифры и символы . _ -'
}
