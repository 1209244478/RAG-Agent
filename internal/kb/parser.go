package kb

import (
	"regexp"
	"strings"
	"unicode"
)

// ParsedBlock 解析后的块（解析器输出，未持久化）
type ParsedBlock struct {
	Content    string         `json:"content"`
	Order      int            `json:"order"`
	Level      int            `json:"level"`
	BlockType  string         `json:"block_type"`
	Properties map[string]any `json:"properties"`
}

// ParsedPage 解析后的页面（解析器输出）
type ParsedPage struct {
	Title      string         `json:"title"`
	FileName   string         `json:"file_name"`
	Namespace  string         `json:"namespace"`
	IsJournal  bool           `json:"is_journal"`
	Tags       []string       `json:"tags"`
	Properties map[string]any `json:"properties"`
	Blocks     []ParsedBlock  `json:"blocks"`
}

// 链接提取正则
var (
	// [[页面名]] 或 [[页面名|别名]]
	pageRefRe = regexp.MustCompile(`\[\[([^\]|]+)(?:\|([^\]]+))?\]\]`)
	// ((块UUID))
	blockRefRe = regexp.MustCompile(`\(\(([0-9a-fA-F-]{36})\)\)`)
	// #标签（非行首）
	tagRe = regexp.MustCompile(`(?:^|\s)#([^\s#]+)`)
	// 行首标签
	lineTagRe = regexp.MustCompile(`^#[^\s#]+$`)
)

// ExtractedLink 从块内容中提取的链接
type ExtractedLink struct {
	TargetPageTitle string
	TargetBlockID   string
	LinkType        string // "page_ref" | "block_ref" | "tag"
}

// ParseMarkdown 将 Markdown 文本解析为块列表
func ParseMarkdown(content string) []ParsedBlock {
	lines := strings.Split(content, "\n")
	var blocks []ParsedBlock
	var frontmatter map[string]any
	contentStart := 0

	// 解析 frontmatter (YAML)
	if len(lines) >= 2 && strings.TrimSpace(lines[0]) == "---" {
		for i := 1; i < len(lines); i++ {
			if strings.TrimSpace(lines[i]) == "---" {
				frontmatter = parseFrontmatter(lines[1:i])
				contentStart = i + 1
				break
			}
		}
	}

	// 跳过空行
	for contentStart < len(lines) && strings.TrimSpace(lines[contentStart]) == "" {
		contentStart++
	}

	order := 0
	i := contentStart
	for i < len(lines) {
		line := lines[i]

		// 跳过空行
		if strings.TrimSpace(line) == "" {
			i++
			continue
		}

		// 计算缩进层级
		level := countIndent(line)
		trimmed := strings.TrimLeft(line, " \t")

		// 判断块类型
		blockType := detectBlockType(trimmed)

		// 收集多行块（代码块、引用块等）
		blockContent := trimmed
		if blockType == "code" {
			// 代码块：收集到结束 ```
			blockContent, i = collectCodeBlock(lines, i, trimmed)
		} else if blockType == "quote" {
			// 引用块：收集连续的 > 行
			blockContent, i = collectQuoteBlock(lines, i, trimmed)
		} else if isListItem(trimmed) {
			// 列表项：单个列表项作为一个块
			blockContent = trimmed
			i++
		} else {
			// 普通段落：收集连续非空行直到遇到空行或结构变化
			blockContent, i = collectParagraph(lines, i, trimmed)
		}

		block := ParsedBlock{
			Content:    strings.TrimSpace(blockContent),
			Order:      order,
			Level:      level,
			BlockType:  blockType,
			Properties: make(map[string]any),
		}

		// 如果有 frontmatter 且是第一个块，附加属性
		if order == 0 && frontmatter != nil {
			block.Properties = frontmatter
		}

		blocks = append(blocks, block)
		order++
	}

	return blocks
}

