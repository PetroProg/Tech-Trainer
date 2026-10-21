package ollama

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/PetroProg/tech-trainer/internal/models"
)

var prefixRegex = regexp.MustCompile(`^(?i)[A-D1-4][\.\)\:\-]\s*`)
var ciscoCidrRegex = regexp.MustCompile(`(?i)ip\s+address\s+[0-9\.]+\s*/\d+`)

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

// UnloadModel frees GPU VRAM immediately by evicting the model from memory.
func (c *Client) UnloadModel(ctx context.Context) error {
	payload := map[string]interface{}{
		"model":      c.modelName,
		"keep_alive": 0,
	}
	data, _ := json.Marshal(payload)
	reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, "POST", c.baseURL+"/api/generate", bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
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

// GenerateQuestion requests a structured technical question from Ollama with strict quality validation.
func (c *Client) GenerateQuestion(ctx context.Context, categoryName, promptInstructions, difficulty string) (*models.GeneratedQuestionDTO, error) {
	systemPrompt := `You are a Principal Software Engineer and Technical Certification Examination Architect (Cisco CCNA/CCNP, AWS, Linux Foundation, CKA, Core Language teams).
Your mission is to formulate exactly 1 high-quality, practical, 100% factually accurate engineering examination challenge.

STRICT RULES & CONSTRAINTS:
1. Difficulty: Junior or Moderate Middle. Realistic real-world situations, configuration syntax, troubleshooting, or code tracing.
2. Language: Professional, concise, grammatical English.
3. Options: Exactly 4 options.
4. Correct Answers Policy:
   - For ~75% of questions: exactly 1 correct answer (single-choice). correct_indices: [X].
   - For ~25% of questions: exactly 2 correct answers (multi-select). correct_indices: [X, Y].
   - NEVER generate 0, 3, or 4 correct answers.
   - MANDATORY CLARITY: If exactly 2 options are correct, the question text MUST end with " (Choose two)".
   - If only 1 option is correct, the question MUST NOT say "select all that apply" or "choose two". Phrasing must be "Which command...", "What is the result...", or "(Choose one)".
5. Option Uniqueness & Formatting:
   - ALL 4 options MUST BE COMPLETELY UNIQUE in text, syntax, and meaning. NEVER duplicate options!
   - DO NOT prefix options with letters or numbers (e.g. do NOT write "A. ", "B. ", "1. ", "Option 1:"). Write only the raw command, code, or answer text.
   - All distractors (incorrect options) must be realistic, plausible mistakes that people commonly make, but definitively invalid/incorrect.
6. Domain Truth & Anti-Hallucination:
   - Cisco / Networking:
     * IPv4 interface address assignment strictly requires dotted-decimal subnet masks: "ip address 192.168.1.1 255.255.255.0". Prefix slash notation (e.g. "/24") is NEVER valid on Cisco IOS IPv4 interfaces!
     * "ip default-gateway" is only for Layer 2 switches or devices with "no ip routing". On a routing Cisco router, use a default static route: "ip route 0.0.0.0 0.0.0.0 <next-hop>".
     * Always distinguish between global config mode, interface mode, and line mode.
   - Code & Linux:
     * Use genuine syntax and standard library behaviors.
7. Explanation:
   - Detailed, rigorous explanation: explain why the correct choice(s) work under the hood, and explicitly explain why each distractor is wrong.

STRICT JSON OUTPUT FORMAT (NO EXTRA KEYS):
{
  "type": "multiple_choice",
  "difficulty": "junior",
  "question": "Question text here... (Choose two if 2 answers are correct)",
  "code": "",
  "language": "",
  "options": [
    "First unique option text",
    "Second unique option text",
    "Third unique option text",
    "Fourth unique option text"
  ],
  "correct_indices": [0],
  "explanation": "Detailed explanation..."
}`

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

	// 1. Structure validation
	if len(dto.Options) != 4 {
		return nil, fmt.Errorf("invalid options count: %d (must be 4)", len(dto.Options))
	}
	if len(dto.CorrectIndices) < 1 || len(dto.CorrectIndices) > 2 {
		return nil, fmt.Errorf("invalid correct_indices count: %d (must be 1 or 2)", len(dto.CorrectIndices))
	}

	// 2. Validate index bounds
	correctSet := make(map[int]bool)
	for _, idx := range dto.CorrectIndices {
		if idx < 0 || idx >= 4 {
			return nil, fmt.Errorf("correct index %d out of bounds (0-3)", idx)
		}
		correctSet[idx] = true
	}

	// 3. Clean options: strip leading A., B., 1., etc., and trim whitespace
	seenOptions := make(map[string]bool)
	for i := range dto.Options {
		cleaned := strings.TrimSpace(dto.Options[i])
		cleaned = prefixRegex.ReplaceAllString(cleaned, "")
		cleaned = strings.TrimSpace(cleaned)
		if len(cleaned) == 0 {
			return nil, fmt.Errorf("option %d is empty", i)
		}
		lowerKey := strings.ToLower(cleaned)
		if seenOptions[lowerKey] {
			return nil, fmt.Errorf("duplicate option detected: %q", cleaned)
		}
		seenOptions[lowerKey] = true
		dto.Options[i] = cleaned
	}

	// 4. Domain specific anti-hallucination checks for Cisco / Networking
	isCisco := strings.Contains(strings.ToLower(categoryName), "cisco") ||
		strings.Contains(strings.ToLower(categoryName), "network") ||
		strings.Contains(strings.ToLower(promptInstructions), "cisco")
	if isCisco {
		for i, opt := range dto.Options {
			// Check if model hallucinated /24 or /CIDR on Cisco interface ip address
			if correctSet[i] && ciscoCidrRegex.MatchString(opt) {
				return nil, fmt.Errorf("cisco hallucination: /CIDR mask in ip address on interface: %q", opt)
			}
			// Check if model put ip default-gateway inside interface config mode
			if correctSet[i] && strings.Contains(strings.ToLower(opt), "config-if") && strings.Contains(strings.ToLower(opt), "ip default-gateway") {
				return nil, fmt.Errorf("cisco hallucination: ip default-gateway in config-if: %q", opt)
			}
		}
	}

	// 5. Ensure question text has appropriate (Choose two) hint if 2 correct answers
	dto.Question = strings.TrimSpace(dto.Question)
	if len(dto.CorrectIndices) == 2 {
		lowerQ := strings.ToLower(dto.Question)
		if !strings.Contains(lowerQ, "choose two") && !strings.Contains(lowerQ, "select two") && !strings.Contains(lowerQ, "select 2") && !strings.Contains(lowerQ, "which two") {
			dto.Question += " (Choose two)"
		}
	} else if len(dto.CorrectIndices) == 1 {
		// If text says "choose two" or "which two" but only 1 index is correct: reject!
		lowerQ := strings.ToLower(dto.Question)
		if strings.Contains(lowerQ, "which two") || strings.Contains(lowerQ, "choose two") || strings.Contains(lowerQ, "select two") {
			return nil, fmt.Errorf("mismatch: question says choose two, but correct_indices has 1 element")
		}
	}

	return &dto, nil
}
