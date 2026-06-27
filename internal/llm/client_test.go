package llm

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// ==================== 纯函数测试 ====================

func TestIsRetryable(t *testing.T) {
	cases := []struct {
		status int
		want   bool
	}{
		{408, true},
		{429, true},
		{500, true},
		{502, true},
		{503, true},
		{504, true},
		{200, false},
		{400, false},
		{401, false},
		{403, false},
		{404, false},
	}
	for _, c := range cases {
		if got := isRetryable(c.status); got != c.want {
			t.Errorf("isRetryable(%d) = %v, want %v", c.status, got, c.want)
		}
	}
}

func TestRetryDelay_NoRetryAfter(t *testing.T) {
	resp := &http.Response{Header: http.Header{}}

	// attempt 0: 1.5 * 1 = 1.5s
	d0 := retryDelay(resp, 0)
	if d0 < 500*time.Millisecond || d0 > 2*time.Second {
		t.Errorf("attempt 0 delay 异常: %v", d0)
	}

	// attempt 越大 delay 越大
	d3 := retryDelay(resp, 3)
	if d3 <= d0 {
		t.Errorf("attempt 3 delay (%v) 应大于 attempt 0 (%v)", d3, d0)
	}

	// 上限 30s
	d20 := retryDelay(resp, 20)
	if d20 > 30*time.Second {
		t.Errorf("delay 不应超过 30s: %v", d20)
	}
}

func TestRetryDelay_WithRetryAfter(t *testing.T) {
	resp := &http.Response{Header: http.Header{}}
	resp.Header.Set("Retry-After", "3")

	d := retryDelay(resp, 0)
	if d != 3*time.Second {
		t.Errorf("应使用 Retry-After 头: 3s, 实际 %v", d)
	}
}

func TestAutoMakeURL(t *testing.T) {
	cases := []struct {
		base, path, want string
	}{
		{"https://api.openai.com", "chat/completions", "https://api.openai.com/v1/chat/completions"},
		{"https://api.openai.com/v1", "chat/completions", "https://api.openai.com/v1/chat/completions"},
		{"https://api.deepseek.com/v2/", "chat/completions", "https://api.deepseek.com/v2/chat/completions"},
		{"https://api.test.com", "/chat/completions", "https://api.test.com/v1/chat/completions"},
	}
	for _, c := range cases {
		got := autoMakeURL(c.base, c.path)
		if got != c.want {
			t.Errorf("autoMakeURL(%q, %q) = %q, want %q", c.base, c.path, got, c.want)
		}
	}
}

func TestParseJSONArgs(t *testing.T) {
	// 合法 JSON
	args := parseJSONArgs(`{"key":"value","num":42}`)
	if args["key"] != "value" {
		t.Errorf("key 不匹配: %v", args["key"])
	}

	// 非法 JSON 应返回 _raw
	args = parseJSONArgs(`invalid json`)
	if args["_raw"] != "invalid json" {
		t.Errorf("非法 JSON 应放入 _raw: %v", args)
	}

	// 空字符串
	args = parseJSONArgs(``)
	if args["_raw"] != "" {
		t.Errorf("空字符串应放入 _raw: %v", args)
	}
}

func TestStrVal(t *testing.T) {
	if strVal("hello") != "hello" {
		t.Error("字符串应原样返回")
	}
	if strVal(42) != "42" {
		t.Error("数字应转为字符串")
	}
	if strVal(nil) != "" {
		t.Error("nil 应返回空字符串")
	}
}

func TestFloatVal(t *testing.T) {
	if floatVal(float64(3.14)) != 3.14 {
		t.Error("float64 应原样返回")
	}
	if floatVal("not a number") != 0 {
		t.Error("非数字应返回 0")
	}
	if floatVal(nil) != 0 {
		t.Error("nil 应返回 0")
	}
}

func TestCleanContent_CodeBlock(t *testing.T) {
	// 短代码块应保留
	short := "```go\nfmt.Println(\"hello\")\n```"
	result := CleanContent(short)
	if !strings.Contains(result, "fmt.Println") {
		t.Error("短代码块应保留内容")
	}

	// 长代码块应被截断
	longLines := make([]string, 20)
	for i := range longLines {
		longLines[i] = "line " + string(rune('a'+i))
	}
	longCode := "```\n" + strings.Join(longLines, "\n") + "\n```"
	result = CleanContent(longCode)
	if !strings.Contains(result, "...") {
		t.Error("长代码块应包含省略标记")
	}
}

func TestCleanContent_FileContent(t *testing.T) {
	text := `before <file_content>some content</file_content> after`
	result := CleanContent(text)
	if strings.Contains(result, "<file_content>") {
		t.Error("应移除 <file_content> 标签")
	}
	if !strings.Contains(result, "before") || !strings.Contains(result, "after") {
		t.Error("应保留其他内容")
	}
}

func TestCleanContent_ToolUse(t *testing.T) {
	text := `text <tool_use>call</tool_use> more <tool_call>cmd</tool_call> end`
	result := CleanContent(text)
	if strings.Contains(result, "<tool_use>") || strings.Contains(result, "<tool_call>") {
		t.Error("应移除 tool 标签")
	}
}

func TestCleanContent_MultipleNewlines(t *testing.T) {
	text := "line1\n\n\n\n\nline2"
	result := CleanContent(text)
	if strings.Contains(result, "\n\n\n") {
		t.Error("多个连续空行应被压缩")
	}
}

func TestMin(t *testing.T) {
	if min(3, 5) != 3 {
		t.Error("min(3,5) 应为 3")
	}
	if min(5, 3) != 3 {
		t.Error("min(5,3) 应为 3")
	}
	if min(-1, 0) != -1 {
		t.Error("min(-1,0) 应为 -1")
	}
}

// ==================== parseJSONResponse 测试 ====================

func TestParseJSONResponse_TextContent(t *testing.T) {
	data := []byte(`{
		"choices": [{
			"message": {
				"role": "assistant",
				"content": "Hello, world!"
			}
		}],
		"usage": {
			"prompt_tokens": 10,
			"completion_tokens": 5
		}
	}`)

	c := &Client{}
	resp, err := c.parseJSONResponse(data)
	if err != nil {
		t.Fatalf("parseJSONResponse 失败: %v", err)
	}
	if resp.Content != "Hello, world!" {
		t.Errorf("Content 不匹配: %s", resp.Content)
	}
	if resp.Usage.InputTokens != 10 {
		t.Errorf("InputTokens 不匹配: %d", resp.Usage.InputTokens)
	}
	if resp.Usage.OutputTokens != 5 {
		t.Errorf("OutputTokens 不匹配: %d", resp.Usage.OutputTokens)
	}
	if len(resp.ContentBlocks) != 1 || resp.ContentBlocks[0].Type != "text" {
		t.Errorf("ContentBlocks 不正确: %v", resp.ContentBlocks)
	}
}

func TestParseJSONResponse_ToolCalls(t *testing.T) {
	data := []byte(`{
		"choices": [{
			"message": {
				"role": "assistant",
				"content": null,
				"tool_calls": [{
					"id": "call_123",
					"type": "function",
					"function": {
						"name": "get_weather",
						"arguments": "{\"city\":\"Beijing\"}"
					}
				}]
			}
		}]
	}`)

	c := &Client{}
	resp, err := c.parseJSONResponse(data)
	if err != nil {
		t.Fatalf("parseJSONResponse 失败: %v", err)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("应有 1 个 ToolCall, 实际 %d", len(resp.ToolCalls))
	}
	tc := resp.ToolCalls[0]
	if tc.ID != "call_123" {
		t.Errorf("ID 不匹配: %s", tc.ID)
	}
	if tc.Name != "get_weather" {
		t.Errorf("Name 不匹配: %s", tc.Name)
	}
	if tc.Arguments != `{"city":"Beijing"}` {
		t.Errorf("Arguments 不匹配: %s", tc.Arguments)
	}

	// 应同时生成 tool_use ContentBlock
	var hasToolUse bool
	for _, b := range resp.ContentBlocks {
		if b.Type == "tool_use" && b.Name == "get_weather" {
			hasToolUse = true
			if b.Input["city"] != "Beijing" {
				t.Errorf("Input.city 不匹配: %v", b.Input["city"])
			}
		}
	}
	if !hasToolUse {
		t.Error("应生成 tool_use ContentBlock")
	}
}

