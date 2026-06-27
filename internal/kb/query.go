package kb

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// Query DSL 查询引擎
// 支持简化版 Logseq 查询语法：
//   {{query (and [[page]] (tag #foo))}}
//   {{query (task TODO)}}
//   {{query (property key value)}}
//   {{query (between -7d +7d)}}

// QueryEngine 查询引擎
type QueryEngine struct {
	store *Store
}

// NewQueryEngine 创建查询引擎
func NewQueryEngine(store *Store) *QueryEngine {
	return &QueryEngine{store: store}
}

// QueryExpr 查询表达式 AST
type QueryExpr interface {
	Match(store *Store, block Block, page Page) bool
	String() string
}

// AndExpr 逻辑与
type AndExpr struct {
	Children []QueryExpr
}

func (e *AndExpr) Match(s *Store, b Block, p Page) bool {
	for _, c := range e.Children {
		if !c.Match(s, b, p) {
			return false
		}
	}
	return true
}
func (e *AndExpr) String() string {
	parts := []string{"(and"}
	for _, c := range e.Children {
		parts = append(parts, c.String())
	}
	return strings.Join(parts, " ") + ")"
}

// OrExpr 逻辑或
type OrExpr struct {
	Children []QueryExpr
}

func (e *OrExpr) Match(s *Store, b Block, p Page) bool {
	for _, c := range e.Children {
		if c.Match(s, b, p) {
			return true
		}
	}
	return false
}
func (e *OrExpr) String() string {
	parts := []string{"(or"}
	for _, c := range e.Children {
		parts = append(parts, c.String())
	}
	return strings.Join(parts, " ") + ")"
}

// NotExpr 逻辑非
type NotExpr struct {
	Child QueryExpr
}

func (e *NotExpr) Match(s *Store, b Block, p Page) bool {
	return !e.Child.Match(s, b, p)
}
func (e *NotExpr) String() string {
	return "(not " + e.Child.String() + ")"
}

// PageRefExpr 页面引用 [[page]]
type PageRefExpr struct {
	PageTitle string
}

func (e *PageRefExpr) Match(s *Store, b Block, p Page) bool {
	// 块内容包含 [[pageTitle]]
	return strings.Contains(b.Content, "[["+e.PageTitle+"]]")
}
func (e *PageRefExpr) String() string {
	return "[[" + e.PageTitle + "]]"
}

// TagExpr 标签 #tag
type TagExpr struct {
	Tag string
}

func (e *TagExpr) Match(s *Store, b Block, p Page) bool {
	// 检查块内容是否包含 #tag
	if strings.Contains(b.Content, "#"+e.Tag) {
		return true
	}
	// 检查页面标签
	for _, t := range p.Tags {
		if t == e.Tag {
			return true
		}
	}
	return false
}
func (e *TagExpr) String() string {
	return "#" + e.Tag
}

// TaskExpr 任务状态过滤
type TaskExpr struct {
	Status string
}

func (e *TaskExpr) Match(s *Store, b Block, p Page) bool {
	status := ExtractTaskStatus(b.Content)
	if e.Status == "ALL" {
		return status != ""
	}
	return status == e.Status
}
func (e *TaskExpr) String() string {
	return "(task " + e.Status + ")"
}

// PropertyExpr 属性过滤
type PropertyExpr struct {
	Key   string
	Value string
}

func (e *PropertyExpr) Match(s *Store, b Block, p Page) bool {
	// 检查块属性
	if v, ok := b.Properties[e.Key]; ok {
		return fmt.Sprintf("%v", v) == e.Value
	}
	// 检查页面属性
	if v, ok := p.Properties[e.Key]; ok {
		return fmt.Sprintf("%v", v) == e.Value
	}
	return false
}
func (e *PropertyExpr) String() string {
	return fmt.Sprintf("(property %s %s)", e.Key, e.Value)
}

// BetweenExpr 时间范围过滤
type BetweenExpr struct {
	From time.Time
	To   time.Time
}

