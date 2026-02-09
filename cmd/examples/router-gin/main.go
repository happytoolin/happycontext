package main

import (
	"log/slog"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/happytoolin/hlog"
	slogadapter "github.com/happytoolin/hlog/adapter/slog"
	ginhlog "github.com/happytoolin/hlog/integration/gin"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	sink := slogadapter.New(logger)

	r := gin.New()
	r.Use(ginhlog.Middleware(hlog.Config{Sink: sink, SamplingRate: 1}))
	r.GET("/users/:id", func(c *gin.Context) {
		hlog.Add(c.Request.Context(), "router", "gin")
		c.Status(200)
	})

	_ = r.Run(":8102")
}
