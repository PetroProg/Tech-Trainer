<script setup lang="ts">
import { ref, onMounted, computed } from 'vue'
import CodeBlock from './components/CodeBlock.vue'
import {
  getCategories,
  createCategory,
  getRandomQuestion,
  submitAnswer,
  deleteQuestion,
  getUserStats,
  getWorkerStatus,
  triggerWorker,
  type Category,
  type Question,
  type SubmitAnswerResponse,
  type UserStats,
  type WorkerStatus
} from './api'

// Состояние
const categories = ref<Category[]>([])
const selectedCategory = ref<string>('all')
const selectedDifficulty = ref<string>('all')
const currentQuestion = ref<Question | null>(null)
const selectedOptionIds = ref<number[]>([])
const answerResult = ref<SubmitAnswerResponse | null>(null)
const loading = ref(false)
const submitting = ref(false)
const errorMessage = ref('')

// Статистика и воркер
const stats = ref<UserStats | null>(null)
const workerStatus = ref<WorkerStatus | null>(null)
const showStatsModal = ref(false)
const showCategoryModal = ref(false)

// Форма создания категории
const newCat = ref({
  name: '',
  description: '',
  prompt_instructions: ''
})
const creatingCategory = ref(false)

// Загрузка первоначальных данных
onMounted(async () => {
  await Promise.all([loadCategories(), loadStats(), loadWorkerStatus()])
  await nextQuestion()
  // Периодическое обновление статуса воркера раз в 15 секунд
  setInterval(loadWorkerStatus, 15000)
})

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

async function nextQuestion() {
  loading.value = true
  errorMessage.value = ''
  answerResult.value = null
  selectedOptionIds.value = []

  try {
    currentQuestion.value = await getRandomQuestion(selectedCategory.value, selectedDifficulty.value)
  } catch (e: any) {
    currentQuestion.value = null
    errorMessage.value = e.message || 'Нет доступных задач'
  } finally {
    loading.value = false
  }
}

function toggleOption(id: number) {
  if (answerResult.value) return // После отправки ответа кликать нельзя
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
    const res = await submitAnswer(currentQuestion.value.id, selectedOptionIds.value)
    answerResult.value = res
    if (stats.value) {
      stats.value.current_streak = res.streak
      stats.value.total_score = res.total_score
      stats.value.total_answered++
      if (res.is_correct) stats.value.total_correct++
      stats.value.accuracy_percent = Math.round((stats.value.total_correct / stats.value.total_answered) * 100)
    }
  } catch (e: any) {
    alert('Ошибка отправки ответа: ' + e.message)
  } finally {
    submitting.value = false
  }
}

async function handleDelete() {
  if (!currentQuestion.value) return
  if (!confirm('Удалить эту задачу из базы данных как некорректную?')) return

  try {
    await deleteQuestion(currentQuestion.value.id)
    await nextQuestion()
    await loadCategories()
  } catch (e: any) {
    alert('Ошибка удаления: ' + e.message)
  }
}

