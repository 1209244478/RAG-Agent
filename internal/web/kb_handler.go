package web

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/genericagent/ga/internal/kb"
)

// KBPageDetail 页面详情（含块结构）
type KBPageDetail struct {
	kb.Page
	Blocks    []kb.Block      `json:"blocks"`
	Backlinks []kb.Backlink   `json:"backlinks"`
	Outlinks  []kb.Link       `json:"outlinks"`
}

// --- 页面管理 ---

// KBListPages 列出所有页面
func (h *Handler) KBListPages(c *gin.Context) {
	if h.kbStore == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "knowledge base not initialized"})
		return
	}

	// 支持过滤参数
	journalOnly := c.Query("journal")
	namespace := c.Query("namespace")

	var pages []kb.Page
	var err error

	if journalOnly == "true" {
		limit, _ := strconv.Atoi(c.DefaultQuery("limit", "30"))
		pages, err = h.kbStore.ListJournalPages(limit)
	} else if namespace != "" {
		pages, err = h.kbStore.ListPagesByNamespace(namespace)
	} else {
		pages, err = h.kbStore.ListPages()
	}

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"pages": pages})
}

// KBGetPage 获取页面详情（含块、反向链接、外部链接）
func (h *Handler) KBGetPage(c *gin.Context) {
	if h.kbStore == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "knowledge base not initialized"})
		return
	}

	title := c.Param("title")
	page, err := h.kbStore.GetPageByTitle(title)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if page == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "page not found"})
		return
	}

	blocks, _ := h.kbStore.GetBlocks(page.ID)
	backlinks, _ := h.kbStore.GetBacklinks(page.ID)
	outlinks, _ := h.kbStore.GetOutlinks(page.ID)

	c.JSON(http.StatusOK, KBPageDetail{
		Page:      *page,
		Blocks:    blocks,
		Backlinks: backlinks,
		Outlinks:  outlinks,
	})
}

// KBCreatePage 创建新页面
func (h *Handler) KBCreatePage(c *gin.Context) {
	if h.kbStore == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "knowledge base not initialized"})
		return
	}

	var req struct {
		Title   string `json:"title" binding:"required"`
		Content string `json:"content"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// 检查是否已存在
	if existing, _ := h.kbStore.GetPageByTitle(req.Title); existing != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "page already exists"})
		return
	}

	fileName := req.Title + ".md"
	page, err := h.kbLinker.IndexPage(req.Content, fileName)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 同时写入文件系统
	h.savePageToFile(fileName, req.Content)

	c.JSON(http.StatusCreated, page)
}

// KBUpdatePage 更新页面内容
func (h *Handler) KBUpdatePage(c *gin.Context) {
	if h.kbStore == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "knowledge base not initialized"})
		return
	}

	title := c.Param("title")
	var req struct {
		Content string `json:"content" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	page, err := h.kbStore.GetPageByTitle(title)
	if err != nil || page == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "page not found"})
		return
	}

	// 重新索引页面
	fileName := page.FileName
	if fileName == "" {
		fileName = title + ".md"
	}
	updatedPage, err := h.kbLinker.IndexPage(req.Content, fileName)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 同步到文件系统
	h.savePageToFile(fileName, req.Content)

	c.JSON(http.StatusOK, updatedPage)
}

// KBDeletePage 删除页面
func (h *Handler) KBDeletePage(c *gin.Context) {
	if h.kbStore == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "knowledge base not initialized"})
		return
	}

	title := c.Param("title")
	page, err := h.kbStore.GetPageByTitle(title)
	if err != nil || page == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "page not found"})
		return
	}

	permanent := c.Query("permanent") == "true"
	if permanent {
		if err := h.kbStore.DeletePage(page.ID); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	} else {
		if err := h.kbStore.SoftDeletePage(page.ID); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	}

	// 删除文件
	if page.FileName != "" {
		filePath := filepath.Join(h.rootDir, "data", "pages", page.FileName)
		os.Remove(filePath)
	}

	c.JSON(http.StatusOK, gin.H{"message": "deleted", "permanent": permanent})
}

