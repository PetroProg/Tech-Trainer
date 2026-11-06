package gemini

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

var (
	prefixRegex      = regexp.MustCompile(`^(?i)(?:option\s*)?[A-D1-4][\.\)\:\-]\s*`)
	dummyOptionRegex = regexp.MustCompile(`^(?i)option\s+[a-d0-9]$`)
	ciscoCidrRegex   = regexp.MustCompile(`(?i)ip\s+address\s+[0-9\.]+\s*/\d+`)
	fenceRegex       = regexp.MustCompile("(?s)```([a-zA-Z0-9_\\-#+]*)\\n?(.*?)\\n?```")
	choiceHintRegex  = regexp.MustCompile(`(?i)\s*\((?:choose|select)\s*(?:one|two|three|four|[1-4])\)\s*`)
)

// Client handles communication with Google Gemini Generative Language REST API.
type Client struct {
	baseURL    string
	apiKey     string
	modelName  string
	httpClient *http.Client
}

// NewClient creates a new Google Gemini API client.
func NewClient(baseURL, apiKey, modelName string) *Client {
	cleanBaseURL := strings.TrimRight(baseURL, "/")
	if cleanBaseURL == "" {
		cleanBaseURL = "https://generativelanguage.googleapis.com/v1beta"
	}
	cleanModel := strings.TrimSpace(modelName)
	if cleanModel == "" {
		cleanModel = "gemini-3.5-flash-lite"
	}
	// Strip "models/" prefix if user configured it with prefix
	cleanModel = strings.TrimPrefix(cleanModel, "models/")

	return &Client{
		baseURL:   cleanBaseURL,
		apiKey:    strings.TrimSpace(apiKey),
		modelName: cleanModel,
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

// GetModelName returns the configured model identifier.
func (c *Client) GetModelName() string {
	return c.modelName
}

// UnloadModel is a no-op for cloud-hosted Gemini (no local GPU VRAM to free).
func (c *Client) UnloadModel(ctx context.Context) error {
	return nil
}

// Ping checks if the Gemini API is reachable with the configured API key and model.
func (c *Client) Ping(ctx context.Context) bool {
	reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	url := fmt.Sprintf("%s/models/%s?key=%s", c.baseURL, c.modelName, c.apiKey)
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, url, nil)
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

type geminiPart struct {
	Text string `json:"text"`
}

type geminiContent struct {
	Role  string       `json:"role,omitempty"`
	Parts []geminiPart `json:"parts"`
}

type geminiGenerationConfig struct {
	ResponseMimeType string  `json:"responseMimeType"`
	Temperature      float64 `json:"temperature"`
}

type geminiRequest struct {
	Contents         []geminiContent        `json:"contents"`
	GenerationConfig geminiGenerationConfig `json:"generationConfig"`
}

type geminiResponse struct {
	Candidates []struct {
		Content struct {
			Parts []struct {
				Text string `json:"text"`
			} `json:"parts"`
			Role string `json:"role"`
		} `json:"content"`
		FinishReason string `json:"finishReason"`
		Index        int    `json:"index"`
	} `json:"candidates"`
	Error *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Status  string `json:"status"`
	} `json:"error,omitempty"`
}

// GenerateQuestion requests a structured technical question from Google Gemini API with strict validation.
func (c *Client) GenerateQuestion(ctx context.Context, categoryName, promptInstructions, difficulty string) (*models.GeneratedQuestionDTO, error) {
	diffLevel := strings.ToLower(strings.TrimSpace(difficulty))
	if diffLevel != "middle" {
		diffLevel = "junior"
	}

	systemPrompt := `You are a Principal Software Engineer and Technical Certification Examination Architect (Cisco CCIE/CCNP, Linux Foundation, Docker/CNCF CKA, and Senior Language Runtimes).
Your mission is to formulate exactly 1 high-quality, practical, factually accurate engineering examination challenge.

CORE ARCHITECTURE & DIVERSITY:
1. Difficulty Handling:
   - When Target difficulty is "middle": Produce substantive, production-grade challenges. Focus on system internals, protocol states, edge cases, subtle bugs, performance trade-offs, configuration pitfalls, and diagnostic reasoning.
   - When Target difficulty is "junior": Focus on foundational syntax, core standard library APIs, basic logic, and essential commands. Keep it clean and accessible.

2. Question Formats (Balanced Mix):
   - You MUST vary between TWO distinct question formats:
     A) Pure Conceptual / Theoretical / Architectural ("type": "multiple_choice"):
        * Deep multiple-choice questions on system behavior, protocol mechanics, architecture, or operational troubleshooting.
        * For these questions, "code" MUST be empty "" and "language" MUST be "". DO NOT invent fake or unnecessary code snippets!
     B) Practical Code / Configuration Analysis ("type": "code_analysis"):
        * The learner inspects a concise, realistic code or configuration snippet (4-12 lines) placed in "code" (e.g. Cisco IOS config, Dockerfile, systemd unit, bash script, language snippet).

3. STRICT ANTI-CLICHE BLACKLIST (CRITICAL):
   - NEVER generate trivial beginner toy loops (e.g. filtering even/odd numbers, summing array items, doubling numbers, reversing a string, printing 1 to N).
   - NEVER generate repetitive single-command recall questions (e.g. "Which Docker command lists running containers? -> docker ps", "Which flag maps a port? -> -p", "Which command views interfaces? -> show ip interface brief").
   - NEVER duplicate identical problem patterns across different programming languages.
   - For Infrastructure (Cisco, Docker, Linux): test real operational mechanisms (e.g. OSPF adjacency states, STP BPDU guard, 802.1Q subinterfaces, multi-stage build caching, PID 1 zombie reaping, Linux process signals, inodes, iptables chains).
   - For DevOps & Linux: ONLY use bash, Dockerfile, yaml, systemd unit files, or terminal diagnostics. NEVER generate application source code (Go, Java, C#, C++ SDKs).

4. Code Snippet Rules & Anti-Spoiler:
   - NEVER place markdown code blocks (triple backticks) in the "question" field! The "question" field must contain ONLY plain text instructions.
   - If there is code/config to inspect, place it strictly in the "code" field without markdown fences.
   - If the question asks "Which command..." or "Which code snippet...", candidate choices belong in the "options" array, NOT in "code".
   - Code snippets must be syntactically complete. Never truncate or cut off code mid-line.
   - CONTRACT INTEGRITY: NEVER ask "Which of the following code snippets accomplishes X?" if the options are scalar values, numbers, or boolean results. If testing evaluation or output, phrase the question as "What is the output of the following code snippet?" and place the snippet in "code". If asking which code snippet accomplishes X, each option MUST be an actual code snippet, and any shared variables or context must be fully self-contained.

5. Options Policy:
   - Exactly 4 completely unique options.
   - NEVER use placeholder option names like "Option A", "Option B", "Option 1", "Option 2".
   - NEVER prefix options with letters or numbers ("A. ", "1. ", "Option A:").
   - NEVER wrap individual options in markdown backticks or code fences .
   - 1 or 2 correct answers (~75% single-choice with 1 correct, ~25% multi-select with 2 correct).
   - If exactly 2 options are correct, question text MUST end with " (Choose two)".
   - If only 1 option is correct, question MUST NOT say "choose two" or "select all".

6. Domain Truth & Anti-Hallucination:
   - Cisco: IPv4 interface IP assignment strictly requires dotted-decimal masks (e.g. 255.255.255.0). Slash /CIDR notation is invalid on Cisco IOS interfaces. Default gateway on a router is configured via static route (e.g. "ip route 0.0.0.0 0.0.0.0 <next-hop>"), never "ip default-gateway".
   - Code & Linux: Genuine syntax and standard library behaviors only.

7. Explanation:
   - Rigorous explanation: why the correct choice(s) work under the hood, and why each distractor is wrong.

STRICT JSON OUTPUT FORMAT (NO EXTRA KEYS):
{
  "type": "multiple_choice",
  "difficulty": "middle",
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

	userPrompt := fmt.Sprintf("Category: %s\nTopic guidelines: %s\nTarget difficulty: %s\n\nFormulate a high-quality engineering challenge strictly matching the JSON format.",
		categoryName, promptInstructions, diffLevel)

	reqBody := geminiRequest{
		Contents: []geminiContent{
			{
				Parts: []geminiPart{
					{Text: systemPrompt + "\n\n" + userPrompt},
				},
			},
		},
		GenerationConfig: geminiGenerationConfig{
			ResponseMimeType: "application/json",
			Temperature:      0.2,
		},
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal gemini request: %w", err)
	}

	url := fmt.Sprintf("%s/models/%s:generateContent?key=%s", c.baseURL, c.modelName, c.apiKey)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create gemini request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("gemini api call failed: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read gemini response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("gemini api returned status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var geminiResp geminiResponse
	if err := json.Unmarshal(bodyBytes, &geminiResp); err != nil {
		return nil, fmt.Errorf("failed to decode gemini response wrapper: %w", err)
	}

	if geminiResp.Error != nil {
		return nil, fmt.Errorf("gemini error: %s (code %d)", geminiResp.Error.Message, geminiResp.Error.Code)
	}

	if len(geminiResp.Candidates) == 0 || len(geminiResp.Candidates[0].Content.Parts) == 0 {
		return nil, fmt.Errorf("gemini returned no content candidates")
	}

	rawJSON := geminiResp.Candidates[0].Content.Parts[0].Text
	rawJSON = strings.TrimSpace(rawJSON)

	// Strip potential markdown code fences from JSON output
	if strings.HasPrefix(rawJSON, "```") {
		firstNewline := strings.Index(rawJSON, "\n")
		if firstNewline != -1 {
			rawJSON = rawJSON[firstNewline+1:]
		}
		if idx := strings.LastIndex(rawJSON, "```"); idx != -1 {
			rawJSON = rawJSON[:idx]
		}
		rawJSON = strings.TrimSpace(rawJSON)
	}

	var dto models.GeneratedQuestionDTO
	if err := json.Unmarshal([]byte(rawJSON), &dto); err != nil {
		return nil, fmt.Errorf("failed to unmarshal question JSON: %w (raw: %s)", err, rawJSON)
	}

	// Overwrite difficulty with target
	dto.Difficulty = diffLevel

	// Strict Quality and Correctness Validation
	if err := c.validateAndCleanDTO(&dto, categoryName, promptInstructions); err != nil {
		return nil, fmt.Errorf("validation error: %w", err)
	}

	return &dto, nil
}

