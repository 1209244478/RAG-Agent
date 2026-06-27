package kb

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	_ "modernc.org/sqlite"

	"github.com/google/uuid"
)

// Store 知识库存储层，管理 SQLite 持久化
type Store struct {
	db *sql.DB
}

// NewStore 创建知识库存储实例
// dbPath: SQLite 数据库文件路径
func NewStore(dbPath string) (*Store, error) {
	db, err := sql.Open("sqlite", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("open kb sqlite: %w", err)
	}
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)

	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		return nil, fmt.Errorf("kb migrate: %w", err)
	}
	return s, nil
}

// NewStoreFromDB 从已有 sql.DB 创建（复用连接）
func NewStoreFromDB(db *sql.DB) (*Store, error) {
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		return nil, fmt.Errorf("kb migrate: %w", err)
	}
	return s, nil
}

func (s *Store) migrate() error {
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS kb_pages (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			title       TEXT    NOT NULL UNIQUE,
			file_name   TEXT    NOT NULL,
			namespace   TEXT    NOT NULL DEFAULT '',
			is_journal  INTEGER NOT NULL DEFAULT 0,
			tags        TEXT    NOT NULL DEFAULT '[]',
			properties  TEXT    NOT NULL DEFAULT '{}',
			created_at  TEXT    NOT NULL DEFAULT (datetime('now')),
			updated_at  TEXT    NOT NULL DEFAULT (datetime('now'))
		)
	`)
	if err != nil {
		return err
	}
	s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_kb_pages_title ON kb_pages(title)`)
	s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_kb_pages_namespace ON kb_pages(namespace)`)
	s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_kb_pages_journal ON kb_pages(is_journal)`)

	_, err = s.db.Exec(`
		CREATE TABLE IF NOT EXISTS kb_blocks (
			id          TEXT    PRIMARY KEY,
			page_id     INTEGER NOT NULL,
			content     TEXT    NOT NULL DEFAULT '',
			block_order INTEGER NOT NULL DEFAULT 0,
			level       INTEGER NOT NULL DEFAULT 0,
			parent_id   TEXT    NOT NULL DEFAULT '',
			block_type  TEXT    NOT NULL DEFAULT 'paragraph',
			properties  TEXT    NOT NULL DEFAULT '{}',
			created_at  TEXT    NOT NULL DEFAULT (datetime('now')),
			updated_at  TEXT    NOT NULL DEFAULT (datetime('now')),
			FOREIGN KEY (page_id) REFERENCES kb_pages(id) ON DELETE CASCADE
		)
	`)
	if err != nil {
		return err
	}
	s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_kb_blocks_page ON kb_blocks(page_id)`)
	s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_kb_blocks_parent ON kb_blocks(parent_id)`)
	s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_kb_blocks_content ON kb_blocks(content)`)

	_, err = s.db.Exec(`
		CREATE TABLE IF NOT EXISTS kb_links (
			id                INTEGER PRIMARY KEY AUTOINCREMENT,
			source_block_id   TEXT    NOT NULL,
			target_page_id    INTEGER NOT NULL DEFAULT 0,
			target_block_id   TEXT    NOT NULL DEFAULT '',
			target_page_title TEXT    NOT NULL DEFAULT '',
			link_type         TEXT    NOT NULL DEFAULT 'page_ref',
			context           TEXT    NOT NULL DEFAULT '',
			FOREIGN KEY (source_block_id) REFERENCES kb_blocks(id) ON DELETE CASCADE
		)
	`)
	if err != nil {
		return err
	}
	s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_kb_links_source ON kb_links(source_block_id)`)
	s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_kb_links_target_page ON kb_links(target_page_id)`)
	s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_kb_links_target_title ON kb_links(target_page_title)`)
	s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_kb_links_type ON kb_links(link_type)`)

	// FTS5 全文索引（带 ICU 分词器，支持中文；不可用时回退到 unicode61）
	if err := s.ensureFTS(); err != nil {
		// FTS 不可用不阻断启动，仅记录
		fmt.Printf("[KB] FTS init warning: %v\n", err)
	}

	// 版本历史表
	_, err = s.db.Exec(`
		CREATE TABLE IF NOT EXISTS kb_page_versions (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			page_id    INTEGER NOT NULL,
			version    INTEGER NOT NULL,
			content    TEXT    NOT NULL,
			created_at TEXT    NOT NULL DEFAULT (datetime('now')),
			FOREIGN KEY (page_id) REFERENCES kb_pages(id) ON DELETE CASCADE
		)
	`)
	if err != nil {
		return err
	}
	s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_kb_versions_page ON kb_page_versions(page_id, version)`)

	// 属性系统、收藏夹、回收站等扩展表
	if err := s.ensurePropertyTables(); err != nil {
		return fmt.Errorf("ensure property tables: %w", err)
	}

	return nil
}

