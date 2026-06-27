package kb

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Importer Markdown 批量导入器
type Importer struct {
	store  *Store
	linker *Linker
}

// NewImporter 创建导入器
func NewImporter(store *Store, linker *Linker) *Importer {
	return &Importer{store: store, linker: linker}
}

// ImportResult 导入结果
type ImportResult struct {
	TotalFiles  int      `json:"total_files"`
	Imported    int      `json:"imported"`
	Skipped     int      `json:"skipped"`
	Errors      []string `json:"errors,omitempty"`
	ImportedPages []string `json:"imported_pages"`
}

// ImportDirectory 导入指定目录下的所有 Markdown 文件
// 支持嵌套目录结构，目录路径转换为命名空间
func (im *Importer) ImportDirectory(dir string) (*ImportResult, error) {
	result := &ImportResult{Errors: []string{}}

	// 检查目录是否存在
	info, err := os.Stat(dir)
	if err != nil {
		return nil, fmt.Errorf("directory not accessible: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("path is not a directory: %s", dir)
	}

	// 遍历目录
	err = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("walk error %s: %v", path, err))
			return nil
		}
		if info.IsDir() {
			// 跳过隐藏目录和常见非内容目录
			name := info.Name()
			if strings.HasPrefix(name, ".") || name == "node_modules" || name == ".git" {
				return filepath.SkipDir
			}
			return nil
		}

		// 只处理 .md 文件
		if !strings.HasSuffix(strings.ToLower(path), ".md") {
			return nil
		}

		result.TotalFiles++

		// 计算相对路径和命名空间
		relPath, err := filepath.Rel(dir, path)
		if err != nil {
			relPath = filepath.Base(path)
		}
		relPath = filepath.ToSlash(relPath)

		// 读取文件内容
		content, err := os.ReadFile(path)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("read %s: %v", path, err))
			result.Skipped++
			return nil
		}

		// 导入单个文件
		pageTitle, err := im.importFile(string(content), relPath, dir)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("import %s: %v", path, err))
			result.Skipped++
			return nil
		}

		result.Imported++
		result.ImportedPages = append(result.ImportedPages, pageTitle)
		return nil
	})

	if err != nil {
		return result, fmt.Errorf("walk directory: %w", err)
	}

	return result, nil
}

// importFile 导入单个 Markdown 文件
func (im *Importer) importFile(content, relPath, baseDir string) (string, error) {
	// 解析 Markdown
	blocks := ParseMarkdown(content)
	if len(blocks) == 0 {
		blocks = []ParsedBlock{{Content: content, Order: 0, Level: 0, BlockType: "paragraph"}}
	}

	// 提取页面标题
	// 优先级：frontmatter title > 第一个标题 > 文件名
	title := extractPageTitle(content, relPath)

	// 计算命名空间（基于目录结构）
	namespace := extractNamespaceFromPath(relPath)

	// 判断是否为日记页面
	isJournal := isJournalPage(title, relPath)

	// 计算文件名（相对路径）
	fileName := relPath

	// 解析 frontmatter 获取标签和属性
	tags, properties := extractFrontmatter(content)

	parsed := ParsedPage{
		Title:      title,
		FileName:   fileName,
		Namespace:  namespace,
		IsJournal:  isJournal,
		Tags:       tags,
		Properties: properties,
		Blocks:     blocks,
	}

	// 存储页面
	pageID, err := im.store.UpsertPage(parsed)
	if err != nil {
		return "", fmt.Errorf("upsert page: %w", err)
	}

	// 存储块
	if err := im.store.UpsertBlocks(pageID, blocks); err != nil {
		return "", fmt.Errorf("upsert blocks: %w", err)
	}

	return title, nil
}

// extractPageTitle 从内容或文件路径提取页面标题
func extractPageTitle(content, relPath string) string {
	// 1. 尝试从 frontmatter 提取
	if fm := parseFrontmatterFromContent(content); fm != nil {
		if title, ok := fm["title"].(string); ok && title != "" {
			return title
		}
	}

	// 2. 尝试从第一个标题提取
	lines := strings.Split(content, "\n")
	inFrontmatter := false
	for i, line := range lines {
		if i == 0 && strings.TrimSpace(line) == "---" {
			inFrontmatter = true
			continue
		}
		if inFrontmatter {
			if strings.TrimSpace(line) == "---" {
				inFrontmatter = false
			}
			continue
		}
		// 匹配 # 标题
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "# ") {
			return strings.TrimSpace(trimmed[2:])
		}
	}

	// 3. 使用文件名（不含扩展名和目录）
	base := filepath.Base(relPath)
	base = strings.TrimSuffix(base, filepath.Ext(base))
	// 替换下划线和连字符为空格
	base = strings.ReplaceAll(base, "_", " ")
	base = strings.ReplaceAll(base, "-", " ")
	return base
}

// extractNamespaceFromPath 从相对路径提取命名空间
func extractNamespaceFromPath(relPath string) string {
	dir := filepath.Dir(relPath)
	if dir == "." || dir == "" {
		return ""
	}
	// 转换为命名空间格式（用 / 分隔）
	dir = filepath.ToSlash(dir)
	// 如果是 journals 目录，标记为日记命名空间
	return dir
}

// isJournalPage 判断是否为日记页面
func isJournalPage(title, relPath string) bool {
	// 路径包含 journals/
	if strings.Contains(filepath.ToSlash(relPath), "journals/") {
		return true
	}
	// 标题符合 yyyy_MM_dd 格式
	if _, err := parseDate(title); err == nil {
		return true
	}
	return false
}

// parseDate 尝试解析日期格式
func parseDate(s string) (interface{}, error) {
	// 简单检查格式 yyyy_MM_dd 或 yyyy-MM-dd
	if len(s) != 10 {
		return nil, fmt.Errorf("invalid length")
	}
	return s, nil
}

// extractFrontmatter 从内容提取 frontmatter 中的标签和属性
func extractFrontmatter(content string) ([]string, map[string]any) {
	properties := make(map[string]any)
	var tags []string

	lines := strings.Split(content, "\n")
	if len(lines) < 2 || strings.TrimSpace(lines[0]) != "---" {
		return tags, properties
	}

	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			break
		}
		// 解析 key: value
		parts := strings.SplitN(lines[i], ":", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])

		// 处理 tags
		if key == "tags" || key == "tag" {
			// 支持 [a, b, c] 或 a, b, c 格式
			value = strings.Trim(value, "[]")
			for _, t := range strings.Split(value, ",") {
				t = strings.TrimSpace(t)
				if t != "" {
					tags = append(tags, t)
				}
			}
			properties[key] = tags
		} else {
			properties[key] = value
		}
	}

	return tags, properties
}

// parseFrontmatterFromContent 解析 frontmatter 返回 map
func parseFrontmatterFromContent(content string) map[string]any {
	lines := strings.Split(content, "\n")
	if len(lines) < 2 || strings.TrimSpace(lines[0]) != "---" {
		return nil
	}

	result := make(map[string]any)
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			break
		}
		parts := strings.SplitN(lines[i], ":", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])
		result[key] = value
	}
	return result
}