// --- 块操作 ---

// KBGetBlock 获取单个块
func (h *Handler) KBGetBlock(c *gin.Context) {
	if h.kbStore == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "knowledge base not initialized"})
		return
	}

	blockID := c.Param("id")
	block, err := h.kbStore.GetBlock(blockID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if block == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "block not found"})
		return
	}
	c.JSON(http.StatusOK, block)
}

// KBUpdateBlock 更新块内容
func (h *Handler) KBUpdateBlock(c *gin.Context) {
	if h.kbStore == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "knowledge base not initialized"})
		return
	}

	blockID := c.Param("id")
	var req struct {
		Content string `json:"content" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := h.kbStore.UpdateBlockContent(blockID, req.Content); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "updated"})
}

// --- 链接与图谱 ---

// KBGetBacklinks 获取页面的反向链接
func (h *Handler) KBGetBacklinks(c *gin.Context) {
	if h.kbStore == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "knowledge base not initialized"})
		return
	}

	title := c.Param("title")
	backlinks, err := h.kbLinker.GetBacklinks(title)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"backlinks": backlinks})
}

// KBGetGraph 获取知识图谱数据
func (h *Handler) KBGetGraph(c *gin.Context) {
	if h.kbGraph == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "knowledge base not initialized"})
		return
	}

	// 支持局部图谱
	pageTitle := c.Query("page")
	depth, _ := strconv.Atoi(c.DefaultQuery("depth", "1"))

	var graphData *kb.GraphData
	var err error

	if pageTitle != "" {
		graphData, err = h.kbGraph.GetLocalGraph(pageTitle, depth)
	} else {
		graphData, err = h.kbGraph.GetGraphData()
	}

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, graphData)
}

// KBGetTags 获取所有标签
func (h *Handler) KBGetTags(c *gin.Context) {
	if h.kbLinker == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "knowledge base not initialized"})
		return
	}

	tags, err := h.kbLinker.GetAllTags()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"tags": tags})
}

// KBGetPagesByTag 获取标签下的页面
func (h *Handler) KBGetPagesByTag(c *gin.Context) {
	if h.kbLinker == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "knowledge base not initialized"})
		return
	}

	tag := c.Param("tag")
	pages, err := h.kbLinker.GetPagesByTag(tag)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"pages": pages})
}

// --- 搜索 ---

// KBSearch 全文搜索
func (h *Handler) KBSearch(c *gin.Context) {
	if h.kbStore == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "knowledge base not initialized"})
		return
	}

	query := c.Query("q")
	if query == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "query parameter 'q' is required"})
		return
	}

	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	results, err := h.kbStore.Search(query, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"results": results})
}

// --- 文件同步 ---

// KBSync 从工作空间目录同步 Markdown 文件
func (h *Handler) KBSync(c *gin.Context) {
	if h.kbGraph == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "knowledge base not initialized"})
		return
	}

	var req struct {
		Directory string `json:"directory"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		// 默认使用工作空间的 pages 目录
		req.Directory = filepath.Join(h.rootDir, "data", "pages")
	}

	if req.Directory == "" {
		req.Directory = filepath.Join(h.rootDir, "data", "pages")
	}

	// 确保目录存在
	if _, err := os.Stat(req.Directory); os.IsNotExist(err) {
		os.MkdirAll(req.Directory, 0755)
	}

	result, err := h.kbGraph.SyncFromDirectory(req.Directory)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, result)
}

// KBExportPage 导出页面为 Markdown
func (h *Handler) KBExportPage(c *gin.Context) {
	if h.kbGraph == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "knowledge base not initialized"})
		return
	}

	title := c.Param("title")
	content, err := h.kbGraph.ExportPage(title)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"content": content})
}

// KBGetStats 获取知识库统计信息
func (h *Handler) KBGetStats(c *gin.Context) {
	if h.kbGraph == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "knowledge base not initialized"})
		return
	}

	stats, err := h.kbGraph.GetStats()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, stats)
}