func (e *BetweenExpr) Match(s *Store, b Block, p Page) bool {
	// 解析页面创建时间
	t, err := time.Parse("2006-01-02 15:04:05", p.CreatedAt)
	if err != nil {
		// 尝试其他格式
		t, err = time.Parse("2006-01-02", p.CreatedAt)
		if err != nil {
			return false
		}
	}
	return (t.Equal(e.From) || t.After(e.From)) && (t.Equal(e.To) || t.Before(e.To))
}
func (e *BetweenExpr) String() string {
	return fmt.Sprintf("(between %s %s)", e.From.Format("2006-01-02"), e.To.Format("2006-01-02"))
}

// ContentExpr 全文内容匹配（关键字搜索）
type ContentExpr struct {
	Keyword string
}

func (e *ContentExpr) Match(s *Store, b Block, p Page) bool {
	kw := strings.ToLower(e.Keyword)
	return strings.Contains(strings.ToLower(b.Content), kw) ||
		strings.Contains(strings.ToLower(p.Title), kw)
}
func (e *ContentExpr) String() string {
	return fmt.Sprintf("(content %q)", e.Keyword)
}

// PageTypeExpr 页面类型过滤（journal/template/normal）
type PageTypeExpr struct {
	Type string
}

func (e *PageTypeExpr) Match(s *Store, b Block, p Page) bool {
	switch strings.ToLower(e.Type) {
	case "journal":
		return isJournalTitle(p.Title)
	case "template":
		return strings.HasPrefix(strings.ToLower(p.Title), "templates/")
	default:
		return !isJournalTitle(p.Title) && !strings.HasPrefix(strings.ToLower(p.Title), "templates/")
	}
}
func (e *PageTypeExpr) String() string {
	return fmt.Sprintf("(page-type %s)", e.Type)
}

// OrphanExpr 孤岛页面（无链接）
type OrphanExpr struct{}

func (e *OrphanExpr) Match(s *Store, b Block, p Page) bool {
	outlinks, _ := s.GetOutlinks(p.ID)
	backlinks, _ := s.GetBacklinks(p.ID)
	return len(outlinks) == 0 && len(backlinks) == 0
}
func (e *OrphanExpr) String() string {
	return "(orphan)"
}

// HubExpr 枢纽节点（链接数 >= N）
type HubExpr struct {
	MinDegree int
}

func (e *HubExpr) Match(s *Store, b Block, p Page) bool {
	outlinks, _ := s.GetOutlinks(p.ID)
	backlinks, _ := s.GetBacklinks(p.ID)
	return len(outlinks)+len(backlinks) >= e.MinDegree
}
func (e *HubExpr) String() string {
	return fmt.Sprintf("(hub %d)", e.MinDegree)
}

// CreatedInExpr 按创建时间过滤（相对天数）
type CreatedInExpr struct {
	Days int
}

func (e *CreatedInExpr) Match(s *Store, b Block, p Page) bool {
	t, err := time.Parse("2006-01-02 15:04:05", p.CreatedAt)
	if err != nil {
		t, err = time.Parse("2006-01-02", p.CreatedAt)
		if err != nil {
			return false
		}
	}
	cutoff := time.Now().AddDate(0, 0, -e.Days)
	return t.After(cutoff)
}
func (e *CreatedInExpr) String() string {
	return fmt.Sprintf("(created-in %dd)", e.Days)
}

// UpdatedInExpr 按更新时间过滤（相对天数）
type UpdatedInExpr struct {
	Days int
}

func (e *UpdatedInExpr) Match(s *Store, b Block, p Page) bool {
	t, err := time.Parse("2006-01-02 15:04:05", p.UpdatedAt)
	if err != nil {
		t, err = time.Parse("2006-01-02", p.UpdatedAt)
		if err != nil {
			return false
		}
	}
	cutoff := time.Now().AddDate(0, 0, -e.Days)
	return t.After(cutoff)
}
func (e *UpdatedInExpr) String() string {
	return fmt.Sprintf("(updated-in %dd)", e.Days)
}

// isJournalTitle 判断是否为日记页面标题
func isJournalTitle(title string) bool {
	for _, layout := range []string{"2006-01-02", "2006_01_02", "Jan 2nd, 2006"} {
		if _, err := time.Parse(layout, title); err == nil {
			return true
		}
	}
	return false
}

