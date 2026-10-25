<script setup lang="ts">
import { ref, computed, onMounted, watch, nextTick } from 'vue'
import Prism from 'prismjs'
import 'prismjs/components/prism-python'
import 'prismjs/components/prism-go'
import 'prismjs/components/prism-bash'
import 'prismjs/components/prism-rust'
import 'prismjs/components/prism-sql'
import 'prismjs/components/prism-docker'
import 'prismjs/components/prism-json'
import 'prismjs/components/prism-javascript'
import 'prismjs/components/prism-typescript'
import 'prismjs/components/prism-markup-templating'
import 'prismjs/components/prism-php'
import 'prismjs/components/prism-csharp'
import 'prismjs/components/prism-powershell'
import 'prismjs/components/prism-c'
import 'prismjs/components/prism-cpp'

const props = defineProps<{
  code: string
  language?: string
}>()

const codeEl = ref<HTMLElement | null>(null)
const copied = ref(false)

const normalizedLang = computed(() => {
  const l = (props.language || '').toLowerCase().trim()
  switch (l) {
    case 'c#':
    case 'cs':
      return 'csharp'
    case 'c++':
      return 'cpp'
    case 'js':
      return 'javascript'
    case 'ts':
      return 'typescript'
    case 'py':
      return 'python'
    case 'golang':
      return 'go'
    case 'ps1':
    case 'ps':
      return 'powershell'
    case 'sh':
      return 'bash'
    default:
      return l || 'text'
  }
})

// Strip leading ```lang and trailing ``` if model wrapped snippet in markdown fences
const cleanCode = computed(() => {
  let c = (props.code || '').trim()
  if (c.startsWith('```')) {
    const firstNewline = c.indexOf('\n')
    if (firstNewline !== -1) {
      c = c.substring(firstNewline + 1)
    } else {
      c = ''
    }
  }
  if (c.endsWith('```')) {
    c = c.substring(0, c.length - 3)
  }
  return c.trim()
})

const highlight = () => {
  nextTick(() => {
    if (codeEl.value) {
      Prism.highlightElement(codeEl.value)
    }
  })
}

onMounted(highlight)
watch(cleanCode, highlight)

const copyCode = async () => {
  try {
    await navigator.clipboard.writeText(cleanCode.value)
    copied.value = true
    setTimeout(() => { copied.value = false }, 2000)
  } catch (e) {
    console.error('Failed to copy', e)
  }
}
</script>

<template>
  <div class="relative group my-3 rounded-xl overflow-hidden border border-slate-700 bg-slate-900 shadow-inner">
    <div class="flex items-center justify-between px-3 py-1.5 bg-slate-800/80 border-b border-slate-700/60 text-xs text-slate-400">
      <span class="font-mono uppercase tracking-wider text-[11px] text-sky-400">{{ language || 'code' }}</span>
      <button 
        @click="copyCode" 
        class="hover:text-slate-200 transition-colors flex items-center gap-1 text-[11px]"
      >
        <span v-if="copied" class="text-emerald-400">Copied!</span>
        <span v-else>Copy</span>
      </button>
    </div>
    <div class="overflow-x-auto p-4 pl-7 sm:pl-8 text-sm font-mono leading-relaxed">
      <pre class="!bg-transparent !p-0 !m-0"><code ref="codeEl" :class="`language-${normalizedLang}`">{{ cleanCode }}</code></pre>
    </div>
  </div>
</template>
