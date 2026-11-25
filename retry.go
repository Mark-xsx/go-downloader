package main

import (
	"errors"
	"fmt"
)

type HTTPError struct {
	StatusCode int
	URL        string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("服务器返回异常状态码：%d(%s)", e.StatusCode, e.URL)
}

func isRetryable(err error) bool {
	var httpErr *HTTPError
	if errors.As(err, &httpErr) {
		return httpErr.StatusCode >= 500 && httpErr.StatusCode < 600
	}
	return true
}
