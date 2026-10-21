import type { Category, Question, SubmitAnswerResponse, UserStats, OfflinePack, SyncAnswerItem, OfflineQuestion } from './api'

const DB_NAME = 'TechTrainerDB'
const DB_VERSION = 1

interface LocalStatsMeta {
  key: string
  total_answered: number
  total_correct: number
  current_streak: number
  total_score: number
  by_category: Record<string, { answered: number; correct: number }>
}

let dbInstance: IDBDatabase | null = null

export function initOfflineDB(): Promise<IDBDatabase> {
  if (dbInstance) return Promise.resolve(dbInstance)

  return new Promise((resolve, reject) => {
    const req = indexedDB.open(DB_NAME, DB_VERSION)

    req.onupgradeneeded = (e) => {
      const db = (e.target as IDBOpenDBRequest).result

      if (!db.objectStoreNames.contains('categories')) {
        db.createObjectStore('categories', { keyPath: 'id' })
      }
      if (!db.objectStoreNames.contains('questions')) {
        const qStore = db.createObjectStore('questions', { keyPath: 'id' })
        qStore.createIndex('category_slug', 'category_slug', { unique: false })
      }
      if (!db.objectStoreNames.contains('pending_answers')) {
        db.createObjectStore('pending_answers', { autoIncrement: true })
      }
      if (!db.objectStoreNames.contains('meta')) {
        db.createObjectStore('meta', { keyPath: 'key' })
      }
    }

    req.onsuccess = () => {
      dbInstance = req.result
      resolve(req.result)
    }

    req.onerror = () => {
      reject(req.error)
    }
  })
}

// saveOfflinePack stores downloaded categories and questions in IndexedDB
export async function saveOfflinePack(pack: OfflinePack): Promise<void> {
  const db = await initOfflineDB()

  return new Promise((resolve, reject) => {
    const tx = db.transaction(['categories', 'questions', 'meta'], 'readwrite')
    const catStore = tx.objectStore('categories')
    const qStore = tx.objectStore('questions')
    const metaStore = tx.objectStore('meta')

    // Preserve local solved state if questions were answered offline
    const getAllReq = qStore.getAll()
    getAllReq.onsuccess = () => {
      const existingQuestions: OfflineQuestion[] = getAllReq.result || []
      const solvedSet = new Set<number>()
      for (const eq of existingQuestions) {
        if (eq.is_solved) solvedSet.add(eq.id)
      }

      for (const c of pack.categories) {
        catStore.put(c)
      }

      for (const q of pack.questions) {
        if (solvedSet.has(q.id)) {
          q.is_solved = true
        }
        qStore.put(q)
      }

      metaStore.put({ key: 'last_sync', timestamp: new Date().toISOString() })
    }

    tx.oncomplete = () => resolve()
    tx.onerror = () => reject(tx.error)
  })
}

// saveOfflineCategory writes or updates a single category in IndexedDB
export async function saveOfflineCategory(category: Category): Promise<void> {
  const db = await initOfflineDB()
  return new Promise((resolve, reject) => {
    const tx = db.transaction('categories', 'readwrite')
    tx.objectStore('categories').put(category)
    tx.oncomplete = () => resolve()
    tx.onerror = () => reject(tx.error)
  })
}

// getOfflineCategories retrieves stored categories and dynamically counts available questions
export async function getOfflineCategories(): Promise<Category[]> {
  const db = await initOfflineDB()

  return new Promise((resolve, reject) => {
    const tx = db.transaction(['categories', 'questions'], 'readonly')
    const catStore = tx.objectStore('categories')
    const qStore = tx.objectStore('questions')

    const catReq = catStore.getAll()
    const qReq = qStore.getAll()

    tx.oncomplete = () => {
      const categories: Category[] = catReq.result || []
      const questions: OfflineQuestion[] = qReq.result || []

      // Recalculate counters based on local state
      const countMap: Record<number, { total: number; solved: number }> = {}
      for (const q of questions) {
        if (!countMap[q.category_id]) {
          countMap[q.category_id] = { total: 0, solved: 0 }
        }
        countMap[q.category_id].total++
        if (q.is_solved) countMap[q.category_id].solved++
      }

      for (const c of categories) {
        const counts = countMap[c.id] || { total: 0, solved: 0 }
        c.total_questions = counts.total
        c.solved_questions = counts.solved
        c.available_questions = Math.max(0, counts.total - counts.solved)
      }

      resolve(categories)
    }

    tx.onerror = () => reject(tx.error)
  })
}

// getOfflineQuestion selects a random challenge from IndexedDB
export async function getOfflineQuestion(categorySlug?: string, difficulty?: string): Promise<Question | null> {
  const db = await initOfflineDB()

  return new Promise((resolve, reject) => {
    const tx = db.transaction(['questions'], 'readonly')
    const qStore = tx.objectStore('questions')
    const req = qStore.getAll()

    req.onsuccess = () => {
      const all: OfflineQuestion[] = req.result || []
      if (all.length === 0) {
        resolve(null)
        return
      }

      // Filter by category and difficulty
      let filtered = all
      if (categorySlug && categorySlug !== 'all') {
        filtered = filtered.filter(q => q.category_slug === categorySlug)
      }
      if (difficulty && difficulty !== 'all') {
        filtered = filtered.filter(q => q.difficulty === difficulty)
      }

      if (filtered.length === 0) {
        resolve(null)
        return
      }

      // Prioritize unsolved questions
      const unsolved = filtered.filter(q => !q.is_solved)
      const pool = unsolved.length > 0 ? unsolved : filtered
      const chosen = pool[Math.floor(Math.random() * pool.length)]

      // Sanitize options before returning to UI
      const sanitizedOptions = chosen.options.map(opt => ({
        id: opt.id,
        option_text: opt.option_text,
        order_idx: opt.order_idx,
        is_correct: false
      }))

      resolve({
        id: chosen.id,
        category_id: chosen.category_id,
        category_slug: chosen.category_slug,
        category_name: chosen.category_name,
        type: chosen.type,
        difficulty: chosen.difficulty,
        question_text: chosen.question_text,
        code_snippet: chosen.code_snippet,
        language: chosen.language,
        explanation: chosen.explanation,
        options: sanitizedOptions,
        expected_correct_count: chosen.correct_option_ids?.length || 1,
        is_solved: chosen.is_solved
      })
    }

    req.onerror = () => reject(req.error)
  })
}