// --- 辅助函数 ---

// savePageToFile 将页面内容保存到文件系统
func (h *Handler) savePageToFile(fileName string, content string) {
	pagesDir := filepath.Join(h.rootDir, "data", "pages")
	os.MkdirAll(filepath.Dir(filepath.Join(pagesDir, fileName)), 0755)
	filePath := filepath.Join(pagesDir, fileName)
	os.WriteFile(filePath, []byte(content), 0644)
}

// --- AI 能力 ---

// KBQA 基于知识库的智能问答（流式）
func (h *Handler) KBQA(c *gin.Context) {
	if h.kbQA == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "QA engine not initialized (LLM not configured)"})
		return
	}

	var req struct {
		Question   string `json:"question" binding:"required"`
		Mode       string `json:"mode"`       // keyword | semantic | hybrid
		MaxContext int    `json:"max_context"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	engine := h.kbQA.GetEngine()

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "streaming not supported"})
		return
	}

	ch := engine.Ask(req.Question, kb.QAOptions{
		Mode:       req.Mode,
		MaxContext: req.MaxContext,
	})

	for chunk := range ch {
		data, _ := json.Marshal(chunk)
		c.Writer.Write([]byte("data: " + string(data) + "\n\n"))
		flusher.Flush()
	}
}

// KBSemanticSearch 语义搜索
func (h *Handler) KBSemanticSearch(c *gin.Context) {
	if h.kbEmbed == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "embedding engine not initialized"})
		return
	}

	var req struct {
		Query string `json:"query" binding:"required"`
		Limit int    `json:"limit"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.Limit <= 0 {
		req.Limit = 10
	}

	results, err := h.kbEmbed.SemanticSearch(req.Query, req.Limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"results": results, "count": len(results)})
}

// KBHybridSearch 混合搜索（关键词 + 语义）
func (h *Handler) KBHybridSearch(c *gin.Context) {
	if h.kbTools == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "knowledge base not initialized"})
		return
	}

	var req struct {
		Query string `json:"query" binding:"required"`
		Limit int    `json:"limit"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	result, err := h.kbTools.Search(req.Query, "hybrid", req.Limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"result": result})
}

// KBSuggestLinks 推荐页面链接
func (h *Handler) KBSuggestLinks(c *gin.Context) {
	if h.kbSuggest == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "suggestion engine not initialized"})
		return
	}

	var req struct {
		Content string `json:"content" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	suggestions, err := h.kbSuggest.SuggestLinks(req.Content)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"suggestions": suggestions})
}

