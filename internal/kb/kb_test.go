package kb

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseMarkdown(t *testing.T) {
	content := `---
title: 测试页面
tags: [Go, 测试]
---

# 标题

这是一个段落，包含 [[双链]] 和 #标签。

- 列表项1
- 列表项2

> 引用块

` + "```" + `
code block
` + "```" + `
`

	blocks := ParseMarkdown(content)

	if len(blocks) == 0 {
		t.Fatal("expected blocks, got 0")
	}

	// 验证 frontmatter 解析
	if blocks[0].Properties["title"] != "测试页面" {
		t.Errorf("expected title '测试页面', got %v", blocks[0].Properties["title"])
	}

	// 验证块类型检测
	foundHeading := false
	foundList := false
	foundCode := false
	foundQuote := false
	for _, b := range blocks {
		switch b.BlockType {
		case "heading":
			foundHeading = true
		case "list":
			foundList = true
		case "code":
			foundCode = true
		case "quote":
			foundQuote = true
		}
	}
	if !foundHeading {
		t.Error("heading block not found")
	}
	if !foundList {
		t.Error("list block not found")
	}
	if !foundCode {
		t.Error("code block not found")
	}
	if !foundQuote {
		t.Error("quote block not found")
	}
}

func TestExtractLinks(t *testing.T) {
	text := "这是 [[页面A]] 和 [[页面B|别名]] 以及 ((12345678-1234-1234-1234-123456789012)) 还有 #标签1"

	links := ExtractLinks(text)

	if len(links) < 3 {
		t.Fatalf("expected at least 3 links, got %d", len(links))
	}

	// 验证页面引用
	foundPageA := false
	foundPageB := false
	foundBlockRef := false
	foundTag := false

	for _, link := range links {
		switch link.LinkType {
		case "page_ref":
			if link.TargetPageTitle == "页面A" {
				foundPageA = true
			}
			if link.TargetPageTitle == "页面B" {
				foundPageB = true
			}
		case "block_ref":
			if link.TargetBlockID == "12345678-1234-1234-1234-123456789012" {
				foundBlockRef = true
			}
		case "tag":
			if link.TargetPageTitle == "标签1" {
				foundTag = true
			}
		}
	}

	if !foundPageA {
		t.Error("page ref '页面A' not found")
	}
	if !foundPageB {
		t.Error("page ref '页面B' not found")
	}
	if !foundBlockRef {
		t.Error("block ref not found")
	}
	if !foundTag {
		t.Error("tag '标签1' not found")
	}
}

func TestStoreAndLinker(t *testing.T) {
	// 使用临时数据库
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test_kb.db")

	store, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	defer store.Close()

	linker := NewLinker(store)

	// 索引第一个页面
	content1 := `# 项目A

这是 [[项目B]] 的描述。

- 任务1 #重要
- 任务2
`
	page1, err := linker.IndexPage(content1, "项目A.md")
	if err != nil {
		t.Fatalf("IndexPage failed: %v", err)
	}
	if page1 == nil {
		t.Fatal("page1 is nil")
	}
	if page1.Title != "项目A" {
		t.Errorf("expected title '项目A', got '%s'", page1.Title)
	}

	// 索引第二个页面（被第一个页面引用）
	content2 := `# 项目B

这是项目B的内容，[[项目A]] 也引用了它。

#测试标签
`
	page2, err := linker.IndexPage(content2, "项目B.md")
	if err != nil {
		t.Fatalf("IndexPage page2 failed: %v", err)
	}
	if page2 == nil {
		t.Fatal("page2 is nil")
	}

	// 验证块
	blocks1, err := store.GetBlocks(page1.ID)
	if err != nil {
		t.Fatalf("GetBlocks failed: %v", err)
	}
	if len(blocks1) == 0 {
		t.Fatal("expected blocks for page1")
	}

	// 验证反向链接
	backlinks, err := store.GetBacklinks(page2.ID)
	if err != nil {
		t.Fatalf("GetBacklinks failed: %v", err)
	}
	if len(backlinks) == 0 {
		t.Fatal("expected backlinks for page2")
	}

	foundBacklink := false
	for _, bl := range backlinks {
		if bl.PageTitle == "项目A" {
			foundBacklink = true
			break
		}
	}
	if !foundBacklink {
		t.Error("backlink from '项目A' to '项目B' not found")
	}

	// 验证搜索
	results, err := store.Search("项目", 10)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected search results")
	}

	// 验证标签
	tags, err := linker.GetAllTags()
	if err != nil {
		t.Fatalf("GetAllTags failed: %v", err)
	}
	if len(tags) == 0 {
		t.Fatal("expected tags")
	}
	if _, ok := tags["重要"]; !ok {
		t.Error("tag '重要' not found")
	}
	if _, ok := tags["测试标签"]; !ok {
		t.Error("tag '测试标签' not found")
	}
}