// ParsePage 解析完整的 Markdown 文件，返回 ParsedPage
func ParsePage(content string, fileName string) ParsedPage {
	blocks := ParseMarkdown(content)
	page := ParsedPage{
		FileName:   fileName,
		Blocks:     blocks,
		Properties: make(map[string]any),
	}

	// 从 frontmatter 提取属性
	if len(blocks) > 0 && len(blocks[0].Properties) > 0 {
		page.Properties = blocks[0].Properties
		if tags, ok := page.Properties["tags"].([]string); ok {
			page.Tags = tags
		}
	}

	// 从文件名推导标题
	page.Title = FileNameToTitle(fileName)
	page.Namespace = extractNamespace(page.Title)
	page.IsJournal = isJournalFile(fileName)

	// 如果没有 frontmatter tags，从内容提取
	if len(page.Tags) == 0 {
		page.Tags = extractTagsFromBlocks(blocks)
	}

	return page
}

// FileNameToTitle 将文件名转换为页面标题
// "项目/子项目.md" -> "项目/子项目"
// "2024_01_15.md" -> "2024_01_15" (日记)
func FileNameToTitle(fileName string) string {
	// 去掉扩展名
	title := fileName
	if idx := strings.LastIndex(title, "."); idx > 0 {
		title = title[:idx]
	}
	// 统一路径分隔符
	title = strings.ReplaceAll(title, "\\", "/")
	return title
}

// extractNamespace 从标题中提取命名空间
// "项目/子项目" -> "项目"
func extractNamespace(title string) string {
	if idx := strings.LastIndex(title, "/"); idx > 0 {
		return title[:idx]
	}
	return ""
}

// isJournalFile 判断是否为日记文件
// 格式：journals/YYYY_MM_DD.md 或 YYYY-MM-DD.md
func isJournalFile(fileName string) bool {
	base := fileName
	if idx := strings.LastIndex(base, "/"); idx >= 0 {
		base = base[idx+1:]
	}
	base = strings.ReplaceAll(base, "_", "-")
	// 匹配 YYYY-MM-DD
	if len(base) >= 10 {
		datePart := base[:10]
		if matched, _ := regexp.MatchString(`^\d{4}-\d{2}-\d{2}`, datePart); matched {
			return true
		}
	}
	return strings.HasPrefix(strings.ToLower(fileName), "journals/")
}

// extractTagsFromBlocks 从所有块中提取标签
func extractTagsFromBlocks(blocks []ParsedBlock) []string {
	tagSet := make(map[string]bool)
	for _, block := range blocks {
		links := ExtractLinks(block.Content)
		for _, link := range links {
			if link.LinkType == "tag" {
				tagSet[link.TargetPageTitle] = true
			}
		}
	}
	var tags []string
	for tag := range tagSet {
		tags = append(tags, tag)
	}
	return tags
}

// ExtractLinks 从文本中提取所有链接
func ExtractLinks(text string) []ExtractedLink {
	var links []ExtractedLink
	seen := make(map[string]bool)

	// 页面引用 [[页面名]]
	for _, match := range pageRefRe.FindAllStringSubmatch(text, -1) {
		title := strings.TrimSpace(match[1])
		key := "page:" + title
		if !seen[key] {
			seen[key] = true
			links = append(links, ExtractedLink{
				TargetPageTitle: title,
				LinkType:        "page_ref",
			})
		}
	}

	// 块引用 ((UUID))
	for _, match := range blockRefRe.FindAllStringSubmatch(text, -1) {
		blockID := match[1]
		key := "block:" + blockID
		if !seen[key] {
			seen[key] = true
			links = append(links, ExtractedLink{
				TargetBlockID: blockID,
				LinkType:      "block_ref",
			})
		}
	}

	// 标签 #标签（排除行首纯标签行，那也是标签但格式不同）
	for _, match := range tagRe.FindAllStringSubmatch(text, -1) {
		tag := strings.TrimSpace(match[1])
		// 过滤掉纯数字（可能是 # 标题语法）
		if isAllDigits(tag) {
			continue
		}
		key := "tag:" + tag
		if !seen[key] {
			seen[key] = true
			links = append(links, ExtractedLink{
				TargetPageTitle: tag,
				LinkType:        "tag",
			})
		}
	}

	// 行首纯标签 #tag
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if lineTagRe.MatchString(line) {
			tag := strings.TrimPrefix(line, "#")
			key := "tag:" + tag
			if !seen[key] {
				seen[key] = true
				links = append(links, ExtractedLink{
					TargetPageTitle: tag,
					LinkType:        "tag",
				})
			}
		}
	}

	return links
}

