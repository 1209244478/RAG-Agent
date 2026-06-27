package kb

import (
	"database/sql"
	"fmt"
)

// Version 版本历史管理器
type Version struct {
	store *Store
}

// PageVersion 页面版本记录
type PageVersion struct {
	ID        int64  `json:"id"`
	PageID    int64  `json:"page_id"`
	Version   int    `json:"version"`
	Content   string `json:"content"`
	CreatedAt string `json:"created_at"`
}

// NewVersion 创建版本管理器
func NewVersion(store *Store) *Version {
	return &Version{store: store}
}

// SaveVersion 保存页面版本快照
// 在每次页面更新前调用，记录上一版本内容
func (v *Version) SaveVersion(pageID int64, content string) error {
	// 获取当前最大版本号
	var maxVersion int
	err := v.store.db.QueryRow(
		"SELECT COALESCE(MAX(version), 0) FROM kb_page_versions WHERE page_id = ?",
		pageID,
	).Scan(&maxVersion)
	if err != nil {
		return fmt.Errorf("get max version: %w", err)
	}

	// 检查内容是否与上一版本相同，相同则跳过
	if maxVersion > 0 {
		var lastContent string
		err := v.store.db.QueryRow(
			"SELECT content FROM kb_page_versions WHERE page_id = ? AND version = ?",
			pageID, maxVersion,
		).Scan(&lastContent)
		if err == nil && lastContent == content {
			return nil // 内容未变化，跳过
		}
	}

	// 插入新版本
	_, err = v.store.db.Exec(
		"INSERT INTO kb_page_versions (page_id, version, content) VALUES (?, ?, ?)",
		pageID, maxVersion+1, content,
	)
	if err != nil {
		return fmt.Errorf("insert version: %w", err)
	}

	// 限制每个页面最多保留 50 个版本
	v.pruneOldVersions(pageID, 50)
	return nil
}

