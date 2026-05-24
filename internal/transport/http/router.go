package http

import "github.com/gin-gonic/gin"

func newRouter(handlers *handlers) *gin.Engine {
	router := gin.Default()

	router.GET("/health", handlers.health)

	api := router.Group("/api/v1")
	api.POST("/memories", handlers.createMemory)
	api.POST("/memories/search", handlers.searchMemory)

	return router
}