func TestParseJSONResponse_InvalidJSON(t *testing.T) {
	c := &Client{}
	_, err := c.parseJSONResponse([]byte(`invalid json`))
	if err == nil {
		t.Error("非法 JSON 应返回错误")
	}
}

func TestParseJSONResponse_Empty(t *testing.T) {
	c := &Client{}
	resp, err := c.parseJSONResponse([]byte(`{}`))
	if err != nil {
		t.Fatalf("空对象不应报错: %v", err)
	}
	if resp.Content != "" {
		t.Errorf("Content 应为空: %s", resp.Content)
	}
	if len(resp.ToolCalls) != 0 {
		t.Error("ToolCalls 应为空")
	}
}

// ==================== buildPayload 测试 ====================

func TestBuildPayload_Basic(t *testing.T) {
	c := &Client{
		Model:       "gpt-4",
		MaxTokens:   1000,
		Temperature: 0.7,
	}

	params := ChatParams{
		Messages: []Message{
			{Role: "system", Content: "You are helpful."},
			{Role: "user", Content: "Hello"},
		},
	}

	payload := c.buildPayload(params, false)

	if payload["model"] != "gpt-4" {
		t.Errorf("model 不匹配: %v", payload["model"])
	}
	if payload["stream"] != false {
		t.Error("stream 应为 false")
	}
	if payload["max_tokens"] != 1000 {
		t.Errorf("max_tokens 不匹配: %v", payload["max_tokens"])
	}
	if payload["temperature"] != 0.7 {
		t.Errorf("temperature 不匹配: %v", payload["temperature"])
	}

	msgs, ok := payload["messages"].([]any)
	if !ok || len(msgs) != 2 {
		t.Errorf("messages 数量不正确: %v", payload["messages"])
	}
}

func TestBuildPayload_Stream(t *testing.T) {
	c := &Client{Model: "test", MaxTokens: 100}
	params := ChatParams{
		Messages: []Message{{Role: "user", Content: "hi"}},
	}

	payload := c.buildPayload(params, true)
	if payload["stream"] != true {
		t.Error("stream 应为 true")
	}
}

func TestBuildPayload_ToolMessage(t *testing.T) {
	c := &Client{Model: "test", MaxTokens: 100}
	params := ChatParams{
		Messages: []Message{
			{Role: "tool", Content: "result", ToolCallID: "call_123"},
		},
	}

	payload := c.buildPayload(params, false)
	msgs := payload["messages"].([]any)
	msg := msgs[0].(map[string]any)
	if msg["role"] != "tool" {
		t.Errorf("role 应为 tool: %v", msg["role"])
	}
	if msg["tool_call_id"] != "call_123" {
		t.Errorf("tool_call_id 不匹配: %v", msg["tool_call_id"])
	}
}

func TestBuildPayload_WithTools(t *testing.T) {
	c := &Client{Model: "test", MaxTokens: 100}
	params := ChatParams{
		Messages: []Message{{Role: "user", Content: "hi"}},
		Tools: []ToolSchema{
			{
				Name:        "get_weather",
				Description: "Get weather",
				InputSchema: map[string]any{"type": "object"},
			},
		},
	}

	payload := c.buildPayload(params, false)
	tools, ok := payload["tools"].([]map[string]any)
	if !ok || len(tools) != 1 {
		t.Fatalf("tools 应有 1 个: %v", payload["tools"])
	}
	fn, ok := tools[0]["function"].(map[string]any)
	if !ok {
		t.Fatalf("function 字段类型错误: %T", tools[0]["function"])
	}
	if fn["name"] != "get_weather" {
		t.Errorf("tool name 不匹配: %v", fn["name"])
	}
}

