package db

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/PetroProg/tech-trainer/internal/models"
	_ "modernc.org/sqlite"
)

type DB struct {
	conn *sql.DB
	mu   sync.RWMutex
}

func Open(dbPath string) (*DB, error) {
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create db directory: %w", err)
	}

	conn, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite: %w", err)
	}

	// 1 соединение для SQLite - минимальный расход RAM и отсутствие блокировок
	conn.SetMaxOpenConns(1)
	conn.SetMaxIdleConns(1)

	pragmas := []string{
		"PRAGMA journal_mode=WAL;",
		"PRAGMA busy_timeout=5000;",
		"PRAGMA foreign_keys=ON;",
		"PRAGMA synchronous=NORMAL;",
		"PRAGMA cache_size=-2000;", // ~2MB RAM
	}
	for _, p := range pragmas {
		if _, err := conn.Exec(p); err != nil {
			return nil, fmt.Errorf("pragma failed (%s): %w", p, err)
		}
	}

	database := &DB{conn: conn}
	if err := database.initSchema(); err != nil {
		return nil, fmt.Errorf("failed to init schema: %w", err)
	}

	return database, nil
}

func (d *DB) Close() error {
	return d.conn.Close()
}

func (d *DB) initSchema() error {
	schema := `
	CREATE TABLE IF NOT EXISTS categories (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		slug TEXT UNIQUE NOT NULL,
		name TEXT NOT NULL,
		description TEXT NOT NULL,
		prompt_instructions TEXT NOT NULL,
		is_active INTEGER NOT NULL DEFAULT 1,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS questions (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		category_id INTEGER NOT NULL REFERENCES categories(id) ON DELETE CASCADE,
		type TEXT NOT NULL, -- 'multiple_choice' or 'code_analysis'
		difficulty TEXT NOT NULL, -- 'junior' or 'middle'
		question_text TEXT NOT NULL,
		code_snippet TEXT NOT NULL DEFAULT '',
		language TEXT NOT NULL DEFAULT '',
		explanation TEXT NOT NULL,
		hash TEXT UNIQUE NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS options (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		question_id INTEGER NOT NULL REFERENCES questions(id) ON DELETE CASCADE,
		option_text TEXT NOT NULL,
		is_correct INTEGER NOT NULL DEFAULT 0,
		order_idx INTEGER NOT NULL DEFAULT 0
	);

	CREATE TABLE IF NOT EXISTS user_answers (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		question_id INTEGER NOT NULL REFERENCES questions(id) ON DELETE CASCADE,
		selected_option_ids TEXT NOT NULL, -- JSON array
		is_correct INTEGER NOT NULL,
		answered_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE INDEX IF NOT EXISTS idx_questions_category ON questions(category_id);
	CREATE INDEX IF NOT EXISTS idx_questions_hash ON questions(hash);
	CREATE INDEX IF NOT EXISTS idx_options_question ON options(question_id);
	CREATE INDEX IF NOT EXISTS idx_user_answers_question ON user_answers(question_id);
	`

	_, err := d.conn.Exec(schema)
	if err != nil {
		return err
	}

	var count int
	if err := d.conn.QueryRow("SELECT COUNT(*) FROM categories").Scan(&count); err != nil {
		return err
	}

	if count == 0 {
		initialCategories := []struct {
			slug, name, desc, prompt string
		}{
			{
				slug: "ai-neural-networks",
				name: "AI & Нейросети",
				desc: "Архитектура трансформеров, механизм внимания, LoRA/QLoRA, RAG, Ollama/vLLM, векторные базы данных.",
				prompt: "Темы: Transformer architecture (Self-Attention, KV-cache, RoPE), Fine-tuning (LoRA, QLoRA, rank, alpha), RAG (embeddings, chunking strategies, hybrid search), Inference (Ollama, vLLM, quantization GGUF/AWQ), Vector DBs (Chroma, Qdrant, Milvus, distance metrics). Уровень сложности: Junior и базовый Middle. Практические вопросы и фрагменты кода на Python (PyTorch/Transformers/LangChain) или конфигурации.",
			},
			{
				slug: "devops-linux",
				name: "DevOps & Linux",
				desc: "Systemd, сети/iptables, Docker, Kubernetes, память/OOM, bash-скрипты, траблшутинг.",
				prompt: "Темы: Linux internals (systemd units, journalctl, cgroups, /proc), Networking (iptables, netfilter, DNS, TCP states, curl, netstat/ss), Docker (multi-stage builds, layers, networking, security, storage), Kubernetes basics (Pods, Deployments, Services, ConfigMaps), OOM killer, exit codes, Bash debugging (set -euo pipefail, regex, sed, awk). Уровень сложности: Junior и умеренный Middle.",
			},
			{
				slug: "it-languages",
				name: "IT & Языки",
				desc: "Go (горутины, каналы, race condition), Python (GIL, async, генераторы), Rust (ownership, lifetimes), SQL (индексы, EXPLAIN).",
				prompt: "Темы: Golang (goroutines, channels, sync.Mutex/RWMutex, defer, interfaces, context, memory leaks), Python (GIL, asyncio, decorators, memory management, generators), Rust (ownership, borrowing, lifetimes basics, Result/Option), SQL (B-Tree indexes, compound indexes, EXPLAIN ANALYZE, transaction isolation, JOIN types). Уровень сложности: Junior и крепкий Middle. Реальные сниппеты кода с подвохом или анализом вывода.",
			},
		}

		stmt, err := d.conn.Prepare(`INSERT INTO categories (slug, name, description, prompt_instructions) VALUES (?, ?, ?, ?)`)
		if err != nil {
			return err
		}
		defer stmt.Close()

		for _, c := range initialCategories {
			if _, err := stmt.Exec(c.slug, c.name, c.desc, c.prompt); err != nil {
				return err
			}
		}
	}

	return nil
}