async function handleTriggerGeneration() {
  try {
    const res = await triggerWorker()
    alert(res.message)
    await loadWorkerStatus()
  } catch (e: any) {
    alert(e.message)
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
    alert('Категория успешно создана! Воркер запущен для генерации задач.')
  } catch (e: any) {
    alert('Ошибка: ' + e.message)
  } finally {
    creatingCategory.value = false
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
</script>

<template>
  <div class="max-w-2xl mx-auto px-4 py-4 min-h-screen flex flex-col justify-between">
    <!-- Шапка приложения -->
    <header class="mb-4">
      <div class="flex items-center justify-between pb-3 border-b border-slate-800">
        <div class="flex items-center gap-2">
          <div class="w-9 h-9 rounded-xl bg-gradient-to-tr from-sky-500 to-indigo-500 flex items-center justify-center shadow-lg shadow-sky-500/20">
            <span class="text-xl">⚡</span>
          </div>
          <div>
            <h1 class="text-lg font-bold tracking-tight text-white flex items-center gap-2">
              Tech Trainer
              <span class="text-[10px] font-medium bg-slate-800 text-sky-400 px-2 py-0.5 rounded-full border border-slate-700">PWA</span>
            </h1>
            <p class="text-[11px] text-slate-400">Локальный AI тренажёр для разработчиков</p>
          </div>
        </div>

        <!-- Трекер и кнопки -->
        <div class="flex items-center gap-2">
          <!-- Стрик -->
          <div class="flex items-center gap-1 bg-slate-900 border border-slate-800 px-2.5 py-1.5 rounded-xl text-xs font-semibold text-amber-400">
            <span>🔥</span>
            <span>{{ stats?.current_streak || 0 }}</span>
          </div>

          <!-- Очки -->
          <div class="flex items-center gap-1 bg-slate-900 border border-slate-800 px-2.5 py-1.5 rounded-xl text-xs font-semibold text-sky-400">
            <span>⭐</span>
            <span>{{ stats?.total_score || 0 }}</span>
          </div>

          <!-- Кнопка статистики -->
          <button 
            @click="showStatsModal = true"
            class="p-2 bg-slate-900 border border-slate-800 hover:border-slate-700 text-slate-300 rounded-xl text-xs transition"
            title="Статистика"
          >
            📊
          </button>
        </div>
      </div>

      <!-- Статус Ollama Worker -->
      <div class="mt-2.5 flex items-center justify-between text-xs px-3 py-1.5 rounded-lg bg-slate-900/60 border border-slate-800/80">
        <div class="flex items-center gap-2">
          <span 
            class="w-2 h-2 rounded-full inline-block animate-pulse"
            :class="workerStatus?.is_running ? 'bg-amber-400' : (workerStatus?.is_online ? 'bg-emerald-400' : 'bg-rose-500')"
          ></span>
          <span class="text-slate-400 text-[11px]">
            Ollama RTX 3060:
            <b class="text-slate-200">
              {{ workerStatus?.is_running ? 'Генерация...' : (workerStatus?.is_online ? 'Онлайн' : 'Офлайн (спит)') }}
            </b>
          </span>
        </div>
        <button 
          v-if="workerStatus?.is_online && !workerStatus?.is_running"
          @click="handleTriggerGeneration"
          class="text-[11px] text-sky-400 hover:text-sky-300 font-medium flex items-center gap-1"
        >
          <span>↻ Сгенерировать пачку</span>
        </button>
      </div>

      <!-- Горизонтальный скролл категорий -->
      <div class="mt-3 flex items-center gap-2 overflow-x-auto pb-1 no-scrollbar text-xs">
        <button
          @click="selectedCategory = 'all'; nextQuestion()"
          class="whitespace-nowrap px-3 py-1.5 rounded-xl font-medium transition"
          :class="selectedCategory === 'all' ? 'bg-sky-500 text-white shadow-md shadow-sky-500/25' : 'bg-slate-900 border border-slate-800 text-slate-400 hover:text-slate-200'"
        >
          Все темы
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
            :class="cat.available_questions < 5 ? 'bg-rose-900/60 text-rose-300' : 'bg-slate-800 text-slate-300'"
          >
            {{ cat.available_questions }}
          </span>
        </button>

        <button
          @click="showCategoryModal = true"
          class="whitespace-nowrap px-3 py-1.5 rounded-xl font-medium bg-slate-800/80 border border-dashed border-slate-700 text-sky-400 hover:border-sky-500 hover:text-sky-300 transition"
        >
          + Категория
        </button>
      </div>
    </header>

    <!-- Основное тело тренажёра -->
    <main class="flex-1 flex flex-col justify-center">
      <!-- Загрузка -->
      <div v-if="loading" class="text-center py-20">
        <div class="inline-block w-8 h-8 border-2 border-sky-400 border-t-transparent rounded-full animate-spin"></div>
        <p class="mt-3 text-sm text-slate-400">Подбор задачи из пула...</p>
      </div>

      <!-- Ошибка / Пустой пул -->
      <div v-else-if="errorMessage || !currentQuestion" class="p-6 text-center bg-slate-900/80 border border-slate-800 rounded-2xl">
        <div class="text-4xl mb-2">📦</div>
        <h3 class="text-base font-semibold text-slate-200">Нет доступных задач</h3>
        <p class="text-xs text-slate-400 mt-1 max-w-sm mx-auto">
          {{ errorMessage || 'В базе данных пока нет нерешённых задач по выбранным фильтрам.' }}
        </p>
        <div class="mt-4 flex justify-center gap-2">
          <button 
            @click="handleTriggerGeneration" 
            class="px-4 py-2 bg-sky-500 hover:bg-sky-400 text-white rounded-xl text-xs font-semibold shadow-lg shadow-sky-500/20"
          >
            Сгенерировать сейчас
          </button>
          <button 
            @click="selectedCategory = 'all'; nextQuestion()" 
            class="px-4 py-2 bg-slate-800 hover:bg-slate-700 text-slate-300 rounded-xl text-xs font-semibold"
          >
            Сбросить фильтр
          </button>
        </div>
      </div>

      <!-- Карточка задачи -->
      <div v-else class="bg-slate-900/90 border border-slate-800/90 rounded-2xl p-5 shadow-2xl backdrop-blur-sm">
        <!-- Метаданные вопроса -->
        <div class="flex items-center justify-between mb-3 text-[11px]">
          <div class="flex items-center gap-2">
            <span class="px-2.5 py-0.5 rounded-full font-semibold bg-sky-950 text-sky-400 border border-sky-800/50">
              {{ currentQuestion.category_name }}
            </span>
            <span 
              class="px-2 py-0.5 rounded-full uppercase tracking-wider font-bold"
              :class="currentQuestion.difficulty === 'middle' ? 'bg-amber-950/60 text-amber-300 border border-amber-800/50' : 'bg-emerald-950/60 text-emerald-300 border border-emerald-800/50'"
            >
              {{ currentQuestion.difficulty }}
            </span>
          </div>

          <span class="text-slate-400 font-medium">
            Правильных: 1–4
          </span>
        </div>

        <!-- Текст вопроса -->
        <h2 class="text-base sm:text-lg font-medium text-slate-100 leading-snug">
          {{ currentQuestion.question_text }}
        </h2>

        <!-- Фрагмент кода (если есть) -->
        <CodeBlock 
          v-if="currentQuestion.code_snippet" 
          :code="currentQuestion.code_snippet" 
          :language="currentQuestion.language" 
        />

        <!-- Варианты ответа (ровно 4) -->
        <div class="mt-4 space-y-2.5">
          <div
            v-for="opt in currentQuestion.options"
            :key="opt.id"
            @click="toggleOption(opt.id)"
            class="flex items-start gap-3 p-3.5 rounded-xl border transition-all cursor-pointer select-none text-sm"
            :class="getOptionClass(opt.id)"
          >
            <!-- Индикатор выбора / чекбокс -->
            <div 
              class="w-5 h-5 rounded-md border flex items-center justify-center shrink-0 mt-0.5 transition-colors"
              :class="selectedOptionIds.includes(opt.id) ? 'bg-sky-500 border-sky-400 text-white' : 'border-slate-700 bg-slate-800/50'"
            >
              <svg v-if="selectedOptionIds.includes(opt.id)" class="w-3.5 h-3.5" viewBox="0 0 20 20" fill="currentColor">
                <path fill-rule="evenodd" d="M16.707 5.293a1 1 0 010 1.414l-8 8a1 1 0 01-1.414 0l-4-4a1 1 0 011.414-1.414L8 12.586l7.293-7.293a1 1 0 011.414 0z" clip-rule="evenodd" />
              </svg>
            </div>
            <div class="flex-1 font-normal leading-relaxed">
              {{ opt.option_text }}
            </div>
          </div>
        </div>

        <!-- Кнопка проверки / Разбор -->
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
            <span v-if="submitting">Проверка...</span>
            <span v-else>Ответить ({{ selectedOptionIds.length }})</span>
          </button>

          <!-- Блок результатов и объяснения -->
          <div v-else class="space-y-4 animate-fade-in">
            <div 
              class="p-4 rounded-xl border flex items-center gap-3"
              :class="answerResult.is_correct ? 'bg-emerald-950/40 border-emerald-500/60 text-emerald-200' : 'bg-rose-950/40 border-rose-500/60 text-rose-200'"
            >
              <span class="text-2xl">{{ answerResult.is_correct ? '🎉' : '❌' }}</span>
              <div>
                <h4 class="font-bold text-sm">
                  {{ answerResult.is_correct ? 'Абсолютно верно!' : 'Есть ошибки в ответе' }}
                </h4>
                <p class="text-xs opacity-90">
                  {{ answerResult.is_correct ? '+10 очков в копилку' : 'Разберите объяснение ниже и двигайтесь дальше' }}
                </p>
              </div>
            </div>

            <!-- Подробный AI разбор -->
            <div class="p-4 bg-slate-950 border border-slate-800 rounded-xl text-xs leading-relaxed text-slate-300">
              <div class="flex items-center gap-1.5 text-sky-400 font-semibold mb-2">
                <span>💡</span>
                <span>Разбор решения (AI Explanation):</span>
              </div>
              <div class="whitespace-pre-wrap font-sans text-slate-200">
                {{ answerResult.explanation }}
              </div>
            </div>

            <!-- Действия: Следующая задача + Удалить -->
            <div class="flex items-center gap-2">
              <button
                @click="nextQuestion"
                class="flex-1 py-3 px-4 bg-sky-500 hover:bg-sky-400 text-white font-semibold text-sm rounded-xl transition shadow-lg shadow-sky-500/20 active:scale-[0.99] flex items-center justify-center gap-2"
              >
                <span>Следующая задача</span>
                <span>→</span>
              </button>

              <button
                @click="handleDelete"
                class="p-3 bg-slate-800/80 hover:bg-rose-900/40 hover:border-rose-700/60 border border-slate-700 text-slate-400 hover:text-rose-300 rounded-xl transition"
                title="Пожаловаться и удалить задачу из базы"
              >
                🗑️
              </button>
            </div>
          </div>
        </div>
      </div>
    </main>

    <!-- Футер -->
    <footer class="mt-6 text-center text-[11px] text-slate-500">
      Self-hosted on <code class="text-slate-400">petroprog</code> • Tailscale Mesh • Powered by Qwen 2.5
    </footer>

    <!-- Модальное окно создания новой категории -->
    <div v-if="showCategoryModal" class="fixed inset-0 z-50 bg-black/75 backdrop-blur-xs flex items-center justify-center p-4">
      <div class="bg-slate-900 border border-slate-800 rounded-2xl p-5 max-w-md w-full shadow-2xl">
        <h3 class="text-base font-bold text-slate-100 flex items-center gap-2">
          <span>📂</span>
          <span>Новая категория задач</span>
        </h3>
        <p class="text-xs text-slate-400 mt-1">
          Добавьте направление. Воркер начнет генерировать под него практические задачи через Ollama.
        </p>

        <form @submit.prevent="handleCreateCategory" class="mt-4 space-y-3">
          <div>
            <label class="block text-xs font-medium text-slate-300 mb-1">Название категории</label>
            <input 
              v-model="newCat.name" 
              placeholder="например: Кибербезопасность или Go Advanced" 
              required
              class="w-full bg-slate-950 border border-slate-800 rounded-xl px-3 py-2 text-xs text-white focus:outline-none focus:border-sky-500"
            />
          </div>

          <div>
            <label class="block text-xs font-medium text-slate-300 mb-1">Краткое описание</label>
            <input 
              v-model="newCat.description" 
              placeholder="Сетевые атаки, XSS, CSRF, TLS, криптография..." 
              required
              class="w-full bg-slate-950 border border-slate-800 rounded-xl px-3 py-2 text-xs text-white focus:outline-none focus:border-sky-500"
            />
          </div>

          <div>
            <label class="block text-xs font-medium text-slate-300 mb-1">Инструкция для промпта Ollama</label>
            <textarea 
              v-model="newCat.prompt_instructions" 
              rows="3"
              placeholder="Темы: SQL-инъекции, анализ pcap, auth bypass. Стек: Python, Bash. Сложность: Junior-Middle." 
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
              Отмена
            </button>
            <button 
              type="submit" 
              :disabled="creatingCategory"
              class="px-4 py-2 bg-sky-500 hover:bg-sky-400 text-white rounded-xl text-xs font-semibold shadow-md shadow-sky-500/20"
            >
              {{ creatingCategory ? 'Создание...' : 'Создать категорию' }}
            </button>
          </div>
        </form>
      </div>
    </div>

    <!-- Модальное окно статистики -->
    <div v-if="showStatsModal" class="fixed inset-0 z-50 bg-black/75 backdrop-blur-xs flex items-center justify-center p-4">
      <div class="bg-slate-900 border border-slate-800 rounded-2xl p-5 max-w-md w-full shadow-2xl">
        <div class="flex items-center justify-between pb-3 border-b border-slate-800">
          <h3 class="text-base font-bold text-slate-100 flex items-center gap-2">
            <span>📊</span>
            <span>Ваша статистика</span>
          </h3>
          <button @click="showStatsModal = false" class="text-slate-400 hover:text-white text-lg">✕</button>
        </div>

        <div class="mt-4 grid grid-cols-3 gap-2 text-center">
          <div class="p-3 bg-slate-950 border border-slate-800 rounded-xl">
            <div class="text-xl font-bold text-sky-400">{{ stats?.total_answered || 0 }}</div>
            <div class="text-[10px] text-slate-400 mt-0.5">Всего ответов</div>
          </div>
          <div class="p-3 bg-slate-950 border border-slate-800 rounded-xl">
            <div class="text-xl font-bold text-emerald-400">{{ stats?.accuracy_percent || 0 }}%</div>
            <div class="text-[10px] text-slate-400 mt-0.5">Точность</div>
          </div>
          <div class="p-3 bg-slate-950 border border-slate-800 rounded-xl">
            <div class="text-xl font-bold text-amber-400">{{ stats?.current_streak || 0 }}</div>
            <div class="text-[10px] text-slate-400 mt-0.5">Текущий стрик</div>
          </div>
        </div>

        <!-- Статистика по категориям -->
        <div class="mt-4">
          <h4 class="text-xs font-semibold text-slate-300 mb-2">По категориям:</h4>
          <div class="space-y-2 max-h-48 overflow-y-auto">
            <div 
              v-for="cs in stats?.by_category" 
              :key="cs.category_slug"
              class="p-2.5 bg-slate-950 border border-slate-800 rounded-xl flex items-center justify-between text-xs"
            >
              <div>
                <div class="font-medium text-slate-200">{{ cs.category_name }}</div>
                <div class="text-[10px] text-slate-400">Решено: {{ cs.total_correct }} из {{ cs.total_answered }}</div>
              </div>
              <div class="font-bold text-sky-400">
                {{ Math.round(cs.accuracy_percent) }}%
              </div>
            </div>
            <div v-if="!stats?.by_category?.length" class="text-center text-xs text-slate-500 py-3">
              Вы пока не решили ни одной задачи
            </div>
          </div>
        </div>

        <button 
          @click="showStatsModal = false"
          class="w-full mt-5 py-2.5 bg-slate-800 hover:bg-slate-700 text-slate-200 rounded-xl text-xs font-medium"
        >
          Закрыть
        </button>
      </div>
    </div>
  </div>
</template>

<style scoped>
.no-scrollbar::-webkit-scrollbar {
  display: none;
}
.no-scrollbar {
  -ms-overflow-style: none;
  scrollbar-width: none;
}
@keyframes fadeIn {
  from { opacity: 0; transform: translateY(6px); }
  to { opacity: 1; transform: translateY(0); }
}
.animate-fade-in {
  animation: fadeIn 0.25s ease-out forwards;
}
</style>
