package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// ==================== 反思模块 ====================
// 实现三大能力:
// 1. 结果评估 (Evaluation): 评估任务执行结果质量
// 2. 策略调整 (Adaptation): 根据评估调整后续策略
// 3. 经验沉淀 (Sedimentation): 将经验持久化, 供未来任务参考

// ------------------- 数据结构 -------------------

// Outcome 执行结果
type Outcome string

const (
	OutcomeSuccess    Outcome = "success"
	OutcomePartial    Outcome = "partial"
	OutcomeFailed     Outcome = "failed"
	OutcomeAborted    Outcome = "aborted"
	OutcomeTimeout    Outcome = "timeout"
	OutcomeMaxTurns   Outcome = "max_turns"
)

// Evaluation 结果评估
type Evaluation struct {
	Outcome          Outcome  `json:"outcome"`
	Score            float64  `json:"score"`            // 0-1 综合评分
	GoalAchieved     bool     `json:"goal_achieved"`    // 目标是否达成
	TurnsUsed        int      `json:"turns_used"`       // 实际使用轮次
	TurnsBudget      int      `json:"turns_budget"`     // 轮次预算
	DurationSeconds  float64  `json:"duration_seconds"` // 执行时长
	ToolCallsTotal   int      `json:"tool_calls_total"` // 工具调用总数
	ErrorCount       int      `json:"error_count"`      // 错误次数
	Strengths        []string `json:"strengths"`        // 做得好的地方
	Weaknesses       []string `json:"weaknesses"`       // 不足之处
	RootCause        string   `json:"root_cause"`       // 失败根因 (失败时)
	Summary          string   `json:"summary"`          // 评估摘要
}

// Strategy 策略建议
type Strategy struct {
	ID              string  `json:"id"`
	GoalPattern     string  `json:"goal_pattern"`     // 目标模式 (用于匹配相似任务)
	RecommendedTurns int    `json:"recommended_turns"` // 推荐轮次预算
	RecommendedTools []string `json:"recommended_tools"` // 推荐工具
	AvoidPatterns   []string `json:"avoid_patterns"`   // 应避免的模式
	Notes           string  `json:"notes"`
	HitCount        int     `json:"hit_count"`        // 命中次数 (被采用次数)
	SuccessRate     float64 `json:"success_rate"`     // 历史成功率
	UpdatedAt       time.Time `json:"updated_at"`
}

// Experience 经验记录
type Experience struct {
	ID              string     `json:"id"`
	TaskID          string     `json:"task_id"`
	Goal            string     `json:"goal"`            // 任务目标
	GoalPattern     string     `json:"goal_pattern"`    // 归一化的目标模式
	Outcome         Outcome    `json:"outcome"`
	Score           float64    `json:"score"`
	TurnsUsed       int        `json:"turns_used"`
	DurationSeconds float64    `json:"duration_seconds"`
	ToolsUsed       []string   `json:"tools_used"`
	Evaluation      Evaluation `json:"evaluation"`
	StrategyApplied string     `json:"strategy_applied"` // 采用了哪个策略
	Lessons         []string   `json:"lessons"`          // 经验教训
	Timestamp       time.Time  `json:"timestamp"`
}

// Reflection 反思结果
type Reflection struct {
	Evaluation Evaluation `json:"evaluation"`
	Strategy   Strategy   `json:"strategy"`
	Lessons    []string   `json:"lessons"`
}

// ------------------- 经验存储 -------------------

// ExperienceStore 经验持久化存储
type ExperienceStore struct {
	mu       sync.RWMutex
	dir      string
	experiences []Experience
	strategies  []Strategy
	loaded   bool
}

// NewExperienceStore 创建经验存储
func NewExperienceStore(dir string) *ExperienceStore {
	return &ExperienceStore{dir: dir}
}

func (s *ExperienceStore) expPath() string  { return filepath.Join(s.dir, "experiences.json") }
func (s *ExperienceStore) stratPath() string { return filepath.Join(s.dir, "strategies.json") }

// Load 从磁盘加载经验
func (s *ExperienceStore) Load() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.loaded {
		return nil
	}

	// 加载经验
	if data, err := os.ReadFile(s.expPath()); err == nil {
		var exps []Experience
		if err := json.Unmarshal(data, &exps); err == nil {
			s.experiences = exps
		}
	}

	// 加载策略
	if data, err := os.ReadFile(s.stratPath()); err == nil {
		var strats []Strategy
		if err := json.Unmarshal(data, &strats); err == nil {
			s.strategies = strats
		}
	}

	s.loaded = true
	return nil
}