func (d *DB) GetCategories() ([]models.Category, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	query := `
	SELECT 
		c.id, c.slug, c.name, c.description, c.prompt_instructions, c.is_active, c.created_at,
		COUNT(DISTINCT q.id) AS total_questions,
		COUNT(DISTINCT ua.question_id) AS solved_questions,
		(COUNT(DISTINCT q.id) - COUNT(DISTINCT ua.question_id)) AS available_questions
	FROM categories c
	LEFT JOIN questions q ON c.id = q.category_id
	LEFT JOIN user_answers ua ON q.id = ua.question_id
	WHERE c.is_active = 1
	GROUP BY c.id
	ORDER BY c.id ASC
	`

	rows, err := d.conn.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var categories []models.Category
	for rows.Next() {
		var c models.Category
		var activeInt int
		var createdAt string
		err := rows.Scan(
			&c.ID, &c.Slug, &c.Name, &c.Description, &c.PromptInstructions,
			&activeInt, &createdAt,
			&c.TotalQuestions, &c.SolvedQuestions, &c.AvailableQuestions,
		)
		if err != nil {
			return nil, err
		}
		c.IsActive = activeInt == 1
		c.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdAt)
		categories = append(categories, c)
	}

	return categories, nil
}

func (d *DB) CreateCategory(c models.Category) (int64, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	res, err := d.conn.Exec(`
		INSERT INTO categories (slug, name, description, prompt_instructions, is_active)
		VALUES (?, ?, ?, ?, 1)
	`, c.Slug, c.Name, c.Description, c.PromptInstructions)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (d *DB) GetCategoryBySlug(slug string) (*models.Category, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	var c models.Category
	var activeInt int
	var createdAt string
	err := d.conn.QueryRow(`
		SELECT id, slug, name, description, prompt_instructions, is_active, created_at
		FROM categories WHERE slug = ?
	`, slug).Scan(&c.ID, &c.Slug, &c.Name, &c.Description, &c.PromptInstructions, &activeInt, &createdAt)
	if err != nil {
		return nil, err
	}
	c.IsActive = activeInt == 1
	c.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdAt)
	return &c, nil
}

func (d *DB) GetUnsolvedCountByCategory() (map[int64]int, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	query := `
	SELECT c.id, (COUNT(DISTINCT q.id) - COUNT(DISTINCT ua.question_id)) AS unsolved_count
	FROM categories c
	LEFT JOIN questions q ON c.id = q.category_id
	LEFT JOIN user_answers ua ON q.id = ua.question_id
	WHERE c.is_active = 1
	GROUP BY c.id
	`
	rows, err := d.conn.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	counts := make(map[int64]int)
	for rows.Next() {
		var catID int64
		var unsolved int
		if err := rows.Scan(&catID, &unsolved); err == nil {
			counts[catID] = unsolved
		}
	}
	return counts, nil
}