// QueryResult 查询结果
type QueryResult struct {
	Blocks []Block `json:"blocks"`
	Pages  []Page  `json:"pages"`
	// 聚合统计
	TotalBlocks int            `json:"total_blocks"`
	TotalPages  int            `json:"total_pages"`
	ByTag       map[string]int `json:"by_tag,omitempty"`
	ByStatus    map[string]int `json:"by_status,omitempty"`
	ByPage      map[string]int `json:"by_page,omitempty"`
}

// Aggregate 对查询结果进行聚合统计
func (qe *QueryEngine) Aggregate(result *QueryResult) *QueryResult {
	result.TotalBlocks = len(result.Blocks)
	result.TotalPages = len(result.Pages)

	result.ByTag = make(map[string]int)
	result.ByStatus = make(map[string]int)
	result.ByPage = make(map[string]int)

	pageMap := make(map[int64]string)
	for _, p := range result.Pages {
		pageMap[p.ID] = p.Title
	}

	for _, b := range result.Blocks {
		// 按状态聚合
		if status := ExtractTaskStatus(b.Content); status != "" {
			result.ByStatus[status]++
		}
		// 按页面聚合
		if title, ok := pageMap[b.PageID]; ok {
			result.ByPage[title]++
		}
		// 按标签聚合（从块内容提取）
		tags := extractTagsFromContent(b.Content)
		for _, t := range tags {
			result.ByTag[t]++
		}
	}

	// 从页面也提取标签
	for _, p := range result.Pages {
		for _, t := range p.Tags {
			result.ByTag[t]++
		}
	}

	return result
}

// extractTagsFromContent 从内容中提取 #tag
func extractTagsFromContent(content string) []string {
	var tags []string
	seen := make(map[string]bool)
	re := regexp.MustCompile(`#([A-Za-z0-9_]+)`)
	matches := re.FindAllStringSubmatch(content, -1)
	for _, m := range matches {
		if len(m) > 1 && !seen[m[1]] {
			seen[m[1]] = true
			tags = append(tags, m[1])
		}
	}
	return tags
}

// Parse 解析查询字符串
func (qe *QueryEngine) Parse(query string) (QueryExpr, error) {
	query = strings.TrimSpace(query)
	// 去除外层 {{query ...}} 包裹
	query = strings.TrimPrefix(query, "{{query")
	query = strings.TrimSuffix(query, "}}")
	query = strings.TrimSpace(query)

	if query == "" {
		return nil, fmt.Errorf("empty query")
	}

	parser := &queryParser{tokens: tokenize(query)}
	expr, err := parser.parseExpr()
	if err != nil {
		return nil, err
	}
	return expr, nil
}

// Execute 执行查询
func (qe *QueryEngine) Execute(expr QueryExpr) (*QueryResult, error) {
	// 获取所有页面和块
	pages, err := qe.store.ListPages()
	if err != nil {
		return nil, fmt.Errorf("list pages: %w", err)
	}

	result := &QueryResult{}
	pageMap := make(map[int64]Page)
	for _, p := range pages {
		pageMap[p.ID] = p
	}

	matchedPages := make(map[int64]bool)
	for _, p := range pages {
		blocks, err := qe.store.GetBlocks(p.ID)
		if err != nil {
			continue
		}
		for _, b := range blocks {
			if expr.Match(qe.store, b, p) {
				result.Blocks = append(result.Blocks, b)
				matchedPages[p.ID] = true
			}
		}
	}

	for pid := range matchedPages {
		if p, ok := pageMap[pid]; ok {
			result.Pages = append(result.Pages, p)
		}
	}

	return result, nil
}

// ExecuteWithAggregate 执行查询并返回聚合结果
func (qe *QueryEngine) ExecuteWithAggregate(expr QueryExpr) (*QueryResult, error) {
	result, err := qe.Execute(expr)
	if err != nil {
		return nil, err
	}
	return qe.Aggregate(result), nil
}

// --- 查询解析器 ---

type queryParser struct {
	tokens []string
	pos    int
}

func tokenize(s string) []string {
	// 简单分词：按空格分割，但保留括号和引号
	var tokens []string
	current := ""
	inString := false
	for _, r := range s {
		switch {
		case r == '"':
			inString = !inString
			current += string(r)
		case r == '(' || r == ')':
			if current != "" {
				tokens = append(tokens, current)
				current = ""
			}
			tokens = append(tokens, string(r))
		case r == ' ' || r == '\t' || r == '\n':
			if !inString && current != "" {
				tokens = append(tokens, current)
				current = ""
			} else if inString {
				current += string(r)
			}
		default:
			current += string(r)
		}
	}
	if current != "" {
		tokens = append(tokens, current)
	}
	return tokens
}

