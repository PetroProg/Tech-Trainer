import {
  saveOfflinePack,
  getOfflineCategories,
  getOfflineQuestion,
  submitOfflineAnswer,
  getPendingAnswers,
  clearPendingAnswers,
  getOfflineStats
} from './offline'

export interface Category {
  id: number;
  slug: string;
  name: string;
  description: string;
  prompt_instructions: string;
  is_active: boolean;
  available_questions: number;
  total_questions: number;
  solved_questions: number;
}

export interface Option {
  id: number;
  option_text: string;
  is_correct: boolean;
  order_idx: number;
}

export interface Question {
  id: number;
  category_id: number;
  category_slug?: string;
  category_name?: string;
  type: string;
  difficulty: string;
  question_text: string;
  code_snippet: string;
  language: string;
  explanation: string;
  options: Option[];
  is_solved?: boolean;
}

export interface OfflineQuestion extends Question {
  correct_option_ids: number[];
}

export interface OfflinePack {
  categories: Category[];
  questions: OfflineQuestion[];
  synced_at: string;
}

export interface SyncAnswerItem {
  question_id: number;
  selected_option_ids: number[];
  answered_at: string;
}

export interface SyncAnswersResponse {
  synced_count: number;
  total_score: number;
  streak: number;
  user_stats: UserStats;
}

export interface SubmitAnswerResponse {
  is_correct: boolean;
  explanation: string;
  correct_option_ids: number[];
  selected_option_ids: number[];
  streak: number;
  total_score: number;
  is_offline?: boolean;
}

export interface CategoryStat {
  category_name: string;
  category_slug: string;
  total_answered: number;
  total_correct: number;
  accuracy_percent: number;
}

export interface UserStats {
  total_answered: number;
  total_correct: number;
  accuracy_percent: number;
  current_streak: number;
  total_score: number;
  by_category: CategoryStat[];
}

export interface WorkerStatus {
  is_online: boolean;
  model_name: string;
  last_check: string;
  is_running: boolean;
  last_result: string;
}

const API_BASE = '/api';

// Helper with timeout to gracefully detect Tailscale disconnect
async function fetchWithTimeout(url: string, options: RequestInit = {}, timeoutMs = 3000): Promise<Response> {
  const controller = new AbortController()
  const id = setTimeout(() => controller.abort(), timeoutMs)
  try {
    const res = await fetch(url, { ...options, signal: controller.signal })
    return res
  } finally {
    clearTimeout(id)
  }
}

// Download fresh offline pack in background
export async function downloadAndCacheOfflinePack(): Promise<boolean> {
  try {
    const res = await fetchWithTimeout(`${API_BASE}/questions/offline-pack`, {}, 5000)
    if (!res.ok) return false
    const pack: OfflinePack = await res.json()
    await saveOfflinePack(pack)
    return true
  } catch {
    return false
  }
}

// Sync all pending offline answers to the backend
export async function syncOfflineAnswersToServer(): Promise<SyncAnswersResponse | null> {
  const pending = await getPendingAnswers()
  if (pending.length === 0) return null

  try {
    const res = await fetchWithTimeout(`${API_BASE}/sync/answers`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ answers: pending })
    }, 6000)

    if (!res.ok) return null
    const result: SyncAnswersResponse = await res.json()
    await clearPendingAnswers()
    // Refresh offline cache after sync
    downloadAndCacheOfflinePack().catch(() => {})
    return result
  } catch {
    return null
  }
}

export async function getCategories(): Promise<Category[]> {
  try {
    const res = await fetchWithTimeout(`${API_BASE}/categories`, {}, 2500)
    if (res.ok) {
      const data: Category[] = await res.json()
      // Background trigger to keep offline pack fresh
      downloadAndCacheOfflinePack().catch(() => {})
      return data
    }
  } catch {
    // Tailscale / network unavailable: fallback to IndexedDB
  }
  return getOfflineCategories()
}

export async function createCategory(data: { name: string; description: string; prompt_instructions: string }): Promise<Category> {
  const res = await fetch(`${API_BASE}/categories`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(data)
  });
  if (!res.ok) {
    const err = await res.json();
    throw new Error(err.error || 'Failed to create category');
  }
  return res.json();
}

export async function getRandomQuestion(categorySlug?: string, difficulty?: string): Promise<Question> {
  try {
    const params = new URLSearchParams();
    if (categorySlug && categorySlug !== 'all') params.append('category', categorySlug);
    if (difficulty && difficulty !== 'all') params.append('difficulty', difficulty);

    const res = await fetchWithTimeout(`${API_BASE}/questions/random?${params.toString()}`, {}, 2500);
    if (res.ok) {
      return await res.json();
    }
  } catch {
    // Offline / Tailscale disconnected
  }

  const offlineQ = await getOfflineQuestion(categorySlug, difficulty)
  if (offlineQ) {
    return offlineQ
  }

  throw new Error('No questions available in cache. Reconnect to Tailscale once to pre-load questions.')
}

export async function submitAnswer(questionId: number, selectedOptionIds: number[]): Promise<SubmitAnswerResponse> {
  try {
    const res = await fetchWithTimeout(`${API_BASE}/questions/${questionId}/answer`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ selected_option_ids: selectedOptionIds })
    }, 3000);

    if (res.ok) {
      const data = await res.json()
      return { ...data, is_offline: false }
    }
  } catch {
    // Network / Tailscale down
  }

  // Autonomous offline submission with local evaluation and sync queueing
  const offlineResult = await submitOfflineAnswer(questionId, selectedOptionIds)
  return { ...offlineResult, is_offline: true }
}

export async function deleteQuestion(questionId: number): Promise<void> {
  const res = await fetch(`${API_BASE}/questions/${questionId}`, {
    method: 'DELETE'
  });
  if (!res.ok) throw new Error('Failed to delete question');
}

export async function getUserStats(): Promise<UserStats> {
  try {
    const res = await fetchWithTimeout(`${API_BASE}/stats`, {}, 2500);
    if (res.ok) return await res.json();
  } catch {
    // Fallback
  }
  return getOfflineStats()
}

export async function getWorkerStatus(): Promise<WorkerStatus> {
  try {
    const res = await fetchWithTimeout(`${API_BASE}/worker/status`, {}, 2000);
    if (res.ok) return await res.json();
  } catch {
    // Fallback
  }
  return {
    is_online: false,
    model_name: 'qwen2.5:7b',
    last_check: new Date().toISOString(),
    is_running: false,
    last_result: 'Tailscale disconnected / Offline'
  }
}

export async function triggerWorker(): Promise<{ message: string }> {
  const res = await fetch(`${API_BASE}/worker/trigger`, {
    method: 'POST'
  });
  const data = await res.json();
  if (!res.ok) throw new Error(data.message || 'Worker trigger failed');
  return data;
}