// Выдача случайной нерешенной задачи
func (d *DB) GetRandomQuestion(categorySlug, difficulty string) (*models.Question, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	whereClauses := []string{
		"q.id NOT IN (SELECT question_id FROM user_answers)",
	}
	args := []interface{}{}

	if categorySlug != "" && categorySlug != "all" {
		whereClauses = append(whereClauses, "c.slug = ?")
		args = append(args, categorySlug)
	}

	if difficulty != "" && difficulty != "all" {
		whereClauses = append(whereClauses, "q.difficulty = ?")
		args = append(args, difficulty)
	}

	query := fmt.Sprintf(`
		SELECT q.id, q.category_id, c.slug, c.name, q.type, q.difficulty,
		       q.question_text, q.code_snippet, q.language, q.explanation, q.created_at
		FROM questions q
		JOIN categories c ON q.category_id = c.id
		WHERE %s
		ORDER BY RANDOM()
		LIMIT 1
	`, strings.Join(whereClauses, " AND "))

	var q models.Question
	var createdAt string
	err := d.conn.QueryRow(query, args...).Scan(
		&q.ID, &q.CategoryID, &q.CategorySlug, &q.CategoryName,
		&q.Type, &q.Difficulty, &q.QuestionText, &q.CodeSnippet,
		&q.Language, &q.Explanation, &createdAt,
	)

	// Если нерешённых задач нет, берем повторно любую решенную задачу
	if err == sql.ErrNoRows {
		fallbackWhere := []string{"1=1"}
		args = []interface{}{}
		if categorySlug != "" && categorySlug != "all" {
			fallbackWhere = append(fallbackWhere, "c.slug = ?")
			args = append(args, categorySlug)
		}
		if difficulty != "" && difficulty != "all" {
			fallbackWhere = append(fallbackWhere, "q.difficulty = ?")
			args = append(args, difficulty)
		}

		fallbackQuery := fmt.Sprintf(`
			SELECT q.id, q.category_id, c.slug, c.name, q.type, q.difficulty,
			       q.question_text, q.code_snippet, q.language, q.explanation, q.created_at
			FROM questions q
			JOIN categories c ON q.category_id = c.id
			WHERE %s
			ORDER BY RANDOM()
			LIMIT 1
		`, strings.Join(fallbackWhere, " AND "))

		err = d.conn.QueryRow(fallbackQuery, args...).Scan(
			&q.ID, &q.CategoryID, &q.CategorySlug, &q.CategoryName,
			&q.Type, &q.Difficulty, &q.QuestionText, &q.CodeSnippet,
			&q.Language, &q.Explanation, &createdAt,
		)
		if err != nil {
			return nil, err
		}
		q.IsSolved = true
	} else if err != nil {
		return nil, err
	}

	q.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdAt)

	// Загрузка вариантов ответов
	optRows, err := d.conn.Query(`
		SELECT id, question_id, option_text, is_correct, order_idx
		FROM options
		WHERE question_id = ?
		ORDER BY order_idx ASC
	`, q.ID)
	if err != nil {
		return nil, err
	}
	defer optRows.Close()

	for optRows.Next() {
		var o models.Option
		var isCorrectInt int
		if err := optRows.Scan(&o.ID, &o.QuestionID, &o.OptionText, &isCorrectInt, &o.OrderIndex); err == nil {
			o.IsCorrect = isCorrectInt == 1
			q.Options = append(q.Options, o)
		}
	}

	return &q, nil
}

func CalculateHash(questionText, codeSnippet string) string {
	normalQuestion := strings.ToLower(strings.TrimSpace(questionText))
	normalCode := strings.TrimSpace(codeSnippet)
	h := sha256.New()
	h.Write([]byte(normalQuestion + "|" + normalCode))
	return hex.EncodeToString(h.Sum(nil))
}

