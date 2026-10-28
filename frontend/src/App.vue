<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import CodeBlock from './components/CodeBlock.vue'
import ModalDialog from './components/ModalDialog.vue'
import {
  getCategories,
  createCategory,
  updateCategory,
  getRandomQuestion,
  prefetchQuestions,
  submitAnswer,
  deleteQuestion,
  getUserStats,
  resetStats,
  getWorkerStatus,
  triggerWorker,
  downloadAndCacheOfflinePack,
  syncOfflineAnswersToServer,
  type Category,
  type Question,
  type SubmitAnswerResponse,
  type UserStats,
  type WorkerStatus
} from './api'
import { getPendingAnswersCount, resetOfflineStats } from './offline'

// Application State
const categories = ref<Category[]>([])
const questionQueue = ref<Question[]>([])
const isBuffering = ref(false)
let lastBufferCategory = 'all'
let lastBufferDifficulty = 'all'
const selectedCategory = ref<string>('all')
const selectedDifficulty = ref<string>('all')
const currentQuestion = ref<Question | null>(null)
const selectedOptionIds = ref<number[]>([])
const answerResult = ref<SubmitAnswerResponse | null>(null)
const loading = ref(false)
const submitting = ref(false)
const errorMessage = ref('')

// Offline & Synchronization State
const isOnline = ref(navigator.onLine)
const pendingCount = ref(0)
const isSyncing = ref(false)
const lastSyncMessage = ref('')

// Analytics & Worker Monitor
const stats = ref<UserStats | null>(null)
const workerStatus = ref<WorkerStatus | null>(null)
const showStatsModal = ref(false)
const showCategoryModal = ref(false)

// New Category Form State
const newCat = ref({
  name: '',
  description: '',
  prompt_instructions: ''
})
const creatingCategory = ref(false)

// Edit Category State
const categoryModalTab = ref<'create' | 'edit'>('create')
const editingCat = ref<{
  id: number
  name: string
  description: string
  prompt_instructions: string
}>({ id: 0, name: '', description: '', prompt_instructions: '' })
const savingCategory = ref(false)
const selectedEditCategoryId = ref<number | null>(null)

// Custom Dialog / Popup State
interface DialogState {
  show: boolean
  title?: string
  message: string
  type?: 'info' | 'success' | 'warning' | 'error' | 'confirm'
  confirmText?: string
  cancelText?: string
  danger?: boolean
  resolve?: (confirmed: boolean) => void
}

const dialog = ref<DialogState>({
  show: false,
  title: '',
  message: '',
  type: 'info',
  confirmText: '',
  cancelText: 'Cancel',
  danger: false
})

function showDialog(options: {
  title?: string
  message: string
  type?: 'info' | 'success' | 'warning' | 'error' | 'confirm'
  confirmText?: string
  cancelText?: string
  danger?: boolean
}): Promise<boolean> {
  return new Promise((resolve) => {
    dialog.value = {
      show: true,
      title: options.title || '',
      message: options.message,
      type: options.type || 'info',
      confirmText: options.confirmText || '',
      cancelText: options.cancelText || 'Cancel',
      danger: !!options.danger,
      resolve
    }
  })
}

function onDialogConfirm() {
  const res = dialog.value.resolve
  dialog.value.show = false
  if (res) res(true)
}

function onDialogCancel() {
  const res = dialog.value.resolve
  dialog.value.show = false
  if (res) res(false)
}

function showAlert(message: string, type: 'info' | 'success' | 'warning' | 'error' = 'info', title?: string) {
  return showDialog({ message, type, title })
}

function showConfirm(message: string, options?: { title?: string; confirmText?: string; cancelText?: string; danger?: boolean }) {
  return showDialog({
    message,
    type: 'confirm',
    title: options?.title || 'Confirm Action',
    confirmText: options?.confirmText || 'Confirm',
    cancelText: options?.cancelText || 'Cancel',
    danger: options?.danger ?? false
  })
}

// Lifecycle
onMounted(async () => {
  window.addEventListener('online', async () => {
    isOnline.value = true
    await handleSync()
  })
  window.addEventListener('offline', () => {
    isOnline.value = false
  })

  await refreshPendingCount()
  await Promise.all([loadCategories(), loadStats(), loadWorkerStatus()])
  await nextQuestion()

  if (pendingCount.value > 0) {
    await handleSync()
  }

  // Pre-load offline pack in background
  downloadAndCacheOfflinePack().catch(() => {})

  // Periodic worker status check
  setInterval(() => {
    if (isOnline.value) loadWorkerStatus()
  }, 20000)

  // Periodic background sync if pending items exist
  setInterval(() => {
    if (isOnline.value && pendingCount.value > 0) {
      handleSync()
    }
  }, 15000)
})

async function refreshPendingCount() {
  try {
    pendingCount.value = await getPendingAnswersCount()
  } catch {
    pendingCount.value = 0
  }
}

async function handleSync() {
  if (isSyncing.value) return
  isSyncing.value = true
  lastSyncMessage.value = 'Syncing...'
  try {
    const res = await syncOfflineAnswersToServer()
    if (res && res.synced_count > 0) {
      lastSyncMessage.value = `Synced ${res.synced_count} answer${res.synced_count > 1 ? 's' : ''}!`
      if (res.user_stats) {
        stats.value = res.user_stats
      }
      await loadCategories()
      setTimeout(() => { lastSyncMessage.value = '' }, 4000)
    } else {
      lastSyncMessage.value = ''
    }
    await refreshPendingCount()
  } catch (e: any) {
    lastSyncMessage.value = 'Sync failed (offline)'
    setTimeout(() => { lastSyncMessage.value = '' }, 3000)
  } finally {
    isSyncing.value = false
  }
}

async function loadCategories() {
  try {
    categories.value = await getCategories()
  } catch (e: any) {
    console.error(e)
  }
}

async function loadStats() {
  try {
    stats.value = await getUserStats()
  } catch (e: any) {
    console.error(e)
  }
}

async function loadWorkerStatus() {
  try {
    workerStatus.value = await getWorkerStatus()
  } catch (e: any) {
    console.error(e)
  }
}

async function ensureQuestionBuffer() {
  if (isBuffering.value) return
  if (questionQueue.value.length >= 4) return

  isBuffering.value = true
  try {
    const needed = 5 - questionQueue.value.length
    const excludeIds: number[] = []
    if (currentQuestion.value) excludeIds.push(currentQuestion.value.id)
    for (const q of questionQueue.value) {
      excludeIds.push(q.id)
    }

    const prefetched = await prefetchQuestions(
      selectedCategory.value,
      selectedDifficulty.value,
      needed,
      excludeIds
    )

    if (prefetched && prefetched.length > 0) {
      const existingIds = new Set([
        currentQuestion.value?.id,
        ...questionQueue.value.map(q => q.id)
      ])
      for (const q of prefetched) {
        if (!existingIds.has(q.id)) {
          questionQueue.value.push(q)
          existingIds.add(q.id)
        }
      }
    }
  } catch (e) {
    console.error('Buffer refill error:', e)
  } finally {
    isBuffering.value = false
  }
}

