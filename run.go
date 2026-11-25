package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/schollz/progressbar/v3"
	"golang.org/x/time/rate"
)

type Result struct {
	URL   string
	Bytes int64
	Err   error
}

func run(ctx context.Context) error {
	urlsFlag := flag.String("u", "", "要下载的URL，多个用逗号分隔")
	outDir := flag.String("d", ".", "保存目录")
	concurrent := flag.Int("c", 3, "最大并发数")
	rateLimit := flag.Int("rate", 0, "限速 (KB/s)，0 表示不限速")
	flag.Parse()

	if *urlsFlag == "" {
		return fmt.Errorf("用法: go run . -u <url1>,<url2> [-c 3] [-rate 100]")
	}

	maxConc := *concurrent
	if maxConc < 1 {
		maxConc = 1
	}

	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		return fmt.Errorf("创建输出目录 %s 失败: %w", *outDir, err)
	}

	var limiter *rate.Limiter
	if *rateLimit > 0 {
		limitBytes := rate.Limit(*rateLimit * 1024)
		limiter = rate.NewLimiter(limitBytes, *rateLimit*1024)
		fmt.Printf("开启限速: %d KB/s\n", *rateLimit)
	}

	urls := strings.Split(*urlsFlag, ",")

	// 【修复】移除 Client.Timeout，改用 Transport 进行分阶段超时控制
	client := &http.Client{
		Transport: &http.Transport{
			DialContext:           (&net.Dialer{Timeout: 10 * time.Second}).DialContext,
			ResponseHeaderTimeout: 15 * time.Second,
			IdleConnTimeout:       90 * time.Second,
		},
	}

	startTime := time.Now()
	fmt.Printf("开始 Worker Pool 并发下载，任务数: %d，并发数: %d\n", len(urls), maxConc)

	results := make([]Result, len(urls))
	var wg sync.WaitGroup

	jobs := make(chan int)

	// 【修复】创建一个全局进度条，所有 Worker 共享。传 -1 表示总大小未知，自动累加
	bar := progressbar.DefaultBytes(-1, "总下载进度")

	for w := 1; w <= maxConc; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for i := range jobs {
				rawURL := urls[i]
				outPath := filepath.Join(*outDir, fmt.Sprintf("out_%d", i+1))

				// 将全局 bar 传给 download
				n, err := download(ctx, client, limiter, bar, rawURL, outPath)
				results[i] = Result{URL: rawURL, Bytes: n, Err: err}
			}
		}(w)
	}

	// 【修复】使用带标签的 break 替代 goto
dispatch:
	for i, rawURL := range urls {
		rawURL = strings.TrimSpace(rawURL)
		if rawURL == "" {
			continue
		}

		select {
		case <-ctx.Done():
			fmt.Println("\n收到中断信号，停止派发新任务...")
			break dispatch
		case jobs <- i:
			// 正常派发
		}
	}

	close(jobs)
	wg.Wait()

	// 结束进度条换行
	fmt.Println()

	totalTime := time.Since(startTime)
	successCount, failCount := 0, 0
	fmt.Println("----------------------------------------")
	fmt.Printf("下载结束! 总耗时: %v\n", totalTime)

	for _, res := range results {
		if res.URL == "" {
			continue
		}
		if res.Err != nil {
			fmt.Printf("  ❌ 失败: %s (原因: %v)\n", res.URL, res.Err)
			failCount++
		} else {
			fmt.Printf("  ✅ 成功: %s (%d 字节)\n", res.URL, res.Bytes)
			successCount++
		}
	}
	fmt.Printf("成功: %d 个, 失败: %d 个\n", successCount, failCount)

	return nil
}
