package task

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/genericagent/ga/internal/llm"
)

// ==================== TaskState / Status 测试 ====================

func TestIsTerminal(t *testing.T) {
	tests := []struct {
		status   Status
		expected bool
	}{
		{StatusPending, false},
		{StatusRunning, false},
		{StatusPaused, false},
		{StatusCompleted, true},
		{StatusFailed, true},
		{StatusKilled, true},
	}

	for _, tt := range tests {
		if got := IsTerminal(tt.status); got != tt.expected {
			t.Errorf("IsTerminal(%s) = %v, 期望 %v", tt.status, got, tt.expected)
		}
	}
}

func TestTaskState_JSONRoundTrip(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	endTime := now.Add(5 * time.Minute)

	original := &TaskState{
		ID:          "task-123",
		Type:        TypeMain,
		Status:      StatusCompleted,
		UserID:      1,
		SessionID:   100,
		Prompt:      "测试任务",
		Description: "这是一个测试",
		StartTime:   now,
		EndTime:     &endTime,
		TurnCount:   10,
		TokenUsage: TokenUsage{
			InputTokens:  1000,
			OutputTokens: 500,
			TotalTokens:  1500,
		},
		OutputFile: "output.log",
		Goal:       "完成测试",
		PlanMode:   true,
	}

	// 序列化
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}

	// 反序列化
	var loaded TaskState
	if err := json.Unmarshal(data, &loaded); err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}

	if loaded.ID != original.ID {
		t.Errorf("ID 不匹配: %s vs %s", loaded.ID, original.ID)
	}
	if loaded.Status != original.Status {
		t.Errorf("Status 不匹配: %s vs %s", loaded.Status, original.Status)
	}
	if loaded.Type != original.Type {
		t.Errorf("Type 不匹配: %s vs %s", loaded.Type, original.Type)
	}
	if loaded.TurnCount != original.TurnCount {
		t.Errorf("TurnCount 不匹配: %d vs %d", loaded.TurnCount, original.TurnCount)
	}
	if loaded.TokenUsage.TotalTokens != original.TokenUsage.TotalTokens {
		t.Errorf("TotalTokens 不匹配: %d vs %d", loaded.TokenUsage.TotalTokens, original.TokenUsage.TotalTokens)
	}
	if loaded.PlanMode != original.PlanMode {
		t.Errorf("PlanMode 不匹配: %v vs %v", loaded.PlanMode, original.PlanMode)
	}
}

func TestTaskState_WithIsolation(t *testing.T) {
	state := &TaskState{
		ID:         "task-isolated",
		Type:       TypeSubagent,
		Status:     StatusRunning,
		Isolation:  IsolationWorktree,
		WorktreePath: "/tmp/worktree-123",
		CwdOverride: "/tmp/worktree-123",
		ForkFrom:   "task-parent",
		AgentName:  "worker-1",
		TeamName:   "team-alpha",
		CacheSafe: &CacheSafeParams{
			Model:        "gpt-4",
			SystemPrompt: "系统提示",
			Temperature:  0.7,
			MaxTokens:    8192,
		},
		ForkDepth: 2,
	}

	data, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}

	var loaded TaskState
	if err := json.Unmarshal(data, &loaded); err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}

	if loaded.Isolation != IsolationWorktree {
		t.Errorf("Isolation 不匹配: %s", loaded.Isolation)
	}
	if loaded.WorktreePath != state.WorktreePath {
		t.Errorf("WorktreePath 不匹配: %s", loaded.WorktreePath)
	}
	if loaded.CacheSafe == nil {
		t.Fatal("CacheSafe 不应为 nil")
	}
	if loaded.CacheSafe.Model != "gpt-4" {
		t.Errorf("CacheSafe.Model 不匹配: %s", loaded.CacheSafe.Model)
	}
	if loaded.ForkDepth != 2 {
		t.Errorf("ForkDepth 不匹配: %d", loaded.ForkDepth)
	}
}

// ==================== Store 测试 ====================

