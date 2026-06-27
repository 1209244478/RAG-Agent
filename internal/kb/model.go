package kb

// Page 表示知识库中的一个页面（对应一个 Markdown 文件）
type Page struct {
	ID         int64          `json:"id"`
	Title      string         `json:"title"`       // 页面标题
	FileName   string         `json:"file_name"`   // 对应的文件名（相对路径）
	Namespace  string         `json:"namespace"`   // 命名空间，如 "项目/子项目"
	IsJournal  bool           `json:"is_journal"`  // 是否为日记页面
	Tags       []string       `json:"tags"`        // 页面标签
	Properties map[string]any `json:"properties"`  // 页面属性（frontmatter）
	CreatedAt  string         `json:"created_at"`
	UpdatedAt  string         `json:"updated_at"`
}

// Block 表示页面中的一个块（段落/列表项/标题等）
type Block struct {
	ID         string         `json:"id"`          // UUID
	PageID     int64          `json:"page_id"`     // 所属页面ID
	Content    string         `json:"content"`     // 块文本内容（纯文本）
	Order      int            `json:"order"`       // 块在页面中的顺序
	Level      int            `json:"level"`       // 缩进层级（0=顶级）
	ParentID   string         `json:"parent_id"`   // 父块ID（空=顶级块）
	BlockType  string         `json:"block_type"`  // "paragraph" | "heading" | "list" | "quote" | "code"
	Properties map[string]any `json:"properties"`  // 块属性
	CreatedAt  string         `json:"created_at"`
	UpdatedAt  string         `json:"updated_at"`
}

// Link 表示块之间的链接关系
type Link struct {
	ID              int64  `json:"id"`
	SourceBlockID   string `json:"source_block_id"`   // 源块ID
	TargetPageID    int64  `json:"target_page_id"`    // 目标页面ID（页面链接时）
	TargetBlockID   string `json:"target_block_id"`   // 目标块ID（块引用时）
	TargetPageTitle string `json:"target_page_title"` // 目标页面标题（冗余，方便查询）
	LinkType        string `json:"link_type"`         // "page_ref" | "block_ref" | "tag"
	Context         string `json:"context"`           // 链接上下文（块内容片段）
}

// GraphNode 图谱节点（用于图谱视图）
type GraphNode struct {
	ID    int64  `json:"id"`
	Label string `json:"label"`  // 页面标题
	Type  string `json:"type"`   // "page" | "tag"
	Size  int    `json:"size"`   // 节点大小（关联数）
}

// GraphEdge 图谱边（用于图谱视图）
type GraphEdge struct {
	Source int64  `json:"source"` // 源页面ID
	Target int64  `json:"target"` // 目标页面ID
	Type   string `json:"type"`   // "link" | "tag"
}

// GraphData 图谱数据
type GraphData struct {
	Nodes []GraphNode `json:"nodes"`
	Edges []GraphEdge `json:"edges"`
}

// SearchResult 搜索结果
type SearchResult struct {
	PageID    int64  `json:"page_id"`
	Title     string `json:"title"`
	BlockID   string `json:"block_id"`
	Content   string `json:"content"`
	Snippet   string `json:"snippet"`   // 匹配片段
	Score     float64 `json:"score"`    // 相关度分数
	Type      string `json:"type"`      // "page" | "block"
}

// Backlink 反向引用
type Backlink struct {
	PageID      int64  `json:"page_id"`
	PageTitle   string `json:"page_title"`
	BlockID     string `json:"block_id"`
	BlockContent string `json:"block_content"`
	LinkType    string `json:"link_type"`
	Context     string `json:"context"`
}
