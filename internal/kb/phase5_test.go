package kb

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// newTestStore 创建测试用 Store
func newPhase5TestStore(t *testing.T) *Store {
	t.Helper()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test_kb.db")
	store, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

// indexTestPage 索引测试页面
func indexTestPage(t *testing.T, store *Store, title, content string) *Page {
	t.Helper()
	linker := NewLinker(store)
	page, err := linker.IndexPage(content, title+".md")
	if err != nil {
		t.Fatalf("IndexPage %s failed: %v", title, err)
	}
	return page
}

// ==================== FTS5 测试 ====================

func TestFTSSearch(t *testing.T) {
	store := newPhase5TestStore(t)

	// 索引测试页面
	indexTestPage(t, store, "Go语言", "# Go语言\n\nGo 是一门编译型语言，由 Google 开发。\n\n## 特点\n\n- 简洁\n- 高效\n- 并发支持\n")
	indexTestPage(t, store, "Rust语言", "# Rust语言\n\nRust 是一门系统编程语言，注重安全和性能。\n\n## 特点\n\n- 内存安全\n- 零成本抽象\n")
	indexTestPage(t, store, "Python入门", "# Python入门\n\nPython 是一门解释型语言，适合快速开发。\n\n## 应用\n\n- Web开发\n- 数据分析\n")

	// 重建 FTS 索引
	if err := store.RebuildFTS(); err != nil {
		t.Fatalf("RebuildFTS failed: %v", err)
	}

	// 测试搜索 "语言"
	results, err := store.SearchFTS("语言", 10)
	if err != nil {
		t.Fatalf("SearchFTS failed: %v", err)
	}
	if len(results) == 0 {
		t.Error("expected results for '语言', got 0")
	}

	// 测试搜索 "Go"
	results, err = store.SearchFTS("Go", 10)
	if err != nil {
		t.Fatalf("SearchFTS failed: %v", err)
	}
	found := false
	for _, r := range results {
		if strings.Contains(r.Title, "Go") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected to find Go page")
	}

	// 测试空查询
	results, err = store.SearchFTS("", 10)
	if err != nil {
		t.Errorf("SearchFTS empty query failed: %v", err)
	}
}

func TestSanitizeFTSQuery(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"hello", "hello*"},
		{"hello world", "hello* world*"},
		{"", ""},
		{"\"quoted\"", "quoted*"},
		{"(test)", "test*"},
		{"a:b", "ab*"},
	}
	for _, tt := range tests {
		got := sanitizeFTSQuery(tt.input)
		if got != tt.expected {
			t.Errorf("sanitizeFTSQuery(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

// ==================== 日记测试 ====================

func TestJournal(t *testing.T) {
	store := newPhase5TestStore(t)
	journal := NewJournal(store)

	// 测试标题生成
	today := journal.TodayTitle()
	expectedFormat := time.Now().Format("2006_01_02")
	if today != expectedFormat {
		t.Errorf("TodayTitle() = %q, want %q", today, expectedFormat)
	}

	// 测试标题解析
	parsed, ok := journal.ParseTitle("2026_01_15")
	if !ok {
		t.Error("ParseTitle failed for valid format")
	}
	if parsed.Format("2006_01_02") != "2026_01_15" {
		t.Errorf("parsed date wrong: %v", parsed)
	}

	// 测试无效标题
	_, ok = journal.ParseTitle("invalid_title")
	if ok {
		t.Error("ParseTitle should fail for invalid format")
	}

	// 测试 IsJournalTitle
	if !journal.IsJournalTitle("2026_01_15") {
		t.Error("IsJournalTitle should return true for valid format")
	}
	if journal.IsJournalTitle("not a journal") {
		t.Error("IsJournalTitle should return false for invalid format")
	}

	// 测试 EnsureToday
	page, err := journal.EnsureToday()
	if err != nil {
		t.Fatalf("EnsureToday failed: %v", err)
	}
	if page == nil {
		t.Fatal("EnsureToday returned nil page")
	}
	if !page.IsJournal {
		t.Error("page should be journal")
	}
	if page.Namespace != "journals" {
		t.Errorf("expected namespace 'journals', got '%s'", page.Namespace)
	}

	// 再次调用 EnsureToday 应返回同一页面
	page2, err := journal.EnsureToday()
	if err != nil {
		t.Fatalf("EnsureToday second call failed: %v", err)
	}
	if page2.ID != page.ID {
		t.Error("EnsureToday should return same page on second call")
	}

	// 测试 EnsureDate
	date := time.Date(2026, 1, 15, 0, 0, 0, 0, time.Local)
	page3, err := journal.EnsureDate(date)
	if err != nil {
		t.Fatalf("EnsureDate failed: %v", err)
	}
	if page3.Title != "2026_01_15" {
		t.Errorf("expected title '2026_01_15', got '%s'", page3.Title)
	}

	// 测试 ListRecent
	pages, err := journal.ListRecent(10)
	if err != nil {
		t.Fatalf("ListRecent failed: %v", err)
	}
	if len(pages) < 2 {
		t.Errorf("expected at least 2 journal pages, got %d", len(pages))
	}

	// 测试 FormatHumanDate
	human := journal.FormatHumanDate(journal.TodayTitle())
	if human != "Today" {
		t.Errorf("expected 'Today', got '%s'", human)
	}

	// 测试 DefaultTemplate
	tmpl := journal.DefaultTemplate()
	if !strings.Contains(tmpl, "# "+journal.TodayTitle()) {
		t.Error("DefaultTemplate should contain today's title")
	}
	if !strings.Contains(tmpl, "TODO") {
		t.Error("DefaultTemplate should contain TODO")
	}
}

// ==================== 任务管理测试 ====================

func TestTaskManager(t *testing.T) {
	store := newPhase5TestStore(t)
	tm := NewTaskManager(store)

	// 索引包含任务的页面
	content := `# 项目计划

- TODO 完成需求分析
- DOING 编写代码
- DONE 搭建环境
- LATER 优化性能
- NOW 修复紧急 bug

## 其他内容

- 普通列表项
`
	page := indexTestPage(t, store, "项目计划", content)

	// 测试 ExtractTaskStatus
	tests := []struct {
		content string
		status  string
	}{
		{"- TODO 任务", "TODO"},
		{"- DOING 任务", "DOING"},
		{"- DONE 任务", "DONE"},
		{"- LATER 任务", "LATER"},
		{"- NOW 任务", "NOW"},
		{"- 普通项", ""},
		{"TODO 没有列表符号", "TODO"},
	}
	for _, tt := range tests {
		got := ExtractTaskStatus(tt.content)
		if got != tt.status {
			t.Errorf("ExtractTaskStatus(%q) = %q, want %q", tt.content, got, tt.status)
		}
	}

	// 测试 IsTaskBlock
	if !IsTaskBlock("- TODO 任务") {
		t.Error("IsTaskBlock should return true for TODO")
	}
	if IsTaskBlock("普通文本") {
		t.Error("IsTaskBlock should return false for normal text")
	}

	// 测试 AllTasks
	tasks, err := tm.AllTasks()
	if err != nil {
		t.Fatalf("AllTasks failed: %v", err)
	}
	if len(tasks) != 5 {
		t.Errorf("expected 5 tasks, got %d", len(tasks))
	}

	// 测试 TasksByStatus
	todoTasks, err := tm.TasksByStatus("TODO")
	if err != nil {
		t.Fatalf("TasksByStatus failed: %v", err)
	}
	if len(todoTasks) != 1 {
		t.Errorf("expected 1 TODO task, got %d", len(todoTasks))
	}

	doneTasks, err := tm.TasksByStatus("DONE")
	if err != nil {
		t.Fatalf("TasksByStatus DONE failed: %v", err)
	}
	if len(doneTasks) != 1 {
		t.Errorf("expected 1 DONE task, got %d", len(doneTasks))
	}

	// 测试 TasksByPage
	pageTasks, err := tm.TasksByPage(page.ID)
	if err != nil {
		t.Fatalf("TasksByPage failed: %v", err)
	}
	if len(pageTasks) != 5 {
		t.Errorf("expected 5 tasks in page, got %d", len(pageTasks))
	}

	// 测试 Stats
	stats, err := tm.Stats()
	if err != nil {
		t.Fatalf("Stats failed: %v", err)
	}
	if stats.Total != 5 {
		t.Errorf("expected total 5, got %d", stats.Total)
	}
	if stats.Todo != 1 {
		t.Errorf("expected todo 1, got %d", stats.Todo)
	}
	if stats.Done != 1 {
		t.Errorf("expected done 1, got %d", stats.Done)
	}

	// 测试 UpdateStatus
	// 找到 TODO 任务的 block_id
	var todoBlockID string
	for _, task := range tasks {
		if task.Status == "TODO" {
			todoBlockID = task.BlockID
			break
		}
	}
	if todoBlockID == "" {
		t.Fatal("no TODO task found")
	}

	err = tm.UpdateStatus(todoBlockID, "DONE")
	if err != nil {
		t.Fatalf("UpdateStatus failed: %v", err)
	}

	// 验证状态已更新
	block, _ := store.GetBlock(todoBlockID)
	if ExtractTaskStatus(block.Content) != "DONE" {
		t.Errorf("expected status DONE after update, got %s", ExtractTaskStatus(block.Content))
	}

	// 测试无效状态
	err = tm.UpdateStatus(todoBlockID, "INVALID")
	if err == nil {
		t.Error("UpdateStatus should fail for invalid status")
	}
}

// ==================== 块嵌入测试 ====================

func TestEmbed(t *testing.T) {
	store := newPhase5TestStore(t)
	embed := NewEmbed(store)

	// 创建源页面
	content1 := `# 源页面

这是一个重要的内容块。

## 子内容

更多细节。
`
	page1 := indexTestPage(t, store, "源页面", content1)

	// 获取一个块的 ID
	blocks, _ := store.GetBlocks(page1.ID)
	if len(blocks) == 0 {
		t.Fatal("no blocks in page")
	}
	targetBlockID := blocks[0].ID

	// 创建包含嵌入的页面
	content2 := `# 引用页面

这里引用了其他内容：

{{embed ((` + targetBlockID + `))}}

结束。
`
	page2 := indexTestPage(t, store, "引用页面", content2)

	_ = page2

	// 测试 HasEmbed
	if !embed.HasEmbed(content2) {
		t.Error("HasEmbed should return true")
	}
	if embed.HasEmbed("普通内容") {
		t.Error("HasEmbed should return false for normal content")
	}

	// 测试 FindEmbeds
	ids := embed.FindEmbeds(content2)
	if len(ids) != 1 {
		t.Errorf("expected 1 embed id, got %d", len(ids))
	}
	if len(ids) > 0 && ids[0] != targetBlockID {
		t.Errorf("expected embed id %s, got %s", targetBlockID, ids[0])
	}

	// 测试 ExpandBlock
	expanded, err := embed.ExpandBlock(targetBlockID)
	if err != nil {
		t.Fatalf("ExpandBlock failed: %v", err)
	}
	if !strings.Contains(expanded, "源页面") {
		t.Error("expanded content should contain source text")
	}

	// 测试 GetEmbedTree
	tree := embed.GetEmbedTree(targetBlockID)
	if tree.BlockID != targetBlockID {
		t.Error("tree root should be target block")
	}

	// 测试不存在的块
	_, err = embed.ExpandBlock("nonexistent-id")
	if err != nil {
		t.Errorf("ExpandBlock for nonexistent should not error: %v", err)
	}
}

func TestEmbedCircular(t *testing.T) {
	store := newPhase5TestStore(t)
	embed := NewEmbed(store)

	// 创建两个互相引用的块
	// 先创建页面获取 block ID
	page1 := indexTestPage(t, store, "循环A", "# 循环A\n\n内容")
	blocks1, _ := store.GetBlocks(page1.ID)
	block1ID := blocks1[0].ID

	page2 := indexTestPage(t, store, "循环B", "# 循环B\n\n{{embed ((" + block1ID + "))}}")
	blocks2, _ := store.GetBlocks(page2.ID)
	block2ID := blocks2[0].ID

	// 更新 block1 引用 block2，形成循环
	store.UpdateBlockContent(block1ID, "# 循环A\n\n{{embed (("+block2ID+"))}}")

	// 展开应该检测到循环
	expanded, err := embed.ExpandBlock(block1ID)
	if err != nil {
		t.Fatalf("ExpandBlock failed: %v", err)
	}
	// 应该包含循环提示
	if !strings.Contains(expanded, "circular") && !strings.Contains(expanded, "循环A") {
		t.Error("should handle circular reference")
	}
}

// ==================== 模板系统测试 ====================

func TestTemplate(t *testing.T) {
	store := newPhase5TestStore(t)
	tmpl := NewTemplate(store)

	// 创建模板页面
	templateContent := `# {{page}}

日期：{{date}}

## 今日任务

- TODO 任务1
- TODO 任务2

## 备注

$1
`
	linker := NewLinker(store)
	templatePage, err := linker.IndexPage(templateContent, "templates/Meeting.md")
	if err != nil {
		t.Fatalf("create template page failed: %v", err)
	}

	// 测试 IsTemplatePage
	if !tmpl.IsTemplatePage(templatePage) {
		t.Error("page should be a template")
	}

	// 测试 ListTemplates
	templates, err := tmpl.ListTemplates()
	if err != nil {
		t.Fatalf("ListTemplates failed: %v", err)
	}
	if len(templates) == 0 {
		t.Error("expected at least 1 template")
	}

	// 测试 GetTemplate
	content, err := tmpl.GetTemplate("templates/Meeting")
	if err != nil {
		t.Fatalf("GetTemplate failed: %v", err)
	}
	if !strings.Contains(content, "{{page}}") {
		t.Error("template content should contain {{page}}")
	}

	// 测试 Render
	rendered, err := tmpl.Render(content, TemplateVar{
		PageTitle: "周会",
		Now:       time.Date(2026, 1, 15, 10, 0, 0, 0, time.Local),
	})
	if err != nil {
		t.Fatalf("Render failed: %v", err)
	}
	if !strings.Contains(rendered, "周会") {
		t.Error("rendered should contain page title")
	}
	if !strings.Contains(rendered, "2026-01-15") {
		t.Error("rendered should contain date")
	}

	// 测试 ApplyTemplate
	applied, err := tmpl.ApplyTemplate("templates/Meeting", "测试会议", []string{"这是备注内容"})
	if err != nil {
		t.Fatalf("ApplyTemplate failed: %v", err)
	}
	if !strings.Contains(applied, "测试会议") {
		t.Error("applied should contain page title")
	}
	if !strings.Contains(applied, "这是备注内容") {
		t.Error("applied should contain arg")
	}

	// 测试不存在的模板
	_, err = tmpl.GetTemplate("nonexistent")
	if err == nil {
		t.Error("GetTemplate should fail for nonexistent template")
	}
}

func TestTemplateBuiltinVars(t *testing.T) {
	store := newPhase5TestStore(t)
	tmpl := NewTemplate(store)

	now := time.Date(2026, 1, 15, 10, 30, 0, 0, time.Local)
	vars := TemplateVar{PageTitle: "测试页", Now: now}

	tests := []struct {
		template string
		expected string
	}{
		{"{{today}}", "2026_01_15"},
		{"{{yesterday}}", "2026_01_14"},
		{"{{tomorrow}}", "2026_01_16"},
		{"{{date}}", "2026-01-15"},
		{"{{time}}", "10:30"},
		{"{{page}}", "测试页"},
		{"{{current_page}}", "测试页"},
	}

	for _, tt := range tests {
		result := tmpl.replaceBuiltinVars(tt.template, vars)
		if result != tt.expected {
			t.Errorf("replaceBuiltinVars(%q) = %q, want %q", tt.template, result, tt.expected)
		}
	}
}

// ==================== 版本历史测试 ====================

func TestVersion(t *testing.T) {
	store := newPhase5TestStore(t)
	version := NewVersion(store)

	// 创建页面
	page := indexTestPage(t, store, "版本测试", "# 版本测试\n\n第一版内容")

	// 保存版本
	err := version.SaveVersion(page.ID, "# 版本测试\n\n第一版内容")
	if err != nil {
		t.Fatalf("SaveVersion failed: %v", err)
	}

	// 保存第二版
	err = version.SaveVersion(page.ID, "# 版本测试\n\n第二版内容，修改了")
	if err != nil {
		t.Fatalf("SaveVersion v2 failed: %v", err)
	}

	// 保存相同内容（应该跳过）
	err = version.SaveVersion(page.ID, "# 版本测试\n\n第二版内容，修改了")
	if err != nil {
		t.Fatalf("SaveVersion duplicate failed: %v", err)
	}

	// 测试 ListVersions
	versions, err := version.ListVersions(page.ID)
	if err != nil {
		t.Fatalf("ListVersions failed: %v", err)
	}
	if len(versions) != 2 {
		t.Errorf("expected 2 versions, got %d", len(versions))
	}

	// 测试 GetVersion
	v1, err := version.GetVersion(page.ID, 1)
	if err != nil {
		t.Fatalf("GetVersion failed: %v", err)
	}
	if v1 == nil {
		t.Fatal("version 1 not found")
	}
	if !strings.Contains(v1.Content, "第一版") {
		t.Error("version 1 should contain '第一版'")
	}

	// 测试 Diff
	diff, err := version.Diff(page.ID, 1, 2)
	if err != nil {
		t.Fatalf("Diff failed: %v", err)
	}
	if diff.FromVersion != 1 || diff.ToVersion != 2 {
		t.Errorf("diff versions wrong: from=%d, to=%d", diff.FromVersion, diff.ToVersion)
	}
	if len(diff.Lines) == 0 {
		t.Error("expected diff lines")
	}

	// 验证 diff 包含 added 和 removed
	hasAdded := false
	hasRemoved := false
	for _, line := range diff.Lines {
		if line.Type == "added" {
			hasAdded = true
		}
		if line.Type == "removed" {
			hasRemoved = true
		}
	}
	if !hasAdded {
		t.Error("diff should have added lines")
	}
	if !hasRemoved {
		t.Error("diff should have removed lines")
	}

	// 测试 Rollback
	content, err := version.Rollback(page.ID, 1)
	if err != nil {
		t.Fatalf("Rollback failed: %v", err)
	}
	if !strings.Contains(content, "第一版") {
		t.Error("rollback should return v1 content")
	}

	// 测试不存在的版本
	_, err = version.GetVersion(page.ID, 999)
	if err != nil {
		t.Errorf("GetVersion nonexistent should not error: %v", err)
	}
}

// ==================== 查询 DSL 测试 ====================

func TestQueryEngine(t *testing.T) {
	store := newPhase5TestStore(t)
	qe := NewQueryEngine(store)

	// 索引测试数据
	indexTestPage(t, store, "Go语言", "# Go语言\n\nGo 是 [[编程语言]]。\n\n- TODO 学习 Go\n- #编程 #语言\n")
	indexTestPage(t, store, "Rust语言", "# Rust语言\n\nRust 是 [[编程语言]]。\n\n- DOING 学习 Rust\n- #编程 #语言\n")
	indexTestPage(t, store, "编程语言", "# 编程语言\n\n编程语言分类。\n\n- DONE 整理笔记\n")

	// 测试页面引用查询
	expr, err := qe.Parse("[[编程语言]]")
	if err != nil {
		t.Fatalf("Parse page ref failed: %v", err)
	}
	result, err := qe.Execute(expr)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if len(result.Blocks) == 0 {
		t.Error("expected blocks referencing 编程语言")
	}

	// 测试标签查询
	expr, err = qe.Parse("#编程")
	if err != nil {
		t.Fatalf("Parse tag failed: %v", err)
	}
	result, err = qe.Execute(expr)
	if err != nil {
		t.Fatalf("Execute tag failed: %v", err)
	}
	if len(result.Blocks) == 0 {
		t.Error("expected blocks with #编程 tag")
	}

	// 测试任务查询
	expr, err = qe.Parse("(task TODO)")
	if err != nil {
		t.Fatalf("Parse task failed: %v", err)
	}
	result, err = qe.Execute(expr)
	if err != nil {
		t.Fatalf("Execute task failed: %v", err)
	}
	if len(result.Blocks) == 0 {
		t.Error("expected TODO tasks")
	}

	// 测试 AND 查询
	expr, err = qe.Parse("(and [[编程语言]] (task TODO))")
	if err != nil {
		t.Fatalf("Parse AND failed: %v", err)
	}
	result, err = qe.Execute(expr)
	if err != nil {
		t.Fatalf("Execute AND failed: %v", err)
	}

	// 测试 OR 查询
	expr, err = qe.Parse("(or (task TODO) (task DONE))")
	if err != nil {
		t.Fatalf("Parse OR failed: %v", err)
	}
	result, err = qe.Execute(expr)
	if err != nil {
		t.Fatalf("Execute OR failed: %v", err)
	}
	if len(result.Blocks) < 2 {
		t.Errorf("expected at least 2 tasks (TODO+DONE), got %d", len(result.Blocks))
	}

	// 测试 NOT 查询
	expr, err = qe.Parse("(not (task DONE))")
	if err != nil {
		t.Fatalf("Parse NOT failed: %v", err)
	}
	result, err = qe.Execute(expr)
	if err != nil {
		t.Fatalf("Execute NOT failed: %v", err)
	}

	// 测试 {{query ...}} 包裹
	expr, err = qe.Parse("{{query (task TODO)}}")
	if err != nil {
		t.Fatalf("Parse wrapped query failed: %v", err)
	}

	// 测试空查询
	_, err = qe.Parse("")
	if err == nil {
		t.Error("Parse should fail for empty query")
	}

	// 测试无效语法
	_, err = qe.Parse("(invalid op)")
	if err == nil {
		t.Error("Parse should fail for invalid operator")
	}
}

// TestQueryEngineAdvanced 测试新增的 DSL 谓词和聚合
func TestQueryEngineAdvanced(t *testing.T) {
	store := newPhase5TestStore(t)
	qe := NewQueryEngine(store)

	// 索引测试数据
	indexTestPage(t, store, "Go语言", "# Go语言\n\nGo 是 [[编程语言]]。\n\n- TODO 学习 Go\n- #编程 #语言\n")
	indexTestPage(t, store, "Rust语言", "# Rust语言\n\nRust 是 [[编程语言]]。\n\n- DOING 学习 Rust\n- #编程 #语言\n")
	indexTestPage(t, store, "编程语言", "# 编程语言\n\n编程语言分类。\n\n- DONE 整理笔记\n")
	indexTestPage(t, store, "孤立页面", "# 孤立页面\n\n这个页面没有链接。\n")

	// 测试 content 谓词
	expr, err := qe.Parse(`(content "学习")`)
	if err != nil {
		t.Fatalf("Parse content failed: %v", err)
	}
	result, err := qe.Execute(expr)
	if err != nil {
		t.Fatalf("Execute content failed: %v", err)
	}
	if len(result.Blocks) == 0 {
		t.Error("expected blocks containing '学习'")
	}

	// 测试 orphan 谓词
	expr, err = qe.Parse("(orphan)")
	if err != nil {
		t.Fatalf("Parse orphan failed: %v", err)
	}
	result, err = qe.Execute(expr)
	if err != nil {
		t.Fatalf("Execute orphan failed: %v", err)
	}
	if len(result.Pages) == 0 {
		t.Error("expected orphan pages")
	}

	// 测试 hub 谓词
	expr, err = qe.Parse("(hub 1)")
	if err != nil {
		t.Fatalf("Parse hub failed: %v", err)
	}
	result, err = qe.Execute(expr)
	if err != nil {
		t.Fatalf("Execute hub failed: %v", err)
	}

	// 测试 created-in 谓词
	expr, err = qe.Parse("(created-in 7)")
	if err != nil {
		t.Fatalf("Parse created-in failed: %v", err)
	}
	result, err = qe.Execute(expr)
	if err != nil {
		t.Fatalf("Execute created-in failed: %v", err)
	}

	// 测试 updated-in 谓词
	expr, err = qe.Parse("(updated-in 7)")
	if err != nil {
		t.Fatalf("Parse updated-in failed: %v", err)
	}
	result, err = qe.Execute(expr)
	if err != nil {
		t.Fatalf("Execute updated-in failed: %v", err)
	}

	// 测试聚合
	expr, err = qe.Parse("(or (task TODO) (task DOING) (task DONE))")
	if err != nil {
		t.Fatalf("Parse tasks for aggregate failed: %v", err)
	}
	aggResult, err := qe.ExecuteWithAggregate(expr)
	if err != nil {
		t.Fatalf("ExecuteWithAggregate failed: %v", err)
	}
	if aggResult.TotalBlocks == 0 {
		t.Error("expected aggregated blocks")
	}
	if len(aggResult.ByStatus) == 0 {
		t.Error("expected status aggregation")
	}

	// 测试 page-type 谓词
	expr, err = qe.Parse("(page-type normal)")
	if err != nil {
		t.Fatalf("Parse page-type failed: %v", err)
	}
	result, err = qe.Execute(expr)
	if err != nil {
		t.Fatalf("Execute page-type failed: %v", err)
	}

	// 测试复合查询：content + task
	expr, err = qe.Parse(`(and (content "Go") (task TODO))`)
	if err != nil {
		t.Fatalf("Parse compound query failed: %v", err)
	}
	result, err = qe.Execute(expr)
	if err != nil {
		t.Fatalf("Execute compound query failed: %v", err)
	}
}

func TestParseRelativeDate(t *testing.T) {
	tests := []struct {
		input    string
		shouldErr bool
	}{
		{"-7d", false},
		{"+7d", false},
		{"-2w", false},
		{"+1m", false},
		{"2026-01-15", false},
		{"2026_01_15", false},
		{"invalid", true},
	}

	for _, tt := range tests {
		_, err := parseRelativeDate(tt.input)
		if tt.shouldErr && err == nil {
			t.Errorf("parseRelativeDate(%q) should error", tt.input)
		}
		if !tt.shouldErr && err != nil {
			t.Errorf("parseRelativeDate(%q) should not error: %v", tt.input, err)
		}
	}
}

// ==================== 导入测试 ====================

func TestImporter(t *testing.T) {
	store := newPhase5TestStore(t)
	linker := NewLinker(store)
	importer := NewImporter(store, linker)

	// 创建测试目录结构
	tmpDir := t.TempDir()

	// 创建 Markdown 文件
	files := map[string]string{
		"Go语言.md":             "---\ntitle: Go语言\ntags: [编程, Go]\n---\n\n# Go语言\n\nGo 是一门编程语言。\n\n- [[Rust语言]]\n",
		"Rust语言.md":           "# Rust语言\n\nRust 是系统编程语言。\n\n- [[Go语言]]\n",
		"notes/笔记.md":         "# 笔记\n\n这是笔记内容。\n",
		"journals/2026_01_15.md": "# 2026_01_15\n\n今日日记。\n",
		"README.md":            "# README\n\n项目说明。\n",
	}

	for path, content := range files {
		fullPath := filepath.Join(tmpDir, path)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			t.Fatalf("MkdirAll failed: %v", err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
			t.Fatalf("WriteFile failed: %v", err)
		}
	}

	// 执行导入
	result, err := importer.ImportDirectory(tmpDir)
	if err != nil {
		t.Fatalf("ImportDirectory failed: %v", err)
	}

	if result.TotalFiles != 5 {
		t.Errorf("expected 5 total files, got %d", result.TotalFiles)
	}
	if result.Imported != 5 {
		t.Errorf("expected 5 imported, got %d", result.Imported)
	}

	// 验证页面已导入
	page, err := store.GetPageByTitle("Go语言")
	if err != nil {
		t.Fatalf("GetPageByTitle failed: %v", err)
	}
	if page == nil {
		t.Fatal("Go语言 page not found after import")
	}

	// 验证 frontmatter 解析
	if len(page.Tags) < 2 {
		t.Errorf("expected at least 2 tags, got %d", len(page.Tags))
	}

	// 验证日记识别
	journalPage, _ := store.GetPageByTitle("2026_01_15")
	if journalPage == nil {
		t.Fatal("journal page not found")
	}
	if !journalPage.IsJournal {
		t.Error("2026_01_15 should be marked as journal")
	}

	// 验证命名空间
	notesPage, _ := store.GetPageByTitle("笔记")
	if notesPage == nil {
		t.Fatal("笔记 page not found")
	}
	if notesPage.Namespace != "notes" {
		t.Errorf("expected namespace 'notes', got '%s'", notesPage.Namespace)
	}
}

func TestExtractPageTitle(t *testing.T) {
	tests := []struct {
		content string
		relPath string
		expect  string
	}{
		{
			"---\ntitle: 自定义标题\n---\n\n# 内容",
			"test.md",
			"自定义标题",
		},
		{
			"# Markdown 标题\n\n内容",
			"test.md",
			"Markdown 标题",
		},
		{
			"无标题内容",
			"my_file.md",
			"my file",
		},
		{
			"无标题内容",
			"my-file-name.md",
			"my file name",
		},
	}

	for _, tt := range tests {
		got := extractPageTitle(tt.content, tt.relPath)
		if got != tt.expect {
			t.Errorf("extractPageTitle() = %q, want %q", got, tt.expect)
		}
	}
}

func TestExtractFrontmatter(t *testing.T) {
	content := `---
title: 测试
tags: [Go, Rust, Python]
category: tech
---

# 内容`

	tags, props := extractFrontmatter(content)
	if len(tags) != 3 {
		t.Errorf("expected 3 tags, got %d", len(tags))
	}
	if props["title"] != "测试" {
		t.Errorf("expected title '测试', got %v", props["title"])
	}
	if props["category"] != "tech" {
		t.Errorf("expected category 'tech', got %v", props["category"])
	}
}

// ==================== 集成测试 ====================

func TestPhase5Integration(t *testing.T) {
	store := newPhase5TestStore(t)

	// 初始化所有引擎
	journal := NewJournal(store)
	tm := NewTaskManager(store)
	embed := NewEmbed(store)
	tmpl := NewTemplate(store)
	version := NewVersion(store)
	qe := NewQueryEngine(store)

	// 1. 创建今日日记
	journalPage, err := journal.EnsureToday()
	if err != nil {
		t.Fatalf("journal.EnsureToday failed: %v", err)
	}

	// 2. 在日记中添加任务
	linker := NewLinker(store)
	updatedContent := "# " + journalPage.Title + "\n\n- TODO 完成阶段5测试\n- DOING 编写测试用例\n- DONE 创建引擎\n"
	linker.IndexPage(updatedContent, journalPage.Title+".md")

	// 3. 查询任务
	tasks, err := tm.AllTasks()
	if err != nil {
		t.Fatalf("tm.AllTasks failed: %v", err)
	}
	if len(tasks) < 3 {
		t.Errorf("expected at least 3 tasks, got %d", len(tasks))
	}

	// 4. 使用查询 DSL
	expr, err := qe.Parse("(task TODO)")
	if err != nil {
		t.Fatalf("qe.Parse failed: %v", err)
	}
	result, err := qe.Execute(expr)
	if err != nil {
		t.Fatalf("qe.Execute failed: %v", err)
	}
	if len(result.Blocks) == 0 {
		t.Error("query should find TODO tasks")
	}

	// 5. 保存版本
	page, _ := store.GetPageByTitle(journalPage.Title)
	version.SaveVersion(page.ID, updatedContent)

	// 6. 修改内容并保存新版本
	newContent := "# " + journalPage.Title + "\n\n- DONE 完成阶段5测试\n- DOING 编写测试用例\n- DONE 创建引擎\n"
	linker.IndexPage(newContent, journalPage.Title+".md")
	version.SaveVersion(page.ID, newContent)

	// 7. 查看版本历史
	versions, err := version.ListVersions(page.ID)
	if err != nil {
		t.Fatalf("version.ListVersions failed: %v", err)
	}
	if len(versions) < 2 {
		t.Errorf("expected at least 2 versions, got %d", len(versions))
	}

	// 8. 重建 FTS 索引
	err = store.RebuildFTS()
	if err != nil {
		t.Fatalf("store.RebuildFTS failed: %v", err)
	}

	// 9. FTS 搜索
	results, err := store.SearchFTS("阶段5", 10)
	if err != nil {
		t.Fatalf("store.SearchFTS failed: %v", err)
	}
	if len(results) == 0 {
		t.Error("FTS search should find results")
	}

	_ = embed
	_ = tmpl
}
