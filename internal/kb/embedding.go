package kb

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"sort"
	"strings"
	"time"
)

// EmbeddingConfig 向量化引擎配置
type EmbeddingConfig struct {
	APIBase string // LLM API 地址
	APIKey  string // API Key
	Model   string // embedding 模型名，如 "text-embedding-3-small"
}

// EmbeddingEngine 向量化引擎
// 负责将块内容向量化并存储，支持语义搜索
type EmbeddingEngine struct {
	store *Store
	cfg   EmbeddingConfig
}

// NewEmbeddingEngine 创建向量化引擎
func NewEmbeddingEngine(store *Store, cfg EmbeddingConfig) *EmbeddingEngine {
	if cfg.Model == "" {
		cfg.Model = "text-embedding-3-small"
	}
	return &EmbeddingEngine{store: store, cfg: cfg}
}

// embeddingRecord 向量记录
type embeddingRecord struct {
	BlockID   string    `json:"block_id"`
	Embedding []float64 `json:"embedding"`
}

// IndexPageEmbeddings 为页面的所有块生成向量
func (e *EmbeddingEngine) IndexPageEmbeddings(pageID int64) (int, error) {
	if e.cfg.APIKey == "" {
		return 0, fmt.Errorf("embedding API key not configured")
	}

	blocks, err := e.store.GetBlocks(pageID)
	if err != nil {
		return 0, fmt.Errorf("get blocks: %w", err)
	}

	count := 0
	for _, block := range blocks {
		// 跳过空块和纯代码块
		content := strings.TrimSpace(block.Content)
		if content == "" || len(content) < 3 {
			continue
		}
		// 代码块跳过（向量意义不大）
		if block.BlockType == "code" {
			continue
		}

		embedding, err := e.getEmbedding(content)
		if err != nil {
			continue
		}

		if err := e.saveEmbedding(block.ID, embedding); err != nil {
			continue
		}
		count++
	}

	return count, nil
}

// IndexAllEmbeddings 为知识库所有块生成向量
func (e *EmbeddingEngine) IndexAllEmbeddings() (int, int, error) {
	pages, err := e.store.ListPages()
	if err != nil {
		return 0, 0, err
	}

	totalIndexed := 0
	totalFailed := 0
	for _, page := range pages {
		indexed, err := e.IndexPageEmbeddings(page.ID)
		if err != nil {
			totalFailed += len(page.Tags) // 近似
			continue
		}
		totalIndexed += indexed
	}
	return totalIndexed, totalFailed, nil
}

// SemanticSearch 语义搜索
// query → embedding → 余弦相似度 → top-K 块
func (e *EmbeddingEngine) SemanticSearch(query string, limit int) ([]SearchResult, error) {
	if limit <= 0 {
		limit = 10
	}

	if e.cfg.APIKey == "" {
		return nil, fmt.Errorf("embedding API key not configured")
	}

	// 生成查询向量
	queryEmbedding, err := e.getEmbedding(query)
	if err != nil {
		return nil, fmt.Errorf("embed query: %w", err)
	}

	// 加载所有向量
	records, err := e.loadAllEmbeddings()
	if err != nil {
		return nil, fmt.Errorf("load embeddings: %w", err)
	}

	if len(records) == 0 {
		return nil, nil
	}

	// 计算余弦相似度
	type scored struct {
		blockID string
		score   float64
	}
	var scoredRecords []scored
	for _, rec := range records {
		score := cosineSimilarity(queryEmbedding, rec.Embedding)
		scoredRecords = append(scoredRecords, scored{blockID: rec.BlockID, score: score})
	}

	// 按相似度排序
	sort.Slice(scoredRecords, func(i, j int) bool {
		return scoredRecords[i].score > scoredRecords[j].score
	})

	// 取 top-K
	if len(scoredRecords) > limit {
		scoredRecords = scoredRecords[:limit]
	}

	// 查询块详情并组装结果
	var results []SearchResult
	for _, sr := range scoredRecords {
		block, err := e.store.GetBlock(sr.blockID)
		if err != nil || block == nil {
			continue
		}
		page, err := e.store.GetPageByID(block.PageID)
		if err != nil || page == nil {
			continue
		}
		results = append(results, SearchResult{
			PageID:  page.ID,
			Title:   page.Title,
			BlockID: block.ID,
			Content: block.Content,
			Snippet: truncateContext(block.Content, 150),
			Score:   sr.score,
			Type:    "block",
		})
	}

	return results, nil
}

