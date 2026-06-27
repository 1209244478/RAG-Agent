package kb

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/genericagent/ga/internal/llm"
)

// SuggestionEngine 智能推荐引擎
// 利用 LLM 分析知识库内容，提供链接推荐、标签推荐、摘要生成
type SuggestionEngine struct {
	store     *Store
	linker    *Linker
	graph     *Graph
	llmClient *llm.Client
}

// NewSuggestionEngine 创建推荐引擎
func NewSuggestionEngine(store *Store, linker *Linker, graph *Graph, llmClient *llm.Client) *SuggestionEngine {
	return &SuggestionEngine{
		store:     store,
		linker:    linker,
		graph:     graph,
		llmClient: llmClient,
	}
}

// LinkSuggestion 链接推荐
type LinkSuggestion struct {
	Text       string `json:"text"`        // 推荐链接的文本
	PageTitle  string `json:"page_title"`  // 推荐的页面标题
	Reason     string `json:"reason"`      // 推荐理由
	Exists     bool   `json:"exists"`      // 目标页面是否已存在
}

// SuggestLinks 为内容推荐相关页面链接
func (s *SuggestionEngine) SuggestLinks(content string) ([]LinkSuggestion, error) {
	if s.llmClient == nil {
		return nil, fmt.Errorf("LLM client not configured")
	}

	// 获取所有页面标题作为候选
	pages, _ := s.store.ListPages()
	var pageTitles []string
	for _, p := range pages {
		pageTitles = append(pageTitles, p.Title)
	}

	// 如果页面太多，只取前 200 个
	if len(pageTitles) > 200 {
		pageTitles = pageTitles[:200]
	}

	systemPrompt := fmt.Sprintf(`你是一个知识库助手。请分析给定内容，推荐应该添加的双向链接。

现有知识库中的页面列表:
%s

请推荐 3-5 个最相关的页面链接。返回 JSON 数组格式:
[{"text": "内容中应该被链接的文本片段", "page_title": "推荐的页面标题", "reason": "推荐理由"}]

要求:
1. page_title 必须从上面的页面列表中选择
2. text 是内容中实际存在的文本片段
3. 只推荐真正相关的内容`, strings.Join(pageTitles, "\n"))

	messages := []llm.Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: "请为以下内容推荐链接:\n\n" + content},
	}

	resp, err := s.callLLM(messages, 0.3)
	if err != nil {
		return nil, err
	}

	// 解析 LLM 返回的 JSON
	var suggestions []LinkSuggestion
	if err := parseJSONResponse(resp, &suggestions); err != nil {
		return nil, fmt.Errorf("parse suggestions: %w", err)
	}

	// 标记页面是否存在
	pageSet := make(map[string]bool)
	for _, t := range pageTitles {
		pageSet[t] = true
	}
	for i := range suggestions {
		suggestions[i].Exists = pageSet[suggestions[i].PageTitle]
	}

	return suggestions, nil
}

// SuggestTags 为页面推荐标签
func (s *SuggestionEngine) SuggestTags(pageTitle string, content string) ([]string, error) {
	if s.llmClient == nil {
		return nil, fmt.Errorf("LLM client not configured")
	}

	// 获取已有标签
	existingTags, _ := s.linker.GetAllTags()
	var tagList []string
	for tag := range existingTags {
		tagList = append(tagList, tag)
	}

	systemPrompt := fmt.Sprintf(`你是一个知识库助手。请为给定内容推荐 3-5 个标签。

知识库中已有的标签（优先复用）:
%s

返回 JSON 数组格式: ["标签1", "标签2", "标签3"]
要求: 标签简洁、有意义，优先复用已有标签。`, strings.Join(tagList, ", "))

	messages := []llm.Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: fmt.Sprintf("页面标题: %s\n\n内容:\n%s", pageTitle, content)},
	}

	resp, err := s.callLLM(messages, 0.3)
	if err != nil {
		return nil, err
	}

	var tags []string
	if err := parseJSONResponse(resp, &tags); err != nil {
		return nil, fmt.Errorf("parse tags: %w", err)
	}

	return tags, nil
}

