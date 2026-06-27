package kb

import (
	"fmt"
	"strings"
)

// Linker 双向链接引擎
// 负责从块内容中提取链接，解析目标页面/块，建立双向关联
type Linker struct {
	store *Store
}

// NewLinker 创建链接引擎
func NewLinker(store *Store) *Linker {
	return &Linker{store: store}
}

// IndexPage 索引一个页面：解析内容，存储块，提取链接
// 这是核心入口：Markdown 文件 → 解析 → 存储 → 链接
func (l *Linker) IndexPage(content string, fileName string) (*Page, error) {
	// 1. 解析 Markdown
	parsed := ParsePage(content, fileName)

	// 2. 存储页面
	pageID, err := l.store.UpsertPage(parsed)
	if err != nil {
		return nil, fmt.Errorf("upsert page: %w", err)
	}

	// 3. 存储块
	if err := l.store.UpsertBlocks(pageID, parsed.Blocks); err != nil {
		return nil, fmt.Errorf("upsert blocks: %w", err)
	}

	// 4. 提取并存储链接
	blocks, err := l.store.GetBlocks(pageID)
	if err != nil {
		return nil, fmt.Errorf("get blocks for linking: %w", err)
	}

	var links []Link
	for _, block := range blocks {
		blockLinks := l.extractBlockLinks(block)
		links = append(links, blockLinks...)
	}

	if len(links) > 0 {
		if err := l.store.AddLinks(links); err != nil {
			return nil, fmt.Errorf("add links: %w", err)
		}
	}

	// 5. 为被引用但尚不存在的页面标题创建占位页面
	l.ensurePlaceholderPages(links)

	// 6. 修复悬空链接：更新 target_page_id 为 0 但 target_page_title 已存在的链接
	l.fixDanglingLinks()

	page, _ := l.store.GetPageByID(pageID)
	return page, nil
}

// extractBlockLinks 从单个块中提取链接
func (l *Linker) extractBlockLinks(block Block) []Link {
	extracted := ExtractLinks(block.Content)
	var links []Link

	for _, ex := range extracted {
		link := Link{
			SourceBlockID: block.ID,
			LinkType:      ex.LinkType,
			Context:       truncateContext(block.Content, 200),
		}

		if ex.LinkType == "page_ref" || ex.LinkType == "tag" {
			link.TargetPageTitle = ex.TargetPageTitle
			// 查找目标页面ID
			if targetPage, _ := l.store.GetPageByTitle(ex.TargetPageTitle); targetPage != nil {
				link.TargetPageID = targetPage.ID
			}
		} else if ex.LinkType == "block_ref" {
			link.TargetBlockID = ex.TargetBlockID
			// 查找目标块所属页面
			if targetBlock, _ := l.store.GetBlock(ex.TargetBlockID); targetBlock != nil {
				link.TargetPageID = targetBlock.PageID
				if targetPage, _ := l.store.GetPageByID(targetBlock.PageID); targetPage != nil {
					link.TargetPageTitle = targetPage.Title
				}
			}
		}

		links = append(links, link)
	}

	return links
}

// ensurePlaceholderPages 确保被引用的页面存在（创建占位页面）
func (l *Linker) ensurePlaceholderPages(links []Link) {
	seen := make(map[string]bool)
	for _, link := range links {
		if link.TargetPageTitle == "" {
			continue
		}
		if seen[link.TargetPageTitle] {
			continue
		}
		seen[link.TargetPageTitle] = true

		// 检查是否已存在
		if existing, _ := l.store.GetPageByTitle(link.TargetPageTitle); existing != nil {
			continue
		}

		// 创建占位页面
		placeholder := ParsedPage{
			Title:     link.TargetPageTitle,
			FileName:  titleToFileName(link.TargetPageTitle),
			Namespace: extractNamespace(link.TargetPageTitle),
		}
		l.store.UpsertPage(placeholder)
	}
}