// KBSuggestTags 推荐标签
func (h *Handler) KBSuggestTags(c *gin.Context) {
	if h.kbSuggest == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "suggestion engine not initialized"})
		return
	}

	var req struct {
		Title   string `json:"title" binding:"required"`
		Content string `json:"content" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	tags, err := h.kbSuggest.SuggestTags(req.Title, req.Content)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"tags": tags})
}

// KBSuggestSummary 生成页面摘要
func (h *Handler) KBSuggestSummary(c *gin.Context) {
	if h.kbSuggest == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "suggestion engine not initialized"})
		return
	}

	title := c.Param("title")
	if title == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "title required"})
		return
	}

	summary, err := h.kbSuggest.SuggestSummary(title)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"title": title, "summary": summary})
}

// KBInsights 获取知识库洞察
func (h *Handler) KBInsights(c *gin.Context) {
	if h.kbSuggest == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "suggestion engine not initialized"})
		return
	}

	insights, err := h.kbSuggest.GetInsights()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, insights)
}

// KBRebuildEmbeddings 重建向量索引
func (h *Handler) KBRebuildEmbeddings(c *gin.Context) {
	if h.kbEmbed == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "embedding engine not initialized"})
		return
	}

	count, _, err := h.kbEmbed.IndexAllEmbeddings()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "embeddings rebuilt", "count": count})
}

// KBGetEmbeddingStats 获取向量索引统计
func (h *Handler) KBGetEmbeddingStats(c *gin.Context) {
	if h.kbEmbed == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "embedding engine not initialized"})
		return
	}

	count, err := h.kbEmbed.GetEmbeddingStats()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"embedding_count": count})
}

// ==================== 阶段 5 高级特性 API ====================

// --- 日记 ---

// KBJournalToday 获取或创建今日日记
func (h *Handler) KBJournalToday(c *gin.Context) {
	if h.kbJournal == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "journal not initialized"})
		return
	}
	page, err := h.kbJournal.EnsureToday()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, page)
}

// KBJournalList 列出最近日记
func (h *Handler) KBJournalList(c *gin.Context) {
	if h.kbJournal == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "journal not initialized"})
		return
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "30"))
	pages, err := h.kbJournal.ListRecent(limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"pages": pages})
}

// KBJournalTemplate 返回日记默认模板内容
func (h *Handler) KBJournalTemplate(c *gin.Context) {
	if h.kbJournal == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "journal not initialized"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"title":   h.kbJournal.TodayTitle(),
		"content": h.kbJournal.DefaultTemplate(),
	})
}

// --- 任务管理 ---

// KBTaskList 列出任务
func (h *Handler) KBTaskList(c *gin.Context) {
	if h.kbTask == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "task manager not initialized"})
		return
	}
	status := c.Query("status")
	pageIDStr := c.Query("page_id")

	var tasks []kb.Task
	var err error
	if pageIDStr != "" {
		pageID, _ := strconv.ParseInt(pageIDStr, 10, 64)
		tasks, err = h.kbTask.TasksByPage(pageID)
	} else if status != "" {
		tasks, err = h.kbTask.TasksByStatus(status)
	} else {
		tasks, err = h.kbTask.AllTasks()
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"tasks": tasks})
}

// KBTaskStats 任务统计
func (h *Handler) KBTaskStats(c *gin.Context) {
	if h.kbTask == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "task manager not initialized"})
		return
	}
	stats, err := h.kbTask.Stats()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, stats)
}

// KBTaskUpdateStatus 更新任务状态
func (h *Handler) KBTaskUpdateStatus(c *gin.Context) {
	if h.kbTask == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "task manager not initialized"})
		return
	}
	blockID := c.Param("block_id")
	var req struct {
		Status string `json:"status" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.kbTask.UpdateStatus(blockID, req.Status); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// --- 块嵌入 ---

// KBBlockEmbed 展开块嵌入
func (h *Handler) KBBlockEmbed(c *gin.Context) {
	if h.kbEmbedEng == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "embed engine not initialized"})
		return
	}
	blockID := c.Param("id")
	content, err := h.kbEmbedEng.ExpandBlock(blockID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"content": content})
}

// KBBlockEmbedTree 获取嵌入树
func (h *Handler) KBBlockEmbedTree(c *gin.Context) {
	if h.kbEmbedEng == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "embed engine not initialized"})
		return
	}
	blockID := c.Param("id")
	tree := h.kbEmbedEng.GetEmbedTree(blockID)
	c.JSON(http.StatusOK, tree)
}

// --- 模板 ---

// KBTemplateList 列出所有模板
func (h *Handler) KBTemplateList(c *gin.Context) {
	if h.kbTemplate == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "template engine not initialized"})
		return
	}
	pages, err := h.kbTemplate.ListTemplates()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"templates": pages})
}

// KBTemplateGet 获取模板内容
func (h *Handler) KBTemplateGet(c *gin.Context) {
	if h.kbTemplate == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "template engine not initialized"})
		return
	}
	name := c.Param("name")
	content, err := h.kbTemplate.GetTemplate(name)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"name": name, "content": content})
}

