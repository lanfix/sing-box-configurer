<script setup lang="ts">
// Ввод списка значений чипами: Enter, запятая или пробел добавляют значение, вставленный текст
// разбивается на элементы, Backspace в пустом поле удаляет последний чип.
import { ref } from 'vue'

import { icons } from '../../icons'
import { parseList } from '../../utils/format'
import SvgIcon from '../SvgIcon.vue'

const model = defineModel<string[]>({ required: true })

const props = defineProps<{
  inputId?: string
  placeholder?: string
  // validate возвращает текст ошибки для значения или пустую строку, если значение корректно.
  validate?: (value: string) => string
  disabled?: boolean
}>()

const draft = ref('')
const input = ref<HTMLInputElement | null>(null)

// commit переносит введенный текст в список, пропуская повторы.
function commit(): void {
  const values = parseList(draft.value).filter((value, index, list) => !model.value.includes(value) && list.indexOf(value) === index)

  if (values.length) {
    model.value = [...model.value, ...values]
  }

  draft.value = ''
}

// onKeydown обрабатывает разделители и удаление последнего чипа.
function onKeydown(event: KeyboardEvent): void {
  if (event.key === 'Enter' || event.key === ',' || event.key === ';' || event.key === ' ') {
    // Enter в пустом поле отправляет форму как обычно.
    if (event.key === 'Enter' && !draft.value.trim()) {
      return
    }

    event.preventDefault()
    commit()

    return
  }

  if (event.key === 'Backspace' && draft.value === '' && model.value.length) {
    model.value = model.value.slice(0, -1)
  }
}

// onPaste разбивает вставленный список на чипы.
function onPaste(event: ClipboardEvent): void {
  const text = event.clipboardData?.getData('text') ?? ''

  if (/[\s,;]/.test(text.trim())) {
    event.preventDefault()
    draft.value += text
    commit()
  }
}

// remove удаляет чип.
function remove(index: number): void {
  model.value = model.value.filter((_, position) => position !== index)
  input.value?.focus()
}

// error возвращает ошибку значения.
function error(value: string): string {
  return props.validate?.(value) ?? ''
}
</script>

<template>
  <div class="chips-input" :class="{ 'is-disabled': disabled }" @click="input?.focus()">
    <span
      v-for="(value, index) in model"
      :key="value"
      class="chips-item"
      :class="{ 'is-invalid': Boolean(error(value)) }"
      :title="error(value) || undefined"
    >
      {{ value }}
      <button v-if="!disabled" type="button" :aria-label="`Убрать ${value}`" @click.stop="remove(index)">
        <SvgIcon :path="icons.close" />
      </button>
    </span>
    <input
      :id="inputId"
      ref="input"
      v-model="draft"
      type="text"
      autocomplete="off"
      spellcheck="false"
      :placeholder="model.length ? '' : placeholder"
      :disabled="disabled"
      @keydown="onKeydown"
      @paste="onPaste"
      @blur="commit"
    >
  </div>
</template>
