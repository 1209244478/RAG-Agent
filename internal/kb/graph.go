package kb

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Graph 图数据库引擎
// 提供图谱视图数据、文件系统同步等高级功能
type Graph struct {
	store  *Store
	linker *Linker
}

// NewGraph 创建图引擎
func NewGraph(store *Store, linker *Linker) *Graph {
	return &Graph{store: store, linker: linker}
}

// GetGraphData 获取完整图谱数据（用于前端力导向图渲染）
func (g *Graph) GetGraphData() (*GraphData, error) {
	// 获取所有页面链接关系
	rows, err := g.store.db.Query(`
		SELECT
			b.page_id AS source_page,
			COALESCE(l.target_page_id, 0) AS target_page,
			l.target_page_title,
			l.link_type
		FROM kb_links l
		JOIN kb_blocks b ON l.source_block_id = b.id
		WHERE l.link_type IN ('page_ref', 'tag')
		AND l.target_page_id > 0`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	nodeSet := make(map[int64]string) // pageID -> title
	edgeSet := make(map[string]bool)  // 去重
	var edges []GraphEdge

	for rows.Next() {
		var sourceID, targetID int64
		var targetTitle, linkType string
		if err := rows.Scan(&sourceID, &targetID, &targetTitle, &linkType); err != nil {
			continue
		}
		if sourceID == 0 || targetID == 0 {
			continue
		}

		// 记录节点
		if page, _ := g.store.GetPageByID(sourceID); page != nil {
			nodeSet[sourceID] = page.Title
		}
		if page, _ := g.store.GetPageByID(targetID); page != nil {
			nodeSet[targetID] = page.Title
		}

		// 去重边
		edgeKey := fmt.Sprintf("%d-%d-%s", sourceID, targetID, linkType)
		if !edgeSet[edgeKey] {
			edgeSet[edgeKey] = true
			edges = append(edges, GraphEdge{
				Source: sourceID,
				Target: targetID,
				Type:   linkType,
			})
		}
	}

	// 补充没有链接关系的独立页面
	allPages, _ := g.store.ListPages()
	for _, page := range allPages {
		if _, exists := nodeSet[page.ID]; !exists {
			nodeSet[page.ID] = page.Title
		}
	}

	// 构建节点列表，计算每个节点的连接数
	degree := make(map[int64]int)
	for _, edge := range edges {
		degree[edge.Source]++
		degree[edge.Target]++
	}

	var nodes []GraphNode
	for id, title := range nodeSet {
		nodes = append(nodes, GraphNode{
			ID:    id,
			Label: title,
			Type:  "page",
			Size:  degree[id] + 1,
		})
	}

	return &GraphData{
		Nodes: nodes,
		Edges: edges,
	}, nil
}

// GetLocalGraph 获取以某页面为中心的局部图谱
func (g *Graph) GetLocalGraph(pageTitle string, depth int) (*GraphData, error) {
	if depth <= 0 {
		depth = 1
	}

	page, err := g.store.GetPageByTitle(pageTitle)
	if err != nil || page == nil {
		return &GraphData{}, nil
	}

	visited := make(map[int64]bool)
	var nodes []GraphNode
	var edges []GraphEdge

	// BFS 遍历
	queue := []int64{page.ID}
	visited[page.ID] = true
	nodes = append(nodes, GraphNode{
		ID:    page.ID,
		Label: page.Title,
		Type:  "page",
		Size:  10, // 中心节点更大
	})

	for d := 0; d < depth && len(queue) > 0; d++ {
		var nextQueue []int64
		for _, pageID := range queue {
			neighbors := g.getNeighbors(pageID)
			for _, neighbor := range neighbors {
				edges = append(edges, GraphEdge{
					Source: pageID,
					Target: neighbor.ID,
					Type:   neighbor.LinkType,
				})
				if !visited[neighbor.ID] {
					visited[neighbor.ID] = true
					nodes = append(nodes, GraphNode{
						ID:    neighbor.ID,
						Label: neighbor.Title,
						Type:  "page",
						Size:  5,
					})
					nextQueue = append(nextQueue, neighbor.ID)
				}
			}
		}
		queue = nextQueue
	}

	return &GraphData{Nodes: nodes, Edges: edges}, nil
}

// neighborInfo 邻居节点信息
type neighborInfo struct {
	ID       int64
	Title    string
	LinkType string
}

// getNeighbors 获取页面的邻居节点（出链+入链）
func (g *Graph) getNeighbors(pageID int64) []neighborInfo {
	var neighbors []neighborInfo
	seen := make(map[int64]bool)

	// 出链
	outlinks, _ := g.store.GetOutlinks(pageID)
	for _, link := range outlinks {
		if link.TargetPageID > 0 && !seen[link.TargetPageID] {
			seen[link.TargetPageID] = true
			if page, _ := g.store.GetPageByID(link.TargetPageID); page != nil {
				neighbors = append(neighbors, neighborInfo{
					ID:       page.ID,
					Title:    page.Title,
					LinkType: link.LinkType,
				})
			}
		}
	}

	// 入链
	backlinks, _ := g.store.GetBacklinks(pageID)
	for _, bl := range backlinks {
		if !seen[bl.PageID] {
			seen[bl.PageID] = true
			neighbors = append(neighbors, neighborInfo{
				ID:       bl.PageID,
				Title:    bl.PageTitle,
				LinkType: bl.LinkType,
			})
		}
	}

	return neighbors
}

// SyncFromDirectory 从目录同步 Markdown 文件到知识库
// 扫描 dir 下的所有 .md 文件，解析并索引
func (g *Graph) SyncFromDirectory(dir string) (*SyncResult, error) {
	result := &SyncResult{}

	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			// 跳过隐藏目录
			if strings.HasPrefix(info.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}

		// 只处理 .md 文件
		if !strings.HasSuffix(strings.ToLower(info.Name()), ".md") {
			return nil
		}

		// 跳过 logseq 配置目录
		relPath, _ := filepath.Rel(dir, path)
		relPath = strings.ReplaceAll(relPath, "\\", "/")
		if strings.HasPrefix(relPath, "logseq/") {
			return nil
		}

		content, err := os.ReadFile(path)
		if err != nil {
			result.Failed = append(result.Failed, relPath+": "+err.Error())
			return nil
		}

		_, err = g.linker.IndexPage(string(content), relPath)
		if err != nil {
			result.Failed = append(result.Failed, relPath+": "+err.Error())
			return nil
		}

		result.Indexed = append(result.Indexed, relPath)
		return nil
	})

	if err != nil {
		return result, err
	}

	// 重建链接关系（确保跨文件引用正确）
	g.linker.RebuildLinks()

	return result, nil
}