// KBTemplateApply 应用模板创建新页面内容
func (h *Handler) KBTemplateApply(c *gin.Context) {
	if h.kbTemplate == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "template engine not initialized"})
		return
	}
	var req struct {
		Template string `json:"template" binding:"required"`
		PageTitle string `json:"page_title" binding:"required"`
		Args      []string `json:"args"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	content, err := h.kbTemplate.ApplyTemplate(req.Template, req.PageTitle, req.Args)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"content": content})
}

// --- 版本历史 ---

// KBVersionListByTitle 通过页面标题列出版本
func (h *Handler) KBVersionListByTitle(c *gin.Context) {
	if h.kbVersion == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "version manager not initialized"})
		return
	}
	title := c.Param("title")
	page, err := h.kbStore.GetPageByTitle(title)
	if err != nil || page == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "page not found"})
		return
	}
	versions, err := h.kbVersion.ListVersions(page.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"versions": versions})
}

// KBVersionList 列出页面版本
func (h *Handler) KBVersionList(c *gin.Context) {
	if h.kbVersion == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "version manager not initialized"})
		return
	}
	pageIDStr := c.Param("page_id")
	pageID, _ := strconv.ParseInt(pageIDStr, 10, 64)
	versions, err := h.kbVersion.ListVersions(pageID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"versions": versions})
}

// KBVersionGet 获取特定版本
func (h *Handler) KBVersionGet(c *gin.Context) {
	if h.kbVersion == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "version manager not initialized"})
		return
	}
	pageID, _ := strconv.ParseInt(c.Param("page_id"), 10, 64)
	version, _ := strconv.Atoi(c.Param("version"))
	pv, err := h.kbVersion.GetVersion(pageID, version)
	if err != nil || pv == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "version not found"})
		return
	}
	c.JSON(http.StatusOK, pv)
}

// KBVersionDiff 比较两个版本
func (h *Handler) KBVersionDiff(c *gin.Context) {
	if h.kbVersion == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "version manager not initialized"})
		return
	}
	pageID, _ := strconv.ParseInt(c.Param("page_id"), 10, 64)
	from, _ := strconv.Atoi(c.Param("from"))
	to, _ := strconv.Atoi(c.Param("to"))
	diff, err := h.kbVersion.Diff(pageID, from, to)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, diff)
}

// KBVersionRollback 回滚到指定版本
func (h *Handler) KBVersionRollback(c *gin.Context) {
	if h.kbVersion == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "version manager not initialized"})
		return
	}
	pageID, _ := strconv.ParseInt(c.Param("page_id"), 10, 64)
	version, _ := strconv.Atoi(c.Param("version"))
	content, err := h.kbVersion.Rollback(pageID, version)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"content": content, "rolled_back_to": version})
}

// --- 查询 DSL ---

// KBQuery 执行查询
func (h *Handler) KBQuery(c *gin.Context) {
	if h.kbQuery == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "query engine not initialized"})
		return
	}
	var req struct {
		Query     string `json:"query" binding:"required"`
		Aggregate bool   `json:"aggregate"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	expr, err := h.kbQuery.Parse(req.Query)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	var result *kb.QueryResult
	if req.Aggregate {
		result, err = h.kbQuery.ExecuteWithAggregate(expr)
	} else {
		result, err = h.kbQuery.Execute(expr)
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

// --- 导入 ---

// KBImport 导入 Markdown 目录
func (h *Handler) KBImport(c *gin.Context) {
	if h.kbImporter == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "importer not initialized"})
		return
	}
	var req struct {
		Directory string `json:"directory" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	result, err := h.kbImporter.ImportDirectory(req.Directory)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

// --- FTS5 重建索引 ---

// KBRebuildFTS 重建全文索引
func (h *Handler) KBRebuildFTS(c *gin.Context) {
	if h.kbStore == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "knowledge base not initialized"})
		return
	}
	if err := h.kbStore.RebuildFTS(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok", "message": "FTS index rebuilt"})
}

// KBSearchFTS FTS5 全文搜索
func (h *Handler) KBSearchFTS(c *gin.Context) {
	if h.kbStore == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "knowledge base not initialized"})
		return
	}
	q := c.Query("q")
	if q == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "query parameter 'q' is required"})
		return
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	results, err := h.kbStore.SearchFTS(q, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"results": results, "query": q})
}

// ============== 属性系统 ==============

// KBSetPageProperty 设置页面属性
func (h *Handler) KBSetPageProperty(c *gin.Context) {
	if h.kbStore == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "knowledge base not initialized"})
		return
	}
	title := c.Param("title")
	page, err := h.kbStore.GetPageByTitle(title)
	if err != nil || page == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "page not found"})
		return
	}
	var body struct {
		Name  string `json:"name"`
		Value any    `json:"value"`
		Type  string `json:"type"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	valType := kb.PropertyType(body.Type)
	if valType == "" {
		valType = kb.PropTypeString
	}
	if err := h.kbStore.SetPageProperty(page.ID, body.Name, body.Value, valType); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// KBGetPageProperties 获取页面属性
func (h *Handler) KBGetPageProperties(c *gin.Context) {
	if h.kbStore == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "knowledge base not initialized"})
		return
	}
	title := c.Param("title")
	page, err := h.kbStore.GetPageByTitle(title)
	if err != nil || page == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "page not found"})
		return
	}
	props, err := h.kbStore.GetPageProperties(page.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"properties": props})
}

