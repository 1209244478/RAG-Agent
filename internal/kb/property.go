package kb

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
)

// PropertyType 属性值类型
type PropertyType string

const (
	PropTypeString  PropertyType = "string"
	PropTypeNumber  PropertyType = "number"
	PropTypeBoolean PropertyType = "boolean"
	PropTypeDate    PropertyType = "date"
	PropTypeURL     PropertyType = "url"
	PropTypePage    PropertyType = "page"    // 页面引用
	PropTypeMulti   PropertyType = "multi"   // 多值
	PropTypeTags    PropertyType = "tags"    // 标签集合
)

// PropertySchema 属性 schema 定义（页面级类型声明）
type PropertySchema struct {
	Name     string       `json:"name"`
	Type     PropertyType `json:"type"`
	Default  any          `json:"default,omitempty"`
	Required bool         `json:"required,omitempty"`
	Options  []string     `json:"options,omitempty"` // 枚举可选值
}

// PropertyValue 属性值（带类型信息）
type PropertyValue struct {
	Name  string `json:"name"`
	Type  PropertyType `json:"type"`
	Value any    `json:"value"`
}

// PropertyManager 属性管理器
type PropertyManager struct {
	store *Store
}

// NewPropertyManager 创建属性管理器
func NewPropertyManager(store *Store) *PropertyManager {
	return &PropertyManager{store: store}
}

// ensurePropertyTables 确保属性相关表存在
func (s *Store) ensurePropertyTables() error {
	// 页面属性表（结构化存储，便于查询）
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS kb_page_properties (
			id       INTEGER PRIMARY KEY AUTOINCREMENT,
			page_id  INTEGER NOT NULL,
			name     TEXT    NOT NULL,
			value    TEXT    NOT NULL,
			val_type TEXT    NOT NULL DEFAULT 'string',
			UNIQUE(page_id, name),
			FOREIGN KEY (page_id) REFERENCES kb_pages(id) ON DELETE CASCADE
		)
	`)
	if err != nil {
		return err
	}
	s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_kb_props_page ON kb_page_properties(page_id)`)
	s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_kb_props_name ON kb_page_properties(name)`)
	s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_kb_props_value ON kb_page_properties(value)`)

	// 块属性表
	_, err = s.db.Exec(`
		CREATE TABLE IF NOT EXISTS kb_block_properties (
			id        INTEGER PRIMARY KEY AUTOINCREMENT,
			block_id  TEXT    NOT NULL,
			name      TEXT    NOT NULL,
			value     TEXT    NOT NULL,
			val_type  TEXT    NOT NULL DEFAULT 'string',
			UNIQUE(block_id, name),
			FOREIGN KEY (block_id) REFERENCES kb_blocks(id) ON DELETE CASCADE
		)
	`)
	if err != nil {
		return err
	}
	s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_kb_bprops_block ON kb_block_properties(block_id)`)
	s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_kb_bprops_name ON kb_block_properties(name)`)

	// 属性 schema 表（页面类型声明）
	_, err = s.db.Exec(`
		CREATE TABLE IF NOT EXISTS kb_property_schemas (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			namespace  TEXT    NOT NULL DEFAULT '',
			name       TEXT    NOT NULL,
			val_type   TEXT    NOT NULL DEFAULT 'string',
			default_val TEXT   NOT NULL DEFAULT '',
			required   INTEGER NOT NULL DEFAULT 0,
			options    TEXT    NOT NULL DEFAULT '[]',
			UNIQUE(namespace, name)
		)
	`)
	if err != nil {
		return err
	}
	s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_kb_pschema_ns ON kb_property_schemas(namespace)`)

	// 收藏表
	_, err = s.db.Exec(`
		CREATE TABLE IF NOT EXISTS kb_favorites (
			id      INTEGER PRIMARY KEY AUTOINCREMENT,
			page_id INTEGER NOT NULL UNIQUE,
			sort_order INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL DEFAULT (datetime('now')),
			FOREIGN KEY (page_id) REFERENCES kb_pages(id) ON DELETE CASCADE
		)
	`)
	if err != nil {
		return err
	}

	// 回收站表（软删除）
	_, err = s.db.Exec(`
		CREATE TABLE IF NOT EXISTS kb_recycle (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			page_id    INTEGER NOT NULL,
			title      TEXT    NOT NULL,
			file_name  TEXT    NOT NULL,
			namespace  TEXT    NOT NULL DEFAULT '',
			is_journal INTEGER NOT NULL DEFAULT 0,
			tags       TEXT    NOT NULL DEFAULT '[]',
			properties TEXT    NOT NULL DEFAULT '{}',
			deleted_at TEXT    NOT NULL DEFAULT (datetime('now'))
		)
	`)
	if err != nil {
		return err
	}
	s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_kb_recycle_title ON kb_recycle(title)`)

	return nil
}

