package agent

import (
	"path/filepath"
	"testing"
	"time"
)

// ==================== Evaluation 测试 ====================

func TestEvaluate_Success(t *testing.T) {
	ev := Evaluate("完成任务", "CURRENT_TASK_DONE", 5, 20, 30*time.Second, 10, 0, []string{"read", "write"})

	if ev.Outcome != OutcomeSuccess {
		t.Errorf("应为 success, 实际 %s", ev.Outcome)
	}
	if !ev.GoalAchieved {
		t.Error("目标应达成")
	}
	if ev.Score < 0.7 || ev.Score > 1.0 {
		t.Errorf("成功评分应在 0.7-1.0, 实际 %.2f", ev.Score)
	}
	if len(ev.Strengths) == 0 {
		t.Error("应有优势分析")
	}
}

func TestEvaluate_Timeout(t *testing.T) {
	ev := Evaluate("超时任务", "TIMEOUT", 20, 20, 10*time.Minute, 30, 5, []string{})

	if ev.Outcome != OutcomeTimeout {
		t.Errorf("应为 timeout, 实际 %s", ev.Outcome)
	}
	if ev.GoalAchieved {
		t.Error("目标不应达成")
	}
	if ev.RootCause == "" {
		t.Error("应有根因分析")
	}
}

func TestEvaluate_MaxTurns(t *testing.T) {
	ev := Evaluate("轮次耗尽", "MAX_TURNS_EXCEEDED", 80, 80, 5*time.Minute, 50, 2, []string{})

	if ev.Outcome != OutcomeMaxTurns {
		t.Errorf("应为 max_turns, 实际 %s", ev.Outcome)
	}
	if ev.GoalAchieved {
		t.Error("目标不应达成")
	}
}

func TestEvaluate_Aborted(t *testing.T) {
	ev := Evaluate("中止任务", "ABORTED", 3, 20, 5*time.Second, 2, 0, []string{})

	if ev.Outcome != OutcomeAborted {
		t.Errorf("应为 aborted, 实际 %s", ev.Outcome)
	}
}

func TestEvaluate_HighEfficiency(t *testing.T) {
	// 用很少轮次完成应有效率加分
	ev := Evaluate("高效任务", "CURRENT_TASK_DONE", 2, 20, 10*time.Second, 3, 0, []string{})

	foundEfficiency := false
	for _, s := range ev.Strengths {
		if contains(s, "高效") {
			foundEfficiency = true
		}
	}
	if !foundEfficiency {
		t.Error("高效完成应在优势中体现")
	}
}

func TestEvaluate_ZeroErrors(t *testing.T) {
	ev := Evaluate("零错误", "CURRENT_TASK_DONE", 5, 20, 10*time.Second, 10, 0, []string{})

	foundZeroErr := false
	for _, s := range ev.Strengths {
		if contains(s, "零错误") {
			foundZeroErr = true
		}
	}
	if !foundZeroErr {
		t.Error("零错误应在优势中体现")
	}
}

func TestEvaluate_ManyErrors(t *testing.T) {
	ev := Evaluate("多错误", "CURRENT_TASK_DONE", 5, 20, 10*time.Second, 10, 5, []string{})

	foundManyErr := false
	for _, w := range ev.Weaknesses {
		if contains(w, "错误") {
			foundManyErr = true
		}
	}
	if !foundManyErr {
		t.Error("多错误应在劣势中体现")
	}
}

func TestEvaluate_LongDuration(t *testing.T) {
	ev := Evaluate("长耗时", "CURRENT_TASK_DONE", 10, 20, 15*time.Minute, 20, 0, []string{})

	foundLongDur := false
	for _, w := range ev.Weaknesses {
		if contains(w, "耗时") {
			foundLongDur = true
		}
	}
	if !foundLongDur {
		t.Error("长耗时应在劣势中体现")
	}
}

func TestEvaluate_Summary(t *testing.T) {
	ev := Evaluate("测试", "CURRENT_TASK_DONE", 5, 20, 30*time.Second, 10, 0, []string{})
	if ev.Summary == "" {
		t.Error("摘要不应为空")
	}
	if !contains(ev.Summary, "success") {
		t.Errorf("摘要应包含 outcome: %s", ev.Summary)
	}
}