func TestStore_SaveLoad(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewStore(tmpDir)

	state := &TaskState{
		ID:        "test-task-1",
		Type:      TypeMain,
		Status:    StatusRunning,
		UserID:    1,
		SessionID: 100,
		Prompt:    "测试任务",
		StartTime: time.Now(),
	}

	// 保存
	if err := store.Save(state); err != nil {
		t.Fatalf("Save 失败: %v", err)
	}

	// 验证文件存在
	statePath := store.statePath(1, "test-task-1")
	if _, err := os.Stat(statePath); os.IsNotExist(err) {
		t.Fatal("state.json 应已创建")
	}

	// 加载
	loaded, err := store.Load(1, "test-task-1")
	if err != nil {
		t.Fatalf("Load 失败: %v", err)
	}
	if loaded.ID != "test-task-1" {
		t.Errorf("ID 不匹配: %s", loaded.ID)
	}
	if loaded.Status != StatusRunning {
		t.Errorf("Status 不匹配: %s", loaded.Status)
	}
}

func TestStore_Save_AtomicWrite(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewStore(tmpDir)

	state := &TaskState{
		ID:     "atomic-test",
		Type:   TypeMain,
		Status: StatusRunning,
		UserID: 1,
	}

	if err := store.Save(state); err != nil {
		t.Fatalf("Save 失败: %v", err)
	}

	// 不应有 .tmp 文件残留
	tmpPath := store.statePath(1, "atomic-test") + ".tmp"
	if _, err := os.Stat(tmpPath); !os.IsNotExist(err) {
		t.Error("原子写入后不应残留 .tmp 文件")
	}
}

func TestStore_Load_NotFound(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewStore(tmpDir)

	_, err := store.Load(999, "nonexistent")
	if err == nil {
		t.Error("加载不存在的任务应返回错误")
	}
}

func TestStore_SaveLoadMessages(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewStore(tmpDir)

	messages := []llm.Message{
		{Role: "system", Content: "系统提示"},
		{Role: "user", Content: "你好"},
		{Role: "assistant", Content: "你好，有什么可以帮你的？"},
	}

	// 保存
	if err := store.SaveMessages(1, "msg-test", messages); err != nil {
		t.Fatalf("SaveMessages 失败: %v", err)
	}

	// 加载
	loaded, err := store.LoadMessages(1, "msg-test")
	if err != nil {
		t.Fatalf("LoadMessages 失败: %v", err)
	}
	if len(loaded) != 3 {
		t.Errorf("消息数量不匹配: %d", len(loaded))
	}
	if loaded[1].Content != "你好" {
		t.Errorf("第二条消息内容不匹配: %v", loaded[1].Content)
	}
}