// SuggestSummary 生成页面摘要
func (s *SuggestionEngine) SuggestSummary(pageTitle string) (string, error) {
	if s.llmClient == nil {
		return "", fmt.Errorf("LLM client not configured")
	}

	page, err := s.store.GetPageByTitle(pageTitle)
	if err != nil || page == nil {
		return "", fmt.Errorf("page not found: %s", pageTitle)
	}

	blocks, err := s.store.GetBlocks(page.ID)
	if err != nil {
		return "", err
	}

	var content strings.Builder
	for _, b := range blocks {
		content.WriteString(b.Content)
		content.WriteString("\n")
	}

	systemPrompt := `你是一个知识库助手。请为给定页面生成简洁的摘要。

要求:
1. 摘要不超过 200 字
2. 概括页面的核心内容
3. 提及页面的主要标签和链接关系（如果有）`

	messages := []llm.Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: fmt.Sprintf("页面标题: %s\n\n内容:\n%s", pageTitle, content.String())},
	}

	resp, err := s.callLLM(messages, 0.5)
	if err != nil {
		return "", err
	}

	return resp, nil
}

// GetInsights 获取知识图谱洞察
// 发现知识孤岛（无链接的页面）、枢纽节点（链接最多的页面）
func (s *SuggestionEngine) GetInsights() (*KBInsights, error) {
	if s.graph == nil {
		return nil, fmt.Errorf("graph not initialized")
	}

	insights := &KBInsights{}

	// 1. 孤岛页面：没有任何链接的页面
	pages, _ := s.store.ListPages()
	for _, page := range pages {
		outlinks, _ := s.store.GetOutlinks(page.ID)
		backlinks, _ := s.store.GetBacklinks(page.ID)
		if len(outlinks) == 0 && len(backlinks) == 0 {
			insights.Orphans = append(insights.Orphans, page.Title)
		}
	}

	// 2. 枢纽节点：链接数最多的页面
	type pageDegree struct {
		Title  string
		Degree int
	}
	var degrees []pageDegree
	for _, page := range pages {
		outlinks, _ := s.store.GetOutlinks(page.ID)
		backlinks, _ := s.store.GetBacklinks(page.ID)
		degree := len(outlinks) + len(backlinks)
		if degree > 0 {
			degrees = append(degrees, pageDegree{Title: page.Title, Degree: degree})
		}
	}

	// 按度数排序，取 top 10
	for i := 0; i < len(degrees)-1; i++ {
		for j := i + 1; j < len(degrees); j++ {
			if degrees[j].Degree > degrees[i].Degree {
				degrees[i], degrees[j] = degrees[j], degrees[i]
			}
		}
	}
	limit := 10
	if len(degrees) < limit {
		limit = len(degrees)
	}
	for i := 0; i < limit; i++ {
		insights.Hubs = append(insights.Hubs, HubPage{
			Title:  degrees[i].Title,
			Degree: degrees[i].Degree,
		})
	}

	// 3. 统计
	insights.TotalPages = len(pages)
	insights.OrphanCount = len(insights.Orphans)
	insights.HubCount = len(insights.Hubs)

	return insights, nil
}

// KBInsights 知识库洞察
type KBInsights struct {
	TotalPages  int        `json:"total_pages"`
	OrphanCount int        `json:"orphan_count"`
	HubCount    int        `json:"hub_count"`
	Orphans     []string   `json:"orphans"`     // 孤岛页面
	Hubs        []HubPage  `json:"hubs"`        // 枢纽节点
}

// HubPage 枢纽页面
type HubPage struct {
	Title  string `json:"title"`
	Degree int    `json:"degree"`
}

