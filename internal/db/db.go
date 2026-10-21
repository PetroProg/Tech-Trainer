package db

import (
	"crypto/sha256"
	"database/sql"
	"math"
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

// DB manages SQLite database connections and operations.
type DB struct {
	conn *sql.DB
	mu   sync.RWMutex
}

// Open creates or opens a SQLite database in WAL mode with optimized settings.
func Open(dbPath string) (*DB, error) {
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create db directory: %w", err)
	}

	conn, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite: %w", err)
	}

	// Connection pool settings: WAL mode allows concurrent readers.
	// Allow multiple connections so concurrent readers and sequential queries never starve.
	conn.SetMaxOpenConns(10)
	conn.SetMaxIdleConns(5)
	conn.SetConnMaxLifetime(time.Hour)

	pragmas := []string{
		"PRAGMA journal_mode=WAL;",
		"PRAGMA busy_timeout=5000;",
		"PRAGMA foreign_keys=ON;",
		"PRAGMA synchronous=NORMAL;",
		"PRAGMA cache_size=-2000;", // ~2MB RAM cache
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

// Close terminates the database connection.
func (d *DB) Close() error {
	return d.conn.Close()
}

// initSchema sets up required tables, indexes, and initial categories.
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
		question_id INTEGER REFERENCES questions(id) ON DELETE SET NULL,
		selected_option_ids TEXT NOT NULL, -- JSON array
		is_correct INTEGER NOT NULL,
		answered_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE INDEX IF NOT EXISTS idx_questions_category ON questions(category_id);
	CREATE INDEX IF NOT EXISTS idx_questions_hash ON questions(hash);
	CREATE INDEX IF NOT EXISTS idx_options_question ON options(question_id);
	CREATE INDEX IF NOT EXISTS idx_user_answers_question ON user_answers(question_id);
	CREATE INDEX IF NOT EXISTS idx_user_answers_time ON user_answers(answered_at);
	`

	_, err := d.conn.Exec(schema)
	if err != nil {
		return err
	}

	// Safe column migration for category_id
	_, _ = d.conn.Exec(`ALTER TABLE user_answers ADD COLUMN category_id INTEGER REFERENCES categories(id) ON DELETE SET NULL;`)
	_, _ = d.conn.Exec(`CREATE INDEX IF NOT EXISTS idx_user_answers_category ON user_answers(category_id);`)

	var count int
	if err := d.conn.QueryRow("SELECT COUNT(*) FROM categories").Scan(&count); err != nil {
		return err
	}

	if count == 0 {
		initialCategories := []struct {
			slug, name, desc, prompt string
		}{
			{
				slug: "python",
				name: "Python",
				desc: "Core syntax, data types, lists, dictionaries, strings, loops, and built-in functions.",
				prompt: "Topics: Python basics (variables, types, string slicing/methods, list comprehensions, dict/set methods, for/while loops, functions, args/kwargs, basic try/except, open() file operations, json/math/random libraries). Level: Junior practical. Questions should be short code snippets (4-8 lines) testing output prediction, return values, or identifying simple bugs.",
			},
			{
				slug: "golang",
				name: "Go",
				desc: "Slices, maps, structs, error handling, pointers, and foundational Go idioms.",
				prompt: "Topics: Go fundamentals (variables, struct definitions, slices and append(), maps, for range loops, functions returning multiple values/errors, defer basics, fmt formatting). Level: Junior practical. Short snippets (5-10 lines) testing code output, type checking, or slice behavior.",
			},
			{
				slug: "javascript",
				name: "JavaScript",
				desc: "Variables (let/const), arrow functions, array methods, objects, JSON, and Promises.",
				prompt: "Topics: Modern JavaScript (ES6+ variables let/const, arrow functions, template literals, array methods map/filter/reduce/find, object destructuring, JSON.parse/stringify, basic Promises/async-await, typeof/equality == vs ===). Level: Junior practical. Fast snippets (4-8 lines) testing return values, truthy/falsy, and array outputs.",
			},
			{
				slug: "php",
				name: "PHP",
				desc: "Arrays, string operations, functions, associative arrays, and core web patterns.",
				prompt: "Topics: PHP fundamentals (variables with $, array functions array_push/array_merge/in_array, associative arrays, foreach loops, string functions explode/implode/str_replace, isset/empty, basic functions, simple class/object properties). In PHP, string escape sequences like \\n MUST use double quotes (\"\\n\"), never single quotes ('\\n'). Level: Junior practical. Short snippets (4-8 lines) testing script output.",
			},
			{
				slug: "csharp",
				name: "C#",
				desc: ".NET fundamentals, types, classes, basic LINQ, lists, and string manipulation.",
				prompt: "Topics: C# & .NET fundamentals (types, strings, List<T>, arrays, foreach loops, basic LINQ queries like .Where() / .Select(), simple class/struct properties, nullables, switch expressions, method signatures). Level: Junior practical. Short clean snippets (5-10 lines) testing console output or return values.",
			},
			{
				slug: "powershell",
				name: "PowerShell",
				desc: "Cmdlets, pipelines, variables, loops, objects, and Windows scripting.",
				prompt: "Topics: PowerShell fundamentals (Get-Command, Get-Service, Get-Process, Where-Object, Select-Object, ForEach-Object, pipeline chaining |, variables with $, arrays, hash tables @{}, -eq / -match operators, string formatting). Level: Junior practical. Short 2-5 line commands/scripts testing command output or pipeline results.",
			},
			{
				slug: "cpp",
				name: "C++",
				desc: "Variables, vectors, basic pointers/references, functions, and standard I/O.",
				prompt: "Topics: C++ fundamentals (std::cout, std::vector, basic loops, functions, references & vs pointers *, pass by value vs reference, struct/class member access, simple string operations, basic header usage). Level: Junior practical. Short snippets (5-10 lines) testing output and basic variable tracing.",
			},
			{
				slug: "devops-linux",
				name: "DevOps & Linux",
				desc: "Linux commands, systemd, basic Docker, file permissions, and bash scripting.",
				prompt: "Topics: Practical Linux basics (chmod/chown, grep, sed, awk, find, curl, systemctl, journalctl, tar, ps/kill, Docker run/ps/logs/build, simple bash script loops and exit codes). Level: Junior practical. Direct questions on command usage, flags, or script output.",
			},
			{
				slug: "cisco-networking",
				name: "Cisco Networking",
				desc: "Cisco IOS configuration, IP addressing, static routing, VLANs, and CCNA essentials.",
				prompt: "Topics: Cisco CCNA fundamentals (Cisco IOS CLI modes, IPv4 subnetting strictly using dotted-decimal masks like 255.255.255.0, static route ip route 0.0.0.0 0.0.0.0 <next-hop>, basic VLANs switchport mode access/trunk, show ip interface brief, show ip route, basic ping/traceroute). Level: Junior practical. Fast CLI command recognition and troubleshooting.",
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

// PruneExpiredQuestions removes questions that meet BOTH conditions:
// 1. The user has submitted an answer to the question.
// 2. At least 1 day (24 hours) has elapsed since the answer was submitted.
// Questions that are unanswered OR solved less than 1 day ago remain strictly in the database.
func (d *DB) PruneExpiredQuestions() (int64, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	query := `
	DELETE FROM questions 
	WHERE id IN (
		SELECT q.id FROM questions q
		JOIN user_answers ua ON q.id = ua.question_id
		GROUP BY q.id
		HAVING MAX(ua.answered_at) <= datetime('now', '-1 day')
	);
	`
	res, err := d.conn.Exec(query)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// getCategoriesLocked queries categories assuming d.mu is already held.
func (d *DB) getCategoriesLocked() ([]models.Category, error) {
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

	categories := make([]models.Category, 0)
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

// GetCategories returns all active categories along with counts of total, solved, and available questions.
func (d *DB) GetCategories() ([]models.Category, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.getCategoriesLocked()
}

// CreateCategory inserts a new category and returns its ID.
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

// GetCategoryBySlug fetches a single category by its slug identifier.
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

// GetCategoryByID fetches a category by its numeric ID.
func (d *DB) GetCategoryByID(id int64) (*models.Category, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	var c models.Category
	var activeInt int
	var createdAt string
	err := d.conn.QueryRow(`
		SELECT id, slug, name, description, prompt_instructions, is_active, created_at
		FROM categories WHERE id = ?
	`, id).Scan(&c.ID, &c.Slug, &c.Name, &c.Description, &c.PromptInstructions, &activeInt, &createdAt)
	if err != nil {
		return nil, err
	}
	c.IsActive = activeInt == 1
	c.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdAt)
	return &c, nil
}

// UpdateCategory modifies an existing category's name, description, and prompt instructions.
func (d *DB) UpdateCategory(id int64, name, description, promptInstructions string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	_, err := d.conn.Exec(`
		UPDATE categories
		SET name = ?, description = ?, prompt_instructions = ?
		WHERE id = ?
	`, name, description, promptInstructions, id)
	return err
}

// GetRandomQuestion selects a random unsolved question matching criteria, with repetition fallback.
func (d *DB) GetRandomQuestion(categorySlug, difficulty string) (*models.Question, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	whereClauses := []string{
		"q.id NOT IN (SELECT question_id FROM user_answers WHERE question_id IS NOT NULL)",
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

	// If no unsolved questions remain, pull from questions currently in the DB for review
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
	q.Options = make([]models.Option, 0)

	// Fetch option choices
	optRows, err := d.conn.Query(`
		SELECT id, question_id, option_text, is_correct, order_idx
		FROM options
		WHERE question_id = ?
		ORDER BY order_idx ASC
	`, q.ID)
	if err != nil {
		return nil, err
	}

	for optRows.Next() {
		var o models.Option
		var isCorrectInt int
		if err := optRows.Scan(&o.ID, &o.QuestionID, &o.OptionText, &isCorrectInt, &o.OrderIndex); err == nil {
			o.IsCorrect = isCorrectInt == 1
			if o.IsCorrect {
				q.ExpectedCorrectCount++
			}
			q.Options = append(q.Options, o)
		}
	}
	optRows.Close()

	return &q, nil
}

// CalculateHash creates a SHA-256 digest of normalized question text and code snippet.
func CalculateHash(questionText, codeSnippet string) string {
	normalQuestion := strings.ToLower(strings.TrimSpace(questionText))
	normalCode := strings.TrimSpace(codeSnippet)
	h := sha256.New()
	h.Write([]byte(normalQuestion + "|" + normalCode))
	return hex.EncodeToString(h.Sum(nil))
}

// SaveGeneratedQuestion writes an AI-generated question to the database with deduplication.
func (d *DB) SaveGeneratedQuestion(dto models.GeneratedQuestionDTO, categoryID int64) (bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	hash := CalculateHash(dto.Question, dto.Code)

	var existsID int64
	err := d.conn.QueryRow("SELECT id FROM questions WHERE hash = ?", hash).Scan(&existsID)
	if err == nil {
		// Duplicate question already exists
		return false, nil
	}

	tx, err := d.conn.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()

	cleanCode := strings.TrimSpace(dto.Code)
	if strings.HasPrefix(cleanCode, "```") {
		lines := strings.Split(cleanCode, "\n")
		if len(lines) >= 2 && strings.HasPrefix(lines[0], "```") && strings.HasSuffix(lines[len(lines)-1], "```") {
			cleanCode = strings.TrimSpace(strings.Join(lines[1:len(lines)-1], "\n"))
		}
	}
	dto.Code = cleanCode

	if len(dto.Code) > 0 {
		lowerQ := strings.ToLower(dto.Question)
		isWhichCmd := strings.Contains(lowerQ, "which command") ||
			strings.Contains(lowerQ, "what command") ||
			strings.Contains(lowerQ, "which of the following command") ||
			strings.Contains(lowerQ, "which powershell command") ||
			strings.Contains(lowerQ, "which code snippet") ||
			strings.Contains(lowerQ, "which snippet")

		isAnalysis := strings.Contains(lowerQ, "output of") ||
			strings.Contains(lowerQ, "what is the output") ||
			strings.Contains(lowerQ, "error") ||
			strings.Contains(lowerQ, "bug")

		normCode := strings.Join(strings.Fields(dto.Code), " ")
		for _, opt := range dto.Options {
			normOpt := strings.Join(strings.Fields(opt), " ")
			if normCode == normOpt || dto.Code == strings.TrimSpace(opt) {
				dto.Code = ""
				dto.Language = ""
				break
			}
		}

		if dto.Code != "" && isWhichCmd && !isAnalysis {
			dto.Code = ""
			dto.Language = ""
		}
	}

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

