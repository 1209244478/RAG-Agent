package kb

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// Template 模板引擎
type Template struct {
	store *Store
}

// templateRe 匹配 {{template name, arg1, arg2}} 或 {{name, arg1}}
var templateRe = regexp.MustCompile(`\{\{(?:template\s+)?([^,}\s]+)\s*(?:,\s*([^}]*))?\s*\}\}`)

// 内置变量正则
var varRe = regexp.MustCompile(`\{\{(today|yesterday|tomorrow|time|date|page|current_page)\}\}`)

// NewTemplate 创建模板引擎
func NewTemplate(store *Store) *Template {
	return &Template{store: store}
}

// TemplateVar 模板变量上下文
type TemplateVar struct {
	PageTitle string
	Now       time.Time
}

// IsTemplatePage 判断页面是否为模板（命名空间为 templates 或标题以 "template/" 开头）
func (t *Template) IsTemplatePage(page *Page) bool {
	if page == nil {
		return false
	}
	if page.Namespace == "templates" || page.Namespace == "template" {
		return true
	}
	if strings.HasPrefix(strings.ToLower(page.Title), "template/") {
		return true
	}
	return false
}

// ListTemplates 列出所有模板页面
func (t *Template) ListTemplates() ([]Page, error) {
	// 查询命名空间为 templates 的页面
	pages, err := t.store.ListPagesByNamespace("templates")
	if err != nil {
		return nil, err
	}
	// 也查询 template 命名空间
	pages2, _ := t.store.ListPagesByNamespace("template")
	pages = append(pages, pages2...)
	return pages, nil
}

// GetTemplate 获取模板内容
func (t *Template) GetTemplate(name string) (string, error) {
	// 尝试完整名称
	page, err := t.store.GetPageByTitle(name)
	if err != nil {
		return "", err
	}
	if page == nil {
		// 尝试 templates/ 前缀
		page, err = t.store.GetPageByTitle("templates/" + name)
		if err != nil {
			return "", err
		}
	}
	if page == nil {
		return "", fmt.Errorf("template not found: %s", name)
	}

	blocks, err := t.store.GetBlocks(page.ID)
	if err != nil {
		return "", err
	}

	var lines []string
	for _, b := range blocks {
		lines = append(lines, b.Content)
	}
	return strings.Join(lines, "\n"), nil
}

// Render 渲染模板，替换变量和嵌套模板引用
func (t *Template) Render(templateContent string, vars TemplateVar) (string, error) {
	// 1. 替换内置变量
	content := t.replaceBuiltinVars(templateContent, vars)

	// 2. 递归替换嵌套模板引用（限制深度防止循环）
	visited := make(map[string]bool)
	rendered, err := t.expandTemplateRefs(content, vars, 0, visited)
	if err != nil {
		return content, err
	}
	return rendered, nil
}

// replaceBuiltinVars 替换内置变量
func (t *Template) replaceBuiltinVars(content string, vars TemplateVar) string {
	now := vars.Now
	if now.IsZero() {
		now = time.Now()
	}

	return varRe.ReplaceAllStringFunc(content, func(match string) string {
		sub := varRe.FindStringSubmatch(match)
		if len(sub) < 2 {
			return match
		}
		switch sub[1] {
		case "today":
			return now.Format("2006_01_02")
		case "yesterday":
			return now.AddDate(0, 0, -1).Format("2006_01_02")
		case "tomorrow":
			return now.AddDate(0, 0, 1).Format("2006_01_02")
		case "time":
			return now.Format("15:04")
		case "date":
			return now.Format("2006-01-02")
		case "page", "current_page":
			return vars.PageTitle
		default:
			return match
		}
	})
}

// expandTemplateRefs 递归展开模板引用
func (t *Template) expandTemplateRefs(content string, vars TemplateVar, depth int, visited map[string]bool) (string, error) {
	if depth > 5 {
		return content, nil
	}

	// 查找所有模板引用
	matches := templateRe.FindAllStringSubmatch(content, -1)
	if len(matches) == 0 {
		return content, nil
	}

	for _, m := range matches {
		fullMatch := m[0]
		templateName := strings.TrimSpace(m[1])
		argsStr := ""
		if len(m) > 2 {
			argsStr = strings.TrimSpace(m[2])
		}

		// 跳过内置变量（已在前面处理）
		if isBuiltinVar(templateName) {
			continue
		}

		// 防止循环引用
		if visited[templateName] {
			continue
		}
		visited[templateName] = true

		// 获取模板内容
		tmplContent, err := t.GetTemplate(templateName)
		if err != nil {
			// 模板不存在，保留原样
			continue
		}

		// 替换模板内的变量
		tmplContent = t.replaceBuiltinVars(tmplContent, vars)

		// 处理模板参数 $1, $2, ...
		if argsStr != "" {
			args := parseArgs(argsStr)
			tmplContent = replaceArgs(tmplContent, args)
		}

		// 递归展开
		expanded, _ := t.expandTemplateRefs(tmplContent, vars, depth+1, visited)
		content = strings.ReplaceAll(content, fullMatch, expanded)

		delete(visited, templateName)
	}

	return content, nil
}

// isBuiltinVar 判断是否为内置变量
func isBuiltinVar(name string) bool {
	switch name {
	case "today", "yesterday", "tomorrow", "time", "date", "page", "current_page":
		return true
	}
	return false
}

// parseArgs 解析模板参数
func parseArgs(argsStr string) []string {
	var args []string
	for _, a := range strings.Split(argsStr, ",") {
		a = strings.TrimSpace(a)
		args = append(args, a)
	}
	return args
}

// replaceArgs 替换模板中的 $1, $2 等参数
var argRe = regexp.MustCompile(`\$(\d+)`)

func replaceArgs(content string, args []string) string {
	return argRe.ReplaceAllStringFunc(content, func(match string) string {
		sub := argRe.FindStringSubmatch(match)
		if len(sub) < 2 {
			return match
		}
		idx := 0
		fmt.Sscanf(sub[1], "%d", &idx)
		if idx < 1 || idx > len(args) {
			return match
		}
		return args[idx-1]
	})
}

// ApplyTemplate 应用模板创建新页面
func (t *Template) ApplyTemplate(templateName string, pageTitle string, args []string) (string, error) {
	tmplContent, err := t.GetTemplate(templateName)
	if err != nil {
		return "", err
	}

	vars := TemplateVar{
		PageTitle: pageTitle,
		Now:       time.Now(),
	}

	rendered, err := t.Render(tmplContent, vars)
	if err != nil {
		return "", err
	}

	// 替换参数
	if len(args) > 0 {
		rendered = replaceArgs(rendered, args)
	}

	return rendered, nil
}
