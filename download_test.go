package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// TestDownloadOK 验证单次下载成功
func TestDownloadOK(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("hello world"))
	}))
	defer ts.Close()

	dir := t.TempDir()
	outPath := filepath.Join(dir, "test.txt")

	// 执行下载（第4个参数传 nil，代表不使用进度条）
	n, err := downloadOnce(context.Background(), ts.Client(), nil, nil, ts.URL, outPath)
	if err != nil {
		t.Fatalf("期望无错误，实际报错: %v", err)
	}
	if n != 11 {
		t.Fatalf("期望下载 11 字节，实际: %d", n)
	}

	// 验证文件内容
	data, _ := os.ReadFile(outPath)
	if string(data) != "hello world" {
		t.Fatalf("期望文件内容为 'hello world'，实际: %q", string(data))
	}
}

// TestResume 验证断点续传（带 Range 请求）
func TestResume(t *testing.T) {
	content := "0123456789"
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rangeHeader := r.Header.Get("Range")
		if rangeHeader == "bytes=5-" {
			w.Header().Set("Content-Range", "bytes 5-9/10")
			w.WriteHeader(http.StatusPartialContent)
			w.Write([]byte(content[5:]))
			return
		}
		w.Write([]byte(content))
	}))
	defer ts.Close()

	dir := t.TempDir()
	outPath := filepath.Join(dir, "resume.txt")
	tmpPath := outPath + ".part"

	// 预置一个 5 字节的断点文件
	if err := os.WriteFile(tmpPath, []byte(content[:5]), 0644); err != nil {
		t.Fatal(err)
	}

	// 执行续传
	n, err := downloadOnce(context.Background(), ts.Client(), nil, nil, ts.URL, outPath)
	if err != nil {
		t.Fatalf("期望无错误，实际报错: %v", err)
	}
	if n != 10 {
		t.Fatalf("期望总字节数为 10，实际: %d", n)
	}

	// 验证最终文件内容拼接正确
	data, _ := os.ReadFile(outPath)
	if string(data) != content {
		t.Fatalf("期望文件内容为 %q，实际: %q", content, string(data))
	}
}

// TestRetry5xx 验证指数退避重试机制
func TestRetry5xx(t *testing.T) {
	var requestCount int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 前两次返回 500，第三次返回 200
		count := atomic.AddInt32(&requestCount, 1)
		if count < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Write([]byte("success"))
	}))
	defer ts.Close()

	dir := t.TempDir()
	outPath := filepath.Join(dir, "retry.txt")

	// 执行带重试的下载
	n, err := download(context.Background(), ts.Client(), nil, nil, ts.URL, outPath)
	if err != nil {
		t.Fatalf("期望重试后成功，实际报错: %v", err)
	}
	if n != 7 {
		t.Fatalf("期望下载 7 字节，实际: %d", n)
	}

	// 验证是否真的请求了 3 次
	if atomic.LoadInt32(&requestCount) != 3 {
		t.Fatalf("期望请求 3 次，实际: %d", requestCount)
	}
}

// TestCancel 验证 context 取消后优雅退出，且保留 .part 文件
func TestCancel(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "100")
		// 模拟慢速服务器，每秒只发 10 个字节
		for i := 0; i < 100; i++ {
			w.Write([]byte("a"))
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			time.Sleep(10 * time.Millisecond)
		}
	}))
	defer ts.Close()

	dir := t.TempDir()
	outPath := filepath.Join(dir, "cancel.txt")

	ctx, cancel := context.WithCancel(context.Background())
	// 100ms 后主动取消
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	// 执行下载
	_, err := downloadOnce(ctx, ts.Client(), nil, nil, ts.URL, outPath)
	if err == nil {
		t.Fatalf("期望取消时返回错误，实际返回了 nil")
	}

	// 验证 .part 文件被保留了下来
	if _, err := os.Stat(outPath + ".part"); os.IsNotExist(err) {
		t.Fatalf("期望取消后保留 .part 文件，实际未找到")
	}
}
