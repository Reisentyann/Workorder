package main

import (
	"flag"
	"log/slog"
	"net/http"
	"os"
	"time"

	"workbench/internal/analyzer"
	"workbench/internal/api"
	"workbench/internal/demo"
	"workbench/internal/engine"
	"workbench/internal/store"
	"workbench/internal/verifier"
)

func main() {
	addr := flag.String("addr", ":8080", "监听地址")
	dataDir := flag.String("data", "data", "JSON 数据目录")
	frontendDir := flag.String("frontend", "../frontend", "前端静态资源目录")
	demoMode := flag.Bool("demo", false, "启用演示/测试接口（/api/v1/mock/*、/api/v1/demo/*）")
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	st, err := store.New(*dataDir)
	if err != nil {
		logger.Error("init store failed", "err", err)
		os.Exit(1)
	}

	llm := analyzer.NewMockLLM()
	v := verifier.New(st.Dataset)
	eng := engine.New(llm, v, st, logger, 30*time.Second)
	eng.Start()

	demoRunner := demo.New(st, eng, llm, logger)
	h := api.New(st, eng, llm, demoRunner, logger)

	root := http.NewServeMux()
	h.Register(root)
	if *demoMode {
		h.RegisterDemo(root)
		logger.Info("demo mode enabled", "mock", "/api/v1/mock/*", "demo", "/api/v1/demo/*")
	}
	root.Handle("/", http.FileServer(http.Dir(*frontendDir)))

	logger.Info("workbench started", "addr", *addr, "frontend", *frontendDir, "data", *dataDir)
	if err := http.ListenAndServe(*addr, root); err != nil {
		logger.Error("server failed", "err", err)
		os.Exit(1)
	}
}
