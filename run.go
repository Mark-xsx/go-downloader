package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Result struct {
	URL   string
	Bytes int64
	Err   error
}

func run() error {
	urlsFlag := flag.String("u", "", "要下载的URL，多个用逗号分隔")
	outDir := flag.String("d", ".", "保存目录")
	concurrent := flag.Int("c", 3, "最大并发数")
	flag.Parse()

	if *urlsFlag == "" {
		return fmt.Errorf("用法：go run . -u <url1>,<url2> [-c 3]")
	}

	maxConc := *concurrent
	if maxConc < 1 {
		maxConc = 1
	}

	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		return fmt.Errorf("创建输出目录%s失败：%w", *outDir, err)
	}

	urls := strings.Split(*urlsFlag, ",")
	client := &http.Client{Timeout: 30 * time.Second}

	startTime := time.Now()
	fmt.Printf("开始并发下载%d个任务...\n", len(urls))

	results := make([]Result, len(urls))
	var wg sync.WaitGroup

	//创建一个无缓冲的任务通道
	jobs := make(chan int)

	//启动固定的N个worker
	for w := 1; w <= maxConc; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			//Worker永不停歇从通道里取任务
			for i := range jobs {
				rawURL := urls[i]
				outPath := filepath.Join(*outDir, fmt.Sprintf("out_%d", i+1))
				fmt.Printf("[Worker%d]开始下载：%s\n", workerID, rawURL)
				n, err := download(client, rawURL, outPath)
				results[i] = Result{URL: rawURL, Bytes: n, Err: err}
			}
			fmt.Printf("[Worker%d]任务已全部完成\n", workerID)
		}(w)
	}

	//主协程派发任务
	for i, rawURL := range urls {
		rawURL = strings.TrimSpace(rawURL)
		if rawURL == "" {
			continue
		}
		jobs <- i
	}

	close(jobs)

	wg.Wait()

	//打印报告逻辑...
	totalTime := time.Since(startTime)
	successCount, failCount := 0, 0
	fmt.Println("---------------------------")
	fmt.Printf("并发下载结束，总耗时：%v\n", totalTime)
	for _, res := range results {
		if res.URL == "" {
			continue
		}
		if res.Err != nil {
			fmt.Printf("失败：%s(原因:%v)\n", res.URL, res.Err)
			failCount++
		} else {
			fmt.Printf("成功：%s(%d字节)\n", res.URL, res.Bytes)
			successCount++
		}
	}
	fmt.Printf("成功：%d个，失败：%d个\n", successCount, failCount)
	return nil

}