// fixDanglingLinks 修复悬空链接：更新 target_page_id 为 0 但 target_page_title 已存在的链接
func (l *Linker) fixDanglingLinks() {
	rows, err := l.store.db.Query(`
		SELECT l.id, l.target_page_title FROM kb_links l
		WHERE l.target_page_id = 0 AND l.target_page_title != ''`)
	if err != nil {
		return
	}
	defer rows.Close()

	type danglingLink struct {
		LinkID   int64
		PageTitle string
	}
	var dangling []danglingLink
	for rows.Next() {
		var dl danglingLink
		if err := rows.Scan(&dl.LinkID, &dl.PageTitle); err != nil {
			continue
		}
		dangling = append(dangling, dl)
	}

	for _, dl := range dangling {
		if page, _ := l.store.GetPageByTitle(dl.PageTitle); page != nil {
			l.store.db.Exec(`UPDATE kb_links SET target_page_id = ? WHERE id = ?`, page.ID, dl.LinkID)
		}
	}
}

// RebuildLinks 重建所有页面的链接关系
// 在批量导入后调用，确保所有 target_page_id 被正确填充
func (l *Linker) RebuildLinks() error {
	pages, err := l.store.ListPages()
	if err != nil {
		return fmt.Errorf("list pages: %w", err)
	}

	for _, page := range pages {
		blocks, err := l.store.GetBlocks(page.ID)
		if err != nil {
			continue
		}

		// 重新提取链接（旧链接在 UpsertBlocks 时已清除）
		var links []Link
		for _, block := range blocks {
			blockLinks := l.extractBlockLinks(block)
			links = append(links, blockLinks...)
		}

		if len(links) > 0 {
			l.store.AddLinks(links)
		}
	}

	return nil
}

// GetBacklinks 获取页面的反向链接（谁引用了此页面）
func (l *Linker) GetBacklinks(pageTitle string) ([]Backlink, error) {
	page, err := l.store.GetPageByTitle(pageTitle)
	if err != nil {
		return nil, err
	}
	if page == nil {
		return nil, nil
	}
	return l.store.GetBacklinks(page.ID)
}

// GetOutlinks 获取页面的外部链接（此页面引用了谁）
func (l *Linker) GetOutlinks(pageTitle string) ([]Link, error) {
	page, err := l.store.GetPageByTitle(pageTitle)
	if err != nil {
		return nil, err
	}
	if page == nil {
		return nil, nil
	}
	return l.store.GetOutlinks(page.ID)
}

// GetAllTags 获取所有标签及其关联页面数
func (l *Linker) GetAllTags() (map[string]int, error) {
	tags := make(map[string]int)

	rows, err := l.store.db.Query(`
		SELECT target_page_title, COUNT(DISTINCT source_block_id) as cnt
		FROM kb_links WHERE link_type = 'tag'
		GROUP BY target_page_title
		ORDER BY cnt DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var tag string
		var cnt int
		if err := rows.Scan(&tag, &cnt); err != nil {
			continue
		}
		tags[tag] = cnt
	}

	return tags, nil
}

// GetPagesByTag 获取带有某标签的所有页面
func (l *Linker) GetPagesByTag(tag string) ([]Page, error) {
	rows, err := l.store.db.Query(`
		SELECT DISTINCT p.id, p.title, p.file_name, p.namespace, p.is_journal, p.tags, p.properties, p.created_at, p.updated_at
		FROM kb_pages p
		JOIN kb_blocks b ON b.page_id = p.id
		JOIN kb_links l ON l.source_block_id = b.id
		WHERE l.link_type = 'tag' AND l.target_page_title = ?
		ORDER BY p.updated_at DESC`, tag)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanPages(rows)
}

// --- 辅助函数 ---

// titleToFileName 将页面标题转换为文件名
// "项目/子项目" -> "项目/子项目.md"
func titleToFileName(title string) string {
	return title + ".md"
}

// truncateContext 截断上下文文本
func truncateContext(text string, maxLen int) string {
	text = strings.TrimSpace(text)
	if len(text) <= maxLen {
		return text
	}
	return text[:maxLen] + "..."
}
