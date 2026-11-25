package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/schollz/progressbar/v3"
	"golang.org/x/time/rate"
)

// download 包含重试与指数退避逻辑
func download(ctx context.Context, client *http.Client, limiter *rate.Limiter, bar *progressbar.ProgressBar, rawURL, outPath string) (int64, error) {
	maxRetries := 3
	var lastErr error
	var lastBytes int64

	for attempt := 1; attempt <= maxRetries; attempt++ {
		n, err := downloadOnce(ctx, client, limiter, bar, rawURL, outPath)
		if err == nil {
			return n, nil
		}

		lastErr = err
		lastBytes = n // 【修复】记录最后一次尝试的字节数

		if ctx.Err() != nil {
			return lastBytes, err
		}

		if !isRetryable(err) {
			return lastBytes, err
		}

		if attempt == maxRetries {
			break
		}

		backoff := time.Duration(1<<uint(attempt-1)) * time.Second
		fmt.Printf("\n  ⚠️ 遇到错误: %v，%v 后进行第 %d 次重试...\n", err, backoff, attempt+1)

		select {
		case <-ctx.Done():
			return lastBytes, fmt.Errorf("用户取消下载")
		case <-time.After(backoff):
			// 继续重试
		}
	}

	// 【修复】失败时返回实际已下载的字节数，而不是 0
	return lastBytes, fmt.Errorf("重试 %d 次后仍然失败: %w", maxRetries, lastErr)
}

// downloadOnce 执行单次下载
func downloadOnce(ctx context.Context, client *http.Client, limiter *rate.Limiter, bar *progressbar.ProgressBar, rawURL, outPath string) (int64, error) {
	tmpPath := outPath + ".part"

	var localSize int64 = 0
	if info, err := os.Stat(tmpPath); err == nil {
		localSize = info.Size()
	}

	req, err := http.NewRequestWithContext(ctx, "GET", rawURL, nil)
	if err != nil {
		return 0, fmt.Errorf("创建请求失败: %w", err)
	}

	if localSize > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", localSize))
	}

	resp, err := client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		if localSize > 0 {
			localSize = 0
		}
	case http.StatusPartialContent:
		// 206 断点续传正常响应
	case http.StatusRequestedRangeNotSatisfiable:
		if err := os.Rename(tmpPath, outPath); err != nil {
			return 0, fmt.Errorf("重命名失败: %w", err)
		}
		return localSize, nil
	default:
		return 0, &HTTPError{StatusCode: resp.StatusCode, URL: rawURL}
	}

	flags := os.O_CREATE | os.O_WRONLY
	if localSize > 0 {
		flags |= os.O_APPEND
	} else {
		flags |= os.O_TRUNC
	}

	f, err := os.OpenFile(tmpPath, flags, 0644)
	if err != nil {
		return 0, fmt.Errorf("打开文件失败: %w", err)
	}

	// 【修复】直接使用传入的全局 bar。
	// 如果是续传，进度条直接加上本地已存在的部分（这会导致全局总进度瞬间跳跃，视觉上合理）
	if bar != nil && localSize > 0 {
		bar.Add64(localSize)
	}

	buf := make([]byte, 32*1024)
	var written int64

	for {
		select {
		case <-ctx.Done():
			f.Close()
			// 【修复】返回已下载字节数
			return written + localSize, fmt.Errorf("用户取消下载")
		default:
		}

		readLen := len(buf)
		if limiter != nil {
			if limiter.Burst() > 0 && readLen > limiter.Burst() {
				readLen = limiter.Burst()
			}
			if err := limiter.WaitN(ctx, readLen); err != nil {
				f.Close()
				return written + localSize, fmt.Errorf("限速等待被取消: %w", err)
			}
		}

		nr, er := resp.Body.Read(buf[:readLen])
		if nr > 0 {
			nw, ew := f.Write(buf[:nr])
			if nw > 0 {
				written += int64(nw)
				// 2. 如果传入了 bar，才更新进度
				if bar != nil {
					bar.Add(nw)
				}
			}
			if ew != nil {
				f.Close()
				return written + localSize, fmt.Errorf("写入文件失败: %w", ew)
			}
		}

		if er != nil {
			if er == io.EOF {
				break
			}
			f.Close()
			return written + localSize, fmt.Errorf("读取响应体失败: %w", er)
		}
	}

	if err := f.Close(); err != nil {
		return written + localSize, fmt.Errorf("文件落盘失败: %w", err)
	}

	if err := os.Rename(tmpPath, outPath); err != nil {
		return written + localSize, fmt.Errorf("重命名文件失败: %w", err)
	}

	return written + localSize, nil
}
