# Go Downloader

一个使用 Go 语言编写的轻量级、高并发文件下载器。

## ✨ 核心特性

- **多任务并发**：基于 `goroutine` 与 `sync.WaitGroup` 实现多 URL 并发下载。
- **精准限流控制**：使用带缓冲的 `channel` 实现信号量（Semaphore），可通过 `-c` 参数控制最大并发数，防止文件句柄耗尽。
- **数据安全保护**：引入 `.part` 临时文件机制。下载完成且落盘成功后才原子重命名，避免下载失败破坏用户原有同名文件。
- **优雅的错误处理**：单个任务失败不会影响其他任务（降级容错），并统一进行错误统计与报告。
- **跨平台兼容**：使用 `filepath.Join` 拼接路径，完美兼容 Linux/Windows/macOS。

## 🚀 快速开始

### 编译与运行

```bash
# 下载单个文件
go run . -u "https://example.com/file.zip"

# 下载多个文件（用英文逗号分隔），并指定保存目录
go run . -u "https://a.com/1.png,https://b.com/2.png" -d "./downloads"

# 自定义并发数（默认 3）
go run . -u "https://a.com,https://b.com" -c 5