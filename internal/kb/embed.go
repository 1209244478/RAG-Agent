package kb

import (
	"fmt"
	"regexp"
	"strings"
)

// Embed 块嵌入引擎，处理 {{embed ((block-id))}} 指令
type Embed struct {
	store    *Store
	maxDepth int // 最大嵌套深度，防止循环
}

// embedRe 匹配 {{embed ((block-uuid))}}
var embedRe = regexp.MustCompile(`\{\{embed\s*\(\(([0-9a-fA-F-]{36})\)\)\s*\}\}`)

// NewEmbed 创建嵌入引擎
func NewEmbed(store *Store) *Embed {
	return &Embed{store: store, maxDepth: 5}
}

// HasEmbed 检查内容是否包含嵌入指令
func (e *Embed) HasEmbed(content string) bool {
	return embedRe.MatchString(content)
}

// FindEmbeds 查找内容中所有嵌入的块 ID
func (e *Embed) FindEmbeds(content string) []string {
	matches := embedRe.FindAllStringSubmatch(content, -1)
	var ids []string
	for _, m := range matches {
		if len(m) > 1 {
			ids = append(ids, m[1])
		}
	}
	return ids
}

// ExpandBlock 展开单个块的嵌入内容
// 返回展开后的内容（递归展开嵌套嵌入）
func (e *Embed) ExpandBlock(blockID string) (string, error) {
	visited := make(map[string]bool)
	return e.expandBlockRecursive(blockID, 0, visited)
}

func (e *Embed) expandBlockRecursive(blockID string, depth int, visited map[string]bool) (string, error) {
	if depth > e.maxDepth {
		return "[embed: max depth reached]", nil
	}
	if visited[blockID] {
		return "[embed: circular reference]", nil
	}
	visited[blockID] = true

	block, err := e.store.GetBlock(blockID)
	if err != nil {
		return "", fmt.Errorf("get block %s: %w", blockID, err)
	}
	if block == nil {
		return "[embed: block not found]", nil
	}

	content := block.Content
	if !e.HasEmbed(content) {
		return content, nil
	}

	// 递归展开所有嵌入
	content = embedRe.ReplaceAllStringFunc(content, func(match string) string {
		sub := embedRe.FindStringSubmatch(match)
		if len(sub) < 2 {
			return match
		}
		expanded, err := e.expandBlockRecursive(sub[1], depth+1, visited)
		if err != nil {
			return "[embed: error]"
		}
		// 嵌入内容缩进对齐
		return expanded
	})

	return content, nil
}

// ExpandPageBlocks 展开页面所有块中的嵌入指令
// 返回新的块列表（不修改原块）
func (e *Embed) ExpandPageBlocks(blocks []Block) []Block {
	result := make([]Block, len(blocks))
	for i, b := range blocks {
		if e.HasEmbed(b.Content) {
			expanded, err := e.ExpandBlock(b.ID)
			if err == nil {
				b.Content = expanded
			}
		}
		result[i] = b
	}
	return result
}

// ExpandText 展开文本中的嵌入指令（用于预览）
func (e *Embed) ExpandText(text string) string {
	if !e.HasEmbed(text) {
		return text
	}
	return embedRe.ReplaceAllStringFunc(text, func(match string) string {
		sub := embedRe.FindStringSubmatch(match)
		if len(sub) < 2 {
			return match
		}
		block, err := e.store.GetBlock(sub[1])
		if err != nil || block == nil {
			return "[embed: not found]"
		}
		// 递归展开
		expanded, err := e.ExpandBlock(sub[1])
		if err != nil {
			return "[embed: error]"
		}
		return expanded
	})
}

// EmbedTree 返回嵌入的树形结构（用于调试和可视化）
type EmbedNode struct {
	BlockID  string      `json:"block_id"`
	Content  string      `json:"content"`
	Children []EmbedNode `json:"children,omitempty"`
	Error    string      `json:"error,omitempty"`
}

// GetEmbedTree 获取嵌入树
func (e *Embed) GetEmbedTree(blockID string) EmbedNode {
	visited := make(map[string]bool)
	return e.getEmbedTreeRecursive(blockID, 0, visited)
}

func (e *Embed) getEmbedTreeRecursive(blockID string, depth int, visited map[string]bool) EmbedNode {
	node := EmbedNode{BlockID: blockID}

	if depth > e.maxDepth {
		node.Error = "max depth reached"
		return node
	}
	if visited[blockID] {
		node.Error = "circular reference"
		return node
	}
	visited[blockID] = true

	block, err := e.store.GetBlock(blockID)
	if err != nil {
		node.Error = "fetch error"
		return node
	}
	if block == nil {
		node.Error = "not found"
		return node
	}

	node.Content = block.Content
	embedIDs := e.FindEmbeds(block.Content)
	for _, id := range embedIDs {
		child := e.getEmbedTreeRecursive(id, depth+1, visited)
		node.Children = append(node.Children, child)
	}
	return node
}

// RenderEmbedTree 将嵌入树渲染为缩进文本
func RenderEmbedTree(node EmbedNode, indent int) string {
	var sb strings.Builder
	prefix := strings.Repeat("  ", indent)
	if node.Error != "" {
		sb.WriteString(prefix + "[ERROR: " + node.Error + "]\n")
		return sb.String()
	}
	// 只取第一行作为摘要
	lines := strings.SplitN(node.Content, "\n", 2)
	sb.WriteString(prefix + lines[0] + "\n")
	for _, child := range node.Children {
		sb.WriteString(RenderEmbedTree(child, indent+1))
	}
	return sb.String()
}
