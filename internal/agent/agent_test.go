package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/genericagent/ga/internal/llm"
)

// ==================== GoalTracker 测试 ====================

func TestGoalTracker_StateTransitions(t *testing.T) {
	g := NewGoalTracker("完成单元测试")
	if g.State() != GoalStateActive {
		t.Errorf("初始状态应为 active, 实际 %s", g.State())
	}
	if g.Objective() != "完成单元测试" {
		t.Errorf("目标描述不匹配: %s", g.Objective())
	}

	// active -> paused
	g.Pause()
	if g.State() != GoalStatePaused {
		t.Errorf("Pause 后应为 paused, 实际 %s", g.State())
	}

	// paused -> active
	g.Resume()
	if g.State() != GoalStateActive {
		t.Errorf("Resume 后应为 active, 实际 %s", g.State())
	}

	// active -> done
	g.Complete("所有测试通过")
	if g.State() != GoalStateDone {
		t.Errorf("Complete 后应为 done, 实际 %s", g.State())
	}

	// done 不可再 pause
	g.Pause()
	if g.State() != GoalStateDone {
		t.Errorf("done 状态 Pause 应无效, 实际 %s", g.State())
	}
}

func TestGoalTracker_Fail(t *testing.T) {
	g := NewGoalTracker("失败测试")
	g.Fail("超时")
	if g.State() != GoalStateFailed {
		t.Errorf("Fail 后应为 failed, 实际 %s", g.State())
	}
}

func TestGoalTracker_ShouldRemind(t *testing.T) {
	g := NewGoalTracker("目标提醒测试")
	g.remindEvery = 5

	// 第 0/1 轮不提醒
	if ok, _ := g.ShouldRemind(0); ok {
		t.Error("第 0 轮不应提醒")
	}
	if ok, _ := g.ShouldRemind(1); ok {
		t.Error("第 1 轮不应提醒")
	}

	// 第 5 轮应提醒
	ok, msg := g.ShouldRemind(5)
	if !ok {
		t.Error("第 5 轮应提醒")
	}
	if !strings.Contains(msg, "目标提醒测试") {
		t.Errorf("提醒消息应包含目标描述: %s", msg)
	}

	// 同一轮再次检查不应提醒
	g.MarkReminded(5)
	if ok, _ := g.ShouldRemind(5); ok {
		t.Error("已提醒的轮次不应再次提醒")
	}

	// 第 10 轮应再次提醒
	if ok, _ := g.ShouldRemind(10); !ok {
		t.Error("第 10 轮应提醒")
	}
}

func TestGoalTracker_ShouldRemind_Paused(t *testing.T) {
	g := NewGoalTracker("暂停测试")
	g.Pause()
	if ok, _ := g.ShouldRemind(10); ok {
		t.Error("暂停状态不应提醒")
	}
}

func TestGoalTracker_StatusReport(t *testing.T) {
	g := NewGoalTracker("报告测试")
	report := g.StatusReport()
	if !strings.Contains(report, "活跃") {
		t.Errorf("active 状态报告应包含'活跃': %s", report)
	}

	g.Pause()
	report = g.StatusReport()
	if !strings.Contains(report, "暂停") {
		t.Errorf("paused 状态报告应包含'暂停': %s", report)
	}

	g.Complete("完成")
	report = g.StatusReport()
	if !strings.Contains(report, "完成") {
		t.Errorf("done 状态报告应包含'完成': %s", report)
	}
}

// ==================== PlanFile 测试 ====================

func TestPlanFile_SaveLoad(t *testing.T) {
	tmpDir := t.TempDir()
	pf := NewPlanFile(tmpDir, "test-task-123")

	content := "1. 分析需求\n2. 编写代码\n3. 运行测试"
	if err := pf.Save(content); err != nil {
		t.Fatalf("Save 失败: %v", err)
	}

	// 验证文件存在
	path := pf.GetPath()
	if path == "" {
		t.Fatal("文件路径不应为空")
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatal("计划文件应已创建")
	}

	// 加载验证
	loaded, err := pf.Load()
	if err != nil {
		t.Fatalf("Load 失败: %v", err)
	}
	if !strings.Contains(loaded, "分析需求") {
		t.Errorf("加载内容应包含计划文本: %s", loaded)
	}
	if !strings.Contains(loaded, "执行计划") {
		t.Errorf("加载内容应包含标题: %s", loaded)
	}
}

func TestPlanFile_EmptyPath(t *testing.T) {
	// baseDir 或 taskID 为空时, 不创建文件
	pf := NewPlanFile("", "task")
	if err := pf.Save("content"); err != nil {
		t.Fatalf("空路径 Save 应成功: %v", err)
	}
	if pf.GetPath() != "" {
		t.Error("空路径时 GetPath 应返回空")
	}
}

func TestPlanFile_Approval(t *testing.T) {
	pf := NewPlanFile(t.TempDir(), "approval-test")
	if pf.IsApproved() {
		t.Error("初始状态应为未审批")
	}
	pf.MarkApproved()
	if !pf.IsApproved() {
		t.Error("MarkApproved 后应为已审批")
	}
}

func TestAllowedPrompts(t *testing.T) {
	ap := NewAllowedPrompts()
	ap.Allow("git status")
	ap.Allow("npm run build")

	if !ap.IsAllowed("git status") {
		t.Error("'git status' 应被允许")
	}
	if !ap.IsAllowed("git status --short") {
		t.Error("前缀匹配: 'git status --short' 应被允许")
	}
	if !ap.IsAllowed("npm run build") {
		t.Error("'npm run build' 应被允许")
	}
	if ap.IsAllowed("rm -rf /") {
		t.Error("'rm -rf /' 不应被允许")
	}
	if ap.IsAllowed("git push") {
		t.Error("'git push' 不应被允许 (只有 git status 前缀)")
	}
}

func TestParseAllowedPromptsFromPlan(t *testing.T) {
	plan := `# 执行计划

## 步骤
1. 分析代码
2. 修改文件

## 允许的命令
- git status
- git diff
- npm run build

## 备注
无`

	ap := ParseAllowedPromptsFromPlan(plan)
	if !ap.IsAllowed("git status") {
		t.Error("应解析出 'git status'")
	}
	if !ap.IsAllowed("git diff") {
		t.Error("应解析出 'git diff'")
	}
	if !ap.IsAllowed("npm run build") {
		t.Error("应解析出 'npm run build'")
	}
	if ap.IsAllowed("rm -rf /") {
		t.Error("'rm -rf /' 不应被允许")
	}
}

func TestParseAllowedPromptsFromPlan_Empty(t *testing.T) {
	ap := ParseAllowedPromptsFromPlan("没有允许命令章节的计划")
	if len(ap.List()) != 0 {
		t.Error("无允许命令章节时应返回空列表")
	}
}

// ==================== ContextManager 测试 ====================

func TestEstimateStringTokens(t *testing.T) {
	// 空字符串
	if estimateStringTokens("") != 0 {
		t.Error("空字符串 token 应为 0")
	}

	// 纯中文 (中文为主, 按字符数*1.5)
	chinese := "你好世界测试代码"
	tokens := estimateStringTokens(chinese)
	expected := int(float64(len([]rune(chinese))) * 1.5)
	if tokens != expected {
		t.Errorf("中文 token 估算: 期望 %d, 实际 %d", expected, tokens)
	}

	// 纯英文 (按词数*1.3)
	english := "hello world this is a test"
	tokens = estimateStringTokens(english)
	expected = int(float64(len(strings.Fields(english))) * 1.3)
	if tokens != expected {
		t.Errorf("英文 token 估算: 期望 %d, 实际 %d", expected, tokens)
	}
}

func TestContextManager_EstimateTokens(t *testing.T) {
	cm := &ContextManager{MaxTokens: 128000}

	messages := []llm.Message{
		{Role: "user", Content: "你好"},
		{Role: "assistant", Content: "你好，有什么可以帮你的？"},
	}

	tokens := cm.EstimateTokens(messages)
	if tokens <= 0 {
		t.Error("token 估算应大于 0")
	}

	// 每条消息额外 4 token 开销
	if tokens < 8 {
		t.Error("至少应包含 2 条消息的开销 (8 token)")
	}
}

func TestContextManager_EstimateTokens_ToolCalls(t *testing.T) {
	cm := &ContextManager{MaxTokens: 128000}

	messages := []llm.Message{
		{
			Role: "assistant",
			Content: "我来执行命令",
			ToolCalls: []llm.ToolCall{
				{ID: "call_1", Name: "code_run", Arguments: `{"command":"ls -la"}`},
			},
		},
	}

	tokens := cm.EstimateTokens(messages)
	if tokens <= 0 {
		t.Error("含工具调用的 token 估算应大于 0")
	}
}

func TestContextManager_ShouldCompact(t *testing.T) {
	cm := &ContextManager{
		MaxTokens:  1000,
		CompactAt:  0.8,
		HardLimit:  0.95,
	}

	// 少量消息不应触发压缩
	small := []llm.Message{{Role: "user", Content: "hi"}}
	if cm.ShouldCompact(small) {
		t.Error("少量消息不应触发压缩")
	}

	// 构造超过 80% 阈值的消息
	var large []llm.Message
	for i := 0; i < 100; i++ {
		large = append(large, llm.Message{
			Role:    "user",
			Content: strings.Repeat("这是一段较长的中文内容用于测试压缩阈值", 10),
		})
	}
	if !cm.ShouldCompact(large) {
		t.Error("超过 80% 阈值应触发压缩")
	}
}

func TestContextManager_ShouldCompact_RecursionGuard(t *testing.T) {
	cm := &ContextManager{
		MaxTokens: 100,
		CompactAt: 0.5,
	}
	cm.recursionGuard.Store(true)

	large := []llm.Message{{Role: "user", Content: strings.Repeat("x", 1000)}}
	if cm.ShouldCompact(large) {
		t.Error("递归守卫激活时不应触发压缩")
	}
}

func TestContextManager_IsOverHardLimit(t *testing.T) {
	cm := &ContextManager{
		MaxTokens: 100,
		HardLimit: 0.95,
	}

	// 超过 95 token 应触发硬上限
	var large []llm.Message
	for i := 0; i < 50; i++ {
		large = append(large, llm.Message{
			Role:    "user",
			Content: strings.Repeat("测试内容", 5),
		})
	}
	if !cm.IsOverHardLimit(large) {
		t.Error("超过 95% 硬上限应返回 true")
	}
}

func TestContextManager_GetWarningLevel(t *testing.T) {
	cm := &ContextManager{
		MaxTokens:        1000,
		WarningThreshold: 0.7,
		ErrorThreshold:   0.85,
		HardLimit:        0.95,
	}

	// ok: 少量消息
	small := []llm.Message{{Role: "user", Content: "hi"}}
	if level := cm.GetWarningLevel(small); level != "ok" {
		t.Errorf("少量消息应为 ok, 实际 %s", level)
	}

	// hard: 超大量消息
	var huge []llm.Message
	for i := 0; i < 200; i++ {
		huge = append(huge, llm.Message{
			Role:    "user",
			Content: strings.Repeat("测试内容测试内容", 10),
		})
	}
	level := cm.GetWarningLevel(huge)
	if level != "hard" && level != "error" {
		t.Errorf("超大量消息应为 hard 或 error, 实际 %s", level)
	}
}

func TestContextManager_Microcompact(t *testing.T) {
	cm := &ContextManager{
		MaxTokens:              128000,
		MicrocompactToolResult: 100, // 阈值设低, 容易触发
	}

	longContent := strings.Repeat("这是一段很长的工具输出内容", 100)
	messages := []llm.Message{
		{Role: "user", Content: "执行命令"},
		{Role: "assistant", Content: "好的"},
		{Role: "tool", Content: longContent, ToolCallID: "call_1"},
	}

	result := cm.Microcompact(messages)
	if len(result) != len(messages) {
		t.Errorf("Microcompact 不应改变消息数量: 期望 %d, 实际 %d", len(messages), len(result))
	}

	// tool 消息内容应被截断
	toolContent, ok := result[2].Content.(string)
	if !ok {
		t.Fatal("tool 内容应为字符串")
	}
	if toolContent == longContent {
		t.Error("长工具结果应被截断")
	}
	if !strings.Contains(toolContent, "truncated") {
		t.Error("截断内容应包含 truncated 标记")
	}
}

func TestContextManager_Microcompact_ShortResult(t *testing.T) {
	cm := &ContextManager{
		MaxTokens:              128000,
		MicrocompactToolResult: 4000,
	}

	messages := []llm.Message{
		{Role: "tool", Content: "短结果", ToolCallID: "call_1"},
	}

	result := cm.Microcompact(messages)
	if result[0].Content != "短结果" {
		t.Error("短结果不应被截断")
	}
}

func TestContextManager_SessionMemoryCompaction(t *testing.T) {
	cm := &ContextManager{
		MaxTokens:  128000,
		KeepRecent: 2,
	}

	// 构造足够多的消息
	var messages []llm.Message
	messages = append(messages, llm.Message{Role: "system", Content: "系统提示"})
	for i := 0; i < 10; i++ {
		messages = append(messages, llm.Message{
			Role:    "user",
			Content: "用户消息" + string(rune('0'+i)),
		})
		messages = append(messages, llm.Message{
			Role:    "assistant",
			Content: "助手回复" + string(rune('0'+i)),
		})
	}

	result := cm.SessionMemoryCompaction(messages)
	if result == nil {
		t.Fatal("消息足够多时应返回压缩结果")
	}

	// 应保留 system 消息
	hasSystem := false
	for _, m := range result {
		if m.Role == "system" {
			hasSystem = true
		}
	}
	if !hasSystem {
		t.Error("压缩结果应保留 system 消息")
	}

	// 应保留最近 KeepRecent 条消息
	if len(result) < cm.KeepRecent+1 { // +1 for system
		t.Errorf("压缩结果至少应保留 system + %d 条消息, 实际 %d", cm.KeepRecent, len(result))
	}
}

