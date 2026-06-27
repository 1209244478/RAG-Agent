package kb

import (
	"encoding/json"
	"fmt"
	"strings"
)

// KBTools 知识库工具集
// 为 Agent 提供知识库操作能力，返回 JSON 字符串结果
type KBTools struct {
	store     *Store
	linker    *Linker
	graph     *Graph
	embedding *EmbeddingEngine
}

// NewKBTools 创建知识库工具集
func NewKBTools(store *Store, linker *Linker, graph *Graph, embedding *EmbeddingEngine) *KBTools {
	return &KBTools{
		store:     store,
		linker:    linker,
		graph:     graph,
		embedding: embedding,
	}
}

// Search 搜索知识库
// mode: "keyword" | "semantic" | "hybrid"
func (t *KBTools) Search(query string, mode string, limit int) (string, error) {
	if t.store == nil {
		return "", fmt.Errorf("knowledge base not initialized")
	}
	if mode == "" {
		mode = "keyword"
	}
	if limit <= 0 {
		limit = 10
	}

	var results []SearchResult
	var err error

	switch mode {
	case "semantic":
		if t.embedding == nil || !t.embedding.HasEmbeddings() {
			return "", fmt.Errorf("semantic search not available (no embeddings), use keyword mode")
		}
		results, err = t.embedding.SemanticSearch(query, limit)
	case "hybrid":
		results = t.hybridSearch(query, limit)
	default:
		results, err = t.store.Search(query, limit)
	}

	if err != nil {
		return "", err
	}

	return t.formatSearchResults(results), nil
}

// ReadPage 读取页面完整内容
func (t *KBTools) ReadPage(title string) (string, error) {
	if t.store == nil {
		return "", fmt.Errorf("knowledge base not initialized")
	}

	page, err := t.store.GetPageByTitle(title)
	if err != nil {
		return "", err
	}
	if page == nil {
		return "", fmt.Errorf("page not found: %s", title)
	}

	blocks, err := t.store.GetBlocks(page.ID)
	if err != nil {
		return "", err
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("# %s\n\n", page.Title))

	if len(page.Tags) > 0 {
		sb.WriteString(fmt.Sprintf("Tags: %s\n\n", strings.Join(page.Tags, ", ")))
	}

	for _, block := range blocks {
		indent := strings.Repeat("  ", block.Level)
		sb.WriteString(indent)
		sb.WriteString(block.Content)
		sb.WriteString("\n")
	}

	// 附带反向链接信息
	backlinks, _ := t.store.GetBacklinks(page.ID)
	if len(backlinks) > 0 {
		sb.WriteString("\n---\n反向链接:\n")
		for _, bl := range backlinks {
			sb.WriteString(fmt.Sprintf("- [[%s]]: %s\n", bl.PageTitle, truncateContext(bl.BlockContent, 80)))
		}
	}

	return sb.String(), nil
}

// GetBacklinks 获取页面的反向链接
func (t *KBTools) GetBacklinks(title string) (string, error) {
	if t.linker == nil {
		return "", fmt.Errorf("knowledge base not initialized")
	}

	backlinks, err := t.linker.GetBacklinks(title)
	if err != nil {
		return "", err
	}

	if len(backlinks) == 0 {
		return fmt.Sprintf("页面 '%s' 没有反向链接", title), nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("页面 '%s' 的反向链接 (%d):\n\n", title, len(backlinks)))
	for i, bl := range backlinks {
		sb.WriteString(fmt.Sprintf("%d. [[%s]]\n   %s\n\n", i+1, bl.PageTitle, truncateContext(bl.BlockContent, 100)))
	}

	return sb.String(), nil
}

// GetGraph 获取知识图谱
func (t *KBTools) GetGraph(pageTitle string, depth int) (string, error) {
	if t.graph == nil {
		return "", fmt.Errorf("knowledge base not initialized")
	}
	if depth <= 0 {
		depth = 1
	}

	var graphData *GraphData
	var err error

	if pageTitle != "" {
		graphData, err = t.graph.GetLocalGraph(pageTitle, depth)
	} else {
		graphData, err = t.graph.GetGraphData()
	}

	if err != nil {
		return "", err
	}

	// 格式化为可读文本
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("知识图谱: %d 个节点, %d 条边\n\n", len(graphData.Nodes), len(graphData.Edges)))

	if pageTitle != "" {
		sb.WriteString(fmt.Sprintf("以 '%s' 为中心 (深度 %d):\n", pageTitle, depth))
	} else {
		sb.WriteString("完整图谱 (Top 20 节点):\n")
	}

	// 列出节点
	limit := 20
	if len(graphData.Nodes) < limit {
		limit = len(graphData.Nodes)
	}
	for i := 0; i < limit; i++ {
		node := graphData.Nodes[i]
		sb.WriteString(fmt.Sprintf("- %s (连接数: %d)\n", node.Label, node.Size-1))
	}

	// 列出边
	if len(graphData.Edges) > 0 && len(graphData.Edges) <= 30 {
		sb.WriteString("\n链接关系:\n")
		nodeMap := make(map[int64]string)
		for _, n := range graphData.Nodes {
			nodeMap[n.ID] = n.Label
		}
		for _, edge := range graphData.Edges {
			src := nodeMap[edge.Source]
			tgt := nodeMap[edge.Target]
			if src != "" && tgt != "" {
				sb.WriteString(fmt.Sprintf("  %s --%s--> %s\n", src, edge.Type, tgt))
			}
		}
	}

	return sb.String(), nil
}

// WritePage 创建或更新页面
func (t *KBTools) WritePage(title string, content string) (string, error) {
	if t.linker == nil {
		return "", fmt.Errorf("knowledge base not initialized")
	}

	fileName := title + ".md"
	page, err := t.linker.IndexPage(content, fileName)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("页面 '%s' 已保存 (ID: %d, 块数: %d)", page.Title, page.ID, len(content)), nil
}

// ListPages 列出页面
func (t *KBTools) ListPages(filter string) (string, error) {
	if t.store == nil {
		return "", fmt.Errorf("knowledge base not initialized")
	}

	var pages []Page
	var err error

	switch filter {
	case "journal":
		pages, err = t.store.ListJournalPages(30)
	case "":
		pages, err = t.store.ListPages()
	default:
		pages, err = t.store.ListPagesByNamespace(filter)
	}

	if err != nil {
		return "", err
	}

	if len(pages) == 0 {
		return "知识库中没有页面", nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("知识库页面 (%d):\n\n", len(pages)))
	for i, p := range pages {
		tags := ""
		if len(p.Tags) > 0 {
			tags = " [" + strings.Join(p.Tags, ", ") + "]"
		}
		sb.WriteString(fmt.Sprintf("%d. %s%s\n", i+1, p.Title, tags))
	}

	return sb.String(), nil
}

// GetStats 获取知识库统计
func (t *KBTools) GetStats() (string, error) {
	if t.graph == nil {
		return "", fmt.Errorf("knowledge base not initialized")
	}

	stats, err := t.graph.GetStats()
	if err != nil {
		return "", err
	}

	var sb strings.Builder
	sb.WriteString("知识库统计:\n")
	sb.WriteString(fmt.Sprintf("- 总页面数: %d\n", stats.TotalPages))
	sb.WriteString(fmt.Sprintf("- 日记页面: %d\n", stats.JournalPages))
	sb.WriteString(fmt.Sprintf("- 总块数: %d\n", stats.TotalBlocks))
	sb.WriteString(fmt.Sprintf("- 总链接数: %d\n", stats.TotalLinks))
	sb.WriteString(fmt.Sprintf("- 总标签数: %d\n", stats.TotalTags))

	if t.embedding != nil {
		embCount, _ := t.embedding.GetEmbeddingStats()
		sb.WriteString(fmt.Sprintf("- 向量索引数: %d\n", embCount))
	}

	return sb.String(), nil
}

// --- 内部方法 ---

// hybridSearch 混合搜索（关键词 + 语义）
func (t *KBTools) hybridSearch(query string, limit int) []SearchResult {
	seen := make(map[string]bool)
	var results []SearchResult

	// 关键词搜索
	if kwResults, err := t.store.Search(query, limit); err == nil {
		for _, r := range kwResults {
			key := r.BlockID + r.Title
			if !seen[key] {
				seen[key] = true
				results = append(results, r)
			}
		}
	}

	// 语义搜索
	if t.embedding != nil && t.embedding.HasEmbeddings() {
		if semResults, err := t.embedding.SemanticSearch(query, limit); err == nil {
			for _, r := range semResults {
				key := r.BlockID + r.Title
				if !seen[key] {
					seen[key] = true
					results = append(results, r)
				} else {
					// 合并分数
					for i, existing := range results {
						if existing.BlockID == r.BlockID {
							results[i].Score = (existing.Score + r.Score) / 2
							break
						}
					}
				}
			}
		}
	}

	if len(results) > limit {
		results = results[:limit]
	}
	return results
}

// formatSearchResults 格式化搜索结果
func (t *KBTools) formatSearchResults(results []SearchResult) string {
	if len(results) == 0 {
		return "未找到匹配结果"
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("搜索结果 (%d):\n\n", len(results)))
	for i, r := range results {
		sb.WriteString(fmt.Sprintf("%d. [%s] %s\n", i+1, r.Title, r.Snippet))
		if r.Score > 0 {
			sb.WriteString(fmt.Sprintf("   相关度: %.2f\n", r.Score))
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

// KBToolsSchema 返回知识库工具的 schema 定义
func KBToolsSchema() []map[string]any {
	return []map[string]any{
		{
			"type": "function",
			"function": map[string]any{
				"name":        "kb_search",
				"description": "搜索知识库。支持关键词搜索(keyword)、语义搜索(semantic)和混合搜索(hybrid)。当需要查找知识库中的信息时使用。",
				"parameters": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"query": map[string]any{
							"type":        "string",
							"description": "搜索查询",
						},
						"mode": map[string]any{
							"type":        "string",
							"enum":         []string{"keyword", "semantic", "hybrid"},
							"description": "搜索模式，默认 keyword",
						},
						"limit": map[string]any{
							"type":        "integer",
							"description": "返回结果数量，默认 10",
						},
					},
					"required": []string{"query"},
				},
			},
		},
		{
			"type": "function",
			"function": map[string]any{
				"name":        "kb_read_page",
				"description": "读取知识库页面的完整内容，包括块结构和反向链接。",
				"parameters": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"title": map[string]any{
							"type":        "string",
							"description": "页面标题",
						},
					},
					"required": []string{"title"},
				},
			},
		},
		{
			"type": "function",
			"function": map[string]any{
				"name":        "kb_get_backlinks",
				"description": "获取知识库页面的反向链接（哪些页面引用了此页面）。",
				"parameters": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"title": map[string]any{
							"type":        "string",
							"description": "页面标题",
						},
					},
					"required": []string{"title"},
				},
			},
		},
		{
			"type": "function",
			"function": map[string]any{
				"name":        "kb_get_graph",
				"description": "获取知识图谱数据，发现页面之间的关联关系。可指定中心页面获取局部图谱。",
				"parameters": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"page": map[string]any{
							"type":        "string",
							"description": "中心页面标题（可选，不指定则返回完整图谱概览）",
						},
						"depth": map[string]any{
							"type":        "integer",
							"description": "图谱深度，默认 1",
						},
					},
				},
			},
		},
		{
			"type": "function",
			"function": map[string]any{
				"name":        "kb_write_page",
				"description": "创建或更新知识库页面。内容为 Markdown 格式，支持 [[双链]] 和 #标签。",
				"parameters": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"title": map[string]any{
							"type":        "string",
							"description": "页面标题",
						},
						"content": map[string]any{
							"type":        "string",
							"description": "页面内容（Markdown 格式）",
						},
					},
					"required": []string{"title", "content"},
				},
			},
		},
		{
			"type": "function",
			"function": map[string]any{
				"name":        "kb_list_pages",
				"description": "列出知识库中的页面。可按类型过滤：journal(日记)、命名空间名称。",
				"parameters": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"filter": map[string]any{
							"type":        "string",
							"description": "过滤条件：journal(日记) 或命名空间名称（可选）",
						},
					},
				},
			},
		},
	}
}

// HandleTool 处理 KB 工具调用，返回结果字符串
// 供 tool.Router 调用
func (t *KBTools) HandleTool(toolName string, args map[string]any) (string, error) {
	switch toolName {
	case "kb_search":
		query, _ := args["query"].(string)
		mode, _ := args["mode"].(string)
		limit := 10
		if l, ok := args["limit"].(float64); ok {
			limit = int(l)
		}
		return t.Search(query, mode, limit)

	case "kb_read_page":
		title, _ := args["title"].(string)
		return t.ReadPage(title)

	case "kb_get_backlinks":
		title, _ := args["title"].(string)
		return t.GetBacklinks(title)

	case "kb_get_graph":
		page, _ := args["page"].(string)
		depth := 1
		if d, ok := args["depth"].(float64); ok {
			depth = int(d)
		}
		return t.GetGraph(page, depth)

	case "kb_write_page":
		title, _ := args["title"].(string)
		content, _ := args["content"].(string)
		return t.WritePage(title, content)

	case "kb_list_pages":
		filter, _ := args["filter"].(string)
		return t.ListPages(filter)

	default:
		return "", fmt.Errorf("unknown KB tool: %s", toolName)
	}
}

// MarshalJSON 序列化工具结果为 JSON
func MarshalToolResult(data any) string {
	b, _ := json.Marshal(data)
	return string(b)
}
