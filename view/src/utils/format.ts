// Форматирование чисел, размеров и дат для интерфейса.

// splitBytes делит размер на значение и единицу (шаг 1024).
export function splitBytes(bytes: number): { value: string; unit: string } {
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let value = Number(bytes) || 0
  let unit = 0

  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024
    unit++
  }

  let text: string

  if (unit === 0 || value >= 100) {
    text = Math.round(value).toString()
  } else {
    text = value.toFixed(value >= 10 ? 1 : 2).replace(/\.?0+$/, '')
  }

  return { value: text, unit: units[unit] }
}

// formatRate форматирует скорость в байтах в секунду.
export function formatRate(bytesPerSecond: number): string {
  const parts = splitBytes(bytesPerSecond)

  return `${parts.value} ${parts.unit}/s`
}

// formatBytesRu форматирует размер с русскими единицами.
export function formatBytesRu(bytes?: number): string {
  if (!bytes) {
    return '0 Б'
  }

  const units = ['Б', 'КБ', 'МБ', 'ГБ', 'ТБ']
  const index = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1)
  const value = bytes / Math.pow(1024, index)

  return `${value.toFixed(value >= 100 || index === 0 ? 0 : 1)} ${units[index]}`
}

// formatAgo возвращает «5 мин назад» и т.п.
export function formatAgo(dateString?: string): string {
  const date = new Date(dateString ?? '')

  if (isNaN(date.getTime()) || date.getFullYear() < 2000) {
    return 'никогда'
  }

  const minutes = Math.round((Date.now() - date.getTime()) / 60000)

  if (minutes < 1) {
    return 'только что'
  }

  if (minutes < 60) {
    return `${minutes} мин назад`
  }

  const hours = Math.round(minutes / 60)

  if (hours < 24) {
    return `${hours} ч назад`
  }

  return `${Math.round(hours / 24)} дн назад`
}

// formatDateTime форматирует дату и время.
export function formatDateTime(dateString?: string): string {
  const date = new Date(dateString ?? '')

  if (isNaN(date.getTime()) || date.getFullYear() < 2000) {
    return 'никогда'
  }

  return date.toLocaleString('ru-RU')
}

// formatDate форматирует дату словами («6 октября 2026 г.»).
export function formatDate(date: Date): string {
  return date.toLocaleDateString('ru-RU', { day: 'numeric', month: 'long', year: 'numeric' })
}

// pluralDays склоняет слово «день».
export function pluralDays(n: number): string {
  const mod10 = n % 10
  const mod100 = n % 100

  if (mod10 === 1 && mod100 !== 11) {
    return 'день'
  }

  if (mod10 >= 2 && mod10 <= 4 && (mod100 < 12 || mod100 > 14)) {
    return 'дня'
  }

  return 'дней'
}

// daysLeft возвращает количество целых дней до даты (отрицательное, если дата прошла).
export function daysLeft(date: Date): number {
  return Math.floor((date.getTime() - Date.now()) / 86400000)
}

// dayLevel возвращает класс индикатора по количеству оставшихся дней.
export function dayLevel(days: number): string {
  if (days < 3) {
    return 'is-bad'
  }

  return days < 7 ? 'is-warn' : 'is-good'
}

// parseList разбирает список, разделенный запятыми, пробелами или переводами строк.
export function parseList(value: string): string[] {
  return value.split(/[\s,;]+/).map((item) => item.trim()).filter((item) => item !== '')
}

// ruleTypeLabel возвращает название типа правила.
export function ruleTypeLabel(type: string): string {
  const labels: Record<string, string> = {
    domain: 'Точный домен',
    domain_suffix: 'Домен и поддомены',
    ip: 'IP-адрес',
    cidr: 'Подсеть',
  }

  return labels[type] ?? type
}
