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

// Client handles communication with the Ollama HTTP API.
type Client struct {
	baseURL    string
	modelName  string
	httpClient *http.Client
}

// NewClient creates a new Ollama API client.
func NewClient(baseURL, modelName string) *Client {
	return &Client{
		baseURL:   strings.TrimRight(baseURL, "/"),
		modelName: modelName,
		httpClient: &http.Client{
			Timeout: 120 * time.Second, // 7B inference timeout
		},
	}
}

// Ping checks if the Ollama service is reachable and responsive.
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

// GenerateQuestion requests a structured technical question from Ollama.
func (c *Client) GenerateQuestion(ctx context.Context, categoryName, promptInstructions, difficulty string) (*models.GeneratedQuestionDTO, error) {
	systemPrompt := "You are a Principal Software Engineer and Technical Examiner. Your mission is to formulate exactly 1 high-quality practical technical challenge for software engineers.\n\n" +
		"RULES AND CONSTRAINTS:\n" +
		"1. Difficulty Level: Junior or Light-Middle (practical, real-world applied questions; avoid pedantic trivia or overly complex algorithmic puzzles).\n" +
		"2. Language: All text (question, options, explanation) must be in clear, professional ENGLISH.\n" +
		"3. Number of Options: There MUST be EXACTLY 4 options.\n" +
		"4. Correct Answers: Between 1 and 4 correct answers (single choice OR multiple choice). Vary between 1, 2, or 3 correct answers to test deep understanding.\n" +
		"5. correct_indices field: A 0-based array of indices of the correct options (e.g. [0], [1, 3], or [0, 2, 3]).\n" +
		"6. type field: 'multiple_choice' (conceptual or architectural question without code) or 'code_analysis' (snippet bug hunt, execution trace, or runtime behavior).\n" +
		"7. If code is present, put pure code in the 'code' field WITHOUT markdown triple backticks. In 'language', specify the language (go, python, bash, rust, sql, dockerfile, json). If no code is needed, set code to empty string \"\".\n" +
		"8. In 'explanation', provide an in-depth breakdown: why the correct choices are valid, what happens under the hood, and why incorrect choices fail.\n\n" +
		"RESPONSE MUST BE STRICT JSON MATCHING THIS EXACT SCHEMA:\n" +
		"{\n" +
		"  \"type\": \"code_analysis\",\n" +
		"  \"difficulty\": \"junior\",\n" +
		"  \"question\": \"Question text here...\",\n" +
		"  \"code\": \"snippet code here...\",\n" +
		"  \"language\": \"go\",\n" +
		"  \"options\": [\n" +
		"    \"Option 1 text\",\n" +
		"    \"Option 2 text\",\n" +
		"    \"Option 3 text\",\n" +
		"    \"Option 4 text\"\n" +
		"  ],\n" +
		"  \"correct_indices\": [1, 2],\n" +
		"  \"explanation\": \"Detailed technical explanation...\"\n" +
		"}"

	userPrompt := fmt.Sprintf("Category: %s\nTopic guidelines: %s\nTarget difficulty: %s\n\nFormulate a practical engineering challenge strictly matching the JSON format.",
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