// ==================== SSE 流解析测试 (httptest) ====================

func TestParseSSEStream_Text(t *testing.T) {
	sseData := strings.Join([]string{
		`data: {"choices":[{"delta":{"content":"Hello"}}]}`,
		``,
		`data: {"choices":[{"delta":{"content":", "}}]}`,
		``,
		`data: {"choices":[{"delta":{"content":"world!"}}]}`,
		``,
		`data: [DONE]`,
		``,
	}, "\n")

	body := io.NopCloser(strings.NewReader(sseData))
	ch := make(chan StreamChunk, 10)

	c := &Client{}
	go func() {
		c.parseSSEStream(body, ch)
		close(ch)
	}()

	var text string
	for chunk := range ch {
		if chunk.Text != "" {
			text += chunk.Text
		}
	}

	if text != "Hello, world!" {
		t.Errorf("文本不匹配: %q", text)
	}
}

func TestParseSSEStream_ToolCalls(t *testing.T) {
	sseData := strings.Join([]string{
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","function":{"name":"get_weather","arguments":""}}]}}]}`,
		``,
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"city\":"}}]}}]}`,
		``,
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"Beijing\"}"}}]}}]}`,
		``,
		`data: [DONE]`,
		``,
	}, "\n")

	body := io.NopCloser(strings.NewReader(sseData))
	ch := make(chan StreamChunk, 10)

	c := &Client{}
	go func() {
		c.parseSSEStream(body, ch)
		close(ch)
	}()

	var toolCalls []ToolCall
	for chunk := range ch {
		toolCalls = append(toolCalls, chunk.ToolCalls...)
	}

	if len(toolCalls) != 1 {
		t.Fatalf("应有 1 个 ToolCall, 实际 %d", len(toolCalls))
	}
	tc := toolCalls[0]
	if tc.ID != "call_1" {
		t.Errorf("ID 不匹配: %s", tc.ID)
	}
	if tc.Name != "get_weather" {
		t.Errorf("Name 不匹配: %s", tc.Name)
	}
	if tc.Arguments != `{"city":"Beijing"}` {
		t.Errorf("Arguments 不匹配: %s", tc.Arguments)
	}
}

func TestParseSSEStream_Empty(t *testing.T) {
	body := io.NopCloser(strings.NewReader(""))
	ch := make(chan StreamChunk, 10)

	c := &Client{}
	go func() {
		c.parseSSEStream(body, ch)
		close(ch)
	}()

	// 应正常结束, 无 chunk
	for range ch {
		// 消费
	}
}

func TestParseSSEStream_InvalidJSON(t *testing.T) {
	sseData := strings.Join([]string{
		`data: invalid json`,
		``,
		`data: {"choices":[{"delta":{"content":"valid"}}]}`,
		``,
		`data: [DONE]`,
		``,
	}, "\n")

	body := io.NopCloser(strings.NewReader(sseData))
	ch := make(chan StreamChunk, 10)

	c := &Client{}
	go func() {
		c.parseSSEStream(body, ch)
		close(ch)
	}()

	var text string
	for chunk := range ch {
		text += chunk.Text
	}

	// 非法 JSON 行应被跳过, 只保留 valid
	if text != "valid" {
		t.Errorf("应跳过非法 JSON, 文本: %q", text)
	}
}

// ==================== Chat (非流式) 集成测试 ====================