// Save 持久化到磁盘
func (s *ExperienceStore) Save() error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if err := os.MkdirAll(s.dir, 0755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}

	// 保存经验
	if data, err := json.MarshalIndent(s.experiences, "", "  "); err == nil {
		if err := os.WriteFile(s.expPath(), data, 0644); err != nil {
			return fmt.Errorf("write experiences: %w", err)
		}
	}

	// 保存策略
	if data, err := json.MarshalIndent(s.strategies, "", "  "); err == nil {
		if err := os.WriteFile(s.stratPath(), data, 0644); err != nil {
			return fmt.Errorf("write strategies: %w", err)
		}
	}
	return nil
}

// AddExperience 记录新经验并更新策略
func (s *ExperienceStore) AddExperience(exp Experience) error {
	s.mu.Lock()
	if exp.ID == "" {
		exp.ID = fmt.Sprintf("exp_%d", time.Now().UnixNano())
	}
	if exp.Timestamp.IsZero() {
		exp.Timestamp = time.Now()
	}
	s.experiences = append(s.experiences, exp)

	// 更新对应策略
	s.updateStrategyLocked(exp)
	s.mu.Unlock()

	return s.Save()
}

// updateStrategyLocked 根据新经验更新策略 (调用方持锁)
func (s *ExperienceStore) updateStrategyLocked(exp Experience) {
	pattern := exp.GoalPattern
	if pattern == "" {
		pattern = normalizeGoal(exp.Goal)
	}

	// 查找现有策略
	var strat *Strategy
	for i := range s.strategies {
		if s.strategies[i].GoalPattern == pattern {
			strat = &s.strategies[i]
			break
		}
	}

	if strat == nil {
		// 新建策略
		s.strategies = append(s.strategies, Strategy{
			ID:          fmt.Sprintf("strat_%d", time.Now().UnixNano()),
			GoalPattern: pattern,
			UpdatedAt:   time.Now(),
		})
		strat = &s.strategies[len(s.strategies)-1]
	}

	// 累计统计
	strat.HitCount++
	// 滑动平均成功率
	if exp.Outcome == OutcomeSuccess {
		strat.SuccessRate = strat.SuccessRate*0.7 + 0.3
	} else if exp.Outcome == OutcomePartial {
		strat.SuccessRate = strat.SuccessRate*0.7 + 0.15
	} else {
		strat.SuccessRate = strat.SuccessRate*0.7
	}

	// 推荐轮次: 取成功经验的中位数
	strat.RecommendedTurns = s.medianTurnsForPattern(pattern)

	// 推荐工具: 取成功经验中最常用的工具
	strat.RecommendedTools = s.topToolsForPattern(pattern, 5)

	// 应避免的模式: 从失败经验中提取
	strat.AvoidPatterns = s.failurePatternsForGoal(pattern)

	strat.UpdatedAt = time.Now()
}

// FindStrategy 查找匹配目标的策略
func (s *ExperienceStore) FindStrategy(goal string) *Strategy {
	s.mu.RLock()
	defer s.mu.RUnlock()

	pattern := normalizeGoal(goal)
	// 精确匹配
	for i := range s.strategies {
		if s.strategies[i].GoalPattern == pattern {
			return &s.strategies[i]
		}
	}
	// 模糊匹配 (包含关系)
	for i := range s.strategies {
		if strings.Contains(pattern, s.strategies[i].GoalPattern) ||
			strings.Contains(s.strategies[i].GoalPattern, pattern) {
			return &s.strategies[i]
		}
	}
	return nil
}

// ListExperiences 列出经验 (按时间倒序)
func (s *ExperienceStore) ListExperiences(limit int) []Experience {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]Experience, len(s.experiences))
	copy(result, s.experiences)
	sort.Slice(result, func(i, j int) bool {
		return result[i].Timestamp.After(result[j].Timestamp)
	})
	if limit > 0 && limit < len(result) {
		result = result[:limit]
	}
	return result
}

// ListStrategies 列出所有策略 (按命中率倒序)
func (s *ExperienceStore) ListStrategies() []Strategy {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]Strategy, len(s.strategies))
	copy(result, s.strategies)
	sort.Slice(result, func(i, j int) bool {
		return result[i].HitCount > result[j].HitCount
	})
	return result
}

// ------------------- 统计辅助 -------------------

func (s *ExperienceStore) medianTurnsForPattern(pattern string) int {
	var turns []int
	for _, e := range s.experiences {
		if e.GoalPattern == pattern && e.Outcome == OutcomeSuccess {
			turns = append(turns, e.TurnsUsed)
		}
	}
	if len(turns) == 0 {
		return 0
	}
	sort.Ints(turns)
	return turns[len(turns)/2]
}

func (s *ExperienceStore) topToolsForPattern(pattern string, n int) []string {
	counts := map[string]int{}
	for _, e := range s.experiences {
		if e.GoalPattern == pattern && e.Outcome == OutcomeSuccess {
			for _, t := range e.ToolsUsed {
				counts[t]++
			}
		}
	}
	type kv struct {
		k string
		v int
	}
	var list []kv
	for k, v := range counts {
		list = append(list, kv{k, v})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].v > list[j].v })
	if n > len(list) {
		n = len(list)
	}
	result := make([]string, n)
	for i := 0; i < n; i++ {
		result[i] = list[i].k
	}
	return result
}

