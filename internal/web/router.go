package web

import "github.com/gin-gonic/gin"

func SetupRouter(h *Handler) *gin.Engine {
	r := gin.Default()

	r.Use(CORSMiddleware())

	r.Static("/static", "./web")
	r.StaticFile("/", "./web/app.html")
	r.StaticFile("/login", "./web/login.html")

	api := r.Group("/api")
	{
		auth := api.Group("/auth")
		{
			auth.POST("/send-code", h.SendCode)
			auth.POST("/register", h.Register)
			auth.POST("/login", h.Login)
		}

		protected := api.Group("")
		protected.Use(AuthMiddleware(h.jwtMgr))
		{
			protected.GET("/user/profile", h.GetProfile)

			protected.POST("/agent/run", h.RunAgent)
			protected.POST("/agent/stream", h.StreamAgent)
			protected.GET("/agent/ws", h.WebSocketAgent)

			// Task API (长任务能力)
			protected.POST("/agent/run-task", h.StartTask)
			protected.GET("/agent/stream-task/:taskId", h.StreamTask)
			protected.POST("/agent/abort-task/:taskId", h.AbortTask)
			protected.POST("/agent/resume-task/:taskId", h.ResumeTask)
			protected.GET("/agent/tasks", h.ListTasks)
			protected.GET("/agent/task/:taskId", h.GetTask)

			// Reflection API (反思模块)
			protected.GET("/agent/experiences", h.ListExperiences)
			protected.GET("/agent/strategies", h.ListStrategies)

			protected.GET("/chat/history", h.GetChatHistory)
			protected.DELETE("/chat/history", h.ClearChatHistory)

			protected.GET("/workspace/files", h.ListFiles)
			protected.GET("/workspace/file", h.ReadFile)
			protected.GET("/workspace/preview", h.PreviewFile)
			protected.POST("/workspace/upload", h.UploadFile)
			protected.POST("/workspace/save", h.SaveFile)
			protected.GET("/workspace/download", h.DownloadFile)
			protected.DELETE("/workspace/file", h.DeleteFile)

			protected.GET("/templates", h.ListTemplates)
			protected.GET("/skills", h.ListSkills)

			protected.GET("/sessions", h.ListSessions)
			protected.POST("/sessions", h.CreateSession)
			protected.DELETE("/sessions", h.DeleteSession)

			// Knowledge Base API (知识库系统)
			kb := protected.Group("/kb")
			{
				kb.GET("/pages", h.KBListPages)
				kb.GET("/pages/:title", h.KBGetPage)
				kb.POST("/pages", h.KBCreatePage)
				kb.PUT("/pages/:title", h.KBUpdatePage)
				kb.DELETE("/pages/:title", h.KBDeletePage)

				kb.GET("/blocks/:id", h.KBGetBlock)
				kb.PUT("/blocks/:id", h.KBUpdateBlock)

				kb.GET("/pages/:title/backlinks", h.KBGetBacklinks)
				kb.GET("/graph", h.KBGetGraph)
				kb.GET("/tags", h.KBGetTags)
				kb.GET("/tags/:tag/pages", h.KBGetPagesByTag)

				kb.GET("/search", h.KBSearch)
			kb.POST("/search/semantic", h.KBSemanticSearch)
			kb.POST("/search/hybrid", h.KBHybridSearch)
			kb.GET("/stats", h.KBGetStats)

			kb.POST("/sync", h.KBSync)
			kb.GET("/export/:title", h.KBExportPage)

			// AI 能力
			kb.POST("/qa", h.KBQA)
			kb.POST("/suggest/links", h.KBSuggestLinks)
			kb.POST("/suggest/tags", h.KBSuggestTags)
			kb.GET("/suggest/summary/:title", h.KBSuggestSummary)
			kb.GET("/insights", h.KBInsights)
			kb.POST("/embeddings/rebuild", h.KBRebuildEmbeddings)
			kb.GET("/embeddings/stats", h.KBGetEmbeddingStats)

			// 阶段 5 高级特性
			// 日记
			kb.GET("/journal/today", h.KBJournalToday)
			kb.GET("/journal/list", h.KBJournalList)
			kb.GET("/journal/template", h.KBJournalTemplate)

			// 任务管理
			kb.GET("/tasks", h.KBTaskList)
			kb.GET("/tasks/stats", h.KBTaskStats)
			kb.PUT("/tasks/:block_id/status", h.KBTaskUpdateStatus)

			// 块嵌入
			kb.GET("/blocks/:id/embed", h.KBBlockEmbed)
			kb.GET("/blocks/:id/embed/tree", h.KBBlockEmbedTree)

			// 模板系统
			kb.GET("/templates", h.KBTemplateList)
			kb.GET("/templates/:name", h.KBTemplateGet)
			kb.POST("/templates/apply", h.KBTemplateApply)

			// 版本历史
			kb.GET("/pages/:title/versions", h.KBVersionListByTitle)
			kb.GET("/versions/:page_id", h.KBVersionList)
			kb.GET("/versions/:page_id/:version", h.KBVersionGet)
			kb.GET("/versions/:page_id/diff/:from/:to", h.KBVersionDiff)
			kb.POST("/versions/:page_id/rollback/:version", h.KBVersionRollback)

			// 查询 DSL
			kb.POST("/query", h.KBQuery)

			// 导入
			kb.POST("/import", h.KBImport)

			// FTS5
			kb.POST("/fts/rebuild", h.KBRebuildFTS)
			kb.GET("/fts/search", h.KBSearchFTS)

			// 属性系统
			kb.PUT("/pages/:title/properties", h.KBSetPageProperty)
			kb.GET("/pages/:title/properties", h.KBGetPageProperties)
			kb.DELETE("/pages/:title/properties/:name", h.KBDeletePageProperty)
			kb.GET("/properties/query", h.KBQueryByProperty)
			kb.GET("/properties/names", h.KBPropertyNames)
			kb.POST("/properties/schemas", h.KBSetPropertySchema)
			kb.GET("/properties/schemas", h.KBGetPropertySchemas)

			// 收藏夹
			kb.POST("/favorites/:title", h.KBFavoriteAdd)
			kb.DELETE("/favorites/:title", h.KBFavoriteRemove)
			kb.GET("/favorites", h.KBFavoriteList)

			// 最近访问
			kb.GET("/recent", h.KBRecentList)

			// 回收站
			kb.GET("/recycle", h.KBRecycleList)
			kb.POST("/recycle/:id/restore", h.KBRecycleRestore)
			kb.DELETE("/recycle/:id", h.KBRecycleDelete)
			kb.DELETE("/recycle", h.KBRecycleEmpty)

			// 块排序/移动
			kb.POST("/blocks/reorder", h.KBReorderBlock)
			kb.POST("/blocks/move", h.KBMoveBlock)

			// Unlinked References
			kb.GET("/pages/:title/unlinked", h.KBUnlinkedRefs)

			// 导出增强
			kb.GET("/export/:title/html", h.KBExportHTML)
			kb.GET("/export/json", h.KBExportJSON)

			// AI 增强
			kb.POST("/suggest/continue", h.KBSuggestContinue)
			kb.POST("/graph/qa", h.KBGraphQA)
			kb.POST("/auto-organize", h.KBAutoOrganize)
		}
		}
	}

	return r
}