func (p *queryParser) parseExpr() (QueryExpr, error) {
	if p.pos >= len(p.tokens) {
		return nil, fmt.Errorf("unexpected end of input")
	}

	tok := p.tokens[p.pos]
	p.pos++

	// 处理括号表达式
	if tok == "(" {
		if p.pos >= len(p.tokens) {
			return nil, fmt.Errorf("expected operator after (")
		}
		op := p.tokens[p.pos]
		p.pos++

		switch op {
		case "and":
			return p.parseAnd()
		case "or":
			return p.parseOr()
		case "not":
			return p.parseNot()
		case "task":
			return p.parseTask()
		case "property":
			return p.parseProperty()
		case "between":
			return p.parseBetween()
		case "content":
			return p.parseContent()
		case "page-type":
			return p.parsePageType()
		case "orphan":
			return p.parseOrphan()
		case "hub":
			return p.parseHub()
		case "created-in":
			return p.parseCreatedIn()
		case "updated-in":
			return p.parseUpdatedIn()
		default:
			return nil, fmt.Errorf("unknown operator: %s", op)
		}
	}

	// 处理原子表达式
	return p.parseAtom(tok)
}

func (p *queryParser) parseAnd() (QueryExpr, error) {
	var children []QueryExpr
	for p.pos < len(p.tokens) && p.tokens[p.pos] != ")" {
		expr, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		children = append(children, expr)
	}
	if p.pos >= len(p.tokens) {
		return nil, fmt.Errorf("missing closing )")
	}
	p.pos++ // consume )
	return &AndExpr{Children: children}, nil
}

func (p *queryParser) parseOr() (QueryExpr, error) {
	var children []QueryExpr
	for p.pos < len(p.tokens) && p.tokens[p.pos] != ")" {
		expr, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		children = append(children, expr)
	}
	if p.pos >= len(p.tokens) {
		return nil, fmt.Errorf("missing closing )")
	}
	p.pos++
	return &OrExpr{Children: children}, nil
}

func (p *queryParser) parseNot() (QueryExpr, error) {
	expr, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	if p.pos >= len(p.tokens) || p.tokens[p.pos] != ")" {
		return nil, fmt.Errorf("missing closing )")
	}
	p.pos++
	return &NotExpr{Child: expr}, nil
}

func (p *queryParser) parseTask() (QueryExpr, error) {
	if p.pos >= len(p.tokens) {
		return nil, fmt.Errorf("expected task status")
	}
	status := strings.ToUpper(p.tokens[p.pos])
	p.pos++
	if p.pos >= len(p.tokens) || p.tokens[p.pos] != ")" {
		return nil, fmt.Errorf("missing closing )")
	}
	p.pos++
	return &TaskExpr{Status: status}, nil
}

func (p *queryParser) parseProperty() (QueryExpr, error) {
	if p.pos+1 >= len(p.tokens) {
		return nil, fmt.Errorf("expected property key and value")
	}
	key := p.tokens[p.pos]
	p.pos++
	value := p.tokens[p.pos]
	p.pos++
	// 去除引号
	value = strings.Trim(value, "\"")
	if p.pos >= len(p.tokens) || p.tokens[p.pos] != ")" {
		return nil, fmt.Errorf("missing closing )")
	}
	p.pos++
	return &PropertyExpr{Key: key, Value: value}, nil
}

func (p *queryParser) parseBetween() (QueryExpr, error) {
	if p.pos+1 >= len(p.tokens) {
		return nil, fmt.Errorf("expected from and to dates")
	}
	fromStr := p.tokens[p.pos]
	p.pos++
	toStr := p.tokens[p.pos]
	p.pos++
	if p.pos >= len(p.tokens) || p.tokens[p.pos] != ")" {
		return nil, fmt.Errorf("missing closing )")
	}
	p.pos++

	from, err := parseRelativeDate(fromStr)
	if err != nil {
		return nil, fmt.Errorf("invalid from date: %w", err)
	}
	to, err := parseRelativeDate(toStr)
	if err != nil {
		return nil, fmt.Errorf("invalid to date: %w", err)
	}
	return &BetweenExpr{From: from, To: to}, nil
}