async function nextQuestion(forceRefresh: boolean = false) {
  answerResult.value = null
  selectedOptionIds.value = []

  // If filter changed or forced, invalidate current queue
  if (forceRefresh || lastBufferCategory !== selectedCategory.value || lastBufferDifficulty !== selectedDifficulty.value) {
    questionQueue.value = []
    lastBufferCategory = selectedCategory.value
    lastBufferDifficulty = selectedDifficulty.value
  }

  // Instant display from preloaded buffer (0ms transition!)
  if (questionQueue.value.length > 0) {
    currentQuestion.value = questionQueue.value.shift()!
    ensureQuestionBuffer().catch(() => {})
    return
  }

  // Buffer is empty (initial startup or category switch)
  loading.value = true
  errorMessage.value = ''

  try {
    currentQuestion.value = await getRandomQuestion(selectedCategory.value, selectedDifficulty.value)
    ensureQuestionBuffer().catch(() => {})
  } catch (e: any) {
    currentQuestion.value = null
    errorMessage.value = e.message || 'No questions available'
  } finally {
    loading.value = false
  }
}

function toggleOption(id: number) {
  if (answerResult.value) return
  const idx = selectedOptionIds.value.indexOf(id)
  if (idx > -1) {
    selectedOptionIds.value.splice(idx, 1)
  } else {
    selectedOptionIds.value.push(id)
  }
}

async function handleAnswer() {
  if (!currentQuestion.value || selectedOptionIds.value.length === 0 || submitting.value) return
  submitting.value = true

  try {
    // Pass currentQuestion.value for instant 0ms local validation!
    const res = await submitAnswer(
      currentQuestion.value.id, 
      selectedOptionIds.value, 
      currentQuestion.value
    )
    answerResult.value = res

    // 1. Live update category counters immediately (instant 0ms response)
    const targetCat = categories.value.find(c => 
      c.id === currentQuestion.value?.category_id || 
      c.slug === currentQuestion.value?.category_slug
    )
    if (targetCat && targetCat.available_questions > 0) {
      targetCat.available_questions--
      targetCat.solved_questions++
    }

    // 2. Live update global and category stats
    if (stats.value && currentQuestion.value) {
      stats.value.current_streak = res.streak
      stats.value.total_score = res.total_score
      stats.value.total_answered++
      if (res.is_correct) stats.value.total_correct++
      stats.value.accuracy_percent = Math.round((stats.value.total_correct / stats.value.total_answered) * 100)

      if (stats.value.by_category) {
        const catSlug = currentQuestion.value.category_slug
        const catStat = stats.value.by_category.find(cs => (cs.category_slug === catSlug || (cs as any).slug === catSlug))
        if (catStat) {
          catStat.total_answered++
          if (res.is_correct) catStat.total_correct++
          catStat.accuracy_percent = Math.round((catStat.total_correct / catStat.total_answered) * 100)
        }
      }
    }

    await refreshPendingCount()

    // 3. Sync full data from server/cache in background
    loadCategories().catch(() => {})
    getUserStats().then(s => { if (s) stats.value = s }).catch(() => {})
  } catch (e: any) {
    await showAlert('Failed to submit answer: ' + (e?.message || e), 'error', 'Submission Failed')
  } finally {
    submitting.value = false
  }
}

async function handleDelete() {
  if (!currentQuestion.value) return
  const confirmed = await showConfirm(
    'Are you sure you want to permanently delete this question from the database as invalid?',
    { title: 'Delete Question', danger: true, confirmText: 'Delete Question' }
  )
  if (!confirmed) return

  try {
    const targetCat = categories.value.find(c => 
      c.id === currentQuestion.value?.category_id || 
      c.slug === currentQuestion.value?.category_slug
    )
    if (targetCat) {
      if (targetCat.available_questions > 0) targetCat.available_questions--
      if (targetCat.total_questions > 0) targetCat.total_questions--
    }

    await deleteQuestion(currentQuestion.value.id)
    await nextQuestion()
    await loadCategories()
  } catch (e: any) {
    await showAlert('Failed to delete question: ' + (e?.message || e), 'error', 'Deletion Error')
  }
}

async function handleResetStats() {
  const confirmed = await showConfirm(
    'Are you sure you want to reset all your statistics and answered question history? This will clear answered questions and start fresh.',
    { title: 'Reset Statistics', danger: true, confirmText: 'Reset Stats' }
  )
  if (!confirmed) return

  try {
    if (isOnline.value) {
      await resetStats()
    }
    await resetOfflineStats().catch(() => {})
    stats.value = {
      total_answered: 0,
      total_correct: 0,
      accuracy_percent: 0,
      current_streak: 0,
      total_score: 0,
      by_category: []
    }
    await refreshPendingCount()
    await loadCategories()
    await nextQuestion()
    await showAlert('Your statistics and question history have been reset!', 'success', 'Stats Reset')
  } catch (e: any) {
    await showAlert('Failed to reset stats: ' + (e?.message || e), 'error', 'Reset Error')
  }
}

async function handleTriggerGeneration() {
  try {
    const res = await triggerWorker()
    await showAlert(res.message, 'info', 'AI Generation')
    await loadWorkerStatus()
  } catch (e: any) {
    await showAlert(e?.message || 'Failed to trigger worker', 'error', 'Generation Error')
  }
}

function openCreateCategoryModal() {
  categoryModalTab.value = 'create'
  showCategoryModal.value = true
}

function openEditCategoryModal(cat: Category) {
  selectedEditCategoryId.value = cat.id
  editingCat.value = {
    id: cat.id,
    name: cat.name,
    description: cat.description,
    prompt_instructions: cat.prompt_instructions
  }
  categoryModalTab.value = 'edit'
  showCategoryModal.value = true
}

function onEditCategorySelect() {
  const found = categories.value.find(c => c.id === selectedEditCategoryId.value)
  if (found) {
    editingCat.value = {
      id: found.id,
      name: found.name,
      description: found.description,
      prompt_instructions: found.prompt_instructions
    }
  }
}

function openEditCategoryForCurrentQuestion() {
  if (!currentQuestion.value) return
  const found = categories.value.find(c => c.id === currentQuestion.value?.category_id || c.slug === currentQuestion.value?.category_slug)
  if (found) {
    openEditCategoryModal(found)
  }
}

async function handleCreateCategory() {
  if (!newCat.value.name) return
  creatingCategory.value = true
  try {
    await createCategory(newCat.value)
    showCategoryModal.value = false
    newCat.value = { name: '', description: '', prompt_instructions: '' }
    await loadCategories()
    await loadWorkerStatus()
    await showAlert('Category created successfully! Background generator started.', 'success', 'Category Created')
  } catch (e: any) {
    await showAlert('Error: ' + (e?.message || e), 'error', 'Category Creation Failed')
  } finally {
    creatingCategory.value = false
  }
}