// KBDeletePageProperty 删除页面属性
func (h *Handler) KBDeletePageProperty(c *gin.Context) {
	if h.kbStore == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "knowledge base not initialized"})
		return
	}
	title := c.Param("title")
	name := c.Param("name")
	page, err := h.kbStore.GetPageByTitle(title)
	if err != nil || page == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "page not found"})
		return
	}
	if err := h.kbStore.DeletePageProperty(page.ID, name); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// KBQueryByProperty 按属性查询页面
func (h *Handler) KBQueryByProperty(c *gin.Context) {
	if h.kbStore == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "knowledge base not initialized"})
		return
	}
	name := c.Query("name")
	value := c.Query("value")
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name parameter required"})
		return
	}
	pages, err := h.kbStore.QueryByProperty(name, value)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"pages": pages})
}

// KBPropertyNames 获取所有属性名
func (h *Handler) KBPropertyNames(c *gin.Context) {
	if h.kbStore == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "knowledge base not initialized"})
		return
	}
	names, err := h.kbStore.GetAllPropertyNames()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"names": names})
}

// KBSetPropertySchema 设置属性 schema
func (h *Handler) KBSetPropertySchema(c *gin.Context) {
	if h.kbStore == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "knowledge base not initialized"})
		return
	}
	var body struct {
		Namespace string             `json:"namespace"`
		Schema    kb.PropertySchema `json:"schema"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.kbStore.SetPropertySchema(body.Namespace, body.Schema); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// KBGetPropertySchemas 获取属性 schema
func (h *Handler) KBGetPropertySchemas(c *gin.Context) {
	if h.kbStore == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "knowledge base not initialized"})
		return
	}
	ns := c.DefaultQuery("namespace", "")
	schemas, err := h.kbStore.GetPropertySchemas(ns)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"schemas": schemas})
}

// ============== 收藏夹 ==============

// KBFavoriteAdd 添加收藏
func (h *Handler) KBFavoriteAdd(c *gin.Context) {
	if h.kbStore == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "knowledge base not initialized"})
		return
	}
	title := c.Param("title")
	page, err := h.kbStore.GetPageByTitle(title)
	if err != nil || page == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "page not found"})
		return
	}
	if err := h.kbStore.AddFavorite(page.ID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// KBFavoriteRemove 移除收藏
func (h *Handler) KBFavoriteRemove(c *gin.Context) {
	if h.kbStore == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "knowledge base not initialized"})
		return
	}
	title := c.Param("title")
	page, err := h.kbStore.GetPageByTitle(title)
	if err != nil || page == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "page not found"})
		return
	}
	if err := h.kbStore.RemoveFavorite(page.ID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// KBFavoriteList 列出收藏
func (h *Handler) KBFavoriteList(c *gin.Context) {
	if h.kbStore == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "knowledge base not initialized"})
		return
	}
	pages, err := h.kbStore.ListFavorites()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"pages": pages})
}

// ============== 最近访问 ==============

// KBRecentList 最近访问
func (h *Handler) KBRecentList(c *gin.Context) {
	if h.kbStore == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "knowledge base not initialized"})
		return
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	pages, err := h.kbStore.ListRecent(limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"pages": pages})
}

// ============== 回收站 ==============

// KBRecycleList 列出回收站
func (h *Handler) KBRecycleList(c *gin.Context) {
	if h.kbStore == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "knowledge base not initialized"})
		return
	}
	items, err := h.kbStore.ListRecycle()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

// KBRecycleRestore 恢复
func (h *Handler) KBRecycleRestore(c *gin.Context) {
	if h.kbStore == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "knowledge base not initialized"})
		return
	}
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	page, err := h.kbStore.RestoreFromRecycle(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"page": page})
}

// KBRecycleDelete 永久删除
func (h *Handler) KBRecycleDelete(c *gin.Context) {
	if h.kbStore == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "knowledge base not initialized"})
		return
	}
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	if err := h.kbStore.DeleteRecyclePermanently(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// KBRecycleEmpty 清空回收站
func (h *Handler) KBRecycleEmpty(c *gin.Context) {
	if h.kbStore == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "knowledge base not initialized"})
		return
	}
	if err := h.kbStore.EmptyRecycle(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// ============== 块排序/移动 ==============

// KBReorderBlock 重排块
func (h *Handler) KBReorderBlock(c *gin.Context) {
	if h.kbStore == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "knowledge base not initialized"})
		return
	}
	var body struct {
		BlockID       string `json:"block_id"`
		AfterBlockID  string `json:"after_block_id"`
		PageID        int64  `json:"page_id"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.kbStore.ReorderBlock(body.BlockID, body.AfterBlockID, body.PageID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// KBMoveBlock 移动块到另一页面
func (h *Handler) KBMoveBlock(c *gin.Context) {
	if h.kbStore == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "knowledge base not initialized"})
		return
	}
	var body struct {
		BlockID       string `json:"block_id"`
		TargetPageID  int64  `json:"target_page_id"`
		AfterBlockID  string `json:"after_block_id"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.kbStore.MoveBlockToPage(body.BlockID, body.TargetPageID, body.AfterBlockID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// ============== Unlinked References ==============

// KBUnlinkedRefs 未链接引用
func (h *Handler) KBUnlinkedRefs(c *gin.Context) {
	if h.kbStore == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "knowledge base not initialized"})
		return
	}
	title := c.Param("title")
	refs, err := h.kbStore.GetUnlinkedReferences(title)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"references": refs})
}

// ============== AI 增强功能 ==============

// KBSuggestContinue AI 续写
func (h *Handler) KBSuggestContinue(c *gin.Context) {
	if h.kbSuggest == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "AI suggestion engine not initialized"})
		return
	}
	var body struct {
		Title   string `json:"title"`
		Content string `json:"content"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	continuation, err := h.kbSuggest.ContinueWriting(body.Title, body.Content)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"continuation": continuation})
}