func (s *ExperienceStore) failurePatternsForGoal(pattern string) []string {
	set := map[string]bool{}
	for _, e := range s.experiences {
		if e.GoalPattern == pattern && (e.Outcome == OutcomeFailed || e.Outcome == OutcomeTimeout) {
			for _, l := range e.Lessons {
				if strings.Contains(strings.ToLower(l), "avoid") ||
					strings.Contains(strings.ToLower(l), "不要") ||
					strings.Contains(strings.ToLower(l), "避免") {
					set[l] = true
				}
			}
		}
	}
	var result []string
	for k := range set {
		result = append(result, k)
	}
	return result
}

// ------------------- 目标归一化 -------------------

// normalizeGoal 将目标文本归一化为模式
// 提取关键词, 去除具体参数, 用于匹配相似任务
func normalizeGoal(goal string) string {
	if goal == "" {
		return ""
	}
	g := strings.ToLower(strings.TrimSpace(goal))
	// 压缩空白
	g = compressSpaces(g)
	// 截断
	if len(g) > 80 {
		g = g[:80]
	}
	return strings.TrimSpace(g)
}

func compressSpaces(s string) string {
	for strings.Contains(s, "  ") {
		s = strings.ReplaceAll(s, "  ", " ")
	}
	return s
}

// ------------------- 评估器 -------------------

// Evaluate 根据执行结果评估任务质量
func Evaluate(goal string, exitReason string, turnsUsed, turnsBudget int, duration time.Duration, toolCalls int, errorCount int, toolsUsed []string) Evaluation {
	ev := Evaluation{
		TurnsUsed:      turnsUsed,
		TurnsBudget:    turnsBudget,
		DurationSeconds: duration.Seconds(),
		ToolCallsTotal: toolCalls,
		ErrorCount:     errorCount,
		Strengths:      []string{},
		Weaknesses:     []string{},
	}

	// 判定结果
	switch strings.ToUpper(exitReason) {
	case "CURRENT_TASK_DONE":
		ev.Outcome = OutcomeSuccess
		ev.GoalAchieved = true
	case "EXITED":
		ev.Outcome = OutcomeSuccess
		ev.GoalAchieved = true
	case "ABORTED":
		ev.Outcome = OutcomeAborted
	case "TIMEOUT":
		ev.Outcome = OutcomeTimeout
	case "MAX_TURNS_EXCEEDED":
		ev.Outcome = OutcomeMaxTurns
	default:
		if strings.Contains(strings.ToUpper(exitReason), "DONE") {
			ev.Outcome = OutcomeSuccess
			ev.GoalAchieved = true
		} else {
			ev.Outcome = OutcomeFailed
		}
	}

	// 综合评分 (0-1)
	score := 0.0
	if ev.GoalAchieved {
		score = 0.7
		// 轮次效率加分 (用得越少越好)
		if turnsBudget > 0 {
			efficiency := float64(turnsBudget-turnsUsed) / float64(turnsBudget)
			if efficiency > 0 {
				score += efficiency * 0.2
			}
		}
		// 错误率扣分
		if toolCalls > 0 {
			errRate := float64(errorCount) / float64(toolCalls)
			score -= errRate * 0.1
		}
	} else {
		// 未达成: 根据进度部分给分
		if turnsUsed > 0 && turnsBudget > 0 {
			progress := float64(turnsUsed) / float64(turnsBudget)
			if progress < 1 {
				score = progress * 0.3
			}
		}
	}
	if score < 0 {
		score = 0
	}
	if score > 1 {
		score = 1
	}
	ev.Score = score

	// 优势分析
	if ev.GoalAchieved {
		ev.Strengths = append(ev.Strengths, "目标成功达成")
	}
	if turnsBudget > 0 && turnsUsed <= turnsBudget/2 {
		ev.Strengths = append(ev.Strengths, fmt.Sprintf("高效完成: 仅用 %d/%d 轮次", turnsUsed, turnsBudget))
	}
	if errorCount == 0 && toolCalls > 0 {
		ev.Strengths = append(ev.Strengths, "零错误执行")
	}

	// 劣势分析
	if !ev.GoalAchieved {
		ev.Weaknesses = append(ev.Weaknesses, "目标未达成")
	}
	if turnsUsed >= turnsBudget {
		ev.Weaknesses = append(ev.Weaknesses, fmt.Sprintf("轮次耗尽: %d/%d", turnsUsed, turnsBudget))
	}
	if errorCount > 3 {
		ev.Weaknesses = append(ev.Weaknesses, fmt.Sprintf("错误较多: %d 次", errorCount))
	}
	if duration > 10*time.Minute {
		ev.Weaknesses = append(ev.Weaknesses, fmt.Sprintf("耗时较长: %.1f 分钟", duration.Minutes()))
	}

	// 根因分析
	switch ev.Outcome {
	case OutcomeTimeout:
		ev.RootCause = "执行超时, 可能任务过于复杂或存在死循环"
	case OutcomeMaxTurns:
		ev.RootCause = "达到最大轮次, 可能需要增加预算或优化策略"
	case OutcomeAborted:
		ev.RootCause = "被外部中止"
	case OutcomeFailed:
		if errorCount > 3 {
			ev.RootCause = "工具调用频繁出错"
		} else if turnsUsed >= turnsBudget {
			ev.RootCause = "轮次预算不足"
		} else {
			ev.RootCause = "任务执行失败, 需进一步分析"
		}
	}

	// 摘要
	ev.Summary = fmt.Sprintf("%s | 评分 %.2f | %d 轮次 | %.1fs | %d 工具调用 | %d 错误",
		ev.Outcome, ev.Score, ev.TurnsUsed, ev.DurationSeconds, ev.ToolCallsTotal, ev.ErrorCount)

	return ev
}