async function handleUpdateCategory() {
  if (!editingCat.value || !editingCat.value.id) return
  savingCategory.value = true
  try {
    const updated = await updateCategory(editingCat.value.id, {
      name: editingCat.value.name,
      description: editingCat.value.description,
      prompt_instructions: editingCat.value.prompt_instructions
    })

    const idx = categories.value.findIndex(c => c.id === updated.id)
    if (idx !== -1) {
      categories.value[idx] = { ...categories.value[idx], ...updated }
    }

    if (currentQuestion.value && (currentQuestion.value.category_id === updated.id || currentQuestion.value.category_slug === updated.slug)) {
      currentQuestion.value.category_name = updated.name
    }

    showCategoryModal.value = false
    await showAlert(`Category "${updated.name}" updated successfully!`, 'success', 'Category Updated')
  } catch (e: any) {
    await showAlert('Error updating category: ' + (e?.message || e), 'error', 'Update Failed')
  } finally {
    savingCategory.value = false
  }
}

function getOptionClass(optionId: number) {
  const isSelected = selectedOptionIds.value.includes(optionId)
  if (!answerResult.value) {
    return isSelected
      ? 'border-sky-500 bg-sky-950/40 text-sky-100 shadow-sm shadow-sky-500/20'
      : 'border-slate-800 bg-slate-900/60 text-slate-300 hover:border-slate-700 hover:bg-slate-900'
  }

  const isCorrect = answerResult.value.correct_option_ids.includes(optionId)

  if (isCorrect) {
    return 'border-emerald-500 bg-emerald-950/40 text-emerald-100 font-medium'
  }
  if (isSelected && !isCorrect) {
    return 'border-rose-500 bg-rose-950/40 text-rose-200'
  }
  return 'border-slate-800/60 bg-slate-900/20 text-slate-500 opacity-60'
}

function formatOptionText(text: string): string {
  if (!text) return ''
  let t = text

  // Unescape accidental literal \n if not inside code quotes
  if (t.includes('\\n') && !t.includes("'\\n'") && !t.includes('"\\n"')) {
    t = t.replace(/\\n/g, '\n')
  }

  // Strip redundant "Outputs: " / "Output: " prefix if present
  if (/^Outputs?:\s+/i.test(t)) {
    t = t.replace(/^Outputs?:\s+/i, '')
  }

  return t.trim()
}

function hasMultipleLines(text: string): boolean {
  if (!text) return false
  return formatOptionText(text).includes('\n')
}

const parsedQuestion = computed(() => {
  if (!currentQuestion.value) {
    return { text: '', code: '' }
  }

  let text = currentQuestion.value.question_text || ''
  let code = currentQuestion.value.code_snippet?.trim() || ''

  // 1. Detect markdown code block fence in question_text (```lang ... ```)
  const fenceRegex = /```(?:[a-zA-Z0-9_\-#+]*)?\n?([\s\S]*?)\n?```/
  const match = text.match(fenceRegex)
  if (match) {
    if (!code) {
      code = match[1].trim()
    }
    // Remove the fence from the question prompt
    text = (text.substring(0, match.index) + '\n' + text.substring((match.index || 0) + match[0].length)).trim()
  }

  // 2. Strip any leaked "Options:\nA. ..." from the question prompt
  const optIdx = text.search(/\n\s*Options:\s*\n/i)
  if (optIdx !== -1) {
    text = text.substring(0, optIdx).trim()
  }

  return { text, code }
})

const displayQuestionText = computed(() => parsedQuestion.value.text)
const displayCodeSnippet = computed(() => parsedQuestion.value.code)