// HasEmbeddings 检查是否有向量数据
func (e *EmbeddingEngine) HasEmbeddings() bool {
	var count int
	e.store.db.QueryRow("SELECT COUNT(*) FROM kb_embeddings").Scan(&count)
	return count > 0
}

// --- 内部方法 ---

// getEmbedding 调用 LLM API 获取文本向量
func (e *EmbeddingEngine) getEmbedding(text string) ([]float64, error) {
	// 截断过长文本
	if len(text) > 8000 {
		text = text[:8000]
	}

	url := strings.TrimRight(e.cfg.APIBase, "/") + "/embeddings"
	if !strings.Contains(e.cfg.APIBase, "/v") {
		url = strings.TrimRight(e.cfg.APIBase, "/") + "/v1/embeddings"
	}

	payload := map[string]any{
		"model": e.cfg.Model,
		"input": text,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if e.cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+e.cfg.APIKey)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		data, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("embedding API error %d: %s", resp.StatusCode, string(data[:min(len(data), 200)]))
	}

	var result struct {
		Data []struct {
			Embedding []float64 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	if len(result.Data) == 0 {
		return nil, fmt.Errorf("empty embedding response")
	}
	return result.Data[0].Embedding, nil
}

// saveEmbedding 保存向量到数据库
func (e *EmbeddingEngine) saveEmbedding(blockID string, embedding []float64) error {
	embeddingJSON, err := json.Marshal(embedding)
	if err != nil {
		return err
	}

	_, err = e.store.db.Exec(`
		INSERT INTO kb_embeddings (block_id, embedding, model, updated_at)
		VALUES (?, ?, ?, datetime('now'))
		ON CONFLICT(block_id) DO UPDATE SET
			embedding = excluded.embedding,
			model = excluded.model,
			updated_at = datetime('now')`,
		blockID, string(embeddingJSON), e.cfg.Model)
	return err
}

// loadAllEmbeddings 加载所有向量
func (e *EmbeddingEngine) loadAllEmbeddings() ([]embeddingRecord, error) {
	// 确保 kb_embeddings 表存在
	e.ensureEmbeddingsTable()

	rows, err := e.store.db.Query(`SELECT block_id, embedding FROM kb_embeddings`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []embeddingRecord
	for rows.Next() {
		var rec embeddingRecord
		var embeddingJSON string
		if err := rows.Scan(&rec.BlockID, &embeddingJSON); err != nil {
			continue
		}
		if err := json.Unmarshal([]byte(embeddingJSON), &rec.Embedding); err != nil {
			continue
		}
		records = append(records, rec)
	}
	return records, nil
}

// ensureEmbeddingsTable 确定向量表存在
func (e *EmbeddingEngine) ensureEmbeddingsTable() {
	e.store.db.Exec(`
		CREATE TABLE IF NOT EXISTS kb_embeddings (
			block_id    TEXT PRIMARY KEY,
			embedding   TEXT NOT NULL,
			model       TEXT NOT NULL,
			updated_at  TEXT NOT NULL DEFAULT (datetime('now'))
		)
	`)
}

// cosineSimilarity 计算余弦相似度
func cosineSimilarity(a, b []float64) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}

	var dotProduct, normA, normB float64
	for i := 0; i < len(a); i++ {
		dotProduct += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}

	if normA == 0 || normB == 0 {
		return 0
	}

	return dotProduct / (math.Sqrt(normA) * math.Sqrt(normB))
}

// GetEmbeddingStats 获取向量统计
func (e *EmbeddingEngine) GetEmbeddingStats() (int, error) {
	e.ensureEmbeddingsTable()
	var count int
	err := e.store.db.QueryRow("SELECT COUNT(*) FROM kb_embeddings").Scan(&count)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return count, err
}