func TestChat_NonStream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 验证请求
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("Authorization 头不正确: %s", r.Header.Get("Authorization"))
		}

		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"choices": [{
				"message": {
					"role": "assistant",
					"content": "Hi there!"
				}
			}]
		}`))
	}))
	defer server.Close()

	c := &Client{
		APIBase:        server.URL,
		APIKey:         "test-key",
		Model:          "test-model",
		MaxTokens:      100,
		ConnectTimeout: 5 * time.Second,
		ReadTimeout:    5 * time.Second,
	}

	ch, err := c.Chat(ChatParams{
		Messages: []Message{{Role: "user", Content: "hello"}},
	})
	if err != nil {
		t.Fatalf("Chat 失败: %v", err)
	}

	var text string
	var done bool
	var streamErr error
	for chunk := range ch {
		if chunk.Error != nil {
			streamErr = chunk.Error
		}
		if chunk.Text != "" {
			text = chunk.Text
		}
		if chunk.Done {
			done = true
		}
	}

	if streamErr != nil {
		t.Fatalf("流错误: %v", streamErr)
	}
	if text != "Hi there!" {
		t.Errorf("文本不匹配: %s", text)
	}
	if !done {
		t.Error("应收到 Done 信号")
	}
}

func TestChat_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":"invalid api key"}`))
	}))
	defer server.Close()

	c := &Client{
		APIBase:        server.URL,
		APIKey:         "bad-key",
		Model:          "test",
		MaxTokens:      100,
		ConnectTimeout: 5 * time.Second,
		ReadTimeout:    5 * time.Second,
	}

	ch, err := c.Chat(ChatParams{
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Chat 不应立即返回错误: %v", err)
	}

	var streamErr error
	for chunk := range ch {
		if chunk.Error != nil {
			streamErr = chunk.Error
		}
	}

	if streamErr == nil {
		t.Error("应返回 HTTP 错误")
	}
}

// ==================== ChatStream (流式) 集成测试 ====================

func TestChatStream_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"Hello\"}}]}\n\n"))
		w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\" stream\"}}]}\n\n"))
		w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	c := &Client{
		APIBase:        server.URL,
		APIKey:         "test-key",
		Model:          "test",
		MaxTokens:      100,
		ConnectTimeout: 5 * time.Second,
		ReadTimeout:    5 * time.Second,
		MaxRetries:     0,
	}

	ch, err := c.ChatStream(ChatParams{
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("ChatStream 失败: %v", err)
	}

	var text string
	var streamErr error
	for chunk := range ch {
		if chunk.Error != nil {
			streamErr = chunk.Error
		}
		text += chunk.Text
	}

	if streamErr != nil {
		t.Fatalf("流错误: %v", streamErr)
	}
	if text != "Hello stream" {
		t.Errorf("文本不匹配: %q", text)
	}
}

func TestChatStream_RetryOn429(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 2 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"recovered\"}}]}\n\n"))
		w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	c := &Client{
		APIBase:        server.URL,
		APIKey:         "test",
		Model:          "test",
		MaxTokens:      100,
		ConnectTimeout: 5 * time.Second,
		ReadTimeout:    5 * time.Second,
		MaxRetries:     2,
	}

	ch, err := c.ChatStream(ChatParams{
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("ChatStream 失败: %v", err)
	}

	var text string
	var streamErr error
	for chunk := range ch {
		if chunk.Error != nil {
			streamErr = chunk.Error
		}
		text += chunk.Text
	}

	if streamErr != nil {
		t.Fatalf("重试后应成功: %v", streamErr)
	}
	if text != "recovered" {
		t.Errorf("文本不匹配: %q", text)
	}
	if attempts < 2 {
		t.Errorf("应至少尝试 2 次, 实际 %d", attempts)
	}
}

func TestChatStream_RetryExhausted(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "0")
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	c := &Client{
		APIBase:        server.URL,
		APIKey:         "test",
		Model:          "test",
		MaxTokens:      100,
		ConnectTimeout: 5 * time.Second,
		ReadTimeout:    5 * time.Second,
		MaxRetries:     1,
	}

	ch, _ := c.ChatStream(ChatParams{
		Messages: []Message{{Role: "user", Content: "hi"}},
	})

	var streamErr error
	for chunk := range ch {
		if chunk.Error != nil {
			streamErr = chunk.Error
		}
	}

	if streamErr == nil {
		t.Error("重试耗尽应返回错误")
	}
}