// ==================== normalizeGoal 测试 ====================

func TestNormalizeGoal_Basic(t *testing.T) {
	g := normalizeGoal("Implement User Login Feature")
	if g != "implement user login feature" {
		t.Errorf("归一化不正确: %s", g)
	}
}

func TestNormalizeGoal_Empty(t *testing.T) {
	if normalizeGoal("") != "" {
		t.Error("空字符串应返回空")
	}
}

func TestNormalizeGoal_Truncate(t *testing.T) {
	long := "this is a very long goal that exceeds the eighty character limit and should be truncated to fit within the constraint"
	g := normalizeGoal(long)
	if len(g) > 80 {
		t.Errorf("应截断到 80 字符, 实际 %d", len(g))
	}
}

func TestNormalizeGoal_CompressSpaces(t *testing.T) {
	g := normalizeGoal("multiple   spaces   here")
	if g != "multiple spaces here" {
		t.Errorf("应压缩空白: %s", g)
	}
}

// ==================== ExperienceStore 测试 ====================

func newTestStore(t *testing.T) *ExperienceStore {
	t.Helper()
	dir := t.TempDir()
	store := NewExperienceStore(dir)
	if err := store.Load(); err != nil {
		t.Fatalf("Load 失败: %v", err)
	}
	return store
}

func TestExperienceStore_AddAndList(t *testing.T) {
	store := newTestStore(t)

	exp := Experience{
		TaskID:      "task-1",
		Goal:        "test goal",
		GoalPattern: "test goal",
		Outcome:     OutcomeSuccess,
		Score:       0.9,
		TurnsUsed:   5,
		ToolsUsed:   []string{"read", "write"},
		Timestamp:   time.Now(),
	}

	if err := store.AddExperience(exp); err != nil {
		t.Fatalf("AddExperience 失败: %v", err)
	}

	exps := store.ListExperiences(10)
	if len(exps) != 1 {
		t.Errorf("应有 1 条经验, 实际 %d", len(exps))
	}
	if exps[0].TaskID != "task-1" {
		t.Errorf("TaskID 不匹配: %s", exps[0].TaskID)
	}
}

func TestExperienceStore_Persistence(t *testing.T) {
	dir := t.TempDir()

	// 写入
	store1 := NewExperienceStore(dir)
	store1.Load()
	store1.AddExperience(Experience{
		TaskID: "persist-test",
		Goal:   "persistence",
		Outcome: OutcomeSuccess,
	})

	// 重新加载
	store2 := NewExperienceStore(dir)
	store2.loaded = false // 强制重新加载
	if err := store2.Load(); err != nil {
		t.Fatalf("重新加载失败: %v", err)
	}

	exps := store2.ListExperiences(0)
	if len(exps) != 1 {
		t.Errorf("持久化后应有 1 条经验, 实际 %d", len(exps))
	}
	if exps[0].TaskID != "persist-test" {
		t.Errorf("TaskID 不匹配: %s", exps[0].TaskID)
	}
}

func TestExperienceStore_StrategyCreation(t *testing.T) {
	store := newTestStore(t)

	// 添加成功经验
	store.AddExperience(Experience{
		Goal:        "refactor code",
		GoalPattern: "refactor code",
		Outcome:     OutcomeSuccess,
		TurnsUsed:   8,
		ToolsUsed:   []string{"read", "edit", "test"},
	})

	strats := store.ListStrategies()
	if len(strats) != 1 {
		t.Fatalf("应创建 1 个策略, 实际 %d", len(strats))
	}
	if strats[0].GoalPattern != "refactor code" {
		t.Errorf("GoalPattern 不匹配: %s", strats[0].GoalPattern)
	}
	if strats[0].HitCount != 1 {
		t.Errorf("HitCount 应为 1, 实际 %d", strats[0].HitCount)
	}
	if strats[0].SuccessRate <= 0 {
		t.Error("成功率应大于 0")
	}
}

