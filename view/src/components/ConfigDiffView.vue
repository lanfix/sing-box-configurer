<script setup lang="ts">
// Итоговый конфиг с изменениями относительно рабочего: добавленные строки подсвечены,
// удаленные показаны прямо в тексте. Только чтение.
import { json } from '@codemirror/lang-json'
import { unifiedMergeView } from '@codemirror/merge'
import { EditorState } from '@codemirror/state'
import { oneDark } from '@codemirror/theme-one-dark'
import { EditorView, basicSetup } from 'codemirror'
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'

const props = defineProps<{
  // original — рабочий конфиг, modified — итоговый.
  original: string
  modified: string
  onlyChanges: boolean
}>()

const container = ref<HTMLElement | null>(null)

let view: EditorView | null = null

// createView пересоздает редактор с текущими параметрами.
function createView(): void {
  view?.destroy()

  view = new EditorView({
    parent: container.value!,
    state: EditorState.create({
      doc: props.modified,
      extensions: [
        basicSetup,
        json(),
        oneDark,
        EditorState.readOnly.of(true),
        EditorView.editable.of(false),
        EditorState.phrases.of({
          '$ unchanged lines': 'Без изменений строк: $',
        }),
        unifiedMergeView({
          original: props.original,
          mergeControls: false,
          highlightChanges: true,
          syntaxHighlightDeletions: true,
          collapseUnchanged: props.onlyChanges ? { margin: 4, minSize: 8 } : undefined,
        }),
      ],
    }),
  })
}

onMounted(createView)

watch(() => [props.original, props.modified, props.onlyChanges], createView)

onBeforeUnmount(() => {
  view?.destroy()
  view = null
})
</script>

<template>
  <div ref="container" class="code-box config-view"></div>
</template>