// ensureFTS 创建 FTS5 虚拟表并同步触发器
func (s *Store) ensureFTS() error {
	// 尝试使用 icu 分词器（支持中文），失败则回退 unicode61
	_, err := s.db.Exec(`CREATE VIRTUAL TABLE IF NOT EXISTS kb_blocks_fts USING fts5(content, block_id UNINDEXED, page_id UNINDEXED, tokenize='icu zh_CN')`)
	if err != nil {
		_, err = s.db.Exec(`CREATE VIRTUAL TABLE IF NOT EXISTS kb_blocks_fts USING fts5(content, block_id UNINDEXED, page_id UNINDEXED)`)
		if err != nil {
			return err
		}
	}

	// 同步触发器：块插入/更新/删除时同步 FTS
	s.db.Exec(`CREATE TRIGGER IF NOT EXISTS kb_blocks_ai AFTER INSERT ON kb_blocks BEGIN
		INSERT INTO kb_blocks_fts(content, block_id, page_id) VALUES (new.content, new.id, new.page_id);
	END`)
	s.db.Exec(`CREATE TRIGGER IF NOT EXISTS kb_blocks_ad AFTER DELETE ON kb_blocks BEGIN
		DELETE FROM kb_blocks_fts WHERE block_id = old.id;
	END`)
	s.db.Exec(`CREATE TRIGGER IF NOT EXISTS kb_blocks_au AFTER UPDATE ON kb_blocks BEGIN
		DELETE FROM kb_blocks_fts WHERE block_id = old.id;
		INSERT INTO kb_blocks_fts(content, block_id, page_id) VALUES (new.content, new.id, new.page_id);
	END`)
	return nil
}

// RebuildFTS 重建全文索引（用于一次性同步存量数据）
func (s *Store) RebuildFTS() error {
	s.db.Exec(`DELETE FROM kb_blocks_fts`)
	_, err := s.db.Exec(`
		INSERT INTO kb_blocks_fts(content, block_id, page_id)
		SELECT content, id, page_id FROM kb_blocks`)
	return err
}

// SearchFTS 使用 FTS5 全文搜索
func (s *Store) SearchFTS(query string, limit int) ([]SearchResult, error) {
	if limit <= 0 {
		limit = 20
	}
	// 转义 FTS5 特殊字符，使用简单关键词匹配
	ftsQuery := sanitizeFTSQuery(query)
	if ftsQuery == "" {
		return s.Search(query, limit)
	}

	rows, err := s.db.Query(`
		SELECT b.id, b.page_id, p.title, b.content,
			snippet(kb_blocks_fts, 0, '<<', '>>', '...', 20) AS snip,
			rank
		FROM kb_blocks_fts f
		JOIN kb_blocks b ON f.block_id = b.id
		JOIN kb_pages p ON b.page_id = p.id
		WHERE kb_blocks_fts MATCH ?
		ORDER BY rank
		LIMIT ?`, ftsQuery, limit)
	if err != nil {
		// FTS 查询失败，回退到 LIKE
		return s.Search(query, limit)
	}
	defer rows.Close()

	var results []SearchResult
	for rows.Next() {
		var r SearchResult
		var content string
		var rank float64
		if err := rows.Scan(&r.BlockID, &r.PageID, &r.Title, &content, &r.Snippet, &rank); err != nil {
			continue
		}
		r.Content = content
		r.Type = "block"
		// rank 越小越相关，转换为 0-1 的分数
		r.Score = 1.0 / (1.0 + rank)
		results = append(results, r)
	}

	// 补充页面标题匹配
	pattern := "%" + query + "%"
	pageRows, err := s.db.Query(`SELECT id, title FROM kb_pages WHERE title LIKE ? LIMIT ?`, pattern, limit)
	if err == nil {
		defer pageRows.Close()
		for pageRows.Next() {
			var r SearchResult
			if err := pageRows.Scan(&r.PageID, &r.Title); err != nil {
				continue
			}
			r.Snippet = r.Title
			r.Type = "page"
			r.Score = 2.0
			results = append(results, r)
		}
	}

	// 如果 FTS 未命中任何结果，回退到 LIKE 搜索（处理中文等分词器不支持的查询）
	if len(results) == 0 {
		return s.Search(query, limit)
	}

	return results, nil
}