func TestExperienceStore_StrategyUpdate(t *testing.T) {
	store := newTestStore(t)

	// 添加两次相同模式的经验
	store.AddExperience(Experience{
		Goal:        "fix bug",
		GoalPattern: "fix bug",
		Outcome:     OutcomeSuccess,
		TurnsUsed:   3,
		ToolsUsed:   []string{"read", "edit"},
	})
	store.AddExperience(Experience{
		Goal:        "fix bug again",
		GoalPattern: "fix bug",
		Outcome:     OutcomeSuccess,
		TurnsUsed:   5,
		ToolsUsed:   []string{"read", "edit", "test"},
	})

	strats := store.ListStrategies()
	if len(strats) != 1 {
		t.Fatalf("应只有 1 个策略, 实际 %d", len(strats))
	}
	if strats[0].HitCount != 2 {
		t.Errorf("HitCount 应为 2, 实际 %d", strats[0].HitCount)
	}
}

func TestExperienceStore_FindStrategy_Exact(t *testing.T) {
	store := newTestStore(t)
	store.AddExperience(Experience{
		Goal:        "deploy app",
		GoalPattern: "deploy app",
		Outcome:     OutcomeSuccess,
	})

	// 精确匹配
	s := store.FindStrategy("deploy app")
	if s == nil {
		t.Fatal("应找到策略")
	}
	if s.GoalPattern != "deploy app" {
		t.Errorf("GoalPattern 不匹配: %s", s.GoalPattern)
	}
}

func TestExperienceStore_FindStrategy_NotFound(t *testing.T) {
	store := newTestStore(t)
	s := store.FindStrategy("nonexistent")
	if s != nil {
		t.Error("不应找到策略")
	}
}

func TestExperienceStore_RecommendedTools(t *testing.T) {
	store := newTestStore(t)

	// 添加多条成功经验, 使用相同工具
	store.AddExperience(Experience{
		GoalPattern: "build feature",
		Outcome:     OutcomeSuccess,
		ToolsUsed:   []string{"read", "write", "test"},
	})
	store.AddExperience(Experience{
		GoalPattern: "build feature",
		Outcome:     OutcomeSuccess,
		ToolsUsed:   []string{"read", "write", "test", "lint"},
	})

	s := store.FindStrategy("build feature")
	if s == nil {
		t.Fatal("应找到策略")
	}
	if len(s.RecommendedTools) == 0 {
		t.Error("应有推荐工具")
	}
	// read/write/test 出现两次, 应排在前面
	if len(s.RecommendedTools) > 0 && s.RecommendedTools[0] != "read" && s.RecommendedTools[0] != "write" && s.RecommendedTools[0] != "test" {
		t.Errorf("最常用工具应排前面: %v", s.RecommendedTools)
	}
}

func TestExperienceStore_MedianTurns(t *testing.T) {
	store := newTestStore(t)

	// 添加多条成功经验, 不同轮次
	for _, turns := range []int{4, 6, 8, 10, 12} {
		store.AddExperience(Experience{
			GoalPattern: "median test",
			Outcome:     OutcomeSuccess,
			TurnsUsed:   turns,
		})
	}

	s := store.FindStrategy("median test")
	if s == nil {
		t.Fatal("应找到策略")
	}
	// 中位数应为 8
	if s.RecommendedTurns != 8 {
		t.Errorf("推荐轮次应为中位数 8, 实际 %d", s.RecommendedTurns)
	}
}

// ==================== Reflect 集成测试 ====================

func TestReflect_Success(t *testing.T) {
	store := newTestStore(t)

	r, err := Reflect(store, "完成单元测试", "task-1", "CURRENT_TASK_DONE", 5, 20, 30*time.Second, 10, 0, []string{"read", "write"}, "")
	if err != nil {
		t.Fatalf("Reflect 失败: %v", err)
	}

	if r.Evaluation.Outcome != OutcomeSuccess {
		t.Errorf("应为 success, 实际 %s", r.Evaluation.Outcome)
	}
	if len(r.Lessons) == 0 {
		t.Error("应有经验教训")
	}
	if r.Strategy.GoalPattern == "" {
		t.Error("应有策略")
	}
}