// ContinueWriting AI 续写：基于已有内容继续生成
func (s *SuggestionEngine) ContinueWriting(title string, content string) (string, error) {
	if s.llmClient == nil {
		return "", fmt.Errorf("LLM client not initialized")
	}

	// 获取相关页面作为上下文
	var contextPages []string
	if s.store != nil {
		pages, _ := s.store.Search(title, 3)
		for _, p := range pages {
			if p.Title != title {
				contextPages = append(contextPages, p.Title+": "+p.Snippet)
			}
		}
	}

	contextStr := ""
	if len(contextPages) > 0 {
		contextStr = "\n\nRelated pages for reference:\n" + strings.Join(contextPages, "\n")
	}

	prompt := fmt.Sprintf(`You are a knowledge base writing assistant. Continue writing the following page in Markdown format. 
Maintain the same style and tone. Generate 3-5 blocks of content that logically follow. Do not repeat existing content. Use [[Page Name]] for links and #tag for tags where appropriate.

Page title: %s
%s

Existing content:
---
%s
---

Continue writing (output only the new content, no preamble):`, title, contextStr, content)

	messages := []llm.Message{
		{Role: "system", Content: "You are a helpful knowledge base writing assistant that continues Markdown content naturally."},
		{Role: "user", Content: prompt},
	}

	continuation, err := s.callLLM(messages, 0.7)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(continuation), nil
}