const displayLanguage = computed(() => {
  if (!currentQuestion.value) return 'text'
  if (currentQuestion.value.language && currentQuestion.value.language.trim() !== '') {
    return currentQuestion.value.language.trim()
  }

  // Check if language was specified in the fence (e.g. ```powershell or ```bash)
  if (currentQuestion.value.question_text) {
    const fenceMatch = currentQuestion.value.question_text.match(/```([a-zA-Z0-9_\-#+]+)\n?/)
    if (fenceMatch && fenceMatch[1]) {
      return fenceMatch[1].trim()
    }
  }

  // Category-based fallback
  const cat = categories.value.find(c => c.id === currentQuestion.value?.category_id || c.slug === currentQuestion.value?.category_slug)
  const slug = cat?.slug || currentQuestion.value?.category_slug || ''
  switch (slug) {
    case 'cisco-networking': return 'text'
    case 'devops-linux': return 'bash'
    case 'powershell': return 'powershell'
    case 'sql': return 'sql'
    case 'golang': return 'go'
    case 'python': return 'python'
    case 'csharp': return 'csharp'
    case 'cpp': return 'cpp'
    case 'php': return 'php'
    case 'javascript': return 'javascript'
    case 'ai-neural-networks': return 'python'
    default: return 'text'
  }
})

function handleCategoryWheel(e: WheelEvent) {
  const container = e.currentTarget as HTMLElement
  if (container && Math.abs(e.deltaY) > Math.abs(e.deltaX)) {
    container.scrollLeft += e.deltaY
  }
}
</script>

<template>
  <div class="max-w-2xl mx-auto px-4 py-4 min-h-screen flex flex-col justify-between">
    <!-- Header -->
    <header class="mb-4">
      <div class="flex items-center justify-between pb-3 border-b border-slate-800">
        <div class="flex items-center gap-2">
          <div class="w-9 h-9 rounded-xl bg-gradient-to-tr from-sky-500 to-indigo-500 flex items-center justify-center shadow-lg shadow-sky-500/20 shrink-0">
            <svg class="w-5 h-5 text-white" fill="currentColor" viewBox="0 0 20 20">
              <path fill-rule="evenodd" d="M11.3 1.046A1 1 0 0112 2v5h4a1 1 0 01.82 1.573l-7 10A1 1 0 018 18v-5H4a1 1 0 01-.82-1.573l7-10a1 1 0 011.12-.38z" clip-rule="evenodd"/>
            </svg>
          </div>
          <div>
            <h1 class="text-lg font-bold tracking-tight text-white flex items-center gap-2">
              Tech Trainer
              <span class="text-[10px] font-medium bg-slate-800 text-sky-400 px-2 py-0.5 rounded-full border border-slate-700">PWA</span>
            </h1>
            <p class="text-[11px] text-slate-400">Personal AI Practice Platform for Engineers</p>
          </div>
        </div>

        <!-- Top Status Indicators -->
        <div class="flex items-center gap-2">
          <!-- Streak Badge -->
          <div class="flex items-center gap-1.5 bg-slate-900 border border-slate-800 px-2.5 py-1.5 rounded-xl text-xs font-semibold text-amber-400" title="Streak">
            <svg class="w-3.5 h-3.5 text-amber-400 shrink-0" fill="currentColor" viewBox="0 0 20 20">
              <path fill-rule="evenodd" d="M12.395 2.553a1 1 0 00-1.45-.385c-.345.23-.614.558-.822.88-.527.82-1.176 1.906-1.513 3.013a7.002 7.002 0 00-1.815 1.583C6.117 8.52 5 10.384 5 12.5 5 16.09 7.91 19 11.5 19s6.5-2.91 6.5-6.5c0-2.316-.948-4.22-2.368-5.748a12.87 12.87 0 00-2.737-2.199 1 1 0 00-.5-.1zM11.5 17c-2.485 0-4.5-2.015-4.5-4.5 0-1.52.793-2.937 1.62-3.834.33-.358.706-.693 1.096-.983.332 1.042.87 1.954 1.544 2.686A4.478 4.478 0 0011 12.5c0 1.243.504 2.37 1.318 3.182A4.475 4.475 0 0011.5 17z" clip-rule="evenodd"/>
            </svg>
            <span>{{ stats?.current_streak || 0 }}</span>
          </div>

          <!-- Total Score -->
          <div class="flex items-center gap-1.5 bg-slate-900 border border-slate-800 px-2.5 py-1.5 rounded-xl text-xs font-semibold text-sky-400" title="Score">
            <svg class="w-3.5 h-3.5 text-sky-400 shrink-0" fill="currentColor" viewBox="0 0 20 20">
              <path d="M9.049 2.927c.3-.921 1.603-.921 1.902 0l1.07 3.292a1 1 0 00.95.69h3.462c.969 0 1.371 1.24.588 1.81l-2.8 2.034a1 1 0 00-.364 1.118l1.07 3.292c.3.921-.755 1.688-1.54 1.118l-2.8-2.034a1 1 0 00-1.175 0l-2.8 2.034c-.784.57-1.838-.197-1.539-1.118l1.07-3.292a1 1 0 00-.364-1.118L2.98 8.72c-.783-.57-.38-1.81.588-1.81h3.461a1 1 0 00.951-.69l1.07-3.292z"/>
            </svg>
            <span>{{ stats?.total_score || 0 }}</span>
          </div>

          <!-- Stats Button -->
          <button 
            @click="showStatsModal = true"
            class="p-2 bg-slate-900 border border-slate-800 hover:border-slate-700 text-slate-300 rounded-xl text-xs transition"
            title="Statistics"
          >
            <svg class="w-3.5 h-3.5 text-slate-300" fill="currentColor" viewBox="0 0 20 20">
              <path d="M2 11a1 1 0 011-1h2a1 1 0 011 1v5a1 1 0 01-1 1H3a1 1 0 01-1-1v-5zM8 7a1 1 0 011-1h2a1 1 0 011 1v9a1 1 0 01-1 1H9a1 1 0 01-1-1V7zM14 4a1 1 0 011-1h2a1 1 0 011 1v12a1 1 0 01-1 1h-2a1 1 0 01-1-1V4z"/>
            </svg>
          </button>
        </div>
      </div>

      <!-- Network / Tailscale & Offline Sync Status Banner -->
      <div 
        class="mt-2.5 flex items-center justify-between text-xs px-3 py-1.5 rounded-lg border transition-colors"
        :class="pendingCount > 0 ? 'bg-amber-950/40 border-amber-800/60' : 'bg-slate-900/60 border-slate-800/80'"
      >
        <div class="flex items-center gap-2">
          <span 
            class="w-2 h-2 rounded-full inline-block"
            :class="pendingCount > 0 ? 'bg-amber-400 animate-pulse' : (isOnline ? 'bg-emerald-400' : 'bg-slate-500')"
          ></span>
          <span class="text-[11px] text-slate-300">
            <span v-if="pendingCount > 0" class="text-amber-300 font-semibold">
              Offline queue: {{ pendingCount }} answer{{ pendingCount > 1 ? 's' : '' }} pending sync
            </span>
            <span v-else-if="isOnline" class="text-slate-400 flex items-center gap-1.5">
              <span>Online</span>
              <span class="w-1 h-1 rounded-full bg-slate-600"></span>
              <span>Ready</span>
            </span>
            <span v-else class="text-slate-400">
              Tailscale Offline - Local Mode Active
            </span>
          </span>
        </div>

        <div class="flex items-center gap-2">
          <span v-if="lastSyncMessage" class="text-[10px] text-emerald-400 font-medium">
            {{ lastSyncMessage }}
          </span>
          <button 
            v-if="pendingCount > 0"
            @click="handleSync"
            :disabled="isSyncing"
            class="text-[11px] px-2 py-0.5 rounded bg-sky-500 hover:bg-sky-400 text-white font-medium flex items-center gap-1 cursor-pointer transition disabled:opacity-50"
          >
            <svg :class="{ 'animate-spin': isSyncing }" class="w-3 h-3" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15"/></svg>
            <span>{{ isSyncing ? 'Syncing...' : 'Sync Now' }}</span>
          </button>
        </div>
      </div>

      <!-- Category Filter Pills -->
      <div @wheel.passive="handleCategoryWheel" class="mt-3 flex items-center gap-2 overflow-x-auto pb-2.5 category-scrollbar text-xs">
        <button
          @click="selectedCategory = 'all'; nextQuestion()"
          class="whitespace-nowrap px-3 py-1.5 rounded-xl font-medium transition"
          :class="selectedCategory === 'all' ? 'bg-sky-500 text-white shadow-md shadow-sky-500/25' : 'bg-slate-900 border border-slate-800 text-slate-400 hover:text-slate-200'"
        >
          All Topics
        </button>

        <button
          v-for="cat in categories"
          :key="cat.id"
          @click="selectedCategory = cat.slug; nextQuestion()"
          class="whitespace-nowrap px-3 py-1.5 rounded-xl font-medium transition flex items-center gap-1.5"
          :class="selectedCategory === cat.slug ? 'bg-sky-500 text-white shadow-md shadow-sky-500/25' : 'bg-slate-900 border border-slate-800 text-slate-400 hover:text-slate-200'"
        >
          <span>{{ cat.name }}</span>
          <span 
            class="text-[10px] px-1.5 py-0.2 rounded-full font-bold"
            :class="cat.available_questions < 5 ? 'bg-rose-900/60 text-rose-300' : (selectedCategory === cat.slug ? 'bg-sky-700 text-white' : 'bg-slate-800 text-slate-300')"
          >
            {{ cat.available_questions }}
          </span>
          <span
            v-if="selectedCategory === cat.slug"
            @click.stop="openEditCategoryModal(cat)"
            class="ml-0.5 p-0.5 hover:bg-sky-600 rounded transition text-sky-100 hover:text-white"
            title="Edit category name & prompt"
          >
            <svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15.232 5.232l3.536 3.536m-2.036-5.036a2.5 2.5 0 113.536 3.536L6.5 21.036H3v-3.572L16.732 3.732z"/>
            </svg>
          </span>
        </button>

        <button
          @click="openCreateCategoryModal"
          class="whitespace-nowrap px-3 py-1.5 rounded-xl font-medium bg-slate-800/80 border border-dashed border-slate-700 text-sky-400 hover:border-sky-500 hover:text-sky-300 transition"
        >
          + Category
        </button>
      </div>
    </header>

    <!-- Main Workspace -->
    <main class="flex-1 flex flex-col justify-center">
      <!-- Loading State -->
      <div v-if="loading" class="text-center py-20">
        <div class="inline-block w-8 h-8 border-2 border-sky-400 border-t-transparent rounded-full animate-spin"></div>
        <p class="mt-3 text-sm text-slate-400">Fetching challenge from pool...</p>
      </div>

      <!-- Empty State -->
      <div v-else-if="errorMessage || !currentQuestion" class="p-6 text-center bg-slate-900/80 border border-slate-800 rounded-2xl">
        <svg class="w-10 h-10 text-slate-500 mx-auto mb-2" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5" d="M20 7l-8-4-8 4m16 0l-8 4m8-4v10l-8 4m0-10L4 7m8 4v10M4 7v10l8 4"/>
        </svg>
        <h3 class="text-base font-semibold text-slate-200">No Questions Available</h3>
        <p class="text-xs text-slate-400 mt-1 max-w-sm mx-auto">
          {{ errorMessage || 'There are currently no unanswered questions in this category.' }}
        </p>
        <div class="mt-4 flex justify-center gap-2">
          <button 
            @click="handleTriggerGeneration" 
            class="px-4 py-2 bg-sky-500 hover:bg-sky-400 text-white rounded-xl text-xs font-semibold shadow-lg shadow-sky-500/20"
          >
            Generate Now
          </button>
          <button 
            @click="selectedCategory = 'all'; nextQuestion()" 
            class="px-4 py-2 bg-slate-800 hover:bg-slate-700 text-slate-300 rounded-xl text-xs font-semibold"
          >
            Reset Filters
          </button>
        </div>
      </div>

      <!-- Active Quiz Card -->
      <div v-else class="bg-slate-900/90 border border-slate-800/90 rounded-2xl p-5 shadow-2xl backdrop-blur-sm">
        <!-- Metadata Row -->
        <div class="flex items-center justify-between gap-2 mb-3.5 text-[11px]">
          <div class="flex items-center gap-1.5 sm:gap-2 flex-wrap min-w-0">
            <span class="px-2.5 py-0.5 rounded-full font-semibold bg-sky-950 text-sky-400 border border-sky-800/50 flex items-center gap-1.5 shrink-0">
              <span class="truncate max-w-[150px] sm:max-w-none">{{ currentQuestion.category_name }}</span>
              <button 
                @click="openEditCategoryForCurrentQuestion" 
                class="hover:text-sky-200 transition p-0.5" 
                title="Edit category name & prompt"
              >
                <svg class="w-3 h-3" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15.232 5.232l3.536 3.536m-2.036-5.036a2.5 2.5 0 113.536 3.536L6.5 21.036H3v-3.572L16.732 3.732z"/>
                </svg>
              </button>
            </span>
            <span 
              class="px-2 py-0.5 rounded-full uppercase tracking-wider font-bold shrink-0"
              :class="currentQuestion.difficulty === 'middle' ? 'bg-amber-950/60 text-amber-300 border border-amber-800/50' : 'bg-emerald-950/60 text-emerald-300 border border-emerald-800/50'"
            >
              {{ currentQuestion.difficulty }}
            </span>
            <span v-if="currentQuestion.is_solved" class="px-2 py-0.5 rounded-full text-slate-400 bg-slate-800 border border-slate-700 text-[10px] shrink-0">
              Review Mode
            </span>
          </div>

          <span class="text-slate-400 font-medium text-right shrink-0 whitespace-nowrap bg-slate-800/60 border border-slate-700/60 px-2.5 py-0.5 rounded-full text-[11px]">
            <span v-if="currentQuestion.expected_correct_count === 1">
              <span class="hidden sm:inline">Single Choice </span>(Select 1)
            </span>
            <span v-else-if="currentQuestion.expected_correct_count && currentQuestion.expected_correct_count > 1">
              <span class="hidden sm:inline">Multiple Choice </span>(Choose {{ currentQuestion.expected_correct_count }})
            </span>
            <span v-else>1 - 4 Correct</span>
          </span>
        </div>

        <!-- Question Prompt -->
        <h2 class="text-base sm:text-lg font-medium text-slate-100 leading-snug whitespace-pre-line">
          {{ displayQuestionText }}
        </h2>

        <!-- Code Snippet -->
        <CodeBlock 
          v-if="displayCodeSnippet" 
          :code="displayCodeSnippet" 
          :language="displayLanguage" 
        />

        <!-- 4 Option Cards -->
        <div class="mt-4 space-y-2.5">
          <div
            v-for="opt in currentQuestion.options"
            :key="opt.id"
            @click="toggleOption(opt.id)"
            class="flex items-start gap-3 p-3.5 rounded-xl border transition-all cursor-pointer select-none text-sm"
            :class="getOptionClass(opt.id)"
          >
            <!-- Custom Checkbox Indicator -->
            <div 
              class="w-5 h-5 rounded-md border flex items-center justify-center shrink-0 mt-0.5 transition-colors"
              :class="selectedOptionIds.includes(opt.id) ? 'bg-sky-500 border-sky-400 text-white' : 'border-slate-700 bg-slate-800/50'"
            >
              <svg v-if="selectedOptionIds.includes(opt.id)" class="w-3.5 h-3.5" viewBox="0 0 20 20" fill="currentColor">
                <path fill-rule="evenodd" d="M16.707 5.293a1 1 0 010 1.414l-8 8a1 1 0 01-1.414 0l-4-4a1 1 0 011.414-1.414L8 12.586l7.293-7.293a1 1 0 011.414 0z" clip-rule="evenodd" />
              </svg>
            </div>
            <div 
              class="flex-1 font-normal leading-relaxed whitespace-pre-line"
              :class="{ 'font-mono text-xs leading-5': hasMultipleLines(opt.option_text) }"
            >
              {{ formatOptionText(opt.option_text) }}
            </div>
          </div>
        </div>

        <!-- Submit & Explanation Footer -->
        <div class="mt-5">
          <button
            v-if="!answerResult"
            @click="handleAnswer"
            :disabled="selectedOptionIds.length === 0 || submitting"
            class="w-full py-3.5 px-4 rounded-xl font-semibold text-sm transition-all duration-200 flex items-center justify-center gap-2 shadow-lg"
            :class="selectedOptionIds.length > 0 
              ? 'bg-sky-500 hover:bg-sky-400 text-white shadow-sky-500/25 active:scale-[0.99]' 
              : 'bg-slate-800 text-slate-500 cursor-not-allowed'"
          >
            <span v-if="submitting">Checking...</span>
            <span v-else>Submit Answer ({{ selectedOptionIds.length }})</span>
          </button>

          <!-- Results & Explanation Container -->
          <div v-else class="space-y-4 animate-fade-in">
            <div 
              class="p-4 rounded-xl border flex items-center justify-between"
              :class="answerResult.is_correct ? 'bg-emerald-950/40 border-emerald-500/60 text-emerald-200' : 'bg-rose-950/40 border-rose-500/60 text-rose-200'"
            >
              <div class="flex items-center gap-3">
                <svg v-if="answerResult.is_correct" class="w-6 h-6 text-emerald-400 shrink-0" fill="currentColor" viewBox="0 0 20 20">
                  <path fill-rule="evenodd" d="M10 18a8 8 0 100-16 8 8 0 000 16zm3.707-9.293a1 1 0 00-1.414-1.414L9 10.586 7.707 9.293a1 1 0 00-1.414 1.414l2 2a1 1 0 001.414 0l4-4z" clip-rule="evenodd"/>
                </svg>
                <svg v-else class="w-6 h-6 text-rose-400 shrink-0" fill="currentColor" viewBox="0 0 20 20">
                  <path fill-rule="evenodd" d="M10 18a8 8 0 100-16 8 8 0 000 16zM8.707 7.293a1 1 0 00-1.414 1.414L8.586 10l-1.293 1.293a1 1 0 101.414 1.414L10 11.414l1.293 1.293a1 1 0 001.414-1.414L11.414 10l1.293-1.293a1 1 0 00-1.414-1.414L10 8.586 8.707 7.293z" clip-rule="evenodd"/>
                </svg>
                <div>
                  <h4 class="font-bold text-sm">
                    {{ answerResult.is_correct ? 'Spot on! Perfect answer.' : 'Incorrect selection.' }}
                  </h4>
                  <p class="text-xs opacity-90">
                    {{ answerResult.is_correct ? '+10 score points awarded' : 'Review the breakdown below and keep moving forward' }}
                  </p>
                </div>
              </div>
              <span v-if="answerResult.is_offline" class="text-[10px] bg-amber-900/60 text-amber-200 px-2 py-0.5 rounded-full font-medium border border-amber-700/50">
                Queued Offline
              </span>
            </div>

            <!-- Deep AI Explanation -->
            <div class="p-4 bg-slate-950 border border-slate-800 rounded-xl text-xs leading-relaxed text-slate-300">
              <div class="flex items-center gap-1.5 text-sky-400 font-semibold mb-2">
                <svg class="w-4 h-4 text-sky-400 shrink-0" fill="currentColor" viewBox="0 0 20 20">
                  <path d="M11 3a1 1 0 10-2 0v1a1 1 0 102 0V3zM15.657 5.757a1 1 0 00-1.414-1.414l-.707.707a1 1 0 001.414 1.414l.707-.707zM18 10a1 1 0 01-1 1h-1a1 1 0 110-2h1a1 1 0 011 1zM5.05 6.464A1 1 0 106.464 5.05l-.707-.707a1 1 0 00-1.414 1.414l.707.707zM5 10a1 1 0 01-1 1H3a1 1 0 110-2h1a1 1 0 011 1zM8 16v-1h4v1a2 2 0 11-4 0zM12 14H8a4 4 0 01-.82-7.915 4.962 4.962 0 011.64-1.085 5 5 0 016.36 6.36A4.962 4.962 0 0112 14z"/>
                </svg>
                <span>AI Solution Breakdown:</span>
              </div>
              <div class="whitespace-pre-wrap font-sans text-slate-200">
                {{ answerResult.explanation }}
              </div>
            </div>

            <!-- Actions Row -->
            <div class="flex items-center gap-2">
              <button
                @click="nextQuestion"
                class="flex-1 py-3 px-4 bg-sky-500 hover:bg-sky-400 text-white font-semibold text-sm rounded-xl transition shadow-lg shadow-sky-500/20 active:scale-[0.99] flex items-center justify-center gap-2"
              >
                <span>Next Question</span>
                <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M14 5l7 7m0 0l-7 7m7-7H3"/></svg>
              </button>

              <button
                @click="handleDelete"
                class="p-3 bg-slate-800/80 hover:bg-rose-900/40 hover:border-rose-700/60 border border-slate-700 text-slate-400 hover:text-rose-300 rounded-xl transition"
                title="Report and delete question from database"
              >
                <svg class="w-4 h-4" fill="currentColor" viewBox="0 0 20 20">
                  <path fill-rule="evenodd" d="M9 2a1 1 0 00-.894.553L7.382 4H4a1 1 0 000 2v10a2 2 0 002 2h8a2 2 0 002-2V6a1 1 0 100-2h-3.382l-.724-1.447A1 1 0 0011 2H9zM7 8a1 1 0 012 0v6a1 1 0 11-2 0V8zm5-1a1 1 0 00-1 1v6a1 1 0 102 0V8a1 1 0 00-1-1z" clip-rule="evenodd"/>
                </svg>
              </button>
            </div>
          </div>
        </div>
      </div>
    </main>

    <!-- Footer -->
    <footer class="mt-6 text-center text-[11px] text-slate-500">
      Self-hosted on <code class="text-slate-400">petroprog</code> <span class="mx-1.5 opacity-40">&bull;</span> Tailscale Mesh <span class="mx-1.5 opacity-40">&bull;</span> Powered by Qwen 2.5
    </footer>

    <!-- Category Modal (Create / Edit) -->
    <Teleport to="body">
      <Transition
        enter-active-class="transition duration-200 ease-out"
        enter-from-class="opacity-0"
        enter-to-class="opacity-100"
        leave-active-class="transition duration-150 ease-in"
        leave-from-class="opacity-100"
        leave-to-class="opacity-0"
      >
        <div v-if="showCategoryModal" class="fixed inset-0 z-50 bg-black/80 backdrop-blur-md flex items-center justify-center p-4" @click.self="showCategoryModal = false">
          <Transition
            enter-active-class="transition duration-200 ease-out"
            enter-from-class="opacity-0 scale-95 translate-y-2"
            enter-to-class="opacity-100 scale-100 translate-y-0"
            leave-active-class="transition duration-150 ease-in"
            leave-from-class="opacity-100 scale-100 translate-y-0"
            leave-to-class="opacity-0 scale-95 translate-y-2"
          >
            <div v-if="showCategoryModal" class="relative bg-slate-900/95 border border-slate-700/80 rounded-2xl p-6 max-w-md w-full shadow-2xl shadow-black/90 overflow-hidden">
              <!-- Top Glow Accent -->
              <div class="absolute -top-14 left-1/2 -translate-x-1/2 w-48 h-32 bg-sky-500/15 rounded-full blur-3xl pointer-events-none"></div>
        <!-- Modal Tabs Header -->
        <div class="flex items-center justify-between border-b border-slate-800 pb-3 mb-4">
          <div class="flex items-center gap-2">
            <button 
              type="button" 
              @click="categoryModalTab = 'create'"
              class="px-3 py-1.5 rounded-xl text-xs font-semibold transition"
              :class="categoryModalTab === 'create' ? 'bg-sky-500 text-white shadow-md shadow-sky-500/20' : 'bg-slate-800 text-slate-400 hover:text-slate-200'"
            >
              + New Category
            </button>
            <button 
              type="button" 
              @click="categoryModalTab = 'edit'; if (!editingCat.id && categories.length) openEditCategoryModal(categories[0])"
              class="px-3 py-1.5 rounded-xl text-xs font-semibold transition flex items-center gap-1.5"
              :class="categoryModalTab === 'edit' ? 'bg-sky-500 text-white shadow-md shadow-sky-500/20' : 'bg-slate-800 text-slate-400 hover:text-slate-200'"
            >
              <svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15.232 5.232l3.536 3.536m-2.036-5.036a2.5 2.5 0 113.536 3.536L6.5 21.036H3v-3.572L16.732 3.732z"/>
              </svg>
              <span>Edit Category</span>
            </button>
          </div>

          <button @click="showCategoryModal = false" class="text-slate-400 hover:text-white">
            <svg class="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12"/>
            </svg>
          </button>
        </div>

        <!-- Create Form -->
        <div v-if="categoryModalTab === 'create'">
          <h3 class="text-base font-bold text-slate-100 flex items-center gap-2">
            <svg class="w-4 h-4 text-sky-400" fill="currentColor" viewBox="0 0 20 20">
              <path d="M2 6a2 2 0 012-2h5l2 2h5a2 2 0 012 2v6a2 2 0 01-2 2H4a2 2 0 01-2-2V6z"/>
            </svg>
            <span>New Question Category</span>
          </h3>
          <p class="text-xs text-slate-400 mt-1">
            Define a technical domain. The local AI worker will begin generating practical challenges for it.
          </p>

          <form @submit.prevent="handleCreateCategory" class="mt-4 space-y-3">
            <div>
              <label class="block text-xs font-medium text-slate-300 mb-1">Category Title</label>
              <input 
                v-model="newCat.name" 
                placeholder="e.g. Cybersecurity, Rust Systems, Kubernetes" 
                required
                class="w-full bg-slate-950 border border-slate-800 rounded-xl px-3 py-2 text-xs text-white focus:outline-none focus:border-sky-500"
              />
            </div>

            <div>
              <label class="block text-xs font-medium text-slate-300 mb-1">Short Description</label>
              <input 
                v-model="newCat.description" 
                placeholder="Network security, TLS handshake, exploits, privilege escalation..." 
                required
                class="w-full bg-slate-950 border border-slate-800 rounded-xl px-3 py-2 text-xs text-white focus:outline-none focus:border-sky-500"
              />
            </div>

            <div>
              <label class="block text-xs font-medium text-slate-300 mb-1">AI Prompt Instructions</label>
              <textarea 
                v-model="newCat.prompt_instructions" 
                rows="3"
                placeholder="Topics: SQL injection, pcap analysis, auth bypass. Stack: Python, Bash. Level: Junior-Middle." 
                required
                class="w-full bg-slate-950 border border-slate-800 rounded-xl px-3 py-2 text-xs text-white focus:outline-none focus:border-sky-500 leading-relaxed"
              ></textarea>
            </div>

            <div class="flex items-center justify-end gap-2 pt-2">
              <button 
                type="button" 
                @click="showCategoryModal = false"
                class="px-4 py-2 bg-slate-800 hover:bg-slate-700 text-slate-300 rounded-xl text-xs font-medium"
              >
                Cancel
              </button>
              <button 
                type="submit" 
                :disabled="creatingCategory"
                class="px-4 py-2 bg-sky-500 hover:bg-sky-400 text-white rounded-xl text-xs font-semibold shadow-md shadow-sky-500/20"
              >
                {{ creatingCategory ? 'Creating...' : 'Create Category' }}
              </button>
            </div>
          </form>
        </div>

        <!-- Edit Form -->
        <div v-else-if="categoryModalTab === 'edit'">
          <h3 class="text-base font-bold text-slate-100 flex items-center gap-2">
            <svg class="w-4 h-4 text-sky-400" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15.232 5.232l3.536 3.536m-2.036-5.036a2.5 2.5 0 113.536 3.536L6.5 21.036H3v-3.572L16.732 3.732z"/>
            </svg>
            <span>Edit Category &amp; Prompts</span>
          </h3>
          <p class="text-xs text-slate-400 mt-1">
            Update category title or customize the generation prompt used by the AI model.
          </p>

          <form @submit.prevent="handleUpdateCategory" class="mt-4 space-y-3">
            <div>
              <label class="block text-xs font-medium text-slate-300 mb-1">Select Category to Edit</label>
              <select 
                v-model="selectedEditCategoryId"
                @change="onEditCategorySelect"
                class="w-full bg-slate-950 border border-slate-800 rounded-xl px-3 py-2 text-xs text-white focus:outline-none focus:border-sky-500"
              >
                <option v-for="cat in categories" :key="cat.id" :value="cat.id">
                  {{ cat.name }} ({{ cat.available_questions }} questions)
                </option>
              </select>
            </div>

            <div>
              <label class="block text-xs font-medium text-slate-300 mb-1">Category Title</label>
              <input 
                v-model="editingCat.name" 
                required
                class="w-full bg-slate-950 border border-slate-800 rounded-xl px-3 py-2 text-xs text-white focus:outline-none focus:border-sky-500"
              />
            </div>

            <div>
              <label class="block text-xs font-medium text-slate-300 mb-1">Short Description</label>
              <input 
                v-model="editingCat.description" 
                class="w-full bg-slate-950 border border-slate-800 rounded-xl px-3 py-2 text-xs text-white focus:outline-none focus:border-sky-500"
              />
            </div>

            <div>
              <label class="block text-xs font-medium text-slate-300 mb-1">AI Prompt Instructions</label>
              <textarea 
                v-model="editingCat.prompt_instructions" 
                rows="4"
                required
                class="w-full bg-slate-950 border border-slate-800 rounded-xl px-3 py-2 text-xs text-white focus:outline-none focus:border-sky-500 leading-relaxed font-mono"
              ></textarea>
              <p class="text-[11px] text-slate-500 mt-1">
                Updated instructions will guide Ollama when generating future questions for this category.
              </p>
            </div>

            <div class="flex items-center justify-end gap-2 pt-2">
              <button 
                type="button" 
                @click="showCategoryModal = false"
                class="px-4 py-2 bg-slate-800 hover:bg-slate-700 text-slate-300 rounded-xl text-xs font-medium"
              >
                Cancel
              </button>
              <button 
                type="submit" 
                :disabled="savingCategory"
                class="px-4 py-2 bg-sky-500 hover:bg-sky-400 text-white rounded-xl text-xs font-semibold shadow-md shadow-sky-500/20"
              >
                {{ savingCategory ? 'Saving...' : 'Save Changes' }}
              </button>
            </div>
          </form>
        </div>
      </div>
          </Transition>
        </div>
      </Transition>
    </Teleport>

    <!-- Statistics Modal -->
    <Teleport to="body">
      <Transition
        enter-active-class="transition duration-200 ease-out"
        enter-from-class="opacity-0"
        enter-to-class="opacity-100"
        leave-active-class="transition duration-150 ease-in"
        leave-from-class="opacity-100"
        leave-to-class="opacity-0"
      >
        <div v-if="showStatsModal" class="fixed inset-0 z-50 bg-black/80 backdrop-blur-md flex items-center justify-center p-4" @click.self="showStatsModal = false">
          <Transition
            enter-active-class="transition duration-200 ease-out"
            enter-from-class="opacity-0 scale-95 translate-y-2"
            enter-to-class="opacity-100 scale-100 translate-y-0"
            leave-active-class="transition duration-150 ease-in"
            leave-from-class="opacity-100 scale-100 translate-y-0"
            leave-to-class="opacity-0 scale-95 translate-y-2"
          >
            <div v-if="showStatsModal" class="relative bg-slate-900/95 border border-slate-700/80 rounded-2xl p-6 sm:p-7 max-w-xl w-full shadow-2xl shadow-black/90 overflow-hidden">
              <!-- Top Glow Accent -->
              <div class="absolute -top-14 left-1/2 -translate-x-1/2 w-64 h-32 bg-sky-500/15 rounded-full blur-3xl pointer-events-none"></div>

              <div class="flex items-center justify-between pb-3.5 border-b border-slate-800/80">
                <h3 class="text-base font-bold text-slate-100 flex items-center gap-2">
                  <div class="p-1.5 rounded-lg bg-sky-500/10 border border-sky-500/20 text-sky-400">
                    <svg class="w-4 h-4" fill="currentColor" viewBox="0 0 20 20">
                      <path d="M2 11a1 1 0 011-1h2a1 1 0 011 1v5a1 1 0 01-1 1H3a1 1 0 01-1-1v-5zM8 7a1 1 0 011-1h2a1 1 0 011 1v9a1 1 0 01-1 1H9a1 1 0 01-1-1V7zM14 4a1 1 0 011-1h2a1 1 0 011 1v12a1 1 0 01-1 1h-2a1 1 0 01-1-1V4z"/>
                    </svg>
                  </div>
                  <span>Your Performance</span>
                </h3>
                <button 
                  @click="showStatsModal = false" 
                  class="text-slate-400 hover:text-white p-1 rounded-lg hover:bg-slate-800 transition"
                >
                  <svg class="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12"/></svg>
                </button>
              </div>

              <!-- Stat Cards Top Row -->
              <div class="mt-4 grid grid-cols-3 gap-2.5 text-center">
                <div class="p-3.5 bg-slate-950/80 border border-slate-800/90 rounded-xl relative overflow-hidden group">
                  <div class="text-2xl font-extrabold text-sky-400">{{ stats?.total_answered || 0 }}</div>
                  <div class="text-[11px] font-medium text-slate-400 mt-0.5">Answered</div>
                </div>
                <div class="p-3.5 bg-slate-950/80 border border-slate-800/90 rounded-xl relative overflow-hidden group">
                  <div class="text-2xl font-extrabold text-emerald-400">{{ Math.round(stats?.accuracy_percent || 0) }}%</div>
                  <div class="text-[11px] font-medium text-slate-400 mt-0.5">Accuracy</div>
                </div>
                <div class="p-3.5 bg-slate-950/80 border border-slate-800/90 rounded-xl relative overflow-hidden group">
                  <div class="text-2xl font-extrabold text-amber-400">{{ stats?.current_streak || 0 }}</div>
                  <div class="text-[11px] font-medium text-slate-400 mt-0.5">Streak</div>
                </div>
              </div>

              <!-- Category Breakdown -->
              <div class="mt-5">
                <div class="flex items-center justify-between mb-2.5">
                  <h4 class="text-xs font-semibold uppercase tracking-wider text-slate-400">By Category</h4>
                  <span class="text-[11px] text-slate-500 font-medium">{{ stats?.by_category?.length || 0 }} categories</span>
                </div>
                <div class="space-y-2 max-h-72 sm:max-h-80 overflow-y-auto pr-1.5 custom-scrollbar">
                  <div 
                    v-for="cs in stats?.by_category" 
                    :key="cs.category_slug"
                    class="p-3 bg-slate-950/70 hover:bg-slate-950 border border-slate-800/80 hover:border-slate-700/80 rounded-xl flex items-center justify-between text-xs transition"
                  >
                    <div class="min-w-0 pr-3">
                      <div class="font-semibold text-slate-200 truncate">{{ cs.category_name }}</div>
                      <div class="text-[11px] text-slate-400 mt-0.5">Solved: {{ cs.total_correct }} of {{ cs.total_answered }}</div>
                    </div>
                    <div class="text-right shrink-0">
                      <div class="font-bold text-sm" :class="cs.accuracy_percent >= 75 ? 'text-emerald-400' : (cs.accuracy_percent >= 50 ? 'text-sky-400' : 'text-amber-400')">
                        {{ Math.round(cs.accuracy_percent) }}%
                      </div>
                      <div class="w-16 bg-slate-800 rounded-full h-1 mt-1 overflow-hidden">
                        <div 
                          class="h-full rounded-full transition-all duration-300"
                          :class="cs.accuracy_percent >= 75 ? 'bg-emerald-500' : (cs.accuracy_percent >= 50 ? 'bg-sky-500' : 'bg-amber-500')"
                          :style="{ width: `${Math.min(100, Math.max(0, cs.accuracy_percent))}%` }"
                        ></div>
                      </div>
                    </div>
                  </div>
                  <div v-if="!stats?.by_category?.length" class="text-center text-xs text-slate-500 py-6">
                    You haven't answered any questions yet
                  </div>
                </div>
              </div>

              <!-- Footer Buttons -->
              <div class="mt-6 flex items-center gap-2.5 pt-2 border-t border-slate-800/60">
                <button 
                  type="button"
                  @click="handleResetStats"
                  class="px-4 py-2.5 bg-rose-950/40 hover:bg-rose-900/60 text-rose-300 border border-rose-800/60 rounded-xl text-xs font-semibold transition active:scale-[0.98] flex items-center justify-center gap-1.5 cursor-pointer"
                  title="Reset all performance statistics"
                >
                  <svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                    <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" />
                  </svg>
                  <span>Reset Stats</span>
                </button>
                <button 
                  type="button"
                  @click="showStatsModal = false"
                  class="flex-1 py-2.5 bg-slate-800 hover:bg-slate-700 text-slate-100 border border-slate-700 rounded-xl text-xs font-semibold transition active:scale-[0.98] cursor-pointer shadow-sm"
                >
                  Close
                </button>
              </div>
            </div>
          </Transition>
        </div>
      </Transition>
    </Teleport>

    <!-- Global Styled Popup / Dialog Modal -->
    <ModalDialog
      :show="dialog.show"
      :title="dialog.title"
      :message="dialog.message"
      :type="dialog.type"
      :confirm-text="dialog.confirmText"
      :cancel-text="dialog.cancelText"
      :danger="dialog.danger"
      @confirm="onDialogConfirm"
      @cancel="onDialogCancel"
    />
  </div>
</template>

<style scoped>
/* Custom Vertical Scrollbar */
.custom-scrollbar {
  -webkit-overflow-scrolling: touch;
  scrollbar-width: thin;
  scrollbar-color: #334155 #0f172a;
}
.custom-scrollbar::-webkit-scrollbar {
  width: 6px;
}
.custom-scrollbar::-webkit-scrollbar-track {
  background: rgba(15, 23, 42, 0.6);
  border-radius: 9999px;
  margin: 3px 0;
}
.custom-scrollbar::-webkit-scrollbar-thumb {
  background: #334155;
  border-radius: 9999px;
  border: 1px solid rgba(255, 255, 255, 0.05);
  transition: background 0.2s ease;
}
.custom-scrollbar::-webkit-scrollbar-thumb:hover {
  background: #0ea5e9;
}

.category-scrollbar {
  -webkit-overflow-scrolling: touch;
  scrollbar-width: thin;
  scrollbar-color: #0ea5e9 #1e293b;
}
.category-scrollbar::-webkit-scrollbar {
  height: 6px;
}
.category-scrollbar::-webkit-scrollbar-track {
  background: #1e293b;
  border-radius: 9999px;
}
.category-scrollbar::-webkit-scrollbar-thumb {
  background: #0ea5e9;
  border-radius: 9999px;
}
.category-scrollbar::-webkit-scrollbar-thumb:hover {
  background: #38bdf8;
}
@keyframes fadeIn {
  from { opacity: 0; transform: translateY(6px); }
  to { opacity: 1; transform: translateY(0); }
}
.animate-fade-in {
  animation: fadeIn 0.25s ease-out forwards;
}
</style>