// submitOfflineAnswer evaluates choices locally and enqueues the result for background sync
export async function submitOfflineAnswer(questionId: number, selectedOptionIds: number[]): Promise<SubmitAnswerResponse> {
  const db = await initOfflineDB()

  return new Promise((resolve, reject) => {
    const tx = db.transaction(['questions', 'pending_answers', 'meta'], 'readwrite')
    const qStore = tx.objectStore('questions')
    const pendingStore = tx.objectStore('pending_answers')
    const metaStore = tx.objectStore('meta')

    const qReq = qStore.get(questionId)
    const statsReq = metaStore.get('stats')

    tx.oncomplete = () => {
      const q: OfflineQuestion = qReq.result
      if (!q) {
        reject(new Error('Question not found in offline database'))
        return
      }

      const correctSet = new Set(q.correct_option_ids)
      const isCorrect = selectedOptionIds.length === q.correct_option_ids.length &&
        selectedOptionIds.every(id => correctSet.has(id))

      // Load or initialize stats
      let stats: LocalStatsMeta = statsReq.result || {
        key: 'stats',
        total_answered: 0,
        total_correct: 0,
        current_streak: 0,
        total_score: 0,
        by_category: {}
      }

      stats.total_answered++
      if (isCorrect) {
        stats.total_correct++
        stats.current_streak++
        stats.total_score += 10
      } else {
        stats.current_streak = 0
      }

      if (!stats.by_category[q.category_slug]) {
        stats.by_category[q.category_slug] = { answered: 0, correct: 0 }
      }
      stats.by_category[q.category_slug].answered++
      if (isCorrect) stats.by_category[q.category_slug].correct++

      // Save updated stats
      metaStore.put(stats)

      // Mark question as solved in offline storage
      q.is_solved = true
      qStore.put(q)

      // Record in pending queue for server synchronization
      const answerItem: SyncAnswerItem = {
        question_id: questionId,
        selected_option_ids: selectedOptionIds,
        answered_at: new Date().toISOString()
      }
      pendingStore.add(answerItem)

      resolve({
        is_correct: isCorrect,
        explanation: q.explanation,
        correct_option_ids: q.correct_option_ids,
        selected_option_ids: selectedOptionIds,
        streak: stats.current_streak,
        total_score: stats.total_score
      })
    }

    tx.onerror = () => reject(tx.error)
  })
}

// getPendingAnswers retrieves all queued answers waiting for connection
export async function getPendingAnswers(): Promise<SyncAnswerItem[]> {
  const db = await initOfflineDB()

  return new Promise((resolve, reject) => {
    const tx = db.transaction(['pending_answers'], 'readonly')
    const req = tx.objectStore('pending_answers').getAll()
    req.onsuccess = () => resolve(req.result || [])
    req.onerror = () => reject(req.error)
  })
}

// clearPendingAnswers purges the queue after confirmed server sync
export async function clearPendingAnswers(): Promise<void> {
  const db = await initOfflineDB()

  return new Promise((resolve, reject) => {
    const tx = db.transaction(['pending_answers'], 'readwrite')
    const req = tx.objectStore('pending_answers').clear()
    req.onsuccess = () => resolve()
    req.onerror = () => reject(req.error)
  })
}

// getPendingAnswersCount returns number of queued items
export async function getPendingAnswersCount(): Promise<number> {
  const db = await initOfflineDB()

  return new Promise((resolve, reject) => {
    const tx = db.transaction(['pending_answers'], 'readonly')
    const req = tx.objectStore('pending_answers').count()
    req.onsuccess = () => resolve(req.result || 0)
    req.onerror = () => resolve(0)
  })
}

// getOfflineStats computes user statistics from offline state
export async function getOfflineStats(): Promise<UserStats> {
  const db = await initOfflineDB()

  return new Promise((resolve, reject) => {
    const tx = db.transaction(['meta'], 'readonly')
    const req = tx.objectStore('meta').get('stats')

    req.onsuccess = () => {
      const stats: LocalStatsMeta = req.result || {
        key: 'stats',
        total_answered: 0,
        total_correct: 0,
        current_streak: 0,
        total_score: 0,
        by_category: {}
      }

      const accuracy = stats.total_answered > 0
        ? Math.round((stats.total_correct / stats.total_answered) * 100)
        : 0

      const byCat = Object.entries(stats.by_category).map(([slug, data]) => ({
        category_name: slug,
        category_slug: slug,
        total_answered: data.answered,
        total_correct: data.correct,
        accuracy_percent: data.answered > 0 ? Math.round((data.correct / data.answered) * 100) : 0
      }))

      resolve({
        total_answered: stats.total_answered,
        total_correct: stats.total_correct,
        accuracy_percent: accuracy,
        current_streak: stats.current_streak,
        total_score: stats.total_score,
        by_category: byCat
      })
    }

    req.onerror = () => reject(req.error)
  })
}
