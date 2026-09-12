package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	neturl "net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

type ImageEntry struct {
	Base64       string
	ModelAnswers []ModelAnswer
}

type ModelAnswer struct {
	Model    string
	Question string
	Answer   string
	Raw      string
	Error    string
}

type visionResponse struct {
	Choices []struct {
		Message struct {
			Content          string `json:"content"`
			ReasoningContent string `json:"reasoning_content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
}

func (a *App) analyzeImages(ctx context.Context, base64Images []string) []ImageEntry {
	models := a.cfg.Models

	items := make([]ImageEntry, len(base64Images))
	var wg sync.WaitGroup
	wg.Add(len(base64Images))

	for i := range base64Images {
		i := i
		go func() {
			defer wg.Done()
			entry := ImageEntry{Base64: base64Images[i]}

			var mu sync.Mutex
			var mwg sync.WaitGroup
			sem := make(chan struct{}, 4)
			entry.ModelAnswers = make([]ModelAnswer, 0, len(models))

			for _, m := range models {
				m := m
				mwg.Add(1)
				go func() {
					defer mwg.Done()
					select {
					case sem <- struct{}{}:
					case <-ctx.Done():
						return
					}
					defer func() { <-sem }()

					ans := a.callVision(ctx, m, base64Images[i])
					mu.Lock()
					entry.ModelAnswers = append(entry.ModelAnswers, ans)
					mu.Unlock()
				}()
			}

			mwg.Wait()
			items[i] = entry
		}()
	}

	wg.Wait()
	return items
}

func (a *App) callVision(ctx context.Context, model, b64 string) ModelAnswer {
	baseURL := strings.TrimSpace(a.cfg.SiliconflowBaseURL)
	apiKey := strings.TrimSpace(a.cfg.SiliconflowAPIKey)
	timeout := a.cfg.visionTimeout()
	result := ModelAnswer{Model: model}
	if apiKey == "" {
		result.Error = "缺少 API Key（请在 config.json 的 siliconflow_api_key 配置中设置）"
		return result
	}

	userContent := []interface{}{
		map[string]interface{}{"type": "text", "text": promptText()},
		map[string]interface{}{
			"type":      "image_url",
			"image_url": map[string]interface{}{"url": "data:image/png;base64," + b64},
		},
	}

	reqBody := map[string]interface{}{
		"model": model,
		"messages": []map[string]interface{}{
			{"role": "system", "content": systemPrompt()},
			{"role": "user", "content": userContent},
		},
		"temperature": 0.2,
		"max_tokens":  a.cfg.visionMaxTokens(),
	}

	endpoint := strings.TrimRight(baseURL, "/") + "/v1/chat/completions"
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		result.Error = fmt.Sprintf("序列化请求失败: %v", err)
		return result
	}

	fmt.Fprintf(os.Stderr, "vision request: model=%s timeout=%s payload=%d endpoint=%s\n", model, timeout, len(bodyBytes), endpoint)
	client := &http.Client{Timeout: timeout + 5*time.Second}
	for attempt := 0; attempt <= a.cfg.VisionMaxRetries; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(bodyBytes))
		if err != nil {
			result.Error = fmt.Sprintf("构造请求失败: %v", err)
			return result
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+apiKey)

		start := time.Now()
		resp, err := client.Do(req)
		if err != nil {
			result.Error = classifyVisionError(err, timeout)
			fmt.Fprintf(os.Stderr, "vision request failed: model=%s attempt=%d elapsed=%s err=%v\n", model, attempt+1, time.Since(start), err)
			return result
		}

		body, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			result.Error = fmt.Sprintf("读取 AI 响应失败: %v", readErr)
			return result
		}
		fmt.Fprintf(os.Stderr, "vision response: model=%s attempt=%d status=%s elapsed=%s\n", model, attempt+1, resp.Status, time.Since(start))

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			var parsed visionResponse
			if err := json.Unmarshal(body, &parsed); err != nil {
				result.Error = fmt.Sprintf("解析 AI 响应失败: %v; 原始: %s", err, truncate(string(body), 500))
				if attempt < a.cfg.VisionMaxRetries && a.waitForOutputRetry(ctx, model, attempt, "响应 JSON 无效") {
					continue
				}
				return result
			}
			if len(parsed.Choices) == 0 {
				result.Error = "AI 响应为空：没有 choices"
				if attempt < a.cfg.VisionMaxRetries && a.waitForOutputRetry(ctx, model, attempt, "缺少 choices") {
					continue
				}
				return result
			}

			choice := parsed.Choices[0]
			content := strings.TrimSpace(choice.Message.Content)
			fmt.Fprintf(
				os.Stderr,
				"vision content: model=%s attempt=%d finish=%s content_length=%d reasoning_length=%d\n",
				model,
				attempt+1,
				choice.FinishReason,
				len(content),
				len(choice.Message.ReasoningContent),
			)
			if content == "" {
				result.Error = "AI 返回内容为空"
				if choice.Message.ReasoningContent != "" {
					result.Error = "AI 仅返回了推理过程，没有返回最终答案"
				}
				if attempt < a.cfg.VisionMaxRetries && a.waitForOutputRetry(ctx, model, attempt, "最终答案为空") {
					continue
				}
				return result
			}

			result.Error = ""
			result.Raw = content
			q, ansText, ok := parseQA(content)
			if !ok {
				q, ansText = roughSplitQA(content)
			}
			result.Question = normalizeDisplayText(q)
			result.Answer = normalizeDisplayText(ansText)
			if result.Answer == "" {
				result.Error = "AI 未返回可显示的答案"
				if attempt < a.cfg.VisionMaxRetries && a.waitForOutputRetry(ctx, model, attempt, "答案字段为空") {
					continue
				}
				return result
			}
			if choice.FinishReason == "length" {
				result.Answer += "\n\n提示：模型输出达到长度上限，内容可能不完整。可增大 vision_max_tokens。"
			}
			return result
		}
		if !retryableVisionStatus(resp.StatusCode) || attempt >= a.cfg.VisionMaxRetries {
			result.Error = fmt.Sprintf("AI API 返回错误: HTTP %d: %s", resp.StatusCode, truncate(string(body), 1000))
			return result
		}

		delay := a.cfg.visionRetryDelay() * time.Duration(1<<attempt)
		fmt.Fprintf(os.Stderr, "vision retry: model=%s status=%d next_attempt=%d delay=%s\n", model, resp.StatusCode, attempt+2, delay)
		if err := waitForRetry(ctx, delay); err != nil {
			result.Error = classifyVisionError(err, timeout)
			return result
		}
	}
	result.Error = "AI 请求未返回结果"
	return result
}

func (a *App) waitForOutputRetry(ctx context.Context, model string, attempt int, reason string) bool {
	delay := a.cfg.visionRetryDelay() * time.Duration(1<<attempt)
	fmt.Fprintf(os.Stderr, "vision retry: model=%s reason=%s next_attempt=%d delay=%s\n", model, reason, attempt+2, delay)
	return waitForRetry(ctx, delay) == nil
}

func retryableVisionStatus(status int) bool {
	switch status {
	case http.StatusTooManyRequests, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}

func waitForRetry(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func classifyVisionError(err error, timeout time.Duration) string {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return fmt.Sprintf("AI 请求超时：在 %s 内未完成响应，可增大 vision_timeout_seconds", timeout)
	case errors.Is(err, context.Canceled):
		return "AI 请求已取消"
	}

	var urlErr *neturl.Error
	if errors.As(err, &urlErr) && urlErr.Timeout() {
		return fmt.Sprintf("AI 请求超时：HTTP 客户端在 %s 内未等到响应头，可增大 vision_timeout_seconds", timeout+5*time.Second)
	}

	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return fmt.Sprintf("AI 网络超时：%v", err)
	}

	var opErr *net.OpError
	if errors.As(err, &opErr) {
		return fmt.Sprintf("AI 连接失败：%v", opErr)
	}

	return fmt.Sprintf("AI 请求失败：%v", err)
}

func systemPrompt() string {
	return "你是一个通用的题目识别与解答助手。你需要分析截图中的内容，识别并解答其中的题目。题目可能是选择题、判断题、填空题、简答题、计算题、算法题或编程题，不要把识别范围局限于编程题。严格输出一个可被标准解析器解析的 JSON 对象，不要输出 Markdown 围栏或 JSON 之外的内容。固定格式为 {\"question\":\"...\",\"answer\":\"...\"}。question 字段用于整理题型、题号、标题、题干、选项和已知条件；answer 字段先给明确答案，再给必要且简洁的解析。仅当题目属于算法题或编程题时，才在 answer 中额外提供推荐解法、复杂度分析和 Java 实现。如果图片明显与任何题目无关，则返回 {\"question\":\"\",\"answer\":\"无法识别为题目\"}。"
}

func promptText() string {
	return "请使用中文识别并解答截图中的题目，并遵守以下规则：1. 题目可能是选择题、判断题、填空题、简答题、计算题、算法题或编程题，请先根据截图判断题型，不要局限于编程题。2. 如果截图包含题号、标题、题干、选项、示例、提示、约束或已知条件中的一项或多项，应优先按题目处理。3. question 字段按实际识别结果组织为：题型、题号、标题、题干、选项、已知条件；截图中没有的项目可以省略，不要虚构。4. answer 字段必须先给出明确答案，再给出简洁、可靠的解析；选择题需写出选项及其内容，判断题需写正确或错误，填空题给出填空内容，简答题和计算题给出推理或步骤。5. 只有算法题或编程题才需要在 answer 中给出推荐解法、复杂度分析和完整 Java 实现。6. 图片不完整时，根据可见内容尽量推断并明确说明不确定之处，不要轻易拒绝。7. 只有图片明显与任何题目无关时，才返回 {\"question\":\"\",\"answer\":\"无法识别为题目\"}。只返回一个合法 JSON 对象；字符串中的换行必须正确转义，不要添加 Markdown 代码围栏，也不要复述这些规则。"
}

func parseQA(s string) (string, string, bool) {
	type qa struct {
		Question *string `json:"question"`
		Answer   *string `json:"answer"`
		TiMu     *string `json:"题目"`
		DaAn     *string `json:"答案"`
	}
	qaValues := func(out qa) (string, string, bool) {
		var q, a string
		if out.Question != nil {
			q = *out.Question
		} else if out.TiMu != nil {
			q = *out.TiMu
		}
		if out.Answer != nil {
			a = *out.Answer
		} else if out.DaAn != nil {
			a = *out.DaAn
		}
		return q, a, out.Question != nil || out.Answer != nil || out.TiMu != nil || out.DaAn != nil
	}

	cleaned := cleanModelContent(s)
	candidates := []string{cleaned}
	for i := 0; i < 2; i++ {
		var decoded string
		if json.Unmarshal([]byte(candidates[len(candidates)-1]), &decoded) != nil {
			break
		}
		decoded = cleanModelContent(decoded)
		candidates = append(candidates, decoded)
	}

	for _, candidate := range candidates {
		jsonCandidates := []string{candidate}
		i := strings.IndexByte(candidate, '{')
		j := strings.LastIndexByte(candidate, '}')
		if i >= 0 && j > i && (i != 0 || j != len(candidate)-1) {
			jsonCandidates = append(jsonCandidates, candidate[i:j+1])
		}
		for _, jsonCandidate := range jsonCandidates {
			var out qa
			if json.Unmarshal([]byte(jsonCandidate), &out) != nil {
				continue
			}
			if q, a, ok := qaValues(out); ok {
				return q, a, true
			}
		}
	}

	q := extractLooseJSONField(cleaned, looseQuestionPattern)
	a := extractLooseJSONField(cleaned, looseAnswerPattern)
	if a == "" {
		a = extractTrailingAnswer(cleaned)
	}
	if q != "" || a != "" {
		return q, a, true
	}

	return "", "", false
}

var (
	looseQuestionPattern = regexp.MustCompile(`(?s)"(?:question|题目)"\s*:\s*"(.*?)"\s*,\s*"(?:answer|答案)"\s*:`)
	looseAnswerPattern   = regexp.MustCompile(`(?s)"(?:answer|答案)"\s*:\s*"(.*)"\s*\}?\s*(?:` + "```" + `)?\s*$`)
	looseAnswerStart     = regexp.MustCompile(`"(?:answer|答案)"\s*:\s*`)
)

func cleanModelContent(s string) string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "```") {
		return s
	}
	if newline := strings.IndexByte(s, '\n'); newline >= 0 {
		s = s[newline+1:]
	}
	s = strings.TrimSpace(s)
	if strings.HasSuffix(s, "```") {
		s = strings.TrimSpace(strings.TrimSuffix(s, "```"))
	}
	return s
}

