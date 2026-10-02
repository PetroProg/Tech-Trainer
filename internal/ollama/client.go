package ollama

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/PetroProg/tech-trainer/internal/models"
)

type Client struct {
	baseURL    string
	modelName  string
	httpClient *http.Client
}

func NewClient(baseURL, modelName string) *Client {
	return &Client{
		baseURL:   strings.TrimRight(baseURL, "/"),
		modelName: modelName,
		httpClient: &http.Client{
			Timeout: 120 * time.Second, // Инференс 7B модели
		},
	}
}

func (c *Client) Ping(ctx context.Context) bool {
	reqCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, "GET", c.baseURL+"/api/tags", nil)
	if err != nil {
		return false
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	return resp.StatusCode == http.StatusOK
}

type generateRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
	System string `json:"system"`
	Format string `json:"format"`
	Stream bool   `json:"stream"`
}

type generateResponse struct {
	Response string `json:"response"`
}

func (c *Client) GenerateQuestion(ctx context.Context, categoryName, promptInstructions, difficulty string) (*models.GeneratedQuestionDTO, error) {
	systemPrompt := "Ты — экспертный технический экзаменатор. Твоя задача — составить 1 практическую задачу для программистов и инженеров.\n\n" +
		"ПРАВИЛА И ТРЕБОВАНИЯ:\n" +
		"1. Уровень сложности: Junior или Light-Middle (прикладные задачи, без академизма и без сложного Senior-уровня).\n" +
		"2. Язык вопроса, вариантов и объяснения — РУССКИЙ (технические термины и код на английском).\n" +
		"3. Вариантов ответа ДОЛЖНО БЫТЬ РОВНО 4.\n" +
		"4. ВАЖНО: Правильных ответов может быть ОТ 1 ДО 4 (один, два, три или все четыре). Не делай всегда 1 правильный ответ! Периодически создавай вопросы с 2 или 3 правильными ответами.\n" +
		"5. Поле correct_indices — массив 0-based индексов правильных ответов (например, [0], или [1, 3], или [0, 1, 2]).\n" +
		"6. Поле type — \"multiple_choice\" (вопрос без кода) или \"code_analysis\" (разбор кода, поиск бага или что выведет код).\n" +
		"7. Если есть код — укажи его в поле code без обратных кавычек, а в language укажи язык (python, go, bash, rust, sql, dockerfile, json). Если кода нет — code: \"\".\n" +
		"8. В поле explanation дай подробный разбор: почему правильные ответы верны, что происходит под капотом, и почему остальные варианты неверны.\n\n" +
		"ОТВЕТ ДОЛЖЕН БЫТЬ СТРОГО В ФОРМАТЕ JSON:\n" +
		"{\n" +
		"  \"type\": \"code_analysis\",\n" +
		"  \"difficulty\": \"junior\",\n" +
		"  \"question\": \"Текст вопроса...\",\n" +
		"  \"code\": \"фрагмент кода...\",\n" +
		"  \"language\": \"go\",\n" +
		"  \"options\": [\n" +
		"    \"Вариант 1\",\n" +
		"    \"Вариант 2\",\n" +
		"    \"Вариант 3\",\n" +
		"    \"Вариант 4\"\n" +
		"  ],\n" +
		"  \"correct_indices\": [1, 2],\n" +
		"  \"explanation\": \"Подробное объяснение решения...\"\n" +
		"}"

	userPrompt := fmt.Sprintf("Категория: %s\nИнструкции по тематике: %s\nЖелаемая сложность: %s\n\nСоставь интересную прикладную задачу строго в указанном формате JSON.",
		categoryName, promptInstructions, difficulty)

	reqBody := generateRequest{
		Model:  c.modelName,
		Prompt: userPrompt,
		System: systemPrompt,
		Format: "json",
		Stream: false,
	}

	jsonBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/api/generate", bytes.NewReader(jsonBytes))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ollama request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ollama returned status %d: %s", resp.StatusCode, string(body))
	}

	var genResp generateResponse
	if err := json.NewDecoder(resp.Body).Decode(&genResp); err != nil {
		return nil, fmt.Errorf("failed to decode ollama response: %w", err)
	}

	var dto models.GeneratedQuestionDTO
	cleanJSON := strings.TrimSpace(genResp.Response)
	if strings.HasPrefix(cleanJSON, "```json") {
		cleanJSON = strings.TrimPrefix(cleanJSON, "```json")
		cleanJSON = strings.TrimSuffix(cleanJSON, "```")
	} else if strings.HasPrefix(cleanJSON, "```") {
		cleanJSON = strings.TrimPrefix(cleanJSON, "```")
		cleanJSON = strings.TrimSuffix(cleanJSON, "```")
	}
	cleanJSON = strings.TrimSpace(cleanJSON)

	if err := json.Unmarshal([]byte(cleanJSON), &dto); err != nil {
		return nil, fmt.Errorf("failed to parse generated JSON: %w (raw: %s)", err, cleanJSON)
	}

	if len(dto.Options) != 4 || len(dto.CorrectIndices) == 0 {
		return nil, fmt.Errorf("invalid question structure: options count=%d, correct count=%d", len(dto.Options), len(dto.CorrectIndices))
	}

	return &dto, nil
}