// sanitizeFTSQuery 清理查询字符串以适配 FTS5 MATCH 语法
func sanitizeFTSQuery(q string) string {
	q = strings.TrimSpace(q)
	if q == "" {
		return ""
	}
	// 拆分为关键词，每个加前缀匹配 *
	fields := strings.Fields(q)
	var parts []string
	for _, f := range fields {
		// 去除 FTS5 特殊字符
		cleaned := strings.Map(func(r rune) rune {
			if r == '"' || r == '*' || r == '(' || r == ')' || r == ':' {
				return -1
			}
			return r
		}, f)
		if cleaned != "" {
			parts = append(parts, cleaned+"*")
		}
	}
	return strings.Join(parts, " ")
}

// Close 关闭数据库
func (s *Store) Close() error {
	return s.db.Close()
}

// --- Page 操作 ---

// UpsertPage 创建或更新页面
func (s *Store) UpsertPage(page ParsedPage) (int64, error) {
	tagsJSON, _ := json.Marshal(page.Tags)
	propsJSON, _ := json.Marshal(page.Properties)

	// 先查找是否已存在
	var existingID int64
	err := s.db.QueryRow("SELECT id FROM kb_pages WHERE title = ?", page.Title).Scan(&existingID)
	if err == nil {
		// 更新
		_, err = s.db.Exec(`
			UPDATE kb_pages SET
				file_name = ?, namespace = ?, is_journal = ?, tags = ?, properties = ?, updated_at = datetime('now')
			WHERE id = ?`,
			page.FileName, page.Namespace, page.IsJournal, string(tagsJSON), string(propsJSON), existingID)
		if err != nil {
			return 0, fmt.Errorf("update page: %w", err)
		}
		return existingID, nil
	}

	// 创建
	result, err := s.db.Exec(`
		INSERT INTO kb_pages (title, file_name, namespace, is_journal, tags, properties)
		VALUES (?, ?, ?, ?, ?, ?)`,
		page.Title, page.FileName, page.Namespace, page.IsJournal, string(tagsJSON), string(propsJSON))
	if err != nil {
		return 0, fmt.Errorf("insert page: %w", err)
	}
	id, _ := result.LastInsertId()
	return id, nil
}