// --- 页面属性操作 ---

// SetPageProperty 设置页面属性
func (s *Store) SetPageProperty(pageID int64, name string, value any, valType PropertyType) error {
	valStr := stringifyValue(value)
	_, err := s.db.Exec(`
		INSERT INTO kb_page_properties (page_id, name, value, val_type)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(page_id, name) DO UPDATE SET value = excluded.value, val_type = excluded.val_type`,
		pageID, name, valStr, string(valType))
	return err
}

// GetPageProperties 获取页面所有属性（结构化）
func (s *Store) GetPageProperties(pageID int64) ([]PropertyValue, error) {
	rows, err := s.db.Query(`
		SELECT name, value, val_type FROM kb_page_properties WHERE page_id = ? ORDER BY name`, pageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var props []PropertyValue
	for rows.Next() {
		var p PropertyValue
		var valStr string
		if err := rows.Scan(&p.Name, &valStr, &p.Type); err != nil {
			continue
		}
		p.Value = parseValue(valStr, p.Type)
		props = append(props, p)
	}
	return props, nil
}

// DeletePageProperty 删除页面属性
func (s *Store) DeletePageProperty(pageID int64, name string) error {
	_, err := s.db.Exec(`DELETE FROM kb_page_properties WHERE page_id = ? AND name = ?`, pageID, name)
	return err
}

// SyncPageProperties 从 frontmatter 同步属性到结构化表
func (s *Store) SyncPageProperties(pageID int64, properties map[string]any) error {
	// 先清除旧属性
	_, err := s.db.Exec(`DELETE FROM kb_page_properties WHERE page_id = ?`, pageID)
	if err != nil {
		return err
	}
	// 插入新属性
	for name, val := range properties {
		valType := inferType(val)
		valStr := stringifyValue(val)
		_, err := s.db.Exec(`
			INSERT INTO kb_page_properties (page_id, name, value, val_type)
			VALUES (?, ?, ?, ?)`, pageID, name, valStr, string(valType))
		if err != nil {
			return err
		}
	}
	return nil
}

// QueryByProperty 按属性查询页面
func (s *Store) QueryByProperty(name string, value string) ([]Page, error) {
	rows, err := s.db.Query(`
		SELECT p.id, p.title, p.file_name, p.namespace, p.is_journal, p.tags, p.properties, p.created_at, p.updated_at
		FROM kb_pages p
		JOIN kb_page_properties pp ON p.id = pp.page_id
		WHERE pp.name = ? AND pp.value LIKE ?
		ORDER BY p.updated_at DESC`, name, "%"+value+"%")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanPages(rows)
}

// GetAllPropertyNames 获取所有属性名（用于自动补全）
func (s *Store) GetAllPropertyNames() ([]string, error) {
	rows, err := s.db.Query(`SELECT DISTINCT name FROM kb_page_properties ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			continue
		}
		names = append(names, n)
	}
	return names, nil
}

// --- 块属性操作 ---

// SetBlockProperty 设置块属性
func (s *Store) SetBlockProperty(blockID string, name string, value any, valType PropertyType) error {
	valStr := stringifyValue(value)
	_, err := s.db.Exec(`
		INSERT INTO kb_block_properties (block_id, name, value, val_type)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(block_id, name) DO UPDATE SET value = excluded.value, val_type = excluded.val_type`,
		blockID, name, valStr, string(valType))
	return err
}

// GetBlockProperties 获取块所有属性
func (s *Store) GetBlockProperties(blockID string) ([]PropertyValue, error) {
	rows, err := s.db.Query(`
		SELECT name, value, val_type FROM kb_block_properties WHERE block_id = ? ORDER BY name`, blockID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var props []PropertyValue
	for rows.Next() {
		var p PropertyValue
		var valStr string
		if err := rows.Scan(&p.Name, &valStr, &p.Type); err != nil {
			continue
		}
		p.Value = parseValue(valStr, p.Type)
		props = append(props, p)
	}
	return props, nil
}

// --- 属性 Schema 操作 ---

// SetPropertySchema 设置属性 schema
func (s *Store) SetPropertySchema(namespace string, schema PropertySchema) error {
	optsJSON, _ := json.Marshal(schema.Options)
	defStr := stringifyValue(schema.Default)
	_, err := s.db.Exec(`
		INSERT INTO kb_property_schemas (namespace, name, val_type, default_val, required, options)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(namespace, name) DO UPDATE SET
			val_type = excluded.val_type, default_val = excluded.default_val,
			required = excluded.required, options = excluded.options`,
		namespace, schema.Name, string(schema.Type), defStr, schema.Required, string(optsJSON))
	return err
}

// GetPropertySchemas 获取命名空间的属性 schema
func (s *Store) GetPropertySchemas(namespace string) ([]PropertySchema, error) {
	rows, err := s.db.Query(`
		SELECT name, val_type, default_val, required, options FROM kb_property_schemas
		WHERE namespace = ? ORDER BY name`, namespace)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var schemas []PropertySchema
	for rows.Next() {
		var s PropertySchema
		var valType, defStr, optsJSON string
		var required int
		if err := rows.Scan(&s.Name, &valType, &defStr, &required, &optsJSON); err != nil {
			continue
		}
		s.Type = PropertyType(valType)
		s.Required = required != 0
		json.Unmarshal([]byte(optsJSON), &s.Options)
		schemas = append(schemas, s)
	}
	return schemas, nil
}

// --- 收藏夹操作 ---

// AddFavorite 添加收藏
func (s *Store) AddFavorite(pageID int64) error {
	var maxOrder int
	s.db.QueryRow(`SELECT COALESCE(MAX(sort_order), 0) FROM kb_favorites`).Scan(&maxOrder)
	_, err := s.db.Exec(`INSERT OR IGNORE INTO kb_favorites (page_id, sort_order) VALUES (?, ?)`, pageID, maxOrder+1)
	return err
}

// RemoveFavorite 移除收藏
func (s *Store) RemoveFavorite(pageID int64) error {
	_, err := s.db.Exec(`DELETE FROM kb_favorites WHERE page_id = ?`, pageID)
	return err
}

// ListFavorites 列出收藏页面
func (s *Store) ListFavorites() ([]Page, error) {
	rows, err := s.db.Query(`
		SELECT p.id, p.title, p.file_name, p.namespace, p.is_journal, p.tags, p.properties, p.created_at, p.updated_at
		FROM kb_pages p
		JOIN kb_favorites f ON p.id = f.page_id
		ORDER BY f.sort_order`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanPages(rows)
}

// IsFavorite 是否已收藏
func (s *Store) IsFavorite(pageID int64) bool {
	var count int
	s.db.QueryRow(`SELECT COUNT(*) FROM kb_favorites WHERE page_id = ?`, pageID).Scan(&count)
	return count > 0
}

// ReorderFavorites 重排收藏顺序
func (s *Store) ReorderFavorites(pageIDs []int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for i, id := range pageIDs {
		if _, err := tx.Exec(`UPDATE kb_favorites SET sort_order = ? WHERE page_id = ?`, i, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// --- 最近访问 ---

// RecordRecent 记录最近访问的页面
func (s *Store) RecordRecent(pageID int64) error {
	// 确保表存在
	s.db.Exec(`CREATE TABLE IF NOT EXISTS kb_recent (
		page_id INTEGER PRIMARY KEY,
		visited_at TEXT NOT NULL DEFAULT (datetime('now'))
	)`)
	_, err := s.db.Exec(`
		INSERT INTO kb_recent (page_id, visited_at) VALUES (?, datetime('now'))
		ON CONFLICT(page_id) DO UPDATE SET visited_at = datetime('now')`, pageID)
	return err
}

// ListRecent 列出最近访问的页面
func (s *Store) ListRecent(limit int) ([]Page, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.db.Query(`
		SELECT p.id, p.title, p.file_name, p.namespace, p.is_journal, p.tags, p.properties, p.created_at, p.updated_at
		FROM kb_pages p
		JOIN kb_recent r ON p.id = r.page_id
		ORDER BY r.visited_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanPages(rows)
}

// --- 回收站操作 ---

// SoftDeletePage 软删除页面到回收站
func (s *Store) SoftDeletePage(id int64) error {
	p, err := s.GetPageByID(id)
	if err != nil || p == nil {
		return fmt.Errorf("page not found")
	}
	tagsJSON, _ := json.Marshal(p.Tags)
	propsJSON, _ := json.Marshal(p.Properties)
	isJournal := 0
	if p.IsJournal {
		isJournal = 1
	}
	// 移入回收站
	_, err = s.db.Exec(`
		INSERT INTO kb_recycle (page_id, title, file_name, namespace, is_journal, tags, properties)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		p.ID, p.Title, p.FileName, p.Namespace, isJournal, string(tagsJSON), string(propsJSON))
	if err != nil {
		return err
	}
	// 硬删除原数据（保留回收站记录）
	_, err = s.db.Exec(`DELETE FROM kb_page_properties WHERE page_id = ?`, id)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`DELETE FROM kb_links WHERE source_block_id IN (SELECT id FROM kb_blocks WHERE page_id = ?)`, id)
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

// ListRecycle 列出回收站
func (s *Store) ListRecycle() ([]RecycleItem, error) {
	rows, err := s.db.Query(`
		SELECT id, page_id, title, file_name, namespace, is_journal, tags, properties, deleted_at
		FROM kb_recycle ORDER BY deleted_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []RecycleItem
	for rows.Next() {
		var item RecycleItem
		var tagsJSON, propsJSON string
		var isJournal int
		if err := rows.Scan(&item.ID, &item.PageID, &item.Title, &item.FileName, &item.Namespace, &isJournal, &tagsJSON, &propsJSON, &item.DeletedAt); err != nil {
			continue
		}
		item.IsJournal = isJournal != 0
		json.Unmarshal([]byte(tagsJSON), &item.Tags)
		json.Unmarshal([]byte(propsJSON), &item.Properties)
		items = append(items, item)
	}
	return items, nil
}

// RestoreFromRecycle 从回收站恢复页面
func (s *Store) RestoreFromRecycle(recycleID int64) (*Page, error) {
	var item RecycleItem
	var tagsJSON, propsJSON string
	var isJournal int
	err := s.db.QueryRow(`
		SELECT id, page_id, title, file_name, namespace, is_journal, tags, properties, deleted_at
		FROM kb_recycle WHERE id = ?`, recycleID,
	).Scan(&item.ID, &item.PageID, &item.Title, &item.FileName, &item.Namespace, &isJournal, &tagsJSON, &propsJSON, &item.DeletedAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("recycle item not found")
	}
	if err != nil {
		return nil, err
	}
	item.IsJournal = isJournal != 0
	json.Unmarshal([]byte(tagsJSON), &item.Tags)
	json.Unmarshal([]byte(propsJSON), &item.Properties)

	// 重新插入页面
	result, err := s.db.Exec(`
		INSERT INTO kb_pages (title, file_name, namespace, is_journal, tags, properties)
		VALUES (?, ?, ?, ?, ?, ?)`,
		item.Title, item.FileName, item.Namespace, isJournal, tagsJSON, propsJSON)
	if err != nil {
		return nil, err
	}
	newID, _ := result.LastInsertId()

	// 从回收站删除
	s.db.Exec(`DELETE FROM kb_recycle WHERE id = ?`, recycleID)

	return s.GetPageByID(newID)
}

// DeleteRecyclePermanently 永久删除回收站项
func (s *Store) DeleteRecyclePermanently(recycleID int64) error {
	_, err := s.db.Exec(`DELETE FROM kb_recycle WHERE id = ?`, recycleID)
	return err
}

// EmptyRecycle 清空回收站
func (s *Store) EmptyRecycle() error {
	_, err := s.db.Exec(`DELETE FROM kb_recycle`)
	return err
}

// --- 块排序操作 ---

// ReorderBlock 移动块到目标位置（同级重排）
func (s *Store) ReorderBlock(blockID string, afterBlockID string, pageID int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// 获取要移动的块
	var moveOrder int
	err = tx.QueryRow(`SELECT block_order FROM kb_blocks WHERE id = ?`, blockID).Scan(&moveOrder)
	if err != nil {
		return fmt.Errorf("block not found: %w", err)
	}

	// 获取目标位置
	var targetOrder int
	if afterBlockID == "" {
		// 移到最前
		targetOrder = -1
	} else {
		err = tx.QueryRow(`SELECT block_order FROM kb_blocks WHERE id = ? AND page_id = ?`, afterBlockID, pageID).Scan(&targetOrder)
		if err != nil {
			return fmt.Errorf("target block not found: %w", err)
		}
	}

	// 临时把要移动的块设为很大值，避免冲突
	tx.Exec(`UPDATE kb_blocks SET block_order = 999999 WHERE id = ?`, blockID)

	// 重新排序：取出所有块（排除移动的），按顺序重新编号
	rows, err := tx.Query(`
		SELECT id FROM kb_blocks WHERE page_id = ? AND id != ? ORDER BY block_order`,
		pageID, blockID)
	if err != nil {
		return err
	}
	var blockIDs []string
	for rows.Next() {
		var id string
		rows.Scan(&id)
		blockIDs = append(blockIDs, id)
	}
	rows.Close()

	// 插入移动的块到目标位置
	newOrder := make([]string, 0, len(blockIDs)+1)
	if afterBlockID == "" {
		newOrder = append(newOrder, blockID)
		newOrder = append(newOrder, blockIDs...)
	} else {
		inserted := false
		for _, id := range blockIDs {
			newOrder = append(newOrder, id)
			if id == afterBlockID {
				newOrder = append(newOrder, blockID)
				inserted = true
			}
		}
		if !inserted {
			newOrder = append(newOrder, blockID)
		}
	}

	// 批量更新顺序
	for i, id := range newOrder {
		if _, err := tx.Exec(`UPDATE kb_blocks SET block_order = ?, updated_at = datetime('now') WHERE id = ?`, i, id); err != nil {
			return err
		}
	}

	return tx.Commit()
}

// MoveBlockToPage 移动块到另一个页面
func (s *Store) MoveBlockToPage(blockID string, targetPageID int64, afterBlockID string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// 获取目标页面最大 order
	var maxOrder int
	tx.QueryRow(`SELECT COALESCE(MAX(block_order), -1) FROM kb_blocks WHERE page_id = ?`, targetPageID).Scan(&maxOrder)

	// 更新块的 page_id 和 order
	newOrder := maxOrder + 1
	if afterBlockID != "" {
		var afterOrder int
		err = tx.QueryRow(`SELECT block_order FROM kb_blocks WHERE id = ? AND page_id = ?`, afterBlockID, targetPageID).Scan(&afterOrder)
		if err == nil {
			// 后移目标页面中 order > afterOrder 的块
			tx.Exec(`UPDATE kb_blocks SET block_order = block_order + 1 WHERE page_id = ? AND block_order > ?`, targetPageID, afterOrder)
			newOrder = afterOrder + 1
		}
	}

	_, err = tx.Exec(`UPDATE kb_blocks SET page_id = ?, block_order = ?, updated_at = datetime('now') WHERE id = ?`,
		targetPageID, newOrder, blockID)
	if err != nil {
		return err
	}

	return tx.Commit()
}

// --- Unlinked References ---

// GetUnlinkedReferences 查找未链接的文本引用（页面标题在其它页面正文中出现但未用 [[]] 包裹）
func (s *Store) GetUnlinkedReferences(pageTitle string) ([]Backlink, error) {
	pattern := "%" + pageTitle + "%"
	rows, err := s.db.Query(`
		SELECT b.id, b.page_id, p.title, b.content
		FROM kb_blocks b
		JOIN kb_pages p ON b.page_id = p.id
		WHERE b.content LIKE ? AND b.content NOT LIKE '%[[%' 
		AND p.title != ?
		ORDER BY b.updated_at DESC`, pattern, pageTitle)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var refs []Backlink
	for rows.Next() {
		var r Backlink
		if err := rows.Scan(&r.BlockID, &r.PageID, &r.PageTitle, &r.BlockContent); err != nil {
			continue
		}
		r.LinkType = "unlinked"
		// 提取上下文
		r.Context = extractContext(r.BlockContent, pageTitle)
		refs = append(refs, r)
	}
	return refs, nil
}

// --- 辅助函数 ---

// RecycleItem 回收站项
type RecycleItem struct {
	ID         int64          `json:"id"`
	PageID     int64          `json:"page_id"`
	Title      string         `json:"title"`
	FileName   string         `json:"file_name"`
	Namespace  string         `json:"namespace"`
	IsJournal  bool           `json:"is_journal"`
	Tags       []string       `json:"tags"`
	Properties map[string]any `json:"properties"`
	DeletedAt  string         `json:"deleted_at"`
}

func stringifyValue(v any) string {
	switch val := v.(type) {
	case string:
		return val
	case nil:
		return ""
	default:
		b, _ := json.Marshal(val)
		return string(b)
	}
}

func parseValue(s string, t PropertyType) any {
	switch t {
	case PropTypeNumber:
		var n float64
		json.Unmarshal([]byte(s), &n)
		if n == 0 {
			// 可能是纯文本数字
			fmt.Sscanf(s, "%f", &n)
		}
		return n
	case PropTypeBoolean:
		return s == "true" || s == "1"
	case PropTypeMulti, PropTypeTags:
		var arr []string
		if err := json.Unmarshal([]byte(s), &arr); err != nil {
			// 尝试逗号分隔
			for _, p := range strings.Split(s, ",") {
				if p = strings.TrimSpace(p); p != "" {
					arr = append(arr, p)
				}
			}
		}
		return arr
	default:
		return s
	}
}

func inferType(v any) PropertyType {
	switch v.(type) {
	case float64, int, int64:
		return PropTypeNumber
	case bool:
		return PropTypeBoolean
	case []any, []string:
		return PropTypeTags
	default:
		return PropTypeString
	}
}

func extractContext(content string, keyword string) string {
	idx := strings.Index(strings.ToLower(content), strings.ToLower(keyword))
	if idx < 0 {
		if len(content) > 100 {
			return content[:100] + "..."
		}
		return content
	}
	start := idx - 30
	if start < 0 {
		start = 0
	}
	end := idx + len(keyword) + 30
	if end > len(content) {
		end = len(content)
	}
	ctx := content[start:end]
	if start > 0 {
		ctx = "..." + ctx
	}
	if end < len(content) {
		ctx = ctx + "..."
	}
	return ctx
}