// validateAndCleanDTO ensures the generated question meets all correctness, formatting, and safety standards.
func (c *Client) validateAndCleanDTO(dto *models.GeneratedQuestionDTO, categoryName, promptInstructions string) error {
	if len(dto.Options) != 4 {
		return fmt.Errorf("invalid options count: expected 4, got %d", len(dto.Options))
	}

	// 1. Strip redundant option prefixes ("A. ", "1. ", "Option 1:", etc.) and reject placeholders
	seen := make(map[string]bool)
	for i, opt := range dto.Options {
		trimmed := strings.TrimSpace(opt)
		if dummyOptionRegex.MatchString(trimmed) {
			return fmt.Errorf("placeholder dummy option detected: %q", trimmed)
		}

		cleaned := prefixRegex.ReplaceAllString(trimmed, "")
		cleaned = strings.TrimSpace(cleaned)

		// Strip markdown code fences if option was wrapped in ```
		if strings.HasPrefix(cleaned, "```") {
			lines := strings.Split(cleaned, "\n")
			if len(lines) >= 2 && strings.HasPrefix(lines[0], "```") && strings.HasSuffix(lines[len(lines)-1], "```") {
				cleaned = strings.TrimSpace(strings.Join(lines[1:len(lines)-1], "\n"))
			}
		}

		if cleaned == "" || dummyOptionRegex.MatchString(cleaned) {
			return fmt.Errorf("option %d is empty or placeholder (%q)", i, opt)
		}
		if seen[cleaned] {
			return fmt.Errorf("duplicate option detected: %q", cleaned)
		}
		seen[cleaned] = true
		dto.Options[i] = cleaned
	}

	// 2. Validate CorrectIndices
	if len(dto.CorrectIndices) < 1 || len(dto.CorrectIndices) > 2 {
		return fmt.Errorf("invalid correct_indices count: expected 1 or 2, got %d", len(dto.CorrectIndices))
	}

	correctSet := make(map[int]bool)
	for _, idx := range dto.CorrectIndices {
		if idx < 0 || idx >= 4 {
			return fmt.Errorf("correct_index %d out of bounds [0..3]", idx)
		}
		if correctSet[idx] {
			return fmt.Errorf("duplicate index %d in correct_indices", idx)
		}
		correctSet[idx] = true
	}

	// 3. Cisco Anti-Hallucination
	isCisco := strings.Contains(strings.ToLower(categoryName), "cisco") ||
		strings.Contains(strings.ToLower(categoryName), "network") ||
		strings.Contains(strings.ToLower(promptInstructions), "cisco")
	if isCisco {
		for i, opt := range dto.Options {
			if correctSet[i] && ciscoCidrRegex.MatchString(opt) {
				return fmt.Errorf("cisco hallucination: /CIDR mask in ip address on interface: %q", opt)
			}
			if correctSet[i] && strings.Contains(strings.ToLower(opt), "config-if") && strings.Contains(strings.ToLower(opt), "ip default-gateway") {
				return fmt.Errorf("cisco hallucination: ip default-gateway in config-if: %q", opt)
			}
		}
	}

	// 4. Extract any code fence from question text if model accidentally placed code in prompt
	if match := fenceRegex.FindStringSubmatch(dto.Question); len(match) > 2 {
		fenceLang := strings.TrimSpace(match[1])
		fenceCode := strings.TrimSpace(match[2])
		dto.Question = strings.TrimSpace(fenceRegex.ReplaceAllString(dto.Question, ""))
		if strings.TrimSpace(dto.Code) == "" || strings.TrimSpace(dto.Code) == fenceCode {
			dto.Code = fenceCode
			if dto.Language == "" && fenceLang != "" {
				dto.Language = fenceLang
			}
		}
	}

	// 5. Code presence vs question prompt check
	cleanCode := strings.TrimSpace(dto.Code)
	if strings.HasPrefix(cleanCode, "```") {
		lines := strings.Split(cleanCode, "\n")
		if len(lines) >= 2 && strings.HasPrefix(lines[0], "```") && strings.HasSuffix(lines[len(lines)-1], "```") {
			cleanCode = strings.TrimSpace(strings.Join(lines[1:len(lines)-1], "\n"))
		}
	}
	dto.Code = cleanCode

	lowerQ := strings.ToLower(dto.Question)
	requiresCodePhrases := []string{
		"following code",
		"following snippet",
		"following configuration",
		"following dockerfile",
		"following script",
		"code snippet below",
		"configuration below",
		"in the code above",
		"in the snippet above",
	}
	for _, phrase := range requiresCodePhrases {
		if strings.Contains(lowerQ, phrase) && dto.Code == "" {
			isWhichSnippet := strings.Contains(lowerQ, "which code snippet") ||
				strings.Contains(lowerQ, "which of the following code") ||
				strings.Contains(lowerQ, "which snippet") ||
				strings.Contains(lowerQ, "which of the following snippet") ||
				strings.Contains(lowerQ, "which configuration") ||
				strings.Contains(lowerQ, "which of the following configuration")
			if !isWhichSnippet {
				return fmt.Errorf("question prompt references a snippet (%q), but code field is empty", phrase)
			}
		}
	}

	// If question asks for code snippet choice, ensure options contain actual code tokens (not scalar numbers or strings)
	isSnippetChoiceQ := strings.Contains(lowerQ, "which code snippet") ||
		strings.Contains(lowerQ, "which of the following code") ||
		strings.Contains(lowerQ, "which snippet") ||
		strings.Contains(lowerQ, "which of the following snippet")

	if isSnippetChoiceQ && dto.Code == "" {
		hasCodeSyntax := false
		for _, opt := range dto.Options {
			optLower := strings.ToLower(opt)
			if strings.ContainsAny(opt, "(){}=><;:") || strings.Contains(opt, "\n") ||
				strings.Contains(optLower, "def ") || strings.Contains(optLower, "func ") ||
				strings.Contains(optLower, "var ") || strings.Contains(optLower, "let ") ||
				strings.Contains(optLower, "const ") || strings.Contains(optLower, "return ") ||
				strings.Contains(optLower, "import ") || strings.Contains(optLower, "class ") ||
				strings.Contains(optLower, "echo ") || strings.Contains(optLower, "print") {
				hasCodeSyntax = true
				break
			}
		}
		if !hasCodeSyntax {
			return fmt.Errorf("question asks for code snippets (%q), but options are scalar values without code syntax", dto.Question)
		}
	}

	// 6. Syntax completeness and truncation guards for code snippets
	if len(dto.Code) > 0 {
		// Balanced parentheses and braces check
		if strings.Count(dto.Code, "(") > strings.Count(dto.Code, ")") {
			return fmt.Errorf("code snippet appears truncated: unclosed parenthesis")
		}
		if strings.Count(dto.Code, "{") > strings.Count(dto.Code, "}") {
			return fmt.Errorf("code snippet appears truncated: unclosed curly brace")
		}

		// Check for unclosed string literal on the final line
		lines := strings.Split(dto.Code, "\n")
		if len(lines) > 0 {
			lastLine := strings.TrimSpace(lines[len(lines)-1])
			if !strings.HasPrefix(lastLine, "//") && !strings.HasPrefix(lastLine, "#") {
				quoteCount := 0
				for j := 0; j < len(lastLine); j++ {
					if lastLine[j] == 34 && (j == 0 || lastLine[j-1] != 92) {
						quoteCount++
					}
				}
				if quoteCount%2 != 0 {
					return fmt.Errorf("code snippet appears truncated with unclosed quote on final line: %q", lastLine)
				}
			}
		}

		// DevOps & Linux: Strictly ban application programming code (Go, Java, C#, C++ SDKs)
		isInfra := strings.Contains(strings.ToLower(categoryName), "devops") ||
			strings.Contains(strings.ToLower(categoryName), "linux") ||
			strings.Contains(strings.ToLower(categoryName), "docker") ||
			strings.Contains(strings.ToLower(categoryName), "container")

		if isInfra {
			lowerLang := strings.ToLower(dto.Language)
			lowerCode := strings.ToLower(dto.Code)
			if lowerLang == "go" || lowerLang == "golang" || lowerLang == "java" || lowerLang == "csharp" || lowerLang == "cpp" ||
				strings.Contains(lowerCode, "package main") || strings.Contains(lowerCode, "func main()") {
				return fmt.Errorf("infra category (%s) must not contain application programming code (%s)", categoryName, lowerLang)
			}
		}

		// Auto-set code_analysis type
		dto.Type = "code_analysis"
	} else {
		// If code is empty, force multiple_choice type and clear language
		dto.Type = "multiple_choice"
		dto.Language = ""
	}

	// 7. Ensure question text has appropriate (Choose two) hint if 2 correct answers, avoiding duplicate hints
	dto.Question = strings.TrimSpace(choiceHintRegex.ReplaceAllString(dto.Question, " "))
	if len(dto.CorrectIndices) == 2 {
		dto.Question += " (Choose two)"
	}

	// 8. Explanation / Correct Index consistency check
	lowerExpl := strings.ToLower(dto.Explanation)
	for i, opt := range dto.Options {
		if !correctSet[i] {
			cleanOpt := strings.TrimSpace(opt)
			if len(cleanOpt) > 0 {
				patterns := []string{
					"correct output is " + strings.ToLower(cleanOpt),
					"correct answer is " + strings.ToLower(cleanOpt),
					"correct result is " + strings.ToLower(cleanOpt),
					"therefore, the correct output is " + strings.ToLower(cleanOpt),
					"therefore, " + strings.ToLower(cleanOpt) + " is the correct",
				}
				for _, pat := range patterns {
					if strings.Contains(lowerExpl, pat) {
						return fmt.Errorf("explanation inconsistency: explanation states %q is correct, but index %d is not in correct_indices", cleanOpt, i)
					}
				}
			}
		}
	}

	// 9. Anti-spoiler guard: ensure code snippet does not duplicate or leak options
	if len(dto.Code) > 0 {
		isWhichCmd := strings.Contains(lowerQ, "which command") ||
			strings.Contains(lowerQ, "what command") ||
			strings.Contains(lowerQ, "which of the following command") ||
			strings.Contains(lowerQ, "which powershell command") ||
			strings.Contains(lowerQ, "which code snippet") ||
			strings.Contains(lowerQ, "which snippet") ||
			strings.Contains(lowerQ, "which of the following code") ||
			strings.Contains(lowerQ, "which of the following snippets")

		isAnalysisQuestion := strings.Contains(lowerQ, "output of") ||
			strings.Contains(lowerQ, "what is the output") ||
			strings.Contains(lowerQ, "error") ||
			strings.Contains(lowerQ, "bug")

		// Check if 2 or more options appear in code snippet
		optMatchesCount := 0
		for _, opt := range dto.Options {
			cleanOpt := strings.TrimSpace(opt)
			if len(cleanOpt) > 3 && strings.Contains(dto.Code, cleanOpt) {
				optMatchesCount++
			}
		}
		if optMatchesCount >= 2 {
			lines := strings.Split(dto.Code, "\n")
			var keptLines []string
			for _, l := range lines {
				trimmedL := strings.TrimSpace(l)
				isOptLine := false
				for _, opt := range dto.Options {
					cleanOpt := strings.TrimSpace(opt)
					if trimmedL == cleanOpt || (len(cleanOpt) > 5 && strings.Contains(trimmedL, cleanOpt)) {
						isOptLine = true
						break
					}
				}
				if !isOptLine {
					keptLines = append(keptLines, l)
				}
			}
			dto.Code = strings.TrimSpace(strings.Join(keptLines, "\n"))
			if dto.Code == "" {
				dto.Language = ""
				dto.Type = "multiple_choice"
			}
		}

		if len(dto.Code) > 0 {
			shouldClearSnippet := false
			normCode := strings.Join(strings.Fields(dto.Code), " ")
			for _, opt := range dto.Options {
				normOpt := strings.Join(strings.Fields(opt), " ")
				if normCode == normOpt || dto.Code == opt {
					shouldClearSnippet = true
					break
				}
			}

			if !shouldClearSnippet && isWhichCmd && !isAnalysisQuestion {
				shouldClearSnippet = true
			}

			if shouldClearSnippet {
				dto.Code = ""
				dto.Language = ""
				dto.Type = "multiple_choice"
			}
		}
	}

	return nil
}
