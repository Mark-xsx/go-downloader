package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
)

// download 负责下载单个文件
func download(client *http.Client, rawURL, outPath string) (int64, error) {
	resp, err := client.Get(rawURL)
	if err != nil {
		return 0, fmt.Errorf("请求失败：%w", err)

	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("状态码异常：%d", resp.StatusCode)
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
		return n, fmt.Errorf("写入失败:%w", err)
	}
	return n, nil

}