func extractLooseJSONField(s string, pattern *regexp.Regexp) string {
	match := pattern.FindStringSubmatch(s)
	if len(match) < 2 {
		return ""
	}
	return decodeLooseJSONString(match[1])
}

func extractTrailingAnswer(s string) string {
	marker := looseAnswerStart.FindStringIndex(s)
	if marker == nil {
		return ""
	}
	raw := strings.TrimSpace(s[marker[1]:])
	raw = strings.TrimSpace(strings.TrimSuffix(raw, "```"))
	raw = strings.TrimSpace(strings.TrimSuffix(raw, "}"))
	raw = strings.TrimPrefix(raw, `"`)
	raw = strings.TrimSuffix(raw, `"`)
	return decodeLooseJSONString(raw)
}

func decodeLooseJSONString(raw string) string {
	raw = strings.TrimSpace(raw)
	if decoded, err := strconv.Unquote(`"` + raw + `"`); err == nil {
		return decoded
	}
	return strings.NewReplacer(
		`\n`, "\n",
		`\r`, "\r",
		`\t`, "\t",
		`\"`, `"`,
		`\\`, `\`,
	).Replace(raw)
}

func normalizeDisplayText(s string) string {
	s = strings.TrimSpace(s)
	if s == "" || strings.ContainsAny(s, "\r\n") {
		return s
	}
	return strings.NewReplacer(`\n`, "\n", `\r`, "\r", `\t`, "\t").Replace(s)
}

func roughSplitQA(s string) (string, string) {
	s = cleanModelContent(s)
	for _, sep := range []string{"答案：", "答：", "参考答案：", "\n答案", "\n答"} {
		if idx := strings.Index(s, sep); idx > 0 {
			return strings.TrimSpace(s[:idx]), strings.TrimSpace(s[idx+len(sep):])
		}
	}
	return "", s
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func splitCSV(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
