package kb

import (
	"fmt"
	"strings"
	"time"
)

// Journal 日记管理器
type Journal struct {
	store *Store
}

// NewJournal 创建日记管理器
func NewJournal(store *Store) *Journal {
	return &Journal{store: store}
}

// TodayTitle 返回今日日记页面标题（格式：yyyy_MM_dd）
func (j *Journal) TodayTitle() string {
	return time.Now().Format("2006_01_02")
}

// DateTitle 根据时间返回日记标题
func (j *Journal) DateTitle(t time.Time) string {
	return t.Format("2006_01_02")
}

// ParseTitle 从标题解析日期（若为日记页面）
func (j *Journal) ParseTitle(title string) (time.Time, bool) {
	t, err := time.Parse("2006_01_02", title)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

// IsJournalTitle 判断标题是否符合日记命名格式
func (j *Journal) IsJournalTitle(title string) bool {
	_, ok := j.ParseTitle(title)
	return ok
}

// EnsureToday 确保今日日记页面存在，返回页面
func (j *Journal) EnsureToday() (*Page, error) {
	return j.EnsureDate(time.Now())
}

// EnsureDate 确保指定日期的日记页面存在
func (j *Journal) EnsureDate(t time.Time) (*Page, error) {
	title := j.DateTitle(t)

	// 检查是否已存在
	page, err := j.store.GetPageByTitle(title)
	if err != nil {
		return nil, fmt.Errorf("get journal page: %w", err)
	}
	if page != nil {
		return page, nil
	}

	// 创建日记页面
	parsed := ParsedPage{
		Title:     title,
		FileName:  "journals/" + title + ".md",
		Namespace: "journals",
		IsJournal: true,
		Tags:      []string{"journal"},
		Properties: map[string]any{
			"date": t.Format("2006-01-02"),
			"day":  t.Weekday().String(),
		},
		Blocks: []ParsedBlock{
			{
				Content:   "# " + title,
				Order:     0,
				Level:     0,
				BlockType: "heading",
			},
			{
				Content:   "",
				Order:     1,
				Level:     0,
				BlockType: "paragraph",
			},
		},
	}

	pageID, err := j.store.UpsertPage(parsed)
	if err != nil {
		return nil, fmt.Errorf("create journal page: %w", err)
	}
	if err := j.store.UpsertBlocks(pageID, parsed.Blocks); err != nil {
		return nil, fmt.Errorf("create journal blocks: %w", err)
	}

	return j.store.GetPageByID(pageID)
}

// ListRecent 列出最近的日记
func (j *Journal) ListRecent(limit int) ([]Page, error) {
	return j.store.ListJournalPages(limit)
}

// GetByDate 获取指定日期的日记
func (j *Journal) GetByDate(t time.Time) (*Page, error) {
	title := j.DateTitle(t)
	return j.store.GetPageByTitle(title)
}

// GetByTitle 通过标题获取日记（验证标题格式）
func (j *Journal) GetByTitle(title string) (*Page, error) {
	if !j.IsJournalTitle(title) {
		return nil, fmt.Errorf("invalid journal title format: %s", title)
	}
	page, err := j.store.GetPageByTitle(title)
	if err != nil {
		return nil, err
	}
	if page == nil || !page.IsJournal {
		return nil, nil
	}
	return page, nil
}

// Range 列出指定日期范围内的日记
func (j *Journal) Range(from, to time.Time) ([]Page, error) {
	rows, err := j.store.db.Query(`
		SELECT id, title, file_name, namespace, is_journal, tags, properties, created_at, updated_at
		FROM kb_pages
		WHERE is_journal = 1 AND title >= ? AND title <= ?
		ORDER BY title DESC`,
		j.DateTitle(from), j.DateTitle(to))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanPages(rows)
}

// WeekTitle 返回本周日记标题列表（周一到周日）
func (j *Journal) WeekTitle(t time.Time) []string {
	// 计算本周周一
	weekday := int(t.Weekday())
	if weekday == 0 {
		weekday = 7
	}
	monday := t.AddDate(0, 0, -(weekday - 1))

	var titles []string
	for i := 0; i < 7; i++ {
		day := monday.AddDate(0, 0, i)
		titles = append(titles, j.DateTitle(day))
	}
	return titles
}

// FormatHumanDate 返回人类可读的日期描述
func (j *Journal) FormatHumanDate(title string) string {
	t, ok := j.ParseTitle(title)
	if !ok {
		return title
	}
	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	// ParseTitle 返回 UTC 时间，转换为本地时区进行比较
	target := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, now.Location())

	diff := today.Sub(target).Hours() / 24
	switch {
	case diff == 0:
		return "Today"
	case diff == 1:
		return "Yesterday"
	case diff == -1:
		return "Tomorrow"
	case diff > 1 && diff < 7:
		return fmt.Sprintf("%d days ago", int(diff))
	case diff < -1 && diff > -7:
		return fmt.Sprintf("in %d days", int(-diff))
	default:
		return t.Format("2006-01-02 (Mon)")
	}
}

// DefaultTemplate 返回日记默认模板内容
func (j *Journal) DefaultTemplate() string {
	title := j.TodayTitle()
	weekday := time.Now().Weekday().String()
	var sb strings.Builder
	sb.WriteString("# " + title + "\n\n")
	sb.WriteString("- **Date**: " + time.Now().Format("2006-01-02") + "\n")
	sb.WriteString("- **Day**: " + weekday + "\n\n")
	sb.WriteString("## Tasks\n\n")
	sb.WriteString("- TODO \n\n")
	sb.WriteString("## Notes\n\n")
	sb.WriteString("- \n\n")
	sb.WriteString("## Log\n\n")
	sb.WriteString("- \n")
	return sb.String()
}
