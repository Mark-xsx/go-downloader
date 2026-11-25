package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// Result 存储单个任务的执行结果
type Result struct {
	URL   string
	Bytes int64
	Err   error
}

// run 接收由 main 传入的 ctx，用于在按下 Ctrl+C 时优雅退出
func run(ctx context.Context) error {
	// 1. 解析命令行参数
	urlsFlag := flag.String("u", "", "要下载的URL，多个用逗号分隔")
	outDir := flag.String("d", ".", "保存目录")
	concurrent := flag.Int("c", 3, "最大并发数")
	rateLimit := flag.Int("rate", 0, "限速 (KB/s)，0 表示不限速") // 【阶段6新增】
	flag.Parse()

	if *urlsFlag == "" {
		return fmt.Errorf("用法: go run . -u <url1>,<url2> [-c 3] [-rate 100]")
	}

	maxConc := *concurrent
	if maxConc < 1 {
		maxConc = 1
	}

	// 2. 准备输出目录
	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		return fmt.Errorf("创建输出目录 %s 失败: %w", *outDir, err)
	}

	// 3. 初始化限速器（令牌桶）【阶段6新增】
	var limiter *rate.Limiter
	if *rateLimit > 0 {
		// rate.Limit 的参数单位是 字节/秒，所以 KB/s 要乘以 1024
		limitBytes := rate.Limit(*rateLimit * 1024)
		// 桶容量设为 1 秒的令牌数，允许一点点突发
		limiter = rate.NewLimiter(limitBytes, *rateLimit*1024)
		fmt.Printf("开启限速: %d KB/s\n", *rateLimit)
	}

	urls := strings.Split(*urlsFlag, ",")
	client := &http.Client{Timeout: 30 * time.Second}

	startTime := time.Now()
	fmt.Printf("开始 Worker Pool 并发下载，任务数: %d，并发数: %d\n", len(urls), maxConc)

	results := make([]Result, len(urls))
	var wg sync.WaitGroup

	// 无缓冲任务通道
	jobs := make(chan int)

	// 4. 启动 N 个 Worker
	for w := 1; w <= maxConc; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for i := range jobs {
				rawURL := urls[i]
				outPath := filepath.Join(*outDir, fmt.Sprintf("out_%d", i+1))

				fmt.Printf("[Worker %d] 开始下载: %s\n", workerID, rawURL)

				// 【阶段6修改】透传 ctx 和 limiter 给 download 函数
				n, err := download(ctx, client, limiter, rawURL, outPath)
				results[i] = Result{URL: rawURL, Bytes: n, Err: err}
			}
			fmt.Printf("[Worker %d] 任务已全部完成，下班。\n", workerID)
		}(w)
	}

	// 5. 主协程派发任务
	for i, rawURL := range urls {
		rawURL = strings.TrimSpace(rawURL)
		if rawURL == "" {
			continue
		}

		// 【阶段6防坑】在派发任务时，也要监听 ctx 的取消信号
		// 如果用户按了 Ctrl+C，主协程不要再傻傻地往 jobs 里塞任务了
		select {
		case <-ctx.Done():
			fmt.Println("收到中断信号，停止派发新任务...")
			goto WaitWorkers // 跳出循环
		case jobs <- i:
			// 正常派发
		}
	}

WaitWorkers:
	// 6. 关闭通道，等待 Worker 下班
	close(jobs)
	wg.Wait()

	// 7. 汇总报告
	totalTime := time.Since(startTime)
	successCount, failCount := 0, 0
	fmt.Println("\n----------------------------------------")
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