// ------------------- 反思主流程 -------------------

// Reflect 执行完整反思流程: 评估 -> 策略调整 -> 经验沉淀
func Reflect(store *ExperienceStore, goal, taskID, exitReason string, turnsUsed, turnsBudget int, duration time.Duration, toolCalls, errorCount int, toolsUsed []string, strategyApplied string) (*Reflection, error) {
	// 1. 评估
	ev := Evaluate(goal, exitReason, turnsUsed, turnsBudget, duration, toolCalls, errorCount, toolsUsed)

	// 2. 提取经验教训
	lessons := extractLessons(ev, toolsUsed)

	// 3. 沉淀经验
	pattern := normalizeGoal(goal)
	exp := Experience{
		TaskID:          taskID,
		Goal:            goal,
		GoalPattern:     pattern,
		Outcome:         ev.Outcome,
		Score:           ev.Score,
		TurnsUsed:       turnsUsed,
		DurationSeconds: ev.DurationSeconds,
		ToolsUsed:       toolsUsed,
		Evaluation:      ev,
		StrategyApplied: strategyApplied,
		Lessons:         lessons,
		Timestamp:       time.Now(),
	}

	if store != nil {
		if err := store.AddExperience(exp); err != nil {
			return nil, fmt.Errorf("sediment experience: %w", err)
		}
	}

	// 4. 获取更新后的策略
	var strat Strategy
	if store != nil {
		if s := store.FindStrategy(goal); s != nil {
			strat = *s
		}
	}

	return &Reflection{
		Evaluation: ev,
		Strategy:   strat,
		Lessons:    lessons,
	}, nil
}

// extractLessons 从评估中提取经验教训
func extractLessons(ev Evaluation, toolsUsed []string) []string {
	var lessons []string

	switch ev.Outcome {
	case OutcomeSuccess:
		if len(ev.Strengths) > 0 {
			lessons = append(lessons, "成功模式: "+strings.Join(ev.Strengths, "; "))
		}
		if len(toolsUsed) > 0 {
			// 去重工具
			seen := map[string]bool{}
			var uniq []string
			for _, t := range toolsUsed {
				if !seen[t] {
					seen[t] = true
					uniq = append(uniq, t)
				}
			}
			lessons = append(lessons, "有效工具组合: "+strings.Join(uniq, ", "))
		}
	case OutcomeFailed, OutcomeTimeout, OutcomeMaxTurns:
		if ev.RootCause != "" {
			lessons = append(lessons, "失败根因: "+ev.RootCause)
		}
		for _, w := range ev.Weaknesses {
			lessons = append(lessons, "避免: "+w)
		}
		if ev.TurnsBudget > 0 && ev.TurnsUsed >= ev.TurnsBudget {
			lessons = append(lessons, fmt.Sprintf("建议: 此类任务可能需要更多轮次预算 (当前 %d)", ev.TurnsBudget))
		}
	case OutcomeAborted:
		lessons = append(lessons, "任务被中止, 无可借鉴经验")
	}

	return lessons
}

// ------------------- 策略建议应用 -------------------

// RecommendStrategy 为新任务推荐策略
func RecommendStrategy(store *ExperienceStore, goal string, defaultTurns int) Strategy {
	if store != nil {
		if s := store.FindStrategy(goal); s != nil {
			return *s
		}
	}
	return Strategy{
		ID:               "default",
		GoalPattern:      normalizeGoal(goal),
		RecommendedTurns: defaultTurns,
		RecommendedTools: []string{},
		AvoidPatterns:    []string{},
		Notes:            "无历史经验, 使用默认策略",
	}
}