func TestContextManager_SessionMemoryCompaction_TooFewMessages(t *testing.T) {
	cm := &ContextManager{
		MaxTokens:  128000,
		KeepRecent: 4,
	}

	// 消息太少, 不应压缩
	messages := []llm.Message{
		{Role: "user", Content: "hi"},
		{Role: "assistant", Content: "hello"},
	}

	result := cm.SessionMemoryCompaction(messages)
	if result != nil {
		t.Error("消息太少时应返回 nil")
	}
}

func TestContextManager_SnipTokensFreed(t *testing.T) {
	cm := &ContextManager{MaxTokens: 1000}
	cm.AddSnipTokensFreed(500)

	// 验证 snip 计数器工作
	freed := cm.snipTokensFreed.Load()
	if freed != 500 {
		t.Errorf("snip 释放量应为 500, 实际 %d", freed)
	}
}

// ==================== NewContextManager 测试 ====================

func TestNewContextManager_Defaults(t *testing.T) {
	client := &llm.Client{ContextWin: 200000}
	cm := NewContextManager(client)

	if cm.MaxTokens != 200000 {
		t.Errorf("MaxTokens 应为 200000, 实际 %d", cm.MaxTokens)
	}
	if cm.CompactAt != 0.8 {
		t.Errorf("CompactAt 默认应为 0.8, 实际 %f", cm.CompactAt)
	}
	if cm.HardLimit != 0.95 {
		t.Errorf("HardLimit 默认应为 0.95, 实际 %f", cm.HardLimit)
	}
	if cm.WarningThreshold != 0.7 {
		t.Errorf("WarningThreshold 默认应为 0.7, 实际 %f", cm.WarningThreshold)
	}
	if cm.ErrorThreshold != 0.85 {
		t.Errorf("ErrorThreshold 默认应为 0.85, 实际 %f", cm.ErrorThreshold)
	}
	if cm.KeepRecent != 4 {
		t.Errorf("KeepRecent 默认应为 4, 实际 %d", cm.KeepRecent)
	}
	if cm.MicrocompactToolResult != 4000 {
		t.Errorf("MicrocompactToolResult 默认应为 4000, 实际 %d", cm.MicrocompactToolResult)
	}
}

func TestNewContextManager_ZeroContextWin(t *testing.T) {
	client := &llm.Client{ContextWin: 0}
	cm := NewContextManager(client)
	if cm.MaxTokens != 128000 {
		t.Errorf("ContextWin=0 时应默认 128000, 实际 %d", cm.MaxTokens)
	}
}

// ==================== 辅助函数测试 ====================

func TestRegexFindAll_WinPath(t *testing.T) {
	text := "文件位于 C:\\Users\\test\\file.go 和 D:/data/config.json"
	paths := regexFindAll(text, `[a-zA-Z]:[\\\/][^\s"',<>|:*?]+`)
	if len(paths) < 2 {
		t.Errorf("应找到至少 2 个 Windows 路径, 实际 %d: %v", len(paths), paths)
	}
}

func TestRegexFindAll_UnixPath(t *testing.T) {
	text := "配置文件在 /etc/config.json, 日志在 /var/log/app.log"
	paths := regexFindAll(text, `/[a-zA-Z][^\s"',<>|:*?]+`)
	if len(paths) < 2 {
		t.Errorf("应找到至少 2 个 Unix 路径, 实际 %d: %v", len(paths), paths)
	}
}

func TestRegexFindAll_CodeFile(t *testing.T) {
	text := "修改了 main.go 和 utils.py 以及 config.json"
	files := regexFindAll(text, `[\w\-./]+\.(go|py|js|ts|json|md)`)
	if len(files) < 3 {
		t.Errorf("应找到至少 3 个代码文件, 实际 %d: %v", len(files), files)
	}
}

// ==================== Agent 基础测试 ====================