func TestReflect_Failure(t *testing.T) {
	store := newTestStore(t)

	r, err := Reflect(store, "不可能完成的任务", "task-2", "MAX_TURNS_EXCEEDED", 80, 80, 10*time.Minute, 50, 10, []string{}, "")
	if err != nil {
		t.Fatalf("Reflect 失败: %v", err)
	}

	if r.Evaluation.Outcome != OutcomeMaxTurns {
		t.Errorf("应为 max_turns, 实际 %s", r.Evaluation.Outcome)
	}
	if r.Evaluation.RootCause == "" {
		t.Error("失败应有根因")
	}
	// 失败经验应包含 "避免" 字样
	foundAvoid := false
	for _, l := range r.Lessons {
		if contains(l, "避免") || contains(l, "建议") || contains(l, "根因") {
			foundAvoid = true
		}
	}
	if !foundAvoid {
		t.Error("失败经验应包含避免/建议/根因")
	}
}

func TestReflect_PersistsExperience(t *testing.T) {
	store := newTestStore(t)

	Reflect(store, "持久化测试", "task-3", "CURRENT_TASK_DONE", 3, 20, 10*time.Second, 5, 0, []string{"read"}, "")

	// 验证经验已持久化
	exps := store.ListExperiences(0)
	if len(exps) != 1 {
		t.Errorf("应有 1 条经验, 实际 %d", len(exps))
	}

	// 验证文件存在
	if _, err := filepath.Abs(store.dir); err != nil {
		t.Fatalf("获取路径失败: %v", err)
	}
}

// ==================== RecommendStrategy 测试 ====================

func TestRecommendStrategy_WithHistory(t *testing.T) {
	store := newTestStore(t)

	// 先积累经验
	store.AddExperience(Experience{
		GoalPattern: "recommend test",
		Outcome:     OutcomeSuccess,
		TurnsUsed:   7,
		ToolsUsed:   []string{"tool1", "tool2"},
	})

	// 推荐策略
	s := RecommendStrategy(store, "recommend test", 20)
	if s.GoalPattern != "recommend test" {
		t.Errorf("应匹配历史策略: %s", s.GoalPattern)
	}
	if s.RecommendedTurns != 7 {
		t.Errorf("推荐轮次应为 7, 实际 %d", s.RecommendedTurns)
	}
}

func TestRecommendStrategy_NoHistory(t *testing.T) {
	store := newTestStore(t)

	s := RecommendStrategy(store, "全新任务", 30)
	if s.RecommendedTurns != 30 {
		t.Errorf("无历史应返回默认轮次 30, 实际 %d", s.RecommendedTurns)
	}
	if s.ID != "default" {
		t.Errorf("无历史应返回默认策略 ID, 实际 %s", s.ID)
	}
}

func TestRecommendStrategy_NilStore(t *testing.T) {
	s := RecommendStrategy(nil, "测试", 25)
	if s.RecommendedTurns != 25 {
		t.Errorf("nil store 应返回默认轮次, 实际 %d", s.RecommendedTurns)
	}
}

// ==================== extractLessons 测试 ====================

func TestExtractLessons_Success(t *testing.T) {
	ev := Evaluation{
		Outcome:   OutcomeSuccess,
		Strengths: []string{"高效完成"},
	}
	lessons := extractLessons(ev, []string{"read", "write"})
	if len(lessons) == 0 {
		t.Error("成功应有经验教训")
	}
}

func TestExtractLessons_Failure(t *testing.T) {
	ev := Evaluation{
		Outcome:   OutcomeFailed,
		RootCause: "测试失败",
		Weaknesses: []string{"轮次耗尽"},
		TurnsUsed: 80,
		TurnsBudget: 80,
	}
	lessons := extractLessons(ev, []string{})
	if len(lessons) == 0 {
		t.Error("失败应有经验教训")
	}
}

func TestExtractLessons_Aborted(t *testing.T) {
	ev := Evaluation{Outcome: OutcomeAborted}
	lessons := extractLessons(ev, []string{})
	if len(lessons) == 0 {
		t.Error("中止应有经验教训")
	}
}

// ==================== 辅助函数 ====================

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsStr(s, substr))
}

func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
