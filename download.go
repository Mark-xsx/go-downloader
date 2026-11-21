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

	tmpPath := outPath + ".part"
	f, err := os.Create(tmpPath)
	if err != nil {
		return 0, fmt.Errorf("创建文件失败：%w", err)
	}

	n, err := io.Copy(f, resp.Body)
	if err != nil {
		f.Close()
		os.Remove(tmpPath)
		return n, fmt.Errorf("写入失败:%w", err)
	}

	if err := f.Close(); err != nil {
		os.Remove(tmpPath)
		return n, fmt.Errorf("文件；落盘失败：%w", err)
	}

	if err := os.Rename(tmpPath, outPath); err != nil {
		os.Remove(tmpPath)
		return n, fmt.Errorf("重命名失败：%w", err)
	}

	return n, nil

}
