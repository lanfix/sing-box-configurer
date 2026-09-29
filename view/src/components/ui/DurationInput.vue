<script setup lang="ts">
// Длительность в формате Go (5s, 3m, 500ms): число и единица вместо ручного ввода строки.
import { ref, watch } from 'vue'

import { durationUnits, formatDuration, parseDuration, type DurationUnit } from '../../utils/duration'

const model = defineModel<string>({ required: true })

const props = withDefaults(defineProps<{
  inputId?: string
  units?: DurationUnit[]
  // defaultUnit — единица для пустого значения.
  defaultUnit?: DurationUnit
  placeholder?: string
  disabled?: boolean
}>(), {
  units: () => ['ms', 's', 'm', 'h'],
  defaultUnit: 's',
})

const amount = ref('')
const unit = ref<DurationUnit>(props.defaultUnit)

// Значение, которое не удалось разобрать (например, «1h30m» при единицах без часов), показывается как есть.
const raw = ref('')

// sync разбирает внешнее значение в число и единицу.
function sync(value: string): void {
  if (value === formatDuration(amount.value, unit.value)) {
    return
  }

  const parsed = parseDuration(value, props.units)

  raw.value = parsed || !value ? '' : value
  amount.value = parsed?.amount ?? ''
  unit.value = parsed?.unit ?? unit.value
}

watch(model, sync, { immediate: true })

// update запоминает число или единицу и собирает из них значение.
function update(next: { amount?: string; unit?: string }): void {
  amount.value = next.amount ?? amount.value
  unit.value = (next.unit as DurationUnit | undefined) ?? unit.value
  raw.value = ''
  model.value = formatDuration(amount.value, unit.value)
}
</script>

<template>
  <div class="input-group">
    <input
      :id="inputId"
      :value="amount"
      class="form-input"
      type="number"
      min="0"
      step="any"
      inputmode="decimal"
      :placeholder="raw || placeholder"
      :disabled="disabled"
      @input="update({ amount: ($event.target as HTMLInputElement).value })"
    >
    <select
      class="form-select"
      :value="unit"
      :disabled="disabled"
      aria-label="Единица"
      @change="update({ unit: ($event.target as HTMLSelectElement).value })"
    >
      <option v-for="item in units" :key="item" :value="item">{{ durationUnits[item].label }}</option>
    </select>
  </div>
</template>
