package main

import (
	"crypto/tls"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/imroc/req/v3"
)

// ProxyRequest 定义客户端请求结构
type ProxyRequest struct {
	Method  string            `json:"method"`  // GET/POST/PUT/DELETE
	URL     string            `json:"url"`     // 目标 URL
	Headers map[string]string `json:"headers"` // 可选请求头
	Body    interface{}       `json:"body"`    // 可选 body
	Timeout int               `json:"timeout"` // 毫秒，可选
}

func main() {
	port := flag.Int("p", 57571, "port to listen on")
	flag.Parse()

	// 创建 req/v3 客户端，忽略 HTTPS 证书验证
	client := req.C().
		SetTLSClientConfig(&tls.Config{InsecureSkipVerify: true})

	// /proxy 接口
	http.HandleFunc("/proxy", func(w http.ResponseWriter, r *http.Request) {
		var reqBody ProxyRequest

		if r.Method == http.MethodGet {
			// GET 请求参数从 query
			reqBody.Method = r.URL.Query().Get("method")
			reqBody.URL = r.URL.Query().Get("url")
			if h := r.URL.Query().Get("headers"); h != "" {
				var hdr map[string]string
				if err := json.Unmarshal([]byte(h), &hdr); err == nil {
					reqBody.Headers = hdr
				}
			}
			if t := r.URL.Query().Get("timeout"); t != "" {
				var ms int
				fmt.Sscanf(t, "%d", &ms)
				reqBody.Timeout = ms
			}
			// GET 请求 body 可以用 query 参数传 JSON
			if b := r.URL.Query().Get("body"); b != "" {
				var body interface{}
				if err := json.Unmarshal([]byte(b), &body); err == nil {
					reqBody.Body = body
				}
			}
		} else {
			// POST/PUT/DELETE 从 JSON body
			data, err := io.ReadAll(r.Body)
			if err != nil {
				http.Error(w, "failed to read body: "+err.Error(), http.StatusBadRequest)
				return
			}
			if err := json.Unmarshal(data, &reqBody); err != nil {
				http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
				return
			}
		}

		// 默认 method 为 GET
		if reqBody.Method == "" {
			reqBody.Method = http.MethodGet
		}

		// 设置请求超时
		if reqBody.Timeout > 0 {
			client.SetTimeout(time.Duration(reqBody.Timeout) * time.Millisecond)
		} else {
			client.SetTimeout(30 * time.Second)
		}

		// 构造请求
		request := client.R()
		if reqBody.Headers != nil {
			request.SetHeaders(reqBody.Headers)
		}
		if reqBody.Body != nil && (reqBody.Method == http.MethodPost || reqBody.Method == http.MethodPut) {
			request.SetBodyJsonMarshal(reqBody.Body)
		}

		// 执行请求
		resp, err := request.Send(reqBody.Method, reqBody.URL)
		if err != nil {
			http.Error(w, "request failed: "+err.Error(), http.StatusInternalServerError)
			return
		}

		// 返回响应
		result := map[string]interface{}{
			"status":  resp.StatusCode,
			"headers": resp.Header,
			"body":    resp.String(),
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
	})

	// 健康检查
	http.HandleFunc("/ping", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "pong")
	})

	addr := fmt.Sprintf(":%d", *port)
	log.Println("Go server running on", addr)
	log.Fatal(http.ListenAndServe(addr, nil))
}