// GraphQA 知识图谱问答：基于图谱结构回答问题
func (s *SuggestionEngine) GraphQA(question string) (string, error) {
	if s.llmClient == nil || s.graph == nil {
		return "", fmt.Errorf("LLM or graph not initialized")
	}

	// 获取图谱统计和结构
	stats, _ := s.graph.GetStats()
	graphData, _ := s.graph.GetGraphData()

	// 构建图谱摘要
	var hubNodes []string
	nodeDegrees := make(map[int64]int)
	for _, e := range graphData.Edges {
		nodeDegrees[e.Source]++
		nodeDegrees[e.Target]++
	}
	type nd struct {
		id  int64
		deg int
	}
	var sorted []nd
	for id, deg := range nodeDegrees {
		sorted = append(sorted, nd{id, deg})
	}
	for i := 0; i < len(sorted)-1; i++ {
		for j := i + 1; j < len(sorted); j++ {
			if sorted[j].deg > sorted[i].deg {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}
	limit := 15
	if len(sorted) < limit {
		limit = len(sorted)
	}
	for i := 0; i < limit; i++ {
		for _, n := range graphData.Nodes {
			if n.ID == sorted[i].id {
				hubNodes = append(hubNodes, fmt.Sprintf("%s (%d connections)", n.Label, sorted[i].deg))
				break
			}
		}
	}

	prompt := fmt.Sprintf(`Answer the user's question about their knowledge base graph structure.

Knowledge base stats:
- Total pages: %d
- Total links: %d
- Total tags: %d

Top hub pages (most connected):
%s

Question: %s

Provide insights about the knowledge base structure, connectivity, and suggestions for improvement. Answer in Chinese.`, stats.TotalPages, len(graphData.Edges), stats.TotalTags, strings.Join(hubNodes, "\n"), question)

	messages := []llm.Message{
		{Role: "system", Content: "You are a knowledge graph analyst. Answer questions about the structure and health of a knowledge base."},
		{Role: "user", Content: prompt},
	}

	return s.callLLM(messages, 0.5)
}

// AutoOrganize 自动整理：建议页面分类和重组
func (s *SuggestionEngine) AutoOrganize() (*OrganizeSuggestion, error) {
	if s.llmClient == nil || s.store == nil {
		return nil, fmt.Errorf("LLM or store not initialized")
	}

	pages, _ := s.store.ListPages()
	if len(pages) == 0 {
		return nil, fmt.Errorf("no pages to organize")
	}

	// 构建页面摘要
	var pageList []string
	limit := 50
	if len(pages) < limit {
		limit = len(pages)
	}
	for i := 0; i < limit; i++ {
		p := pages[i]
		tags := ""
		if len(p.Tags) > 0 {
			tags = " [" + strings.Join(p.Tags, ",") + "]"
		}
		pageList = append(pageList, fmt.Sprintf("- %s%s", p.Title, tags))
	}

	prompt := fmt.Sprintf(`Analyze the following knowledge base pages and suggest an organization structure.
Group related pages into categories/namespaces. Suggest tags for untagged pages.

Pages:
%s

Return a JSON object with this exact structure:
{
  "categories": [{"name": "Category Name", "pages": ["page1", "page2"]}],
  "tag_suggestions": [{"page": "page title", "tags": ["tag1", "tag2"]}],
  "merge_suggestions": [{"pages": ["page1", "page2"], "reason": "these cover similar topics"}]
}

Return only the JSON, no markdown formatting.`, strings.Join(pageList, "\n"))

	messages := []llm.Message{
		{Role: "system", Content: "You are a knowledge organization expert. Analyze pages and suggest structure improvements. Always return valid JSON."},
		{Role: "user", Content: prompt},
	}

	resp, err := s.callLLM(messages, 0.3)
	if err != nil {
		return nil, err
	}

	var suggestion OrganizeSuggestion
	if err := parseJSONResponse(resp, &suggestion); err != nil {
		// 返回原始文本作为 fallback
		return &OrganizeSuggestion{RawAnalysis: resp}, nil
	}
	return &suggestion, nil
}

// OrganizeSuggestion 整理建议
type OrganizeSuggestion struct {
	Categories       []OrganizeCategory `json:"categories"`
	TagSuggestions   []TagSuggestion     `json:"tag_suggestions"`
	MergeSuggestions []MergeSuggestion   `json:"merge_suggestions"`
	RawAnalysis      string              `json:"raw_analysis,omitempty"`
}

// OrganizeCategory 分类建议
type OrganizeCategory struct {
	Name  string   `json:"name"`
	Pages []string `json:"pages"`
}

// TagSuggestion 标签建议
type TagSuggestion struct {
	Page string   `json:"page"`
	Tags []string `json:"tags"`
}

// MergeSuggestion 合并建议
type MergeSuggestion struct {
	Pages  []string `json:"pages"`
	Reason string   `json:"reason"`
}

// --- 内部方法 ---

// callLLM 调用 LLM（非流式）
func (s *SuggestionEngine) callLLM(messages []llm.Message, temperature float64) (string, error) {
	streamCh, err := s.llmClient.ChatStream(llm.ChatParams{
		Messages:    messages,
		MaxTokens:   1024,
		Temperature: temperature,
	})
	if err != nil {
		return "", err
	}

	var fullText string
	for chunk := range streamCh {
		if chunk.Error != nil {
			return "", chunk.Error
		}
		if chunk.Text != "" {
			fullText += chunk.Text
		}
		if chunk.Done {
			break
		}
	}

	return fullText, nil
}

// parseJSONResponse 从 LLM 回答中提取 JSON
// LLM 可能返回 ```json ... ``` 包裹的 JSON
func parseJSONResponse(text string, target any) error {
	text = strings.TrimSpace(text)

	// 去除 markdown 代码块包裹
	if strings.HasPrefix(text, "```") {
		lines := strings.Split(text, "\n")
		// 去掉首行 ``` 和末行 ```
		if len(lines) >= 2 {
			lines = lines[1:]
			if len(lines) > 0 && strings.HasPrefix(strings.TrimSpace(lines[len(lines)-1]), "```") {
				lines = lines[:len(lines)-1]
			}
			text = strings.Join(lines, "\n")
		}
	}

	text = strings.TrimSpace(text)

	// 尝试找到 JSON 数组或对象的起始位置
	start := strings.IndexAny(text, "[{")
	if start < 0 {
		return fmt.Errorf("no JSON found in response")
	}

	// 找到匹配的结束位置
	end := strings.LastIndexAny(text, "]}")
	if end < 0 || end <= start {
		return fmt.Errorf("incomplete JSON in response")
	}

	jsonStr := text[start : end+1]
	return json.Unmarshal([]byte(jsonStr), target)
}
