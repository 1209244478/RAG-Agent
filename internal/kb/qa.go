package kb

import (
	"fmt"
	"strings"
	"sync"

	"github.com/genericagent/ga/internal/llm"
)

// QAOptions 问答选项
type QAOptions struct {
	Mode       string // "keyword" | "semantic" | "hybrid" (默认 hybrid)
	MaxContext int    // 最大上下文块数（默认 10）
	Stream     bool   // 是否流式输出（默认 true）
}

// QAChunk 流式问答片段
type QAChunk struct {
	Text    string    `json:"text"`
	Sources []QASource `json:"sources,omitempty"`
	Done    bool      `json:"done"`
	Error   string    `json:"error,omitempty"`
}

// QASource 引用来源
type QASource struct {
	PageTitle string  `json:"page_title"`
	BlockID   string  `json:"block_id"`
	Snippet   string  `json:"snippet"`
	Score     float64 `json:"score"`
}

// QAEngine 知识库问答引擎
// 基于知识库内容，通过多路检索 + LLM 生成回答
type QAEngine struct {
	store     *Store
	linker    *Linker
	graph     *Graph
	embedding *EmbeddingEngine // 可选，无则降级为关键词搜索
	llmClient *llm.Client
}

// NewQAEngine 创建问答引擎
func NewQAEngine(store *Store, linker *Linker, graph *Graph, embedding *EmbeddingEngine, llmClient *llm.Client) *QAEngine {
	return &QAEngine{
		store:     store,
		linker:    linker,
		graph:     graph,
		embedding: embedding,
		llmClient: llmClient,
	}
}

// Ask 基于知识库回答问题
// 返回流式回答 channel
func (q *QAEngine) Ask(question string, opts QAOptions) <-chan QAChunk {
	ch := make(chan QAChunk, 64)

	go func() {
		defer close(ch)

		if q.llmClient == nil {
			ch <- QAChunk{Error: "LLM client not configured", Done: true}
			return
		}

		if opts.Mode == "" {
			opts.Mode = "hybrid"
		}
		if opts.MaxContext <= 0 {
			opts.MaxContext = 10
		}

		// 1. 多路检索
		sources := q.retrieve(question, opts)
		if len(sources) == 0 {
			ch <- QAChunk{
				Text:  "抱歉，在知识库中未找到与您问题相关的内容。请尝试换个关键词，或先通过同步功能导入知识库内容。",
				Done:  true,
			}
			return
		}

		// 2. 组装 RAG 上下文
		context := q.buildContext(sources)

		// 3. 构建消息
		systemPrompt := `你是一个知识库助手。请基于以下知识库内容回答用户的问题。

要求：
1. 只基于提供的知识库内容回答，不要编造信息
2. 如果知识库内容不足以回答问题，请明确说明
3. 回答时在相关内容后标注来源，格式：[[页面名]]
4. 保持回答简洁、准确、有条理

知识库内容：
` + context

		messages := []llm.Message{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: question},
		}

		// 4. LLM 流式生成
		streamCh, err := q.llmClient.ChatStream(llm.ChatParams{
			Messages:    messages,
			MaxTokens:   q.llmClient.MaxTokens,
			Temperature: 0.3, // 问答用低温度，保证准确性
		})
		if err != nil {
			ch <- QAChunk{Error: fmt.Sprintf("LLM error: %v", err), Done: true}
			return
		}

		// 5. 流式输出
		var fullText string
		for chunk := range streamCh {
			if chunk.Error != nil {
				ch <- QAChunk{Error: chunk.Error.Error(), Done: true}
				return
			}
			if chunk.Text != "" {
				fullText += chunk.Text
				ch <- QAChunk{Text: chunk.Text}
			}
			if chunk.Done {
				break
			}
		}

		// 6. 发送来源信息
		ch <- QAChunk{
			Text:    fullText,
			Sources: sources,
			Done:    true,
		}
	}()

	return ch
}

// retrieve 多路检索
func (q *QAEngine) retrieve(question string, opts QAOptions) []QASource {
	var allSources []QASource
	seen := make(map[string]bool) // 去重 key: blockID

	// 关键词搜索
	if opts.Mode == "keyword" || opts.Mode == "hybrid" {
		results, err := q.store.Search(question, opts.MaxContext)
		if err == nil {
			for _, r := range results {
				if r.BlockID == "" {
					continue
				}
				key := r.BlockID
				if seen[key] {
					continue
				}
				seen[key] = true
				allSources = append(allSources, QASource{
					PageTitle: r.Title,
					BlockID:   r.BlockID,
					Snippet:   r.Snippet,
					Score:     r.Score,
				})
			}
		}
	}

	// 语义搜索
	if (opts.Mode == "semantic" || opts.Mode == "hybrid") && q.embedding != nil {
		if q.embedding.HasEmbeddings() {
			results, err := q.embedding.SemanticSearch(question, opts.MaxContext)
			if err == nil {
				for _, r := range results {
					if r.BlockID == "" {
						continue
					}
					key := r.BlockID
					if seen[key] {
						// 合并分数：已存在的来源提升分数
						for i, s := range allSources {
							if s.BlockID == key {
								allSources[i].Score = (s.Score + r.Score) / 2
								break
							}
						}
						continue
					}
					seen[key] = true
					allSources = append(allSources, QASource{
						PageTitle: r.Title,
						BlockID:   r.BlockID,
						Snippet:   r.Snippet,
						Score:     r.Score,
					})
				}
			}
		}
	}

	// 图谱扩展：找到相关页面后，获取其邻居页面的块作为补充上下文
	if len(allSources) > 0 && opts.Mode == "hybrid" {
		expandedSources := q.expandByGraph(allSources, seen, opts.MaxContext)
		allSources = append(allSources, expandedSources...)
	}

	// 限制总数
	if len(allSources) > opts.MaxContext {
		allSources = allSources[:opts.MaxContext]
	}

	return allSources
}

// expandByGraph 通过图谱扩展相关内容
func (q *QAEngine) expandByGraph(sources []QASource, seen map[string]bool, maxCount int) []QASource {
	if q.graph == nil {
		return nil
	}

	var expanded []QASource
	pageSet := make(map[int64]bool)

	// 收集已有来源的页面
	for _, s := range sources {
		if page, _ := q.store.GetPageByTitle(s.PageTitle); page != nil {
			pageSet[page.ID] = true
		}
	}

	// 对每个页面获取邻居
	for pageID := range pageSet {
		if len(expanded) >= maxCount/2 {
			break
		}
		neighbors := q.getNeighborBlocks(pageID)
		for _, nb := range neighbors {
			if seen[nb.blockID] {
				continue
			}
			seen[nb.blockID] = true
			expanded = append(expanded, QASource{
				PageTitle: nb.pageTitle,
				BlockID:   nb.blockID,
				Snippet:   nb.content,
				Score:     0.3, // 图谱扩展的分数较低
			})
		}
	}

	return expanded
}

// neighborBlock 邻居块信息
type neighborBlock struct {
	blockID   string
	pageTitle string
	content   string
}

// getNeighborBlocks 获取页面的邻居块
func (q *QAEngine) getNeighborBlocks(pageID int64) []neighborBlock {
	var result []neighborBlock

	// 获取出链目标页面的第一个块
	outlinks, _ := q.store.GetOutlinks(pageID)
	for _, link := range outlinks {
		if link.TargetPageID <= 0 {
			continue
		}
		blocks, _ := q.store.GetBlocks(link.TargetPageID)
		if len(blocks) > 0 {
			page, _ := q.store.GetPageByID(link.TargetPageID)
			title := ""
			if page != nil {
				title = page.Title
			}
			result = append(result, neighborBlock{
				blockID:   blocks[0].ID,
				pageTitle: title,
				content:   truncateContext(blocks[0].Content, 200),
			})
		}
		if len(result) >= 3 {
			break
		}
	}

	return result
}

// buildContext 组装 RAG 上下文
func (q *QAEngine) buildContext(sources []QASource) string {
	var sb strings.Builder

	for i, s := range sources {
		sb.WriteString(fmt.Sprintf("【来源 %d】页面: %s (相关度: %.2f)\n", i+1, s.PageTitle, s.Score))
		sb.WriteString(s.Snippet)
		sb.WriteString("\n\n")
	}

	return sb.String()
}

// AskSync 同步问答（非流式），返回完整回答
func (q *QAEngine) AskSync(question string, opts QAOptions) (string, []QASource, error) {
	var fullText string
	var sources []QASource
	var errMsg string

	ch := q.Ask(question, opts)
	for chunk := range ch {
		if chunk.Error != "" {
			errMsg = chunk.Error
		}
		if chunk.Text != "" && !chunk.Done {
			fullText += chunk.Text
		}
		if chunk.Done {
			if chunk.Text != "" {
				fullText = chunk.Text
			}
			sources = chunk.Sources
		}
	}

	if errMsg != "" {
		return "", nil, fmt.Errorf("%s", errMsg)
	}
	return fullText, sources, nil
}

// ExtractReferencedPages 从回答文本中提取引用的页面名
func ExtractReferencedPages(answer string) []string {
	links := ExtractLinks(answer)
	var pages []string
	seen := make(map[string]bool)
	for _, link := range links {
		if link.LinkType == "page_ref" && !seen[link.TargetPageTitle] {
			seen[link.TargetPageTitle] = true
			pages = append(pages, link.TargetPageTitle)
		}
	}
	return pages
}

// EnsureQAEngine 确保问答引擎依赖就绪（线程安全懒加载）
type QAEngineFactory struct {
	store     *Store
	linker    *Linker
	graph     *Graph
	embedding *EmbeddingEngine
	llmClient *llm.Client

	mu      sync.Mutex
	engine  *QAEngine
}

func NewQAEngineFactory(store *Store, linker *Linker, graph *Graph, embedding *EmbeddingEngine, llmClient *llm.Client) *QAEngineFactory {
	return &QAEngineFactory{
		store:     store,
		linker:    linker,
		graph:     graph,
		embedding: embedding,
		llmClient: llmClient,
	}
}

func (f *QAEngineFactory) GetEngine() *QAEngine {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.engine == nil {
		f.engine = NewQAEngine(f.store, f.linker, f.graph, f.embedding, f.llmClient)
	}
	return f.engine
}