func (p *queryParser) parseContent() (QueryExpr, error) {
	if p.pos >= len(p.tokens) {
		return nil, fmt.Errorf("expected content keyword")
	}
	kw := p.tokens[p.pos]
	p.pos++
	kw = strings.Trim(kw, "\"")
	if p.pos >= len(p.tokens) || p.tokens[p.pos] != ")" {
		return nil, fmt.Errorf("missing closing )")
	}
	p.pos++
	return &ContentExpr{Keyword: kw}, nil
}

func (p *queryParser) parsePageType() (QueryExpr, error) {
	if p.pos >= len(p.tokens) {
		return nil, fmt.Errorf("expected page type")
	}
	t := p.tokens[p.pos]
	p.pos++
	if p.pos >= len(p.tokens) || p.tokens[p.pos] != ")" {
		return nil, fmt.Errorf("missing closing )")
	}
	p.pos++
	return &PageTypeExpr{Type: t}, nil
}

func (p *queryParser) parseOrphan() (QueryExpr, error) {
	if p.pos >= len(p.tokens) || p.tokens[p.pos] != ")" {
		return nil, fmt.Errorf("missing closing )")
	}
	p.pos++
	return &OrphanExpr{}, nil
}

func (p *queryParser) parseHub() (QueryExpr, error) {
	if p.pos >= len(p.tokens) {
		return nil, fmt.Errorf("expected hub min degree")
	}
	var n int
	fmt.Sscanf(p.tokens[p.pos], "%d", &n)
	p.pos++
	if p.pos >= len(p.tokens) || p.tokens[p.pos] != ")" {
		return nil, fmt.Errorf("missing closing )")
	}
	p.pos++
	return &HubExpr{MinDegree: n}, nil
}

func (p *queryParser) parseCreatedIn() (QueryExpr, error) {
	if p.pos >= len(p.tokens) {
		return nil, fmt.Errorf("expected days")
	}
	var n int
	fmt.Sscanf(p.tokens[p.pos], "%d", &n)
	p.pos++
	if p.pos >= len(p.tokens) || p.tokens[p.pos] != ")" {
		return nil, fmt.Errorf("missing closing )")
	}
	p.pos++
	return &CreatedInExpr{Days: n}, nil
}

func (p *queryParser) parseUpdatedIn() (QueryExpr, error) {
	if p.pos >= len(p.tokens) {
		return nil, fmt.Errorf("expected days")
	}
	var n int
	fmt.Sscanf(p.tokens[p.pos], "%d", &n)
	p.pos++
	if p.pos >= len(p.tokens) || p.tokens[p.pos] != ")" {
		return nil, fmt.Errorf("missing closing )")
	}
	p.pos++
	return &UpdatedInExpr{Days: n}, nil
}

func (p *queryParser) parseAtom(tok string) (QueryExpr, error) {
	// 页面引用 [[page]]
	if strings.HasPrefix(tok, "[[") && strings.HasSuffix(tok, "]]") {
		title := tok[2 : len(tok)-2]
		return &PageRefExpr{PageTitle: title}, nil
	}
	// 标签 #tag
	if strings.HasPrefix(tok, "#") {
		return &TagExpr{Tag: tok[1:]}, nil
	}
	return nil, fmt.Errorf("unknown atom: %s", tok)
}

// parseRelativeDate 解析相对日期（如 -7d, +7d）或绝对日期
var relDateRe = regexp.MustCompile(`^([+-]?\d+)([dwm])$`)

func parseRelativeDate(s string) (time.Time, error) {
	// 尝试相对日期
	if m := relDateRe.FindStringSubmatch(s); m != nil {
		var n int
		fmt.Sscanf(m[1], "%d", &n)
		now := time.Now()
		switch m[2] {
		case "d":
			return now.AddDate(0, 0, n), nil
		case "w":
			return now.AddDate(0, 0, n*7), nil
		case "m":
			return now.AddDate(0, n, 0), nil
		}
	}

	// 尝试绝对日期
	for _, layout := range []string{"2006-01-02", "2006_01_02"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("invalid date format: %s", s)
}
