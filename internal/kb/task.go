package kb

import (
	"database/sql"
	"fmt"
	"regexp"
	"strings"
)

// 任务状态常量
const (
	TaskTodo  = "TODO"
	TaskDoing = "DOING"
	TaskDone  = "DONE"
	TaskLater = "LATER"
	TaskNow   = "NOW"
)

// Task 表示一个任务块
type Task struct {
	BlockID   string `json:"block_id"`
	PageID    int64  `json:"page_id"`
	PageTitle string `json:"page_title"`
	Content   string `json:"content"`
	Status    string `json:"status"`
	Priority  string `json:"priority"` // A/B/C 或空
	Scheduled string `json:"scheduled"` // 计划日期 YYYY-MM-DD
	Deadline  string `json:"deadline"`  // 截止日期 YYYY-MM-DD
	Order     int    `json:"order"`
	Level     int    `json:"level"`
}

// TaskManager 任务管理器
type TaskManager struct {
	store *Store
}

// 任务关键字正则：行首的 TODO/DOING/DONE/LATER/NOW
var taskStatusRe = regexp.MustCompile(`^\s*[-*]?\s*(TODO|DOING|DONE|LATER|NOW)\b`)
var taskPriorityRe = regexp.MustCompile(`\[#[A-C]\]`)
var scheduledRe = regexp.MustCompile(`SCHEDULED:\s*<(\d{4}-\d{2}-\d{2})`)
var deadlineRe = regexp.MustCompile(`DEADLINE:\s*<(\d{4}-\d{2}-\d{2})`)

// NewTaskManager 创建任务管理器
func NewTaskManager(store *Store) *TaskManager {
	return &TaskManager{store: store}
}

// ExtractTaskStatus 从块内容中提取任务状态
func ExtractTaskStatus(content string) string {
	m := taskStatusRe.FindStringSubmatch(content)
	if m == nil {
		return ""
	}
	return m[1]
}

// IsTaskBlock 判断块是否为任务
func IsTaskBlock(content string) bool {
	return ExtractTaskStatus(content) != ""
}

// ExtractPriority 提取优先级 [A]/[B]/[C]
func ExtractPriority(content string) string {
	m := taskPriorityRe.FindStringSubmatch(content)
	if m == nil {
		return ""
	}
	// 返回 A/B/C
	return string(m[0][2])
}

// ExtractScheduled 提取计划日期
func ExtractScheduled(content string) string {
	m := scheduledRe.FindStringSubmatch(content)
	if m == nil {
		return ""
	}
	return m[1]
}

// ExtractDeadline 提取截止日期
func ExtractDeadline(content string) string {
	m := deadlineRe.FindStringSubmatch(content)
	if m == nil {
		return ""
	}
	return m[1]
}

// parseBlockToTask 将块转换为任务对象
func parseBlockToTask(b Block, pageTitle string) Task {
	t := Task{
		BlockID:   b.ID,
		PageID:    b.PageID,
		PageTitle: pageTitle,
		Content:   b.Content,
		Status:    ExtractTaskStatus(b.Content),
		Priority:  ExtractPriority(b.Content),
		Scheduled: ExtractScheduled(b.Content),
		Deadline:  ExtractDeadline(b.Content),
		Order:     b.Order,
		Level:     b.Level,
	}
	return t
}

