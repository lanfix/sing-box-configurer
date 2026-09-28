<script setup lang="ts">
// Редактор JSON на CodeMirror. Подключается через defineAsyncComponent, чтобы CodeMirror
// загружался только на страницах, где он нужен.
import { json } from '@codemirror/lang-json'
import { EditorState } from '@codemirror/state'
import { oneDark } from '@codemirror/theme-one-dark'
import { EditorView, basicSetup } from 'codemirror'
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'

const props = withDefaults(defineProps<{
  modelValue: string
  height?: string
  invalid?: boolean
}>(), {
  height: '240px',
  invalid: false,
})

const emit = defineEmits<{
  'update:modelValue': [value: string]
}>()

const container = ref<HTMLElement | null>(null)

let view: EditorView | null = null

onMounted(() => {
  view = new EditorView({
    parent: container.value!,
    state: EditorState.create({
      doc: props.modelValue,
      extensions: [
        basicSetup,
        json(),
        oneDark,
        EditorView.theme({
          '&': { height: props.height },
          '.cm-scroller': { overflow: 'auto' },
        }),
        EditorView.updateListener.of((update) => {
          if (update.docChanged) {
            emit('update:modelValue', update.state.doc.toString())
          }
        }),
      ],
    }),
  })
})

// Значение, заданное снаружи (например, при открытии другой записи), заменяет содержимое редактора.
watch(() => props.modelValue, (value) => {
  if (view && value !== view.state.doc.toString()) {
    view.dispatch({
      changes: { from: 0, to: view.state.doc.length, insert: value },
    })
  }
})

onBeforeUnmount(() => {
  view?.destroy()
  view = null
})
</script>

<template>
  <div ref="container" class="code-box" :class="{ 'is-invalid': invalid }"></div>
</template>