func TestStore_AppendOutput(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewStore(tmpDir)

	// 追加多次
	store.AppendOutput(1, "output-test", "第一行\n")
	store.AppendOutput(1, "output-test", "第二行\n")
	store.AppendOutput(1, "output-test", "第三行\n")

	// 验证内容
	data, err := os.ReadFile(store.outputPath(1, "output-test"))
	if err != nil {
		t.Fatalf("读取输出文件失败: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "第一行") {
		t.Error("输出应包含'第一行'")
	}
	if !strings.Contains(content, "第二行") {
		t.Error("输出应包含'第二行'")
	}
	if !strings.Contains(content, "第三行") {
		t.Error("输出应包含'第三行'")
	}
}

func TestStore_ListByUser(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewStore(tmpDir)

	// 创建多个任务
	for i := 0; i < 3; i++ {
		state := &TaskState{
			ID:     "list-test-" + string(rune('0'+i)),
			Type:   TypeMain,
			Status: StatusCompleted,
			UserID: 42,
		}
		store.Save(state)
	}

	// 列出
	states, err := store.ListByUser(42)
	if err != nil {
		t.Fatalf("ListByUser 失败: %v", err)
	}
	if len(states) != 3 {
		t.Errorf("应列出 3 个任务, 实际 %d", len(states))
	}
}

func TestStore_ListByUser_Empty(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewStore(tmpDir)

	states, err := store.ListByUser(999)
	if err != nil {
		t.Fatalf("ListByUser 不应返回错误: %v", err)
	}
	if len(states) != 0 {
		t.Errorf("无任务时应返回空列表, 实际 %d", len(states))
	}
}

func TestStore_ListAllUsers(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewStore(tmpDir)

	// 为不同用户创建任务
	store.Save(&TaskState{ID: "u1-task", Type: TypeMain, Status: StatusCompleted, UserID: 1})
	store.Save(&TaskState{ID: "u2-task", Type: TypeMain, Status: StatusCompleted, UserID: 2})
	store.Save(&TaskState{ID: "u3-task", Type: TypeMain, Status: StatusCompleted, UserID: 3})

	userIDs, err := store.ListAllUsers()
	if err != nil {
		t.Fatalf("ListAllUsers 失败: %v", err)
	}
	if len(userIDs) != 3 {
		t.Errorf("应列出 3 个用户, 实际 %d", len(userIDs))
	}
}

func TestStore_DirectoryStructure(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewStore(tmpDir)

	state := &TaskState{
		ID:     "dir-test",
		Type:   TypeMain,
		Status: StatusRunning,
		UserID: 5,
	}
	store.Save(state)

	// 验证目录结构: <baseDir>/users/u5/tasks/dir-test/state.json
	expectedDir := filepath.Join(tmpDir, "users", "u5", "tasks", "dir-test")
	if _, err := os.Stat(expectedDir); os.IsNotExist(err) {
		t.Errorf("任务目录应存在: %s", expectedDir)
	}
}

// ==================== MessageRouter 测试 ====================

func TestMessageRouter_RegisterUnregister(t *testing.T) {
	router := NewMessageRouter()

	task := &Task{
		State: &TaskState{AgentName: "agent-1", TeamName: "team-a"},
		inbox: make(chan MessageEnvelope, 10),
	}

	router.Register("agent-1", task)

	agents := router.ListAgents()
	if len(agents) != 1 || agents[0] != "agent-1" {
		t.Errorf("注册后应列出 agent-1: %v", agents)
	}

	router.Unregister("agent-1")
	agents = router.ListAgents()
	if len(agents) != 0 {
		t.Errorf("注销后列表应为空: %v", agents)
	}
}

func TestMessageRouter_DirectSend(t *testing.T) {
	router := NewMessageRouter()

	task := &Task{
		State: &TaskState{AgentName: "receiver", TeamName: "team-a"},
		inbox: make(chan MessageEnvelope, 10),
	}
	router.Register("receiver", task)

	msg := MessageEnvelope{
		From:    "sender",
		To:      "receiver",
		Content: "你好",
		SentAt:  time.Now(),
	}

	if err := router.Send(msg); err != nil {
		t.Fatalf("Send 失败: %v", err)
	}

	// 验证接收
	select {
	case received := <-task.inbox:
		if received.Content != "你好" {
			t.Errorf("消息内容不匹配: %s", received.Content)
		}
	case <-time.After(time.Second):
		t.Error("未收到消息")
	}
}

func TestMessageRouter_BroadcastSend(t *testing.T) {
	router := NewMessageRouter()

	// 创建同团队的 3 个 agent
	sender := &Task{
		State: &TaskState{AgentName: "sender", TeamName: "team-b"},
		inbox: make(chan MessageEnvelope, 10),
	}
	member1 := &Task{
		State: &TaskState{AgentName: "member1", TeamName: "team-b"},
		inbox: make(chan MessageEnvelope, 10),
	}
	member2 := &Task{
		State: &TaskState{AgentName: "member2", TeamName: "team-b"},
		inbox: make(chan MessageEnvelope, 10),
	}

	router.Register("sender", sender)
	router.Register("member1", member1)
	router.Register("member2", member2)

	// 广播
	msg := MessageEnvelope{
		From:    "sender",
		To:      "all",
		Content: "团队消息",
		SentAt:  time.Now(),
	}

	if err := router.Send(msg); err != nil {
		t.Fatalf("广播失败: %v", err)
	}

	// sender 不应收到自己的消息
	select {
	case <-sender.inbox:
		t.Error("sender 不应收到自己的广播消息")
	case <-time.After(100 * time.Millisecond):
		// 正确
	}

	// member1 和 member2 应收到
	for _, name := range []string{"member1", "member2"} {
		var task *Task
		if name == "member1" {
			task = member1
		} else {
			task = member2
		}
		select {
		case received := <-task.inbox:
			if received.Content != "团队消息" {
				t.Errorf("%s 收到的消息内容不匹配: %s", name, received.Content)
			}
		case <-time.After(time.Second):
			t.Errorf("%s 未收到广播消息", name)
		}
	}
}

func TestMessageRouter_SendToUnknown(t *testing.T) {
	router := NewMessageRouter()

	msg := MessageEnvelope{
		From:    "sender",
		To:      "nonexistent",
		Content: "测试",
	}

	if err := router.Send(msg); err == nil {
		t.Error("发送给不存在的 agent 应返回错误")
	}
}

func TestMessageRouter_BroadcastNoTeam(t *testing.T) {
	router := NewMessageRouter()

	// agent 不属于任何团队
	sender := &Task{
		State: &TaskState{AgentName: "lone-agent"},
		inbox: make(chan MessageEnvelope, 10),
	}
	router.Register("lone-agent", sender)

	msg := MessageEnvelope{
		From:    "lone-agent",
		To:      "all",
		Content: "广播",
	}

	if err := router.Send(msg); err == nil {
		t.Error("不属于团队的 agent 广播应返回错误")
	}
}

func TestMessageRouter_History(t *testing.T) {
	router := NewMessageRouter()

	task := &Task{
		State: &TaskState{AgentName: "receiver", TeamName: "team-c"},
		inbox: make(chan MessageEnvelope, 100),
	}
	router.Register("receiver", task)

	// 发送多条消息
	for i := 0; i < 5; i++ {
		router.Send(MessageEnvelope{
			From:    "sender",
			To:      "receiver",
			Content: "消息" + string(rune('0'+i)),
			SentAt:  time.Now(),
		})
	}

	history := router.GetHistory()
	if len(history) != 5 {
		t.Errorf("历史记录应为 5 条, 实际 %d", len(history))
	}
}

func TestMessageRouter_HistoryCap(t *testing.T) {
	router := NewMessageRouter()

	task := &Task{
		State: &TaskState{AgentName: "receiver", TeamName: "team-d"},
		inbox: make(chan MessageEnvelope, 1000),
	}
	router.Register("receiver", task)

	// 发送超过上限的消息
	total := TeammateMessagesUICap + 20
	for i := 0; i < total; i++ {
		router.Send(MessageEnvelope{
			From:    "sender",
			To:      "receiver",
			Content: "消息",
			SentAt:  time.Now(),
		})
	}

	history := router.GetHistory()
	if len(history) > TeammateMessagesUICap {
		t.Errorf("历史记录不应超过上限 %d, 实际 %d", TeammateMessagesUICap, len(history))
	}
}

func TestMessageRouter_ShutdownProtocol(t *testing.T) {
	router := NewMessageRouter()

	target := &Task{
		State: &TaskState{AgentName: "target", TeamName: "team-e"},
		inbox: make(chan MessageEnvelope, 10),
	}
	router.Register("target", target)

	// 发送 shutdown 请求
	err := router.SendShutdown("controller", "target", "任务完成")
	if err != nil {
		t.Fatalf("SendShutdown 失败: %v", err)
	}

	// 验证 inbox 收到 shutdown 消息
	select {
	case msg := <-target.inbox:
		if !strings.HasPrefix(msg.Content, ShutdownRequestPrefix) {
			t.Errorf("应为 shutdown 请求: %s", msg.Content)
		}
	case <-time.After(time.Second):
		t.Error("未收到 shutdown 消息")
	}
}

func TestMessageRouter_ShutdownResponse(t *testing.T) {
	router := NewMessageRouter()

	target := &Task{
		State: &TaskState{AgentName: "target", TeamName: "team-f"},
		inbox: make(chan MessageEnvelope, 10),
	}
	router.Register("target", target)

	// 发送 shutdown 响应 (仅记录, 不路由到 inbox)
	msg := MessageEnvelope{
		From:    "target",
		To:      "controller",
		Content: ShutdownResponsePrefix + " 已关闭",
		SentAt:  time.Now(),
	}

	if err := router.Send(msg); err != nil {
		t.Fatalf("Send 失败: %v", err)
	}

	// inbox 不应收到 (shutdown 响应仅记录)
	select {
	case <-target.inbox:
		t.Error("shutdown 响应不应路由到 inbox")
	case <-time.After(100 * time.Millisecond):
		// 正确
	}

	// 但历史应记录
	history := router.GetHistory()
	if len(history) != 1 {
		t.Errorf("历史应记录 1 条, 实际 %d", len(history))
	}
}

// ==================== WorktreeManager 测试 ====================

func TestWorktreeManager_NotGitRepo(t *testing.T) {
	tmpDir := t.TempDir()
	wm := NewWorktreeManager(filepath.Join(tmpDir, "worktrees"))

	// tmpDir 不是 git 仓库
	_, _, err := wm.CreateWorktree(tmpDir, "test-task")
	if err == nil {
		t.Error("非 git 仓库应返回错误")
	}
	if !strings.Contains(err.Error(), "not a git repo") {
		t.Errorf("错误信息应包含 'not a git repo': %v", err)
	}
}

func TestWorktreeManager_BaseDir(t *testing.T) {
	wm := NewWorktreeManager("/custom/path")
	// filepath.Abs 会将路径转为绝对路径 (Windows 下 /custom/path -> C:\custom\path)
	abs, _ := filepath.Abs("/custom/path")
	if wm.baseDir != abs {
		t.Errorf("baseDir 应为 %s, 实际 %s", abs, wm.baseDir)
	}
}

func TestWorktreeManager_DefaultBaseDir(t *testing.T) {
	wm := NewWorktreeManager("")
	if wm.baseDir == "" {
		t.Error("空 baseDir 应使用默认值")
	}
}

// ==================== 并发安全测试 ====================

func TestStore_ConcurrentSave(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewStore(tmpDir)

	done := make(chan error, 10)
	for i := 0; i < 10; i++ {
		go func(n int) {
			state := &TaskState{
				ID:     "concurrent-" + string(rune('0'+n)),
				Type:   TypeMain,
				Status: StatusRunning,
				UserID: 1,
			}
			done <- store.Save(state)
		}(i)
	}

	for i := 0; i < 10; i++ {
		if err := <-done; err != nil {
			t.Errorf("并发 Save 失败: %v", err)
		}
	}
}

func TestMessageRouter_ConcurrentRegister(t *testing.T) {
	router := NewMessageRouter()
	done := make(chan bool, 20)

	for i := 0; i < 20; i++ {
		go func(n int) {
			name := "agent-" + string(rune('0'+n))
			task := &Task{
				State: &TaskState{AgentName: name, TeamName: "team-concurrent"},
				inbox: make(chan MessageEnvelope, 10),
			}
			router.Register(name, task)
			done <- true
		}(i)
	}

	for i := 0; i < 20; i++ {
		<-done
	}

	agents := router.ListAgents()
	if len(agents) != 20 {
		t.Errorf("应注册 20 个 agent, 实际 %d", len(agents))
	}
}

// ==================== 集成测试 ====================

func TestStore_FullWorkflow(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewStore(tmpDir)

	// 1. 创建并保存任务
	state := &TaskState{
		ID:        "workflow-test",
		Type:      TypeMain,
		Status:    StatusRunning,
		UserID:    1,
		SessionID: 100,
		Prompt:    "完成项目",
		Goal:      "交付可运行代码",
		StartTime: time.Now(),
	}
	if err := store.Save(state); err != nil {
		t.Fatalf("初始 Save 失败: %v", err)
	}

	// 2. 保存消息历史
	messages := []llm.Message{
		{Role: "user", Content: "开始任务"},
		{Role: "assistant", Content: "好的，开始执行"},
	}
	if err := store.SaveMessages(1, "workflow-test", messages); err != nil {
		t.Fatalf("SaveMessages 失败: %v", err)
	}

	// 3. 追加输出
	store.AppendOutput(1, "workflow-test", "开始执行...\n")
	store.AppendOutput(1, "workflow-test", "任务完成\n")

	// 4. 更新状态为完成
	state.Status = StatusCompleted
	state.TurnCount = 5
	endTime := time.Now()
	state.EndTime = &endTime
	if err := store.Save(state); err != nil {
		t.Fatalf("更新 Save 失败: %v", err)
	}

	// 5. 加载验证
	loaded, err := store.Load(1, "workflow-test")
	if err != nil {
		t.Fatalf("Load 失败: %v", err)
	}
	if loaded.Status != StatusCompleted {
		t.Errorf("状态应为 completed, 实际 %s", loaded.Status)
	}
	if loaded.TurnCount != 5 {
		t.Errorf("TurnCount 应为 5, 实际 %d", loaded.TurnCount)
	}

	// 6. 加载消息
	loadedMsgs, err := store.LoadMessages(1, "workflow-test")
	if err != nil {
		t.Fatalf("LoadMessages 失败: %v", err)
	}
	if len(loadedMsgs) != 2 {
		t.Errorf("消息数量应为 2, 实际 %d", len(loadedMsgs))
	}

	// 7. 列出用户任务
	states, _ := store.ListByUser(1)
	if len(states) != 1 {
		t.Errorf("应列出 1 个任务, 实际 %d", len(states))
	}
}