// AllTasks 获取所有任务
func (tm *TaskManager) AllTasks() ([]Task, error) {
	rows, err := tm.store.db.Query(`
		SELECT b.id, b.page_id, b.content, b.block_order, b.level, p.title
		FROM kb_blocks b
		JOIN kb_pages p ON b.page_id = p.id
		WHERE b.content LIKE '%TODO%' OR b.content LIKE '%DOING%'
		   OR b.content LIKE '%DONE%' OR b.content LIKE '%LATER%'
		   OR b.content LIKE '%NOW%'
		ORDER BY p.updated_at DESC, b.block_order`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTaskRows(rows)
}

// TasksByStatus 按状态筛选任务
func (tm *TaskManager) TasksByStatus(status string) ([]Task, error) {
	status = strings.ToUpper(status)
	rows, err := tm.store.db.Query(`
		SELECT b.id, b.page_id, b.content, b.block_order, b.level, p.title
		FROM kb_blocks b
		JOIN kb_pages p ON b.page_id = p.id
		WHERE b.content LIKE ? AND b.content LIKE ?
		ORDER BY p.updated_at DESC, b.block_order`,
		"%"+status+"%", "%"+status+"%")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tasks []Task
	for rows.Next() {
		var b Block
		var pageTitle string
		if err := rows.Scan(&b.ID, &b.PageID, &b.Content, &b.Order, &b.Level, &pageTitle); err != nil {
			continue
		}
		// 二次过滤：确保状态匹配（避免 DONE 匹配 TODO 的子串问题）
		if ExtractTaskStatus(b.Content) != status {
			continue
		}
		tasks = append(tasks, parseBlockToTask(b, pageTitle))
	}
	return tasks, nil
}

// TasksByPage 获取指定页面的任务
func (tm *TaskManager) TasksByPage(pageID int64) ([]Task, error) {
	rows, err := tm.store.db.Query(`
		SELECT b.id, b.page_id, b.content, b.block_order, b.level, p.title
		FROM kb_blocks b
		JOIN kb_pages p ON b.page_id = p.id
		WHERE b.page_id = ?
		ORDER BY b.block_order`, pageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tasks []Task
	for rows.Next() {
		var b Block
		var pageTitle string
		if err := rows.Scan(&b.ID, &b.PageID, &b.Content, &b.Order, &b.Level, &pageTitle); err != nil {
			continue
		}
		if !IsTaskBlock(b.Content) {
			continue
		}
		tasks = append(tasks, parseBlockToTask(b, pageTitle))
	}
	return tasks, nil
}

// UpdateStatus 更新任务状态
func (tm *TaskManager) UpdateStatus(blockID string, newStatus string) error {
	newStatus = strings.ToUpper(newStatus)
	valid := map[string]bool{
		TaskTodo: true, TaskDoing: true, TaskDone: true,
		TaskLater: true, TaskNow: true,
	}
	if !valid[newStatus] {
		return fmt.Errorf("invalid task status: %s", newStatus)
	}

	// 获取原块
	b, err := tm.store.GetBlock(blockID)
	if err != nil {
		return fmt.Errorf("get block: %w", err)
	}
	if b == nil {
		return fmt.Errorf("block not found: %s", blockID)
	}

	oldStatus := ExtractTaskStatus(b.Content)
	if oldStatus == "" {
		return fmt.Errorf("block is not a task: %s", blockID)
	}

	// 替换状态关键字
	newContent := taskStatusRe.ReplaceAllStringFunc(b.Content, func(match string) string {
		// 保留前缀（缩进和列表符号），只替换状态词
		idx := strings.Index(strings.ToUpper(match), oldStatus)
		if idx < 0 {
			return match
		}
		return match[:idx] + newStatus + match[idx+len(oldStatus):]
	})

	return tm.store.UpdateBlockContent(blockID, newContent)
}

// TaskStats 任务统计
type TaskStats struct {
	Total   int `json:"total"`
	Todo    int `json:"todo"`
	Doing   int `json:"doing"`
	Done    int `json:"done"`
	Later   int `json:"later"`
	Now     int `json:"now"`
	Overdue int `json:"overdue"`
}

// Stats 返回任务统计
func (tm *TaskManager) Stats() (*TaskStats, error) {
	tasks, err := tm.AllTasks()
	if err != nil {
		return nil, err
	}
	stats := &TaskStats{}
	for _, t := range tasks {
		stats.Total++
		switch t.Status {
		case TaskTodo:
			stats.Todo++
		case TaskDoing:
			stats.Doing++
		case TaskDone:
			stats.Done++
		case TaskLater:
			stats.Later++
		case TaskNow:
			stats.Now++
		}
		// 简单判断是否逾期（有 deadline 且未完成且日期已过）
		if t.Deadline != "" && t.Status != TaskDone {
			// 此处简化，实际应比较日期
			stats.Overdue++
		}
	}
	return stats, nil
}

// scanTaskRows 扫描任务查询结果
func scanTaskRows(rows *sql.Rows) ([]Task, error) {
	var tasks []Task
	for rows.Next() {
		var b Block
		var pageTitle string
		if err := rows.Scan(&b.ID, &b.PageID, &b.Content, &b.Order, &b.Level, &pageTitle); err != nil {
			continue
		}
		if !IsTaskBlock(b.Content) {
			continue
		}
		tasks = append(tasks, parseBlockToTask(b, pageTitle))
	}
	return tasks, nil
}
