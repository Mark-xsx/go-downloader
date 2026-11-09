package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
)

func main() {
	if len(os.Args) < 3 {
		fmt.Println("用法：go run main.go <url> <output>")
		return
	}

	url := os.Args[1]
	output := os.Args[2]

	//发请求
	resp, err := http.Get(url)
	if err != nil {
		fmt.Println("请求失败：", err)
		return
	}
	defer resp.Body.Close()

	//检查状态码
	if resp.StatusCode != http.StatusOK {
		fmt.Println("状态码异常：", resp.StatusCode)
		return
	}

	//创建文件
	f, err := os.Create(output)
	if err != nil {
		fmt.Println("创建文件失败：", err)
		return
	}
	defer f.Close()

	//把响应体写入文件
	n, err := io.Copy(f, resp.Body)
	if err != nil {
		fmt.Println("写入失败：", err)
		return

	}
	fmt.Printf("下载完成：%d 字节 -> %s\n", n, output)

}