// KBGraphQA 知识图谱问答
func (h *Handler) KBGraphQA(c *gin.Context) {
	if h.kbSuggest == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "AI suggestion engine not initialized"})
		return
	}
	var body struct {
		Question string `json:"question"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	answer, err := h.kbSuggest.GraphQA(body.Question)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"answer": answer})
}

// KBAutoOrganize 自动整理
func (h *Handler) KBAutoOrganize(c *gin.Context) {
	if h.kbSuggest == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "AI suggestion engine not initialized"})
		return
	}
	suggestion, err := h.kbSuggest.AutoOrganize()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"suggestion": suggestion})
}

// ============== 导出增强 ==============

// KBExportHTML 导出页面为 HTML
func (h *Handler) KBExportHTML(c *gin.Context) {
	if h.kbStore == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "knowledge base not initialized"})
		return
	}
	title := c.Param("title")
	page, err := h.kbStore.GetPageByTitle(title)
	if err != nil || page == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "page not found"})
		return
	}
	blocks, err := h.kbStore.GetBlocks(page.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	html := renderPageHTML(page, blocks)
	c.Header("Content-Disposition", `attachment; filename="`+title+".html")
	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(html))
}

// KBExportJSON 导出整个知识库为 JSON
func (h *Handler) KBExportJSON(c *gin.Context) {
	if h.kbStore == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "knowledge base not initialized"})
		return
	}
	pages, err := h.kbStore.ListPages()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	type exportPage struct {
		kb.Page
		Blocks []kb.Block `json:"blocks"`
	}
	var export []exportPage
	for _, p := range pages {
		blocks, _ := h.kbStore.GetBlocks(p.ID)
		export = append(export, exportPage{Page: p, Blocks: blocks})
	}
	c.Header("Content-Disposition", `attachment; filename="knowledge_base.json"`)
	c.JSON(http.StatusOK, gin.H{"pages": export, "exported_at": ""})
}