// GetPageByTitle 根据标题获取页面
func (s *Store) GetPageByTitle(title string) (*Page, error) {
	p := &Page{}
	var tagsJSON, propsJSON string
	var isJournal int
	err := s.db.QueryRow(`
		SELECT id, title, file_name, namespace, is_journal, tags, properties, created_at, updated_at
		FROM kb_pages WHERE title = ?`, title,
	).Scan(&p.ID, &p.Title, &p.FileName, &p.Namespace, &isJournal, &tagsJSON, &propsJSON, &p.CreatedAt, &p.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	p.IsJournal = isJournal != 0
	json.Unmarshal([]byte(tagsJSON), &p.Tags)
	json.Unmarshal([]byte(propsJSON), &p.Properties)
	return p, nil
}

// GetPageByID 根据 ID 获取页面
func (s *Store) GetPageByID(id int64) (*Page, error) {
	p := &Page{}
	var tagsJSON, propsJSON string
	var isJournal int
	err := s.db.QueryRow(`
		SELECT id, title, file_name, namespace, is_journal, tags, properties, created_at, updated_at
		FROM kb_pages WHERE id = ?`, id,
	).Scan(&p.ID, &p.Title, &p.FileName, &p.Namespace, &isJournal, &tagsJSON, &propsJSON, &p.CreatedAt, &p.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	p.IsJournal = isJournal != 0
	json.Unmarshal([]byte(tagsJSON), &p.Tags)
	json.Unmarshal([]byte(propsJSON), &p.Properties)
	return p, nil
}

// ListPages 列出所有页面
func (s *Store) ListPages() ([]Page, error) {
	rows, err := s.db.Query(`
		SELECT id, title, file_name, namespace, is_journal, tags, properties, created_at, updated_at
		FROM kb_pages ORDER BY updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanPages(rows)
}

// ListJournalPages 列出日记页面
func (s *Store) ListJournalPages(limit int) ([]Page, error) {
	if limit <= 0 {
		limit = 30
	}
	rows, err := s.db.Query(`
		SELECT id, title, file_name, namespace, is_journal, tags, properties, created_at, updated_at
		FROM kb_pages WHERE is_journal = 1 ORDER BY title DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanPages(rows)
}

// ListPagesByNamespace 按命名空间列出页面
func (s *Store) ListPagesByNamespace(ns string) ([]Page, error) {
	rows, err := s.db.Query(`
		SELECT id, title, file_name, namespace, is_journal, tags, properties, created_at, updated_at
		FROM kb_pages WHERE namespace = ? ORDER BY title`, ns)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanPages(rows)
}

// DeletePage 删除页面（级联删除块和链接）
func (s *Store) DeletePage(id int64) error {
	// 先删除该页面所有块的链接
	_, err := s.db.Exec(`DELETE FROM kb_links WHERE source_block_id IN (SELECT id FROM kb_blocks WHERE page_id = ?)`, id)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`DELETE FROM kb_blocks WHERE page_id = ?`, id)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`DELETE FROM kb_pages WHERE id = ?`, id)
	return err
}

// --- Block 操作 ---

// UpsertBlocks 批量替换页面的所有块
func (s *Store) UpsertBlocks(pageID int64, blocks []ParsedBlock) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// 删除旧块和链接
	tx.Exec(`DELETE FROM kb_links WHERE source_block_id IN (SELECT id FROM kb_blocks WHERE page_id = ?)`, pageID)
	tx.Exec(`DELETE FROM kb_blocks WHERE page_id = ?`, pageID)

	// 构建父子关系映射
	parentStack := make(map[int]string) // level -> blockID

	for _, block := range blocks {
		blockID := generateUUID()
		propsJSON, _ := json.Marshal(block.Properties)

		// 查找父块
		parentID := ""
		for level, id := range parentStack {
			if level < block.Level {
				parentID = id
			}
		}
		// 更新 parentStack
		parentStack[block.Level] = blockID
		// 清除更深层级
		for level := range parentStack {
			if level > block.Level {
				delete(parentStack, level)
			}
		}

		_, err := tx.Exec(`
			INSERT INTO kb_blocks (id, page_id, content, block_order, level, parent_id, block_type, properties)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			blockID, pageID, block.Content, block.Order, block.Level, parentID, block.BlockType, string(propsJSON))
		if err != nil {
			return fmt.Errorf("insert block: %w", err)
		}
	}

	return tx.Commit()
}

// GetBlocks 获取页面的所有块
func (s *Store) GetBlocks(pageID int64) ([]Block, error) {
	rows, err := s.db.Query(`
		SELECT id, page_id, content, block_order, level, parent_id, block_type, properties, created_at, updated_at
		FROM kb_blocks WHERE page_id = ? ORDER BY block_order`, pageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var blocks []Block
	for rows.Next() {
		var b Block
		var propsJSON string
		if err := rows.Scan(&b.ID, &b.PageID, &b.Content, &b.Order, &b.Level, &b.ParentID, &b.BlockType, &propsJSON, &b.CreatedAt, &b.UpdatedAt); err != nil {
			continue
		}
		json.Unmarshal([]byte(propsJSON), &b.Properties)
		blocks = append(blocks, b)
	}
	return blocks, nil
}

// GetBlock 获取单个块
func (s *Store) GetBlock(blockID string) (*Block, error) {
	b := &Block{}
	var propsJSON string
	err := s.db.QueryRow(`
		SELECT id, page_id, content, block_order, level, parent_id, block_type, properties, created_at, updated_at
		FROM kb_blocks WHERE id = ?`, blockID,
	).Scan(&b.ID, &b.PageID, &b.Content, &b.Order, &b.Level, &b.ParentID, &b.BlockType, &propsJSON, &b.CreatedAt, &b.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	json.Unmarshal([]byte(propsJSON), &b.Properties)
	return b, nil
}

// UpdateBlockContent 更新块内容
func (s *Store) UpdateBlockContent(blockID string, content string) error {
	_, err := s.db.Exec(`UPDATE kb_blocks SET content = ?, updated_at = datetime('now') WHERE id = ?`, content, blockID)
	return err
}

// --- Link 操作 ---

// AddLink 添加链接
func (s *Store) AddLink(link Link) error {
	_, err := s.db.Exec(`
		INSERT INTO kb_links (source_block_id, target_page_id, target_block_id, target_page_title, link_type, context)
		VALUES (?, ?, ?, ?, ?, ?)`,
		link.SourceBlockID, link.TargetPageID, link.TargetBlockID, link.TargetPageTitle, link.LinkType, link.Context)
	return err
}

// AddLinks 批量添加链接
func (s *Store) AddLinks(links []Link) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for _, link := range links {
		_, err := tx.Exec(`
			INSERT INTO kb_links (source_block_id, target_page_id, target_block_id, target_page_title, link_type, context)
			VALUES (?, ?, ?, ?, ?, ?)`,
			link.SourceBlockID, link.TargetPageID, link.TargetBlockID, link.TargetPageTitle, link.LinkType, link.Context)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

// GetBacklinks 获取指向某页面的反向链接
func (s *Store) GetBacklinks(pageID int64) ([]Backlink, error) {
	rows, err := s.db.Query(`
		SELECT b.page_id, p.title, l.source_block_id, b.content, l.link_type, l.context
		FROM kb_links l
		JOIN kb_blocks b ON l.source_block_id = b.id
		JOIN kb_pages p ON b.page_id = p.id
		WHERE l.target_page_id = ?
		ORDER BY p.updated_at DESC`, pageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var backlinks []Backlink
	for rows.Next() {
		var bl Backlink
		if err := rows.Scan(&bl.PageID, &bl.PageTitle, &bl.BlockID, &bl.BlockContent, &bl.LinkType, &bl.Context); err != nil {
			continue
		}
		backlinks = append(backlinks, bl)
	}
	return backlinks, nil
}

// GetOutlinks 获取页面指向的外部链接
func (s *Store) GetOutlinks(pageID int64) ([]Link, error) {
	rows, err := s.db.Query(`
		SELECT l.id, l.source_block_id, l.target_page_id, l.target_block_id, l.target_page_title, l.link_type, l.context
		FROM kb_links l
		JOIN kb_blocks b ON l.source_block_id = b.id
		WHERE b.page_id = ?`, pageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var links []Link
	for rows.Next() {
		var l Link
		if err := rows.Scan(&l.ID, &l.SourceBlockID, &l.TargetPageID, &l.TargetBlockID, &l.TargetPageTitle, &l.LinkType, &l.Context); err != nil {
			continue
		}
		links = append(links, l)
	}
	return links, nil
}

// --- 搜索 ---

// Search 全文搜索（基于 LIKE 模糊匹配）
func (s *Store) Search(query string, limit int) ([]SearchResult, error) {
	if limit <= 0 {
		limit = 20
	}
	pattern := "%" + query + "%"

	// 搜索块内容
	rows, err := s.db.Query(`
		SELECT b.id, b.page_id, p.title, b.content, b.block_type
		FROM kb_blocks b
		JOIN kb_pages p ON b.page_id = p.id
		WHERE b.content LIKE ?
		ORDER BY p.updated_at DESC
		LIMIT ?`, pattern, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []SearchResult
	for rows.Next() {
		var r SearchResult
		var content, blockType string
		if err := rows.Scan(&r.BlockID, &r.PageID, &r.Title, &content, &blockType); err != nil {
			continue
		}
		r.Content = content
		r.Snippet = extractSnippet(content, query, 100)
		r.Type = "block"
		r.Score = 1.0
		results = append(results, r)
	}

	// 搜索页面标题
	pageRows, err := s.db.Query(`
		SELECT id, title FROM kb_pages WHERE title LIKE ? LIMIT ?`, pattern, limit)
	if err != nil {
		return results, nil
	}
	defer pageRows.Close()

	for pageRows.Next() {
		var r SearchResult
		if err := pageRows.Scan(&r.PageID, &r.Title); err != nil {
			continue
		}
		r.Snippet = r.Title
		r.Type = "page"
		r.Score = 2.0 // 页面标题匹配优先级更高
		results = append(results, r)
	}

	return results, nil
}

// --- 辅助函数 ---

func scanPages(rows *sql.Rows) ([]Page, error) {
	var pages []Page
	for rows.Next() {
		var p Page
		var tagsJSON, propsJSON string
		var isJournal int
		if err := rows.Scan(&p.ID, &p.Title, &p.FileName, &p.Namespace, &isJournal, &tagsJSON, &propsJSON, &p.CreatedAt, &p.UpdatedAt); err != nil {
			continue
		}
		p.IsJournal = isJournal != 0
		json.Unmarshal([]byte(tagsJSON), &p.Tags)
		json.Unmarshal([]byte(propsJSON), &p.Properties)
		pages = append(pages, p)
	}
	return pages, nil
}

// extractSnippet 提取匹配关键词的上下文片段
func extractSnippet(content string, query string, maxLen int) string {
	lowerContent := strings.ToLower(content)
	lowerQuery := strings.ToLower(query)

	idx := strings.Index(lowerContent, lowerQuery)
	if idx < 0 {
		if len(content) > maxLen {
			return content[:maxLen] + "..."
		}
		return content
	}

	start := idx - maxLen/2 + len(query)/2
	if start < 0 {
		start = 0
	}
	end := start + maxLen
	if end > len(content) {
		end = len(content)
	}

	snippet := content[start:end]
	if start > 0 {
		snippet = "..." + snippet
	}
	if end < len(content) {
		snippet = snippet + "..."
	}
	return snippet
}

// generateUUID 生成 UUID v4
func generateUUID() string {
	return uuid.NewString()
}