// SyncResult 同步结果
type SyncResult struct {
	Indexed []string `json:"indexed"` // 成功索引的文件
	Failed  []string `json:"failed"`  // 失败的文件
}

// ExportPage 导出页面为 Markdown 文本
func (g *Graph) ExportPage(pageTitle string) (string, error) {
	page, err := g.store.GetPageByTitle(pageTitle)
	if err != nil || page == nil {
		return "", fmt.Errorf("page not found: %s", pageTitle)
	}

	blocks, err := g.store.GetBlocks(page.ID)
	if err != nil {
		return "", err
	}

	var sb strings.Builder

	// 写入 frontmatter
	if len(page.Properties) > 0 {
		sb.WriteString("---\n")
		if tags, ok := page.Properties["tags"].([]string); ok && len(tags) > 0 {
			sb.WriteString("tags: [")
			sb.WriteString(strings.Join(tags, ", "))
			sb.WriteString("]\n")
		}
		for k, v := range page.Properties {
			if k == "tags" {
				continue
			}
			sb.WriteString(fmt.Sprintf("%s: %v\n", k, v))
		}
		sb.WriteString("---\n\n")
	}

	// 写入块
	for _, block := range blocks {
		indent := strings.Repeat("  ", block.Level)
		sb.WriteString(indent)
		sb.WriteString(block.Content)
		sb.WriteString("\n")
	}

	return sb.String(), nil
}

// GetStats 获取知识库统计信息
func (g *Graph) GetStats() (*KBStats, error) {
	stats := &KBStats{}

	g.store.db.QueryRow("SELECT COUNT(*) FROM kb_pages").Scan(&stats.TotalPages)
	g.store.db.QueryRow("SELECT COUNT(*) FROM kb_pages WHERE is_journal = 1").Scan(&stats.JournalPages)
	g.store.db.QueryRow("SELECT COUNT(*) FROM kb_blocks").Scan(&stats.TotalBlocks)
	g.store.db.QueryRow("SELECT COUNT(*) FROM kb_links").Scan(&stats.TotalLinks)
	g.store.db.QueryRow("SELECT COUNT(DISTINCT target_page_title) FROM kb_links WHERE link_type = 'tag'").Scan(&stats.TotalTags)

	return stats, nil
}

// KBStats 知识库统计
type KBStats struct {
	TotalPages    int `json:"total_pages"`
	JournalPages  int `json:"journal_pages"`
	TotalBlocks   int `json:"total_blocks"`
	TotalLinks    int `json:"total_links"`
	TotalTags     int `json:"total_tags"`
}
