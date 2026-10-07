package quizcraft

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// The prompt is compiled and versioned with the policy. A prompt change must
// change LearningPromptVersion so queued work is regenerated rather than mixed.
const LearningPromptVersion = "learning-prompt-v1"

const (
	learningProviderDefaultTimeout = 60 * time.Second
	learningProviderMaxTimeout     = 120 * time.Second
	// The envelope around the model message is larger than the bounded content
	// it carries, so the wire read limit is separate from the decision limit.
	learningProviderResponseMaxBytes = 256 << 10
)

var ErrLearningProviderUnavailable = errors.New("learning model provider is unavailable")

type LearningProviderConfig struct {
	BaseURL    string
	APIKey     string
	Model      string
	HTTPClient *http.Client
}

// The system prompt restates the server-side validation rules so a compliant
// provider returns an acceptable decision. It is not a security boundary: the
// validator in learning_analysis.go remains authoritative.
const learningModelSystemPrompt = `你是学习诊断助手。用户消息是一段 JSON（标签、服务端统计和作答证据）。
只输出一个 JSON 对象，不要输出解释、Markdown 代码块或任何其他文字。

输出结构：
{"findings":[{"tag_id":"标签ID","status":"supported|tentative|uncovered","evidence_ids":["证据ID"],"possible_reason":"可能..."}],"primary_tag_id":"标签ID或空字符串"}

规则：
- findings 最多 3 条，tag_id 必须来自输入中的统计标签，不得重复。
- status=supported：需要该标签至少 3 道不同题目，且引用证据中至少 2 道不同错题。
- status=tentative：该标签至少 1 次作答，且必须引用真实证据 ID。
- status=uncovered：该标签没有任何作答，此时 evidence_ids 必须为空且不要写 possible_reason。
- evidence_ids 只能引用输入里出现过的证据 ID，且必须属于该标签。
- possible_reason 必须以“可能”开头，使用假设语气；不得出现任何数字或百分号，也不要复述统计。
- primary_tag_id 必须是 findings 中的某个 tag_id；没有 findings 时给空字符串。
- 不要输出统计数字、题号、链接或用户身份信息。`

// NewOpenAICompatibleLearningModel returns the LearningModelCall that talks to
// an operator-configured OpenAI-compatible /chat/completions endpoint. It only
// ever sends the bounded LearningModelInput; the internal snapshot never leaves
// the process. Construction fails loudly on incomplete or unsafe configuration
// so an operator mistake cannot silently leave reports pending forever.
func NewOpenAICompatibleLearningModel(config LearningProviderConfig) (LearningModelCall, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(config.BaseURL), "/")
	apiKey := strings.TrimSpace(config.APIKey)
	model := strings.TrimSpace(config.Model)
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" ||
		(parsed.Scheme != "https" && parsed.Scheme != "http") || apiKey == "" ||
		!learningText(model, 160) || strings.ContainsAny(model, "\r\n\x00") {
		return nil, ErrLearningProviderUnavailable
	}
	if parsed.Scheme == "http" {
		address := net.ParseIP(parsed.Hostname())
		if parsed.Hostname() != "localhost" && (address == nil || !address.IsLoopback()) {
			return nil, ErrLearningProviderUnavailable
		}
	}
	client := http.Client{Timeout: learningProviderDefaultTimeout}
	if config.HTTPClient != nil {
		client = *config.HTTPClient
	}
	if client.Timeout <= 0 || client.Timeout > learningProviderMaxTimeout {
		client.Timeout = learningProviderMaxTimeout
	}
	// A redirect would replay the provider credential to another origin.
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	endpoint := baseURL + "/chat/completions"
	return func(ctx context.Context, input LearningModelInput, versions LearningJobVersions) ([]byte, error) {
		// A job queued for another model, policy or prompt must never be
		// answered by this deployment's provider.
		if input.PolicyVersion != LearningAnalysisPolicyVersion || !validLearningJobVersions(versions) || versions.Model != model {
			return nil, ErrLearningProviderUnavailable
		}
		payload, err := json.Marshal(input)
		if err != nil || len(payload) == 0 || len(payload) > learningModelInputMaxBytes {
			return nil, ErrLearningProviderUnavailable
		}
		requestBody, err := json.Marshal(map[string]any{
			"model":       model,
			"stream":      false,
			"temperature": 0.1,
			"messages": []map[string]string{
				{"role": "system", "content": learningModelSystemPrompt},
				{"role": "user", "content": string(payload)},
			},
		})
		if err != nil {
			return nil, ErrLearningProviderUnavailable
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(requestBody))
		if err != nil {
			return nil, ErrLearningProviderUnavailable
		}
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Accept", "application/json")
		request.Header.Set("Authorization", "Bearer "+apiKey)
		response, err := client.Do(request)
		if err != nil {
			return nil, ErrLearningProviderUnavailable
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			return nil, ErrLearningProviderUnavailable
		}
		raw, err := io.ReadAll(io.LimitReader(response.Body, learningProviderResponseMaxBytes+1))
		if err != nil || len(raw) > learningProviderResponseMaxBytes {
			return nil, ErrLearningProviderUnavailable
		}
		var envelope struct {
			Choices []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			} `json:"choices"`
		}
		if json.Unmarshal(raw, &envelope) != nil || len(envelope.Choices) == 0 {
			return nil, ErrLearningProviderUnavailable
		}
		// The provider may wrap the object in a Markdown fence; strip one fence
		// pair rather than rejecting an otherwise valid decision.
		content := strings.TrimSpace(envelope.Choices[0].Message.Content)
		if strings.HasPrefix(content, "```") {
			content = strings.TrimPrefix(content, "```json")
			content = strings.TrimPrefix(content, "```")
			content = strings.TrimSuffix(strings.TrimSpace(content), "```")
			content = strings.TrimSpace(content)
		}
		if content == "" || len(content) > learningModelOutputMaxBytes {
			return nil, ErrLearningProviderUnavailable
		}
		return []byte(content), nil
	}, nil
}