// SubmitAnswer evaluates selected options and calculates streak and total score.
func (d *DB) SubmitAnswer(questionID int64, selectedIDs []int64) (*models.SubmitAnswerResponse, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	var explanation string
	var categoryID int64
	err := d.conn.QueryRow("SELECT explanation, category_id FROM questions WHERE id = ?", questionID).Scan(&explanation, &categoryID)
	if err != nil {
		return nil, err
	}

	rows, err := d.conn.Query("SELECT id FROM options WHERE question_id = ? AND is_correct = 1", questionID)
	if err != nil {
		return nil, err
	}

	var correctIDs []int64
	correctMap := make(map[int64]bool)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err == nil {
			correctIDs = append(correctIDs, id)
			correctMap[id] = true
		}
	}
	rows.Close() // Explicitly close rows immediately!

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
		INSERT INTO user_answers (question_id, category_id, selected_option_ids, is_correct)
		VALUES (?, ?, ?, ?)
	`, questionID, categoryID, string(selectedJSON), isCorrectInt)
	if err != nil {
		return nil, err
	}

	var streak int
	streakRows, err := d.conn.Query(`SELECT is_correct FROM user_answers ORDER BY answered_at DESC, id DESC LIMIT 50`)
	if err == nil {
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
		streakRows.Close() // Explicitly close immediately!
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

// SyncAnswers records multiple answers solved while disconnected.
func (d *DB) SyncAnswers(items []models.SyncAnswerItem) (*models.SyncAnswersResponse, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	synced := 0
	for _, item := range items {
		var categoryID int64
		err := d.conn.QueryRow("SELECT category_id FROM questions WHERE id = ?", item.QuestionID).Scan(&categoryID)
		if err != nil {
			continue // question deleted or missing
		}

		rows, err := d.conn.Query("SELECT id FROM options WHERE question_id = ? AND is_correct = 1", item.QuestionID)
		if err != nil {
			continue
		}
		var correctIDs []int64
		correctMap := make(map[int64]bool)
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err == nil {
				correctIDs = append(correctIDs, id)
				correctMap[id] = true
			}
		}
		rows.Close()

		isAllCorrect := len(item.SelectedOptionIDs) == len(correctIDs)
		if isAllCorrect {
			for _, selID := range item.SelectedOptionIDs {
				if !correctMap[selID] {
					isAllCorrect = false
					break
				}
			}
		}

		selectedJSON, _ := json.Marshal(item.SelectedOptionIDs)
		isCorrectInt := 0
		if isAllCorrect {
			isCorrectInt = 1
		}

		answeredTime := item.AnsweredAt
		if answeredTime.IsZero() {
			answeredTime = time.Now()
		}
		timeStr := answeredTime.Format("2006-01-02 15:04:05")

		_, err = d.conn.Exec(`
			INSERT INTO user_answers (question_id, category_id, selected_option_ids, is_correct, answered_at)
			VALUES (?, ?, ?, ?, ?)
		`, item.QuestionID, categoryID, string(selectedJSON), isCorrectInt, timeStr)
		if err == nil {
			synced++
		}
	}

	var streak int
	streakRows, err := d.conn.Query(`SELECT is_correct FROM user_answers ORDER BY answered_at DESC, id DESC LIMIT 50`)
	if err == nil {
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
		streakRows.Close()
	}

	var totalScore int
	_ = d.conn.QueryRow(`SELECT COALESCE(SUM(CASE WHEN is_correct = 1 THEN 10 ELSE 0 END), 0) FROM user_answers`).Scan(&totalScore)

	stats := &models.UserStats{
		CurrentStreak: streak,
		TotalScore:    totalScore,
		ByCategory:    make([]models.CategoryStat, 0),
	}
	_ = d.conn.QueryRow(`
		SELECT 
			COUNT(*),
			COALESCE(SUM(CASE WHEN is_correct = 1 THEN 1 ELSE 0 END), 0)
		FROM user_answers
	`).Scan(&stats.TotalAnswered, &stats.TotalCorrect)
	if stats.TotalAnswered > 0 {
		stats.AccuracyPercent = math.Round(float64(stats.TotalCorrect) / float64(stats.TotalAnswered) * 100.0)
	}

	return &models.SyncAnswersResponse{
		SyncedCount: synced,
		TotalScore:  totalScore,
		Streak:      streak,
		UserStats:   stats,
	}, nil
}

// GetOfflinePack packages all active categories and their challenges for offline client storage.
func (d *DB) GetOfflinePack() (*models.OfflinePack, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	categories, err := d.getCategoriesLocked()
	if err != nil {
		return nil, err
	}

	answeredMap := make(map[int64]bool)
	ansRows, err := d.conn.Query("SELECT DISTINCT question_id FROM user_answers WHERE question_id IS NOT NULL")
	if err == nil {
		for ansRows.Next() {
			var qID int64
			if err := ansRows.Scan(&qID); err == nil {
				answeredMap[qID] = true
			}
		}
		ansRows.Close()
	}

	qQuery := `
		SELECT q.id, q.category_id, c.slug, c.name, q.type, q.difficulty,
		       q.question_text, q.code_snippet, q.language, q.explanation, q.created_at
		FROM questions q
		JOIN categories c ON q.category_id = c.id
		WHERE c.is_active = 1
		ORDER BY q.id ASC
	`
	qRows, err := d.conn.Query(qQuery)
	if err != nil {
		return nil, err
	}

	questions := make([]models.OfflineQuestion, 0)
	qMap := make(map[int64]*models.OfflineQuestion)

	for qRows.Next() {
		var oq models.OfflineQuestion
		var createdAt string
		err := qRows.Scan(
			&oq.ID, &oq.CategoryID, &oq.CategorySlug, &oq.CategoryName,
			&oq.Type, &oq.Difficulty, &oq.QuestionText, &oq.CodeSnippet,
			&oq.Language, &oq.Explanation, &createdAt,
		)
		if err != nil {
			qRows.Close()
			return nil, err
		}
		oq.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdAt)
		oq.Options = make([]models.Option, 0)
		oq.CorrectOptionIDs = make([]int64, 0)
		oq.IsSolved = answeredMap[oq.ID]
		questions = append(questions, oq)
	}
	qRows.Close()

	for i := range questions {
		qMap[questions[i].ID] = &questions[i]
	}

	optRows, err := d.conn.Query(`
		SELECT id, question_id, option_text, is_correct, order_idx
		FROM options
		ORDER BY question_id ASC, order_idx ASC
	`)
	if err == nil {
		for optRows.Next() {
			var o models.Option
			var isCorrectInt int
			if err := optRows.Scan(&o.ID, &o.QuestionID, &o.OptionText, &isCorrectInt, &o.OrderIndex); err == nil {
				o.IsCorrect = isCorrectInt == 1
				if q, ok := qMap[o.QuestionID]; ok {
					q.Options = append(q.Options, o)
					if o.IsCorrect {
						q.CorrectOptionIDs = append(q.CorrectOptionIDs, o.ID)
						q.ExpectedCorrectCount++
					}
				}
			}
		}
		optRows.Close()
	}

	return &models.OfflinePack{
		Categories: categories,
		Questions:  questions,
		SyncedAt:   time.Now(),
	}, nil
}

// DeleteQuestion removes a question and cascading records.
func (d *DB) DeleteQuestion(questionID int64) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	_, err := d.conn.Exec("DELETE FROM questions WHERE id = ?", questionID)
	return err
}

// GetUserStats calculates performance metrics including accuracy and category statistics.
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
		streakRows.Close() // Explicitly close immediately!
	}

	// Persistent category metrics based on user_answers even after questions expire
	catRows, err := d.conn.Query(`
		SELECT 
			c.name, c.slug,
			COUNT(ua.id) AS answered,
			SUM(CASE WHEN ua.is_correct = 1 THEN 1 ELSE 0 END) AS correct
		FROM categories c
		JOIN user_answers ua ON c.id = ua.category_id
		GROUP BY c.id
	`)
	if err == nil {
		for catRows.Next() {
			var cs models.CategoryStat
			var corr sql.NullInt64
			if err := catRows.Scan(&cs.CategoryName, &cs.CategorySlug, &cs.TotalAnswered, &corr); err == nil {
				cs.TotalCorrect = int(corr.Int64)
				if cs.TotalAnswered > 0 {
					cs.AccuracyPercent = math.Round(float64(cs.TotalCorrect) / float64(cs.TotalAnswered) * 100.0)
				}
				stats.ByCategory = append(stats.ByCategory, cs)
			}
		}
		catRows.Close() // Explicitly close immediately!
	}

	return stats, nil
}

// ResetStats clears all recorded user answers, resetting scores and progress.
func (d *DB) ResetStats() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	_, err := d.conn.Exec(`DELETE FROM user_answers`)
	return err
}
