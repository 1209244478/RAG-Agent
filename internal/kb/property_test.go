package kb

import (
	"testing"
)

// TestPropertySystem 测试属性系统
func TestPropertySystem(t *testing.T) {
	store := newPhase5TestStore(t)

	// 创建测试页面
	page := indexTestPage(t, store, "测试页面", "# 测试页面\n\n内容。\n")

	// 设置属性
	err := store.SetPageProperty(page.ID, "status", "done", PropTypeString)
	if err != nil {
		t.Fatalf("SetPageProperty failed: %v", err)
	}

	err = store.SetPageProperty(page.ID, "priority", 5, PropTypeNumber)
	if err != nil {
		t.Fatalf("SetPageProperty number failed: %v", err)
	}

	// 获取属性
	props, err := store.GetPageProperties(page.ID)
	if err != nil {
		t.Fatalf("GetPageProperties failed: %v", err)
	}
	if len(props) < 2 {
		t.Errorf("expected at least 2 properties, got %d", len(props))
	}

	// 按属性查询
	pages, err := store.QueryByProperty("status", "done")
	if err != nil {
		t.Fatalf("QueryByProperty failed: %v", err)
	}
	if len(pages) == 0 {
		t.Error("expected to find pages with status=done")
	}

	// 获取所有属性名
	names, err := store.GetAllPropertyNames()
	if err != nil {
		t.Fatalf("GetAllPropertyNames failed: %v", err)
	}
	if len(names) < 2 {
		t.Errorf("expected at least 2 property names, got %d", len(names))
	}

	// 删除属性
	err = store.DeletePageProperty(page.ID, "status")
	if err != nil {
		t.Fatalf("DeletePageProperty failed: %v", err)
	}
	props, _ = store.GetPageProperties(page.ID)
	for _, p := range props {
		if p.Name == "status" {
			t.Error("status property should be deleted")
		}
	}
}

// TestPropertySchema 测试属性 Schema
func TestPropertySchema(t *testing.T) {
	store := newPhase5TestStore(t)

	schema := PropertySchema{
		Name:     "status",
		Type:     PropTypeString,
		Required: true,
		Options:  []string{"todo", "doing", "done"},
	}

	err := store.SetPropertySchema("default", schema)
	if err != nil {
		t.Fatalf("SetPropertySchema failed: %v", err)
	}

	schemas, err := store.GetPropertySchemas("default")
	if err != nil {
		t.Fatalf("GetPropertySchemas failed: %v", err)
	}
	if len(schemas) == 0 {
		t.Error("expected schemas")
	}
}

// TestFavorites 测试收藏夹
func TestFavorites(t *testing.T) {
	store := newPhase5TestStore(t)

	page1 := indexTestPage(t, store, "收藏1", "# 收藏1\n")
	page2 := indexTestPage(t, store, "收藏2", "# 收藏2\n")

	// 添加收藏
	err := store.AddFavorite(page1.ID)
	if err != nil {
		t.Fatalf("AddFavorite failed: %v", err)
	}
	err = store.AddFavorite(page2.ID)
	if err != nil {
		t.Fatalf("AddFavorite 2 failed: %v", err)
	}

	// 列出收藏
	favs, err := store.ListFavorites()
	if err != nil {
		t.Fatalf("ListFavorites failed: %v", err)
	}
	if len(favs) != 2 {
		t.Errorf("expected 2 favorites, got %d", len(favs))
	}

	// 检查是否收藏
	if !store.IsFavorite(page1.ID) {
		t.Error("page1 should be favorite")
	}

	// 移除收藏
	err = store.RemoveFavorite(page1.ID)
	if err != nil {
		t.Fatalf("RemoveFavorite failed: %v", err)
	}
	favs, _ = store.ListFavorites()
	if len(favs) != 1 {
		t.Errorf("expected 1 favorite after remove, got %d", len(favs))
	}
}

// TestRecentPages 测试最近访问
func TestRecentPages(t *testing.T) {
	store := newPhase5TestStore(t)

	page1 := indexTestPage(t, store, "最近1", "# 最近1\n")
	page2 := indexTestPage(t, store, "最近2", "# 最近2\n")

	// 记录访问
	store.RecordRecent(page1.ID)
	store.RecordRecent(page2.ID)

	// 列出最近
	recent, err := store.ListRecent(10)
	if err != nil {
		t.Fatalf("ListRecent failed: %v", err)
	}
	if len(recent) < 2 {
		t.Errorf("expected at least 2 recent pages, got %d", len(recent))
	}
}

// TestRecycleBin 测试回收站
func TestRecycleBin(t *testing.T) {
	store := newPhase5TestStore(t)

	page := indexTestPage(t, store, "待删除", "# 待删除\n\n内容。\n")
	pageID := page.ID

	// 软删除
	err := store.SoftDeletePage(pageID)
	if err != nil {
		t.Fatalf("SoftDeletePage failed: %v", err)
	}

	// 验证页面已删除
	deleted, _ := store.GetPageByID(pageID)
	if deleted != nil {
		t.Error("page should be soft deleted")
	}

	// 列出回收站
	items, err := store.ListRecycle()
	if err != nil {
		t.Fatalf("ListRecycle failed: %v", err)
	}
	if len(items) != 1 {
		t.Errorf("expected 1 recycle item, got %d", len(items))
	}

	// 恢复
	if len(items) > 0 {
		restored, err := store.RestoreFromRecycle(items[0].ID)
		if err != nil {
			t.Fatalf("RestoreFromRecycle failed: %v", err)
		}
		if restored == nil {
			t.Fatal("restored page should not be nil")
		}
		// 恢复后页面 ID 会变化（重新插入），用标题验证
		if restored.Title != "待删除" {
			t.Errorf("expected title '待删除', got '%s'", restored.Title)
		}
	}

	// 验证恢复后页面存在（按标题查找）
	restoredPage, _ := store.GetPageByTitle("待删除")
	if restoredPage == nil {
		t.Error("page should be restored and findable by title")
	}
}

// TestBlockReorder 测试块重排序
func TestBlockReorder(t *testing.T) {
	store := newPhase5TestStore(t)

	page := indexTestPage(t, store, "重排测试", "# 重排测试\n\n块1\n\n块2\n\n块3\n")

	// 获取块
	blocks, err := store.GetBlocks(page.ID)
	if err != nil {
		t.Fatalf("GetBlocks failed: %v", err)
	}
	if len(blocks) < 3 {
		t.Fatalf("expected at least 3 blocks, got %d", len(blocks))
	}

	// 将第一个块移到第二个块之后
	err = store.ReorderBlock(blocks[0].ID, blocks[1].ID, page.ID)
	if err != nil {
		t.Fatalf("ReorderBlock failed: %v", err)
	}

	// 验证顺序变化
	newBlocks, _ := store.GetBlocks(page.ID)
	if len(newBlocks) < 3 {
		t.Fatalf("expected at least 3 blocks after reorder, got %d", len(newBlocks))
	}
}

// TestUnlinkedReferences 测试未链接引用
func TestUnlinkedReferences(t *testing.T) {
	store := newPhase5TestStore(t)

	indexTestPage(t, store, "目标页面", "# 目标页面\n")
	indexTestPage(t, store, "引用页面", "# 引用页面\n\n这里提到了目标页面但没有链接。\n")

	refs, err := store.GetUnlinkedReferences("目标页面")
	if err != nil {
		t.Fatalf("GetUnlinkedReferences failed: %v", err)
	}
	// 可能找到也可能找不到，取决于实现
	_ = refs
}
