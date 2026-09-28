// Построение SVG-путей для графиков.

export interface Point {
  x: number
  y: number
}

// monotonePath строит сглаженную линию монотонной кубической интерполяцией (Фрич — Карлсон):
// по построению она не выходит за пределы соседних значений.
export function monotonePath(points: Point[]): string {
  const n = points.length

  if (n === 0) {
    return ''
  }

  if (n === 1) {
    return `M${points[0].x},${points[0].y}`
  }

  const dx: number[] = []
  const slope: number[] = []

  for (let i = 0; i < n - 1; i++) {
    dx[i] = points[i + 1].x - points[i].x
    slope[i] = (points[i + 1].y - points[i].y) / dx[i]
  }

  const tangent = new Array<number>(n)

  tangent[0] = slope[0]
  tangent[n - 1] = slope[n - 2]

  for (let i = 1; i < n - 1; i++) {
    if (slope[i - 1] * slope[i] <= 0) {
      tangent[i] = 0
    } else {
      const w1 = 2 * dx[i] + dx[i - 1]
      const w2 = dx[i] + 2 * dx[i - 1]

      tangent[i] = (w1 + w2) / (w1 / slope[i - 1] + w2 / slope[i])
    }
  }

  const r = (v: number) => Math.round(v * 100) / 100

  let d = `M${r(points[0].x)},${r(points[0].y)}`

  for (let i = 0; i < n - 1; i++) {
    const c1x = points[i].x + dx[i] / 3
    const c1y = points[i].y + tangent[i] * dx[i] / 3
    const c2x = points[i + 1].x - dx[i] / 3
    const c2y = points[i + 1].y - tangent[i + 1] * dx[i] / 3

    d += `C${r(c1x)},${r(c1y)} ${r(c2x)},${r(c2y)} ${r(points[i + 1].x)},${r(points[i + 1].y)}`
  }

  return d
}

// niceSteps — «круглые» значения верха шкалы в единице отображения (шаг 1024).
const niceSteps = [1, 2, 2.5, 5, 10, 20, 25, 50, 100, 200, 250, 500, 1024]

// niceMax округляет верх шкалы вверх до круглого числа, иначе подписи оси выглядят как «9.54 MB/s».
export function niceMax(value: number): number {
  if (value <= 0) {
    return 1024
  }

  let scale = 1

  while (value / scale >= 1024) {
    scale *= 1024
  }

  const normalized = value / scale
  const step = niceSteps.find((candidate) => candidate >= normalized) ?? 1024

  return step * scale
}