func renderPageHTML(page *kb.Page, blocks []kb.Block) string {
	var sb strings.Builder
	sb.WriteString("<!DOCTYPE html><html><head><meta charset='utf-8'>")
	sb.WriteString("<title>" + page.Title + "</title>")
	sb.WriteString("<style>body{font-family:sans-serif;max-width:800px;margin:40px auto;padding:0 20px;line-height:1.6}")
	sb.WriteString("h1{color:#333}h2{color:#444}code{background:#f4f4f4;padding:2px 6px;border-radius:3px}")
	sb.WriteString("pre{background:#f4f4f4;padding:12px;border-radius:6px;overflow-x:auto}")
	sb.WriteString("blockquote{border-left:4px solid #ddd;margin:0;padding-left:16px;color:#666}")
	sb.WriteString(".tag{color:#0066cc}.wikilink{color:#0066cc;text-decoration:none}</style></head><body>")
	sb.WriteString("<h1>" + page.Title + "</h1>")
	for _, b := range blocks {
		sb.WriteString(renderBlockHTML(b))
	}
	if len(page.Tags) > 0 {
		sb.WriteString("<hr><p>")
		for _, t := range page.Tags {
			sb.WriteString("<span class='tag'>#" + t + "</span> ")
		}
		sb.WriteString("</p>")
	}
	sb.WriteString("</body></html>")
	return sb.String()
}

func renderBlockHTML(b kb.Block) string {
	content := b.Content
	// 简单 Markdown 转 HTML
	content = strings.ReplaceAll(content, "&", "&amp;")
	content = strings.ReplaceAll(content, "<", "&lt;")
	content = strings.ReplaceAll(content, ">", "&gt;")
	// wikilink
	content = regexpReplace(content, `\[\[([^\]]+)\]\]`, "<a class='wikilink' href='#$1'>$1</a>")
	// tag
	content = regexpReplace(content, `(^|\s)#(\w[\w-]*)`, "$1<span class='tag'>#$2</span>")
	// bold
	content = regexpReplace(content, `\*\*([^*]+)\*\*`, "<strong>$1</strong>")
	// italic
	content = regexpReplace(content, `\*([^*]+)\*`, "<em>$1</em>")
	// code
	content = regexpReplace(content, "`([^`]+)`", "<code>$1</code>")

	switch b.BlockType {
	case "heading":
		level := b.Level + 1
		if level > 6 {
			level = 6
		}
		return "<h" + strconv.Itoa(level) + ">" + content + "</h" + strconv.Itoa(level) + ">"
	case "code":
		return "<pre><code>" + content + "</code></pre>"
	case "quote":
		return "<blockquote>" + content + "</blockquote>"
	case "list":
		return "<div style='padding-left:20px'>" + content + "</div>"
	default:
		return "<p>" + content + "</p>"
	}
}

func regexpReplace(s, pattern, replacement string) string {
	re := regexp.MustCompile(pattern)
	return re.ReplaceAllString(s, replacement)
}