// Сохранение сгенерированной задачи с дедупликацией по хэшу
func (d *DB) SaveGeneratedQuestion(dto models.GeneratedQuestionDTO, categoryID int64) (bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	hash := CalculateHash(dto.Question, dto.Code)

	var existsID int64
	err := d.conn.QueryRow("SELECT id FROM questions WHERE hash = ?", hash).Scan(&existsID)
	if err == nil {
		// Дубликат найден
		return false, nil
	}

	tx, err := d.conn.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()

	qType := dto.Type
	if qType == "" {
		if strings.TrimSpace(dto.Code) != "" {
			qType = "code_analysis"
		} else {
			qType = "multiple_choice"
		}
	}

	diff := dto.Difficulty
	if diff == "" {
		diff = "junior"
	}

	res, err := tx.Exec(`
		INSERT INTO questions (category_id, type, difficulty, question_text, code_snippet, language, explanation, hash)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, categoryID, qType, diff, dto.Question, dto.Code, dto.Language, dto.Explanation, hash)
	if err != nil {
		return false, err
	}

	qID, err := res.LastInsertId()
	if err != nil {
		return false, err
	}

	correctSet := make(map[int]bool)
	for _, idx := range dto.CorrectIndices {
		correctSet[idx] = true
	}

	for i, optText := range dto.Options {
		isCorrect := 0
		if correctSet[i] {
			isCorrect = 1
		}
		_, err := tx.Exec(`
			INSERT INTO options (question_id, option_text, is_correct, order_idx)
			VALUES (?, ?, ?, ?)
		`, qID, optText, isCorrect, i)
		if err != nil {
			return false, err
		}
	}

	return true, tx.Commit()
}

func (d *DB) SubmitAnswer(questionID int64, selectedIDs []int64) (*models.SubmitAnswerResponse, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	var explanation string
	err := d.conn.QueryRow("SELECT explanation FROM questions WHERE id = ?", questionID).Scan(&explanation)
	if err != nil {
		return nil, err
	}

	rows, err := d.conn.Query("SELECT id FROM options WHERE question_id = ? AND is_correct = 1", questionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var correctIDs []int64
	correctMap := make(map[int64]bool)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err == nil {
			correctIDs = append(correctIDs, id)
			correctMap[id] = true
		}
	}

	isAllCorrect := len(selectedIDs) == len(correctIDs)
	if isAllCorrect {
		for _, selID := range selectedIDs {
			if !correctMap[selID] {
				isAllCorrect = false
				break
			}
		}
	}

	selectedJSON, _ := json.Marshal(selectedIDs)
	isCorrectInt := 0
	if isAllCorrect {
		isCorrectInt = 1
	}

	_, err = d.conn.Exec(`
		INSERT INTO user_answers (question_id, selected_option_ids, is_correct)
		VALUES (?, ?, ?)
	`, questionID, string(selectedJSON), isCorrectInt)
	if err != nil {
		return nil, err
	}

	var streak int
	streakRows, err := d.conn.Query(`SELECT is_correct FROM user_answers ORDER BY answered_at DESC, id DESC LIMIT 50`)
	if err == nil {
		defer streakRows.Close()
		for streakRows.Next() {
			var corr int
			if err := streakRows.Scan(&corr); err == nil {
				if corr == 1 {
					streak++
				} else {
					break
				}
			}
		}
	}

	var totalScore int
	_ = d.conn.QueryRow(`SELECT COALESCE(SUM(CASE WHEN is_correct = 1 THEN 10 ELSE 0 END), 0) FROM user_answers`).Scan(&totalScore)

	return &models.SubmitAnswerResponse{
		IsCorrect:         isAllCorrect,
		Explanation:       explanation,
		CorrectOptionIDs:  correctIDs,
		SelectedOptionIDs: selectedIDs,
		Streak:            streak,
		TotalScore:        totalScore,
	}, nil
}

func (d *DB) DeleteQuestion(questionID int64) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	_, err := d.conn.Exec("DELETE FROM questions WHERE id = ?", questionID)
	return err
}

func (d *DB) GetUserStats() (*models.UserStats, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	stats := &models.UserStats{
		ByCategory: make([]models.CategoryStat, 0),
	}

	_ = d.conn.QueryRow(`
		SELECT 
			COUNT(*),
			COALESCE(SUM(CASE WHEN is_correct = 1 THEN 1 ELSE 0 END), 0)
		FROM user_answers
	`).Scan(&stats.TotalAnswered, &stats.TotalCorrect)

	if stats.TotalAnswered > 0 {
		stats.AccuracyPercent = float64(stats.TotalCorrect) / float64(stats.TotalAnswered) * 100.0
	}
	stats.TotalScore = stats.TotalCorrect * 10

	streakRows, err := d.conn.Query(`SELECT is_correct FROM user_answers ORDER BY answered_at DESC, id DESC LIMIT 50`)
	if err == nil {
		defer streakRows.Close()
		for streakRows.Next() {
			var corr int
			if err := streakRows.Scan(&corr); err == nil {
				if corr == 1 {
					stats.CurrentStreak++
				} else {
					break
				}
			}
		}
	}

	catRows, err := d.conn.Query(`
		SELECT 
			c.name, c.slug,
			COUNT(ua.id) AS answered,
			SUM(CASE WHEN ua.is_correct = 1 THEN 1 ELSE 0 END) AS correct
		FROM categories c
		JOIN questions q ON c.id = q.category_id
		JOIN user_answers ua ON q.id = ua.question_id
		GROUP BY c.id
	`)
	if err == nil {
		defer catRows.Close()
		for catRows.Next() {
			var cs models.CategoryStat
			var corr sql.NullInt64
			if err := catRows.Scan(&cs.CategoryName, &cs.CategorySlug, &cs.TotalAnswered, &corr); err == nil {
				cs.TotalCorrect = int(corr.Int64)
				if cs.TotalAnswered > 0 {
					cs.AccuracyPercent = float64(cs.TotalCorrect) / float64(cs.TotalAnswered) * 100.0
				}
				stats.ByCategory = append(stats.ByCategory, cs)
			}
		}
	}

	return stats, nil
}
