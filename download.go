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

// downloadOnce 执行单次下载
func downloadOnce(ctx context.Context, client *http.Client, limiter *rate.Limiter, rawURL, outPath string) (int64, error) {
	tmpPath := outPath + ".part"

	// 1. 检查本地是否已有下载了一半的临时文件
	var localSize int64 = 0
	if info, err := os.Stat(tmpPath); err == nil {
		localSize = info.Size()
	}

	// 2. 创建带 ctx 的请求（这样在等待服务器响应时按 Ctrl+C 也能中断）
	req, err := http.NewRequestWithContext(ctx, "GET", rawURL, nil)
	if err != nil {
		return 0, fmt.Errorf("创建请求失败: %w", err)
	}

	// 3. 如果存在本地断点，添加 Range 请求头
	if localSize > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", localSize))
	}

	// 4. 发送请求
	resp, err := client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()

	// 5. 处理服务器返回的状态码
	switch resp.StatusCode {
	case http.StatusOK:
		// 200 OK。如果 localSize > 0 说明服务器不支持断点续传，直接返回了完整文件
		if localSize > 0 {
			// 必须从零开始重下
			localSize = 0
		}
	case http.StatusPartialContent:
		// 206 Partial Content。完美的断点续传，接着写
	case http.StatusRequestedRangeNotSatisfiable:
		// 416 范围请求错误。通常说明本地 .part 文件已经大于等于服务器文件了（已下完）
		if err := os.Rename(tmpPath, outPath); err != nil {
			return 0, fmt.Errorf("重命名失败: %w", err)
		}
		return localSize, nil
	default:
		return 0, &HTTPError{StatusCode: resp.StatusCode, URL: rawURL}
	}

	// 6. 打开文件（续传用 O_APPEND，重下用 O_TRUNC）
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

	// 7. 初始化进度条
	totalSize := resp.ContentLength
	if totalSize > 0 {
		totalSize += localSize // 总大小 = 剩余大小 + 已下载大小
	}

	// 使用 progressbar.DefaultBytes 自动处理单位换算和速度计算
	bar := progressbar.DefaultBytes(totalSize, "下载中")
	if localSize > 0 {
		bar.Add64(localSize) // 如果是从断点续传，直接把进度条填到对应位置
	}

	// 8. 循环读取写入（为了支持 ctx 取消和限速）
	buf := make([]byte, 32*1024) // 32KB 缓冲区
	var written int64

	for {
		// 检查是否收到了取消信号 (Ctrl+C)
		select {
		case <-ctx.Done():
			f.Close()
			return written, fmt.Errorf("用户取消下载")
		default:
		}

		// 【修正点】动态计算本次读取量，绝对不能超过桶的容量
		readLen := len(buf)
		if limiter != nil {
			// 如果桶容量小于缓冲区，就把本次读取量缩小到桶容量
			if limiter.Burst() > 0 && readLen > limiter.Burst() {
				readLen = limiter.Burst()
			}
			// 去拿令牌，现在 readLen 绝对小于等于桶容量，不会再报错了
			if err := limiter.WaitN(ctx, readLen); err != nil {
				f.Close()
				return written, fmt.Errorf("限速等待被取消: %w", err)
			}
		}

		// 读取网络数据，注意这里要用 buf[:readLen]
		nr, er := resp.Body.Read(buf[:readLen])
		if nr > 0 {
			// 写入本地文件
			nw, ew := f.Write(buf[:nr])
			if nw > 0 {
				written += int64(nw)
				bar.Add(nw) // 更新进度条
			}
			if ew != nil {
				f.Close()
				return written, fmt.Errorf("写入文件失败: %w", ew)
			}
		}

		if er != nil {
			if er == io.EOF {
				break // 正常读完
			}
			f.Close()
			return written, fmt.Errorf("读取响应体失败: %w", er)
		}
	}
	// 9. 落盘并原子重命名
	if err := f.Close(); err != nil {
		return written, fmt.Errorf("文件落盘失败: %w", err)
	}

	if err := os.Rename(tmpPath, outPath); err != nil {
		return written, fmt.Errorf("重命名文件失败: %w", err)
	}

	// 返回：本次写入大小 + 之前已经存在的大小
	return written + localSize, nil
}

// download 包含重试与指数退避逻辑
func download(ctx context.Context, client *http.Client, limiter *rate.Limiter, rawURL, outPath string) (int64, error) {
	maxRetries := 3
	var lastErr error

	for attempt := 1; attempt <= maxRetries; attempt++ {
		// 透传 ctx 和 limiter
		n, err := downloadOnce(ctx, client, limiter, rawURL, outPath)
		if err == nil {
			return n, nil // 成功，立刻返回
		}

		lastErr = err

		// 【防坑】如果错误是因为用户按了 Ctrl+C，不要再重试了，直接退出
		if ctx.Err() != nil {
			return n, err
		}

		// 遇到不可重试的错误（如 404），直接失败
		if !isRetryable(err) {
			return n, err
		}

		// 如果是最后一次尝试，直接退出
		if attempt == maxRetries {
			break
		}

		// 指数退避：1s, 2s, 4s...
		backoff := time.Duration(1<<uint(attempt-1)) * time.Second
		fmt.Printf("  ⚠️ 遇到错误: %v，%v 后进行第 %d 次重试...\n", err, backoff, attempt+1)

		// 退避等待时也要监听 ctx，万一用户在睡觉的时候按了取消呢？
		select {
		case <-ctx.Done():
			return n, fmt.Errorf("用户取消下载")
		case <-time.After(backoff):
			// 睡饱了，继续重试
		}
	}

	return 0, fmt.Errorf("重试 %d 次后仍然失败: %w", maxRetries, lastErr)
}
