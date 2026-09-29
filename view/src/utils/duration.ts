// Длительности в формате Go (time.ParseDuration): 500ms, 5s, 3m, 1h30m.

export type DurationUnit = 'ms' | 's' | 'm' | 'h'

// durationUnits — длительность единицы в миллисекундах и подпись.
export const durationUnits: Record<DurationUnit, { ms: number; label: string }> = {
  ms: { ms: 1, label: 'мс' },
  s: { ms: 1000, label: 'сек' },
  m: { ms: 60000, label: 'мин' },
  h: { ms: 3600000, label: 'ч' },
}

// parseDuration переводит строку Go в число и самую крупную из допустимых единиц, в которой число целое.
// Возвращает null, если строка не разбирается.
export function parseDuration(value: string, units: DurationUnit[]): { amount: string; unit: DurationUnit } | null {
  const text = value.trim()

  if (!text || !/^(\d+(\.\d+)?(ms|s|m|h))+$/.test(text)) {
    return null
  }

  let total = 0

  for (const match of text.matchAll(/(\d+(?:\.\d+)?)(ms|s|m|h)/g)) {
    total += Number(match[1]) * durationUnits[match[2] as DurationUnit].ms
  }

  const sorted = [...units].sort((a, b) => durationUnits[b].ms - durationUnits[a].ms)
  const unit = sorted.find((item) => total % durationUnits[item].ms === 0) ?? sorted[sorted.length - 1]

  if (!unit) {
    return null
  }

  return { amount: String(+(total / durationUnits[unit].ms).toFixed(3)), unit }
}

// formatDuration собирает строку Go из числа и единицы. Пустое число — пустая строка (значение по умолчанию).
export function formatDuration(amount: string | number, unit: DurationUnit): string {
  const text = String(amount).trim()

  return text === '' ? '' : `${text}${unit}`
}