// countIndent 计算行首缩进层级（2空格=1级）
func countIndent(line string) int {
	count := 0
	for _, ch := range line {
		if ch == ' ' {
			count++
		} else if ch == '\t' {
			count += 2
		} else {
			break
		}
	}
	return count / 2
}

// detectBlockType 检测块类型
func detectBlockType(line string) string {
	if strings.HasPrefix(line, "```") {
		return "code"
	}
	if strings.HasPrefix(line, "> ") || line == ">" {
		return "quote"
	}
	if strings.HasPrefix(line, "# ") || strings.HasPrefix(line, "## ") || strings.HasPrefix(line, "### ") ||
		strings.HasPrefix(line, "#### ") || strings.HasPrefix(line, "##### ") || strings.HasPrefix(line, "###### ") {
		return "heading"
	}
	if isListItem(line) {
		return "list"
	}
	if strings.HasPrefix(line, "|") {
		return "table"
	}
	return "paragraph"
}

// isListItem 判断是否为列表项
func isListItem(line string) bool {
	if strings.HasPrefix(line, "- ") || strings.HasPrefix(line, "* ") || strings.HasPrefix(line, "+ ") {
		return true
	}
	// 有序列表 1. 2. 等
	if matched, _ := regexp.MatchString(`^\d+\.\s`, line); matched {
		return true
	}
	return false
}

// collectCodeBlock 收集代码块
func collectCodeBlock(lines []string, start int, firstLine string) (string, int) {
	var sb strings.Builder
	sb.WriteString(firstLine)
	sb.WriteString("\n")
	i := start + 1
	for i < len(lines) {
		if strings.TrimSpace(lines[i]) == "```" {
			sb.WriteString(lines[i])
			return sb.String(), i + 1
		}
		sb.WriteString(lines[i])
		sb.WriteString("\n")
		i++
	}
	return sb.String(), i
}

// collectQuoteBlock 收集引用块
func collectQuoteBlock(lines []string, start int, firstLine string) (string, int) {
	var sb strings.Builder
	sb.WriteString(firstLine)
	sb.WriteString("\n")
	i := start + 1
	for i < len(lines) {
		line := lines[i]
		if strings.TrimSpace(line) == "" {
			break
		}
		trimmed := strings.TrimLeft(line, " \t")
		if strings.HasPrefix(trimmed, "> ") || trimmed == ">" {
			sb.WriteString(line)
			sb.WriteString("\n")
			i++
		} else {
			break
		}
	}
	return sb.String(), i
}

// collectParagraph 收集普通段落
func collectParagraph(lines []string, start int, firstLine string) (string, int) {
	var sb strings.Builder
	sb.WriteString(firstLine)
	i := start + 1
	for i < len(lines) {
		line := lines[i]
		if strings.TrimSpace(line) == "" {
			break
		}
		trimmed := strings.TrimLeft(line, " \t")
		// 遇到结构变化就停止
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "> ") ||
			strings.HasPrefix(trimmed, "# ") || isListItem(trimmed) {
			break
		}
		sb.WriteString("\n")
		sb.WriteString(line)
		i++
	}
	return sb.String(), i
}

// parseFrontmatter 解析简单的 YAML frontmatter
func parseFrontmatter(lines []string) map[string]any {
	result := make(map[string]any)
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		idx := strings.Index(line, ":")
		if idx < 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		val := strings.TrimSpace(line[idx+1:])

		// 去掉引号
		if len(val) >= 2 && (val[0] == '"' && val[len(val)-1] == '"' ||
			val[0] == '\'' && val[len(val)-1] == '\'') {
			val = val[1 : len(val)-1]
		}

		// tags 特殊处理：支持 [a, b, c] 格式
		if key == "tags" || key == "Tags" {
			if strings.HasPrefix(val, "[") && strings.HasSuffix(val, "]") {
				inner := val[1 : len(val)-1]
				parts := strings.Split(inner, ",")
				var tags []string
				for _, p := range parts {
					p = strings.TrimSpace(p)
					if p != "" {
						tags = append(tags, p)
					}
				}
				result["tags"] = tags
			} else if val != "" {
				result["tags"] = []string{val}
			}
		} else {
			result[key] = val
		}
	}
	return result
}

// isAllDigits 判断字符串是否全为数字
func isAllDigits(s string) bool {
	for _, r := range s {
		if !unicode.IsDigit(r) {
			return false
		}
	}
	return len(s) > 0
}
