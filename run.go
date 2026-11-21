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

	sem := make(chan struct{}, maxConc)

	for i, rawURL := range urls {
		rawURL = strings.TrimSpace(rawURL)
		if rawURL == "" {
			continue
		}

		wg.Add(1)
		go func(index int, u string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			outPath := filepath.Join(*outDir, fmt.Sprintf("out_%d", index+1))
			fmt.Printf("[%d]正在下载：%s\n", index+1, u)

			n, err := download(client, u, outPath)
			results[index] = Result{URL: u, Bytes: n, Err: err}

		}(i, rawURL)
	}
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
