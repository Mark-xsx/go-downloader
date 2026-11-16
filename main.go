package main

import (
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

func download(client *http.Client, rawURL, outPath string) (int64, error) {
	resp, err := client.Get(rawURL)
	if err != nil {
		return 0, fmt.Errorf("请求失败：%w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("状态码异常：%w", resp.StatusCode)
	}

	f, err := os.Create(outPath)
	if err != nil {
		return 0, fmt.Errorf("创建文件失败：%w", err)
	}
	defer f.Close()

	n, err := io.Copy(f, resp.Body)
	if err != nil {
		f.Close()
		os.Remove(outPath)
		return n, fmt.Errorf("写入失败：%w", err)

	}
	return n, nil

}

// run 负责解析参数、串行调度、统计结果
func run() error {
	//1.解析参数（接收逗号分隔的多个URL）
	urlsFlag := flag.String("u", "", "要下载的URLL，多个用逗号分隔")
	outDir := flag.String("d", ".", "保存目录（暂时不用，目前固定保存到当前目录）")
	flag.Parse()

	if *urlsFlag == "" {
		return fmt.Errorf("用法：go run main.go -u <urls1>,<urls2>,<urls3>")

	}

	//把"urls1,urls2"切割成切片["urls1","urls2"]
	urls := strings.Split(*urlsFlag, ",")

	//2.创建带超时的客户端（全程序共用一个连接池）
	client := &http.Client{Timeout: 30 * time.Second}

	//3.准备统计变量
	startTime := time.Now()
	successCount := 0
	failCount := 0

	fmt.Printf("开始串行下载%d个任务...\n", len(urls))

	//4.串行循环下载（核心）
	for i, rawURL := range urls {
		rawURL = strings.TrimSpace(rawURL)
		if rawURL == "" {
			continue
		}
		//生成输出文件名（用out_1,out_2命名）
		outPath := fmt.Sprintf("%s/out_%d", *outDir, i+1)
		fmt.Printf("[%d/%d]正在下载：%s -> %s\n", i+1, len(urls), rawURL, outPath)

		//调用单文件下载
		n, err := download(client, rawURL, outPath)
		if err != nil {
			fmt.Printf("失败：%v\n", err)
			failCount++
			//注意这里没有return,也没有break，所以哪怕失败，循环也会继续
		} else {
			fmt.Printf("成功：%d 字节\n", n)
			successCount++
		}
	}

	//汇总报告
	totalTime := time.Since(startTime)
	fmt.Println("-------------------------")
	fmt.Printf("下载结束，总耗时：%v\n", totalTime)
	fmt.Printf("成功：%d个，失败：%d个\n", successCount, failCount)

	return nil
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
