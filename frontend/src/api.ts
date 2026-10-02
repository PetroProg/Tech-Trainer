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

export interface SubmitAnswerResponse {
  is_correct: boolean;
  explanation: string;
  correct_option_ids: number[];
  selected_option_ids: number[];
  streak: number;
  total_score: number;
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

export async function getCategories(): Promise<Category[]> {
  const res = await fetch(`${API_BASE}/categories`);
  if (!res.ok) throw new Error('Failed to load categories');
  return res.json();
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
  const params = new URLSearchParams();
  if (categorySlug && categorySlug !== 'all') params.append('category', categorySlug);
  if (difficulty && difficulty !== 'all') params.append('difficulty', difficulty);

  const res = await fetch(`${API_BASE}/questions/random?${params.toString()}`);
  if (!res.ok) {
    const err = await res.json();
    throw new Error(err.error || 'Failed to get question');
  }
  return res.json();
}

export async function submitAnswer(questionId: number, selectedOptionIds: number[]): Promise<SubmitAnswerResponse> {
  const res = await fetch(`${API_BASE}/questions/${questionId}/answer`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ selected_option_ids: selectedOptionIds })
  });
  if (!res.ok) throw new Error('Failed to submit answer');
  return res.json();
}

export async function deleteQuestion(questionId: number): Promise<void> {
  const res = await fetch(`${API_BASE}/questions/${questionId}`, {
    method: 'DELETE'
  });
  if (!res.ok) throw new Error('Failed to delete question');
}

export async function getUserStats(): Promise<UserStats> {
  const res = await fetch(`${API_BASE}/stats`);
  if (!res.ok) throw new Error('Failed to load stats');
  return res.json();
}

export async function getWorkerStatus(): Promise<WorkerStatus> {
  const res = await fetch(`${API_BASE}/worker/status`);
  if (!res.ok) throw new Error('Failed to get worker status');
  return res.json();
}

export async function triggerWorker(): Promise<{ message: string }> {
  const res = await fetch(`${API_BASE}/worker/trigger`, {
    method: 'POST'
  });
  const data = await res.json();
  if (!res.ok) throw new Error(data.message || 'Trigger failed');
  return data;
}
