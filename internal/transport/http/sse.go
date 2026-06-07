package http

import (
	"encoding/json"
	"fmt"
	stdhttp "net/http"

	"github.com/gin-gonic/gin"
)

func prepareSSE(c *gin.Context) {
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")
	c.Status(stdhttp.StatusOK)
	c.Writer.Flush()
}

func writeSSE(c *gin.Context, event string, body any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(c.Writer, "event: %s\n", event); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(c.Writer, "data: %s\n\n", data); err != nil {
		return err
	}
	c.Writer.Flush()
	return nil
}