// ListVersions 列出页面的所有版本
func (v *Version) ListVersions(pageID int64) ([]PageVersion, error) {
	rows, err := v.store.db.Query(`
		SELECT id, page_id, version, content, created_at
		FROM kb_page_versions
		WHERE page_id = ?
		ORDER BY version DESC
		LIMIT 50`, pageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var versions []PageVersion
	for rows.Next() {
		var pv PageVersion
		if err := rows.Scan(&pv.ID, &pv.PageID, &pv.Version, &pv.Content, &pv.CreatedAt); err != nil {
			continue
		}
		versions = append(versions, pv)
	}
	return versions, nil
}

// GetVersion 获取特定版本
func (v *Version) GetVersion(pageID int64, version int) (*PageVersion, error) {
	pv := &PageVersion{}
	err := v.store.db.QueryRow(`
		SELECT id, page_id, version, content, created_at
		FROM kb_page_versions
		WHERE page_id = ? AND version = ?`,
		pageID, version,
	).Scan(&pv.ID, &pv.PageID, &pv.Version, &pv.Content, &pv.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return pv, nil
}

// Diff 计算两个版本的差异（简单行级 diff）
type DiffResult struct {
	FromVersion int        `json:"from_version"`
	ToVersion   int        `json:"to_version"`
	Lines       []DiffLine `json:"lines"`
}

type DiffLine struct {
	Type    string `json:"type"` // "added" | "removed" | "context"
	Content string `json:"content"`
}

// Diff 比较两个版本
func (v *Version) Diff(pageID int64, fromVersion, toVersion int) (*DiffResult, error) {
	from, err := v.GetVersion(pageID, fromVersion)
	if err != nil {
		return nil, fmt.Errorf("get from version: %w", err)
	}
	if from == nil {
		return nil, fmt.Errorf("version %d not found", fromVersion)
	}

	to, err := v.GetVersion(pageID, toVersion)
	if err != nil {
		return nil, fmt.Errorf("get to version: %w", err)
	}
	if to == nil {
		return nil, fmt.Errorf("version %d not found", toVersion)
	}

	// 简单的行级 diff（LCS 算法）
	fromLines := splitLines(from.Content)
	toLines := splitLines(to.Content)
	diffLines := simpleDiff(fromLines, toLines)

	return &DiffResult{
		FromVersion: fromVersion,
		ToVersion:   toVersion,
		Lines:       diffLines,
	}, nil
}

// Rollback 回滚到指定版本
func (v *Version) Rollback(pageID int64, version int) (string, error) {
	pv, err := v.GetVersion(pageID, version)
	if err != nil {
		return "", fmt.Errorf("get version: %w", err)
	}
	if pv == nil {
		return "", fmt.Errorf("version %d not found", version)
	}

	// 在回滚前保存当前内容作为新版本
	page, err := v.store.GetPageByID(pageID)
	if err != nil {
		return "", fmt.Errorf("get page: %w", err)
	}
	if page == nil {
		return "", fmt.Errorf("page not found")
	}

	// 获取当前内容
	blocks, err := v.store.GetBlocks(pageID)
	if err != nil {
		return "", fmt.Errorf("get blocks: %w", err)
	}
	currentContent := blocksToText(blocks)

	// 保存当前版本
	if err := v.SaveVersion(pageID, currentContent); err != nil {
		return "", fmt.Errorf("save current version: %w", err)
	}

	return pv.Content, nil
}

// pruneOldVersions 清理旧版本，保留最近 N 个
func (v *Version) pruneOldVersions(pageID int64, keep int) {
	// 获取版本总数
	var count int
	err := v.store.db.QueryRow(
		"SELECT COUNT(*) FROM kb_page_versions WHERE page_id = ?",
		pageID,
	).Scan(&count)
	if err != nil || count <= keep {
		return
	}

	// 删除超出数量的旧版本
	v.store.db.Exec(`
		DELETE FROM kb_page_versions
		WHERE page_id = ? AND version NOT IN (
			SELECT version FROM kb_page_versions
			WHERE page_id = ?
			ORDER BY version DESC
			LIMIT ?
		)`, pageID, pageID, keep)
}

// 辅助函数
func splitLines(s string) []string {
	var lines []string
	current := ""
	for _, r := range s {
		if r == '\n' {
			lines = append(lines, current)
			current = ""
		} else {
			current += string(r)
		}
	}
	if current != "" {
		lines = append(lines, current)
	}
	return lines
}

func blocksToText(blocks []Block) string {
	var lines []string
	for _, b := range blocks {
		lines = append(lines, b.Content)
	}
	return joinLines(lines)
}

func joinLines(lines []string) string {
	result := ""
	for i, l := range lines {
		if i > 0 {
			result += "\n"
		}
		result += l
	}
	return result
}

// simpleDiff 简单的 LCS diff 算法
func simpleDiff(a, b []string) []DiffLine {
	// 构建 LCS 表
	m, n := len(a), len(b)
	dp := make([][]int, m+1)
	for i := range dp {
		dp[i] = make([]int, n+1)
	}

	for i := 1; i <= m; i++ {
		for j := 1; j <= n; j++ {
			if a[i-1] == b[j-1] {
				dp[i][j] = dp[i-1][j-1] + 1
			} else if dp[i-1][j] > dp[i][j-1] {
				dp[i][j] = dp[i-1][j]
			} else {
				dp[i][j] = dp[i][j-1]
			}
		}
	}

	// 回溯生成 diff
	var result []DiffLine
	i, j := m, n
	for i > 0 && j > 0 {
		if a[i-1] == b[j-1] {
			result = append([]DiffLine{{Type: "context", Content: a[i-1]}}, result...)
			i--
			j--
		} else if dp[i-1][j] > dp[i][j-1] {
			result = append([]DiffLine{{Type: "removed", Content: a[i-1]}}, result...)
			i--
		} else {
			result = append([]DiffLine{{Type: "added", Content: b[j-1]}}, result...)
			j--
		}
	}
	for i > 0 {
		result = append([]DiffLine{{Type: "removed", Content: a[i-1]}}, result...)
		i--
	}
	for j > 0 {
		result = append([]DiffLine{{Type: "added", Content: b[j-1]}}, result...)
		j--
	}

	return result
}