func TestGraph(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test_graph.db")

	store, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	defer store.Close()

	linker := NewLinker(store)
	graph := NewGraph(store, linker)

	// 索引多个页面
	pages := []struct {
		content  string
		fileName string
	}{
		{"# 中心页面\n\n链接到 [[页面A]] 和 [[页面B]]", "中心页面.md"},
		{"# 页面A\n\n链接回 [[中心页面]]", "页面A.md"},
		{"# 页面B\n\n链接到 [[页面A]]", "页面B.md"},
	}

	for _, p := range pages {
		_, err := linker.IndexPage(p.content, p.fileName)
		if err != nil {
			t.Fatalf("IndexPage %s failed: %v", p.fileName, err)
		}
	}

	// 获取完整图谱
	graphData, err := graph.GetGraphData()
	if err != nil {
		t.Fatalf("GetGraphData failed: %v", err)
	}
	if len(graphData.Nodes) < 3 {
		t.Errorf("expected at least 3 nodes, got %d", len(graphData.Nodes))
	}
	if len(graphData.Edges) < 3 {
		t.Errorf("expected at least 3 edges, got %d", len(graphData.Edges))
	}

	// 获取局部图谱
	localGraph, err := graph.GetLocalGraph("中心页面", 1)
	if err != nil {
		t.Fatalf("GetLocalGraph failed: %v", err)
	}
	if len(localGraph.Nodes) < 3 {
		t.Errorf("expected at least 3 local nodes, got %d", len(localGraph.Nodes))
	}

	// 获取统计
	stats, err := graph.GetStats()
	if err != nil {
		t.Fatalf("GetStats failed: %v", err)
	}
	if stats.TotalPages < 3 {
		t.Errorf("expected at least 3 pages, got %d", stats.TotalPages)
	}

	// 测试导出
	exported, err := graph.ExportPage("中心页面")
	if err != nil {
		t.Fatalf("ExportPage failed: %v", err)
	}
	if exported == "" {
		t.Error("exported content is empty")
	}
}

func TestSyncFromDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test_sync.db")

	store, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	defer store.Close()

	linker := NewLinker(store)
	graph := NewGraph(store, linker)

	// 创建测试 Markdown 文件
	pagesDir := filepath.Join(tmpDir, "pages")
	os.MkdirAll(pagesDir, 0755)

	os.WriteFile(filepath.Join(pagesDir, "笔记1.md"), []byte("# 笔记1\n\n内容 [[笔记2]]"), 0644)
	os.WriteFile(filepath.Join(pagesDir, "笔记2.md"), []byte("# 笔记2\n\n内容 [[笔记1]]"), 0644)

	// 同步
	result, err := graph.SyncFromDirectory(pagesDir)
	if err != nil {
		t.Fatalf("SyncFromDirectory failed: %v", err)
	}
	if len(result.Indexed) != 2 {
		t.Errorf("expected 2 indexed files, got %d", len(result.Indexed))
	}

	// 验证同步后数据
	pages, _ := store.ListPages()
	if len(pages) < 2 {
		t.Errorf("expected at least 2 pages after sync, got %d", len(pages))
	}
}

func TestFileNameToTitle(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"项目/子项目.md", "项目/子项目"},
		{"2024-01-15.md", "2024-01-15"},
		{"index.md", "index"},
	}

	for _, tt := range tests {
		got := FileNameToTitle(tt.input)
		if got != tt.expected {
			t.Errorf("FileNameToTitle(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestIsJournalFile(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"journals/2024-01-15.md", true},
		{"2024-01-15.md", true},
		{"2024_01_15.md", true},
		{"pages/note.md", false},
		{"项目/子项目.md", false},
	}

	for _, tt := range tests {
		got := isJournalFile(tt.input)
		if got != tt.expected {
			t.Errorf("isJournalFile(%q) = %v, want %v", tt.input, got, tt.expected)
		}
	}
}