func TestNewAgent(t *testing.T) {
	client := &llm.Client{Model: "test-model"}
	agent := New(client, "系统提示", []llm.ToolSchema{
		{Name: "test_tool", Description: "测试工具", InputSchema: map[string]any{"type": "object"}},
	})

	if agent.Client != client {
		t.Error("Client 应正确设置")
	}
	if agent.SystemPrompt != "系统提示" {
		t.Error("SystemPrompt 应正确设置")
	}
	if len(agent.ToolsSchema) != 1 {
		t.Error("ToolsSchema 应正确设置")
	}
	if agent.MaxTurns != 80 {
		t.Errorf("MaxTurns 默认应为 80, 实际 %d", agent.MaxTurns)
	}
	if !agent.Verbose {
		t.Error("Verbose 默认应为 true")
	}
}

func TestAgent_InjectMessage(t *testing.T) {
	agent := New(&llm.Client{}, "", nil)
	agent.InjectMessage("测试消息1")
	agent.InjectMessage("测试消息2")

	// drainInjectedMessages 是私有方法, 通过 injectMu 保护的字段验证
	agent.injectMu.Lock()
	if len(agent.injectedMsgs) != 2 {
		t.Errorf("应注入 2 条消息, 实际 %d", len(agent.injectedMsgs))
	}
	agent.injectedMsgs = nil
	agent.injectMu.Unlock()
}

// ==================== 并发安全测试 ====================

func TestGoalTracker_ConcurrentAccess(t *testing.T) {
	g := NewGoalTracker("并发测试")
	done := make(chan bool, 2)

	// 并发读
	go func() {
		for i := 0; i < 100; i++ {
			_ = g.State()
			_ = g.Objective()
		}
		done <- true
	}()

	// 并发写
	go func() {
		for i := 0; i < 100; i++ {
			g.Pause()
			g.Resume()
		}
		done <- true
	}()

	<-done
	<-done
}

func TestPlanFile_ConcurrentSave(t *testing.T) {
	pf := NewPlanFile(t.TempDir(), "concurrent-test")
	done := make(chan error, 10)

	for i := 0; i < 10; i++ {
		go func(n int) {
			done <- pf.Save("计划内容")
		}(i)
	}

	for i := 0; i < 10; i++ {
		if err := <-done; err != nil {
			t.Errorf("并发 Save 失败: %v", err)
		}
	}
}

func TestAllowedPrompts_ConcurrentAccess(t *testing.T) {
	ap := NewAllowedPrompts()
	done := make(chan bool, 2)

	go func() {
		for i := 0; i < 100; i++ {
			ap.Allow("cmd")
		}
		done <- true
	}()

	go func() {
		for i := 0; i < 100; i++ {
			ap.IsAllowed("cmd")
		}
		done <- true
	}()

	<-done
	<-done
}

// ==================== 集成测试 ====================

func TestGoalTracker_FullLifecycle(t *testing.T) {
	g := NewGoalTracker("完成项目")
	g.remindEvery = 3

	// 模拟 Agent 循环
	for turn := 1; turn <= 10; turn++ {
		// 检查是否需要提醒
		if ok, msg := g.ShouldRemind(turn); ok {
			if !strings.Contains(msg, "完成项目") {
				t.Errorf("轮次 %d 提醒消息应包含目标: %s", turn, msg)
			}
			g.MarkReminded(turn)
		}

		// 模拟工作
		time.Sleep(time.Millisecond)
	}

	// 完成目标
	g.Complete("项目已完成")
	if g.State() != GoalStateDone {
		t.Error("最终状态应为 done")
	}

	report := g.StatusReport()
	if !strings.Contains(report, "完成") {
		t.Errorf("最终报告应包含'完成': %s", report)
	}
}

func TestPlanFile_FullWorkflow(t *testing.T) {
	tmpDir := t.TempDir()
	pf := NewPlanFile(tmpDir, "workflow-test")

	// 1. 保存计划
	plan := "## 步骤\n1. 分析\n2. 实现\n3. 测试\n\n## 允许的命令\n- go test\n- go build"
	if err := pf.Save(plan); err != nil {
		t.Fatalf("Save 失败: %v", err)
	}

	// 2. 验证文件存在
	if _, err := os.Stat(pf.GetPath()); err != nil {
		t.Fatalf("文件应存在: %v", err)
	}

	// 3. 加载验证
	loaded, err := pf.Load()
	if err != nil {
		t.Fatalf("Load 失败: %v", err)
	}
	if !strings.Contains(loaded, "分析") {
		t.Error("加载内容应包含计划")
	}

	// 4. 审批
	pf.MarkApproved()
	if !pf.IsApproved() {
		t.Error("应已审批")
	}

	// 5. 验证文件路径格式
	expectedSuffix := filepath.Join("plans", "plan-workflow-test.md")
	if !strings.HasSuffix(pf.GetPath(), expectedSuffix) {
		t.Errorf("文件路径应以 %s 结尾, 实际 %s", expectedSuffix, pf.GetPath())
	}
}
