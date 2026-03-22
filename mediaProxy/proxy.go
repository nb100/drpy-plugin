package main

import (
	// 标准库

	"bytes"
	"context"
	"embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	handleUrl "net/url"
	"os"
	"os/signal"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	// 本地包
	"MediaProxy/base"

	// 第三方库
	"github.com/go-resty/resty/v2"
	"github.com/patrickmn/go-cache"
	"github.com/sirupsen/logrus"
)

//go:embed static
var indexHTML embed.FS

var mediaCache = cache.New(4*time.Hour, 10*time.Minute)
var authKey string

type Chunk struct {
	startOffset int64
	endOffset   int64
	bufferChan  chan []byte
}

func newChunk(start int64, end int64) *Chunk {
	return &Chunk{
		startOffset: start,
		endOffset:   end,
		bufferChan:  make(chan []byte, 1),
	}
}

func (ch *Chunk) get(ctx context.Context) []byte {
	select {
	case <-ctx.Done():
		return nil
	case buf := <-ch.bufferChan:
		return buf
	}
}

func (ch *Chunk) put(buffer []byte) {
	select {
	case ch.bufferChan <- buffer:
	default:
	}
}

type ProxyDownloadStruct struct {
	ProxyRunning         bool
	NextChunkStartOffset int64
	CurrentOffset        int64
	CurrentChunk         int64
	ChunkSize            int64
	MaxBufferedChunk     int64
	startOffset          int64
	EndOffset            int64
	ProxyMutex           *sync.Mutex
	ProxyTimeout         int64
	ReadyChunkQueue      chan *Chunk
	ThreadCount          int64
	DownloadUrl          string
	CookieJar            *cookiejar.Jar
	Ctx                  context.Context
	Cancel               context.CancelFunc
}

func newProxyDownloadStruct(parentCtx context.Context, downloadUrl string, proxyTimeout int64, maxBuferredChunk int64, chunkSize int64, startOffset int64, endOffset int64, numTasks int64, cookiejar *cookiejar.Jar) *ProxyDownloadStruct {
	ctx, cancel := context.WithCancel(parentCtx)
	return &ProxyDownloadStruct{
		ProxyRunning:         true,
		MaxBufferedChunk:     int64(maxBuferredChunk),
		ProxyTimeout:         proxyTimeout,
		ReadyChunkQueue:      make(chan *Chunk, maxBuferredChunk),
		ProxyMutex:           &sync.Mutex{},
		ChunkSize:            chunkSize,
		NextChunkStartOffset: startOffset,
		CurrentOffset:        startOffset,
		startOffset:          startOffset,
		EndOffset:            endOffset,
		ThreadCount:          numTasks,
		DownloadUrl:          downloadUrl,
		CookieJar:            cookiejar,
		Ctx:                  ctx,
		Cancel:               cancel,
	}
}

func ConcurrentDownload(ctx context.Context, downloadUrl string, rangeStart int64, rangeEnd int64, fileSize int64, splitSize int64, numTasks int64, emitter *base.Emitter, req *http.Request) {
	jar, _ := cookiejar.New(nil)
	cookies := req.Cookies()
	if len(cookies) > 0 {
		// 将 cookies 添加到 cookie jar 中
		u, _ := handleUrl.Parse(downloadUrl)
		jar.SetCookies(u, cookies)
	}

	totalLength := rangeEnd - rangeStart + 1
	numSplits := int64(totalLength/int64(splitSize)) + 1
	if numSplits > int64(numTasks) {
		numSplits = int64(numTasks)
	}

	// 协程、读取超时设置
	proxyTimeout := int64(10)

	logrus.Debugf("正在处理: %+v, rangeStart: %+v, rangeEnd: %+v, contentLength :%+v, splitSize: %+v, numSplits: %+v, numTasks: %+v", downloadUrl, rangeStart, rangeEnd, totalLength, splitSize, numSplits, numSplits)
	maxChunks := int64(128*1024*1024) / splitSize
	p := newProxyDownloadStruct(ctx, downloadUrl, proxyTimeout, maxChunks, splitSize, rangeStart, rangeEnd, numTasks, jar)
	for numSplit := 0; numSplit < int(numSplits); numSplit++ {
		go p.ProxyWorker(req)
	}

	defer func() {
		p.ProxyStop()
		emitter.Close() // 确保在函数结束时关闭emitter
		p = nil
	}()

	for {
		buffer := p.ProxyRead()

		if buffer == nil || len(buffer) == 0 {
			p.ProxyStop()
			logrus.Debugf("ProxyRead执行失败或返回空: buffer == nil? %v, len=%d", buffer == nil, len(buffer))
			buffer = nil
			return
		}

		_, err := emitter.Write(buffer)

		if err != nil {
			if !strings.Contains(err.Error(), "write on closed pipe") && !strings.Contains(err.Error(), "client disconnected") && !strings.Contains(err.Error(), "forcibly closed") && !errors.Is(err, syscall.EPIPE) && !errors.Is(err, syscall.ECONNRESET) {
				logrus.Errorf("emitter写入失败, 错误: %+v", err)
			}
			p.ProxyStop()
			buffer = nil
			return
		}

		if p.CurrentOffset > rangeEnd {
			p.ProxyStop()
			logrus.Debugf("所有服务已经完成大小: %+v", totalLength)
			buffer = nil
			return
		}
		buffer = nil
	}
}

func (p *ProxyDownloadStruct) ProxyRead() []byte {
	// 判断文件是否下载结束
	if p.CurrentOffset > p.EndOffset {
		p.ProxyStop()
		return nil
	}

	// 获取当前的chunk的数据
	var currentChunk *Chunk
	select {
	case <-p.Ctx.Done():
		return nil
	case currentChunk = <-p.ReadyChunkQueue:
		break
	case <-time.After(time.Duration(p.ProxyTimeout) * time.Second):
		logrus.Debugf("执行 ProxyRead 超时")
		p.ProxyStop()
		return nil
	}

	if !p.ProxyRunning {
		return nil
	}
	
	buffer := currentChunk.get(p.Ctx)
	// 如果获取到 nil，说明该 chunk 下载失败（例如 416），停止代理并返回 nil
	if buffer == nil {
		logrus.Debugf("ProxyRead 接收到 nil buffer (可能因为 416 或其他错误)，停止并返回")
		p.ProxyStop()
		return nil
	}
	
	if len(buffer) == 0 {
		logrus.Debugf("ProxyRead 接收到空 buffer (len=0)")
		p.ProxyStop()
		return nil
	}

	p.CurrentOffset += int64(len(buffer))
	return buffer
}

func (p *ProxyDownloadStruct) ProxyStop() {
	p.ProxyRunning = false
	if p.Cancel != nil {
		p.Cancel()
	}
	for {
		select {
		case <-p.ReadyChunkQueue:
		default:
			return
		}
	}
}

func (p *ProxyDownloadStruct) ProxyWorker(req *http.Request) {
	for {
		if !p.ProxyRunning {
			break
		}

		p.ProxyMutex.Lock()
		if len(p.ReadyChunkQueue) >= int(p.MaxBufferedChunk) {
			p.ProxyMutex.Unlock()
			select {
			case <-p.Ctx.Done():
				return
			case <-time.After(100 * time.Millisecond):
			}
			continue
		}

		// 生成下一个chunk
		var chunk *Chunk
		chunk = nil
		startOffset := p.NextChunkStartOffset
		if startOffset <= p.EndOffset {
			p.NextChunkStartOffset += p.ChunkSize
			endOffset := startOffset + p.ChunkSize - 1
			if endOffset > p.EndOffset {
				endOffset = p.EndOffset
			}
			chunk = newChunk(startOffset, endOffset)
			p.ReadyChunkQueue <- chunk
		}
		p.ProxyMutex.Unlock()

		// 所有chunk已下载完
		if chunk == nil {
			break
		}

		for {
			if !p.ProxyRunning {
				break
			} else {
				// 建立连接
				rangeStr := fmt.Sprintf("bytes=%d-%d", chunk.startOffset, chunk.endOffset)
				newHeader := make(map[string][]string)
				for name, value := range req.Header {
					if !shouldFilterHeaderName(name) {
						newHeader[name] = value
					}
				}
				newHeader["Accept-Encoding"] = []string{"identity"}

				maxRetries := 5
				if startOffset < int64(1048576) || (p.EndOffset-startOffset)/p.EndOffset*1000 < 2 {
					maxRetries = 10 // 增加重试次数
				}

				var resp *resty.Response
				var err error
				for retry := 0; retry < maxRetries; retry++ {
					resp, err = base.RestyClient.
						SetTimeout(30*time.Second).
						SetRetryCount(1).
						SetCookieJar(p.CookieJar).
						R().
						SetContext(p.Ctx).
						SetHeaderMultiValues(newHeader).
						SetHeader("Range", rangeStr).
						Get(p.DownloadUrl)

					if err != nil {
						// 检查是否是被取消的上下文
						if errors.Is(err, context.Canceled) {
							logrus.Debugf("任务被取消: range=%d-%d", chunk.startOffset, chunk.endOffset)
							resp = nil
							return
						}
						logrus.Errorf("处理 %+v 链接 range=%d-%d 部分失败: %+v", p.DownloadUrl, chunk.startOffset, chunk.endOffset, err)
						select {
						case <-p.Ctx.Done():
							return
						case <-time.After(1 * time.Second):
						}
						resp = nil
						continue
					}
					if !strings.HasPrefix(resp.Status(), "20") {
						if resp.StatusCode() == 503 || resp.StatusCode() == 429 {
							// 迅雷等网盘限制并发或请求过快，进行退避重试
							logrus.Debugf("触发服务器限制(statusCode: %d)，等待重试... range=%d-%d", resp.StatusCode(), chunk.startOffset, chunk.endOffset)
							select {
							case <-p.Ctx.Done():
								logrus.Debugf("任务被取消(退避期间): range=%d-%d", chunk.startOffset, chunk.endOffset)
								return
							case <-time.After(time.Duration(2+retry) * time.Second):
							} // 递增等待时间
							resp = nil
							continue
						}
						if resp.StatusCode() == 416 {
							logrus.Debugf("处理 %+v 链接 range=%d-%d 到达文件末尾 (416)", p.DownloadUrl, chunk.startOffset, chunk.endOffset)
							resp = nil
							break // 跳出重试循环，标记此 chunk 为结束
						}
						
						logrus.Debugf("处理 %+v 链接 range=%d-%d 部分失败, statusCode: %+v: %s", p.DownloadUrl, chunk.startOffset, chunk.endOffset, resp.StatusCode(), resp.String())
						resp = nil
						break // 跳出重试循环，标记此 chunk 失败
					}
					break
				}

				if err != nil {
					logrus.Errorf("处理链接 range=%d-%d 最终失败: %+v", chunk.startOffset, chunk.endOffset, err)
					resp = nil
				}

				// 接收数据
				if resp != nil {
					body := resp.Body()
					expectedLen := int(chunk.endOffset - chunk.startOffset + 1)
					if len(body) != expectedLen {
						logrus.Warnf("【警告】收到数据长度不匹配! 请求 range=%d-%d (预期 %d), 实际收到 %d bytes, Content-Range: %s", chunk.startOffset, chunk.endOffset, expectedLen, len(body), resp.Header().Get("Content-Range"))
					}
					chunk.put(body)
				} else {
					logrus.Debugf("Chunk range=%d-%d 无法获取数据，写入 nil 并停止调度新任务", chunk.startOffset, chunk.endOffset)
					chunk.put(nil) // 放入 nil 标记此 chunk 失败或结束
					
					// 停止调度新的 chunk
					p.ProxyMutex.Lock()
					if p.NextChunkStartOffset <= p.EndOffset {
						p.NextChunkStartOffset = p.EndOffset + 1
					}
					p.ProxyMutex.Unlock()
					
					return // 直接结束当前 worker 协程
				}
				resp = nil
				break
			}
		}
	}
}



func handleMethod(w http.ResponseWriter, req *http.Request) {
	switch req.Method {
	case http.MethodGet:
		// 处理 GET 请求
		logrus.Info("正在 GET 请求")
		// 检查查询参数是否为空
		if req.URL.RawQuery == "" {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.Write([]byte("欢迎使用drpyS专用多线程媒体代理服务，由道长于2026年开发"))
		} else {
			// 如果有查询参数，则返回自定义的内容
			handleGetMethod(w, req)
		}
	default:
		// 处理其他方法的请求
		logrus.Infof("正在处理 %v 请求", req.Method)
		handleOtherMethod(w, req)
	}
}

func handleGetMethod(w http.ResponseWriter, req *http.Request) {

	logrus.Debugf("当前活跃的协程数量: %d", runtime.NumGoroutine())

	var url string
	query := req.URL.Query()
	url = query.Get("url")
	strForm := query.Get("form")
	strHeader := query.Get("headers")
	strAuth := query.Get("auth")
	strThread := req.URL.Query().Get("thread")
	strSplitSize := req.URL.Query().Get("size")
	if strSplitSize == "" {
		strSplitSize = req.URL.Query().Get("chunkSize")
	}

	// 验证auth参数
	if authKey != "" && strAuth != authKey {
		http.Error(w, "无效的认证参数", http.StatusUnauthorized)
		return
	}

	if url != "" {
		if strForm == "base64" {
			bytesUrl, err := base64.StdEncoding.DecodeString(url)
			if err != nil {
				http.Error(w, fmt.Sprintf("无效的 Base64 Url: %v", err), http.StatusBadRequest)
				return
			}
			url = string(bytesUrl)
		}
	} else {
		http.Error(w, "缺少url参数", http.StatusBadRequest)
		return
	}

	if strHeader != "" {
		if strForm == "base64" {
			bytesStrHeader, err := base64.StdEncoding.DecodeString(strHeader)
			if err != nil {
				http.Error(w, fmt.Sprintf("无效的Base64 Headers: %v", err), http.StatusBadRequest)
				return
			}
			strHeader = string(bytesStrHeader)
		}
		var headers map[string]string
		err := json.Unmarshal([]byte(strHeader), &headers)
		if err != nil {
			http.Error(w, fmt.Sprintf("Header Json格式化错误: %v", err), http.StatusInternalServerError)
			return
		}
		for key, value := range headers {
			req.Header.Set(key, value)
		}
	}

	newHeader := make(map[string][]string)
	for name, value := range req.Header {
		if !shouldFilterHeaderName(name) {
			newHeader[name] = value
		}
	}
	// 强制要求服务器不进行 gzip 压缩，否则可能导致分片数据大小不匹配
	newHeader["Accept-Encoding"] = []string{"identity"}

	// 移除错误的URL参数追加逻辑，因为这会破坏原始URL
	// 代理服务应该直接使用解码后的完整URL，而不是修改它

	jar, _ := cookiejar.New(nil)
	cookies := req.Cookies()
	if len(cookies) > 0 {
		// 将 cookies 添加到 cookie jar 中
		u, _ := handleUrl.Parse(url)
		jar.SetCookies(u, cookies)
	}

	var statusCode int
	var rangeStart, rangeEnd = int64(0), int64(0)
	requestRange := req.Header.Get("Range")
	rangeRegex := regexp.MustCompile(`bytes= *([0-9]+) *- *([0-9]*)`)
	matchGroup := rangeRegex.FindStringSubmatch(requestRange)
	if matchGroup != nil {
		statusCode = 206
		rangeStart, _ = strconv.ParseInt(matchGroup[1], 10, 64)
		if len(matchGroup) > 2 && matchGroup[2] != "" {
			rangeEnd, _ = strconv.ParseInt(matchGroup[2], 10, 64)
		}
	} else {
		statusCode = 200
	}

	cacheTimeKey := url + "#LastModified"
	lastModifiedCache, found := mediaCache.Get(cacheTimeKey)
	var lastModified int64
	if found {
		lastModified = lastModifiedCache.(int64)
	} else {
		lastModified = int64(0)
	}

	headersKey := url + "#Headers"
	curTime := time.Now().Unix()
	var cachedHeaders interface{}
	cachedHeaders, found = mediaCache.Get(headersKey)

	// 深拷贝 Header，避免并发修改和互相污染
	responseHeaders := make(http.Header)
	if found {
		for k, v := range cachedHeaders.(http.Header) {
			responseHeaders[k] = append([]string(nil), v...)
		}
	}

	if !found || curTime-lastModified > 60 {
		// 创建专用的客户端用于获取头信息，避免修改全局设置
		headClient := base.NewRestyClient()
		resp, err := headClient.
			SetTimeout(30*time.Second).
			SetRetryCount(3).
			SetCookieJar(jar).
			R().
			SetDoNotParseResponse(true).
			SetOutput(os.DevNull).
			SetHeaderMultiValues(newHeader).
			SetHeader("Range", "bytes=0-1023").
			Get(url)
		if err != nil {
			http.Error(w, fmt.Sprintf("下载 %v 链接失败: %v", url, err), http.StatusInternalServerError)
			return
		}
		if resp.StatusCode() < 200 || resp.StatusCode() >= 400 {
			http.Error(w, resp.Status(), resp.StatusCode())
			return
		}

		// 深拷贝以防止修改缓存
		responseHeaders = make(http.Header)
		for k, v := range resp.Header() {
			responseHeaders[k] = append([]string(nil), v...)
		}

		logrus.Debugf("请求头: %+v", responseHeaders)

		var fileName string
		contentDisposition := strings.ToLower(responseHeaders.Get("Content-Disposition"))
		if contentDisposition != "" {
			regCompile := regexp.MustCompile(`^.*filename=\"([^\"]+)\".*$`)
			if regCompile.MatchString(contentDisposition) {
				fileName = regCompile.ReplaceAllString(contentDisposition, "$1")
			}
		} else {
			// 找到最后一个 "/" 的索引
			lastSlashIndex := strings.LastIndex(url, "/")
			// 找到第一个 "?" 的索引
			queryIndex := strings.Index(url, "?")
			if queryIndex == -1 {
				// 如果没有 "?"，则提取从最后一个 "/" 到结尾的字符串
				fileName = url[lastSlashIndex+1:]
			} else {
				// 如果存在 "?"，则提取从最后一个 "/" 到 "?" 之间的字符串
				fileName = url[lastSlashIndex+1 : queryIndex]
			}
		}

		contentType := responseHeaders.Get("Content-Type")
		if contentType == "" || contentType == "application/octet-stream" {
			urlLower := strings.ToLower(url)
			if strings.HasSuffix(fileName, ".webm") || strings.Contains(urlLower, "fext=webm") || strings.Contains(urlLower, ".webm") {
				contentType = "video/webm"
			} else if strings.HasSuffix(fileName, ".avi") || strings.Contains(urlLower, "fext=avi") || strings.Contains(urlLower, ".avi") {
				contentType = "video/x-msvideo"
			} else if strings.HasSuffix(fileName, ".wmv") || strings.Contains(urlLower, "fext=wmv") || strings.Contains(urlLower, ".wmv") {
				contentType = "video/x-ms-wmv"
			} else if strings.HasSuffix(fileName, ".flv") || strings.Contains(urlLower, "fext=flv") || strings.Contains(urlLower, ".flv") {
				contentType = "video/x-flv"
			} else if strings.HasSuffix(fileName, ".mov") || strings.Contains(urlLower, "fext=mov") || strings.Contains(urlLower, ".mov") {
				contentType = "video/quicktime"
			} else if strings.HasSuffix(fileName, ".mkv") || strings.Contains(urlLower, "fext=mkv") || strings.Contains(urlLower, ".mkv") {
				contentType = "video/x-matroska"
			} else if strings.HasSuffix(fileName, ".ts") || strings.Contains(urlLower, "fext=ts") || strings.Contains(urlLower, ".ts") {
				contentType = "video/mp2t"
			} else if strings.HasSuffix(fileName, ".mpeg") || strings.HasSuffix(fileName, ".mpg") {
				contentType = "video/mpeg"
			} else if strings.HasSuffix(fileName, ".3gpp") || strings.HasSuffix(fileName, ".3gp") {
				contentType = "video/3gpp"
			} else if strings.HasSuffix(fileName, ".mp4") || strings.HasSuffix(fileName, ".m4s") || strings.Contains(urlLower, "fext=mp4") || strings.Contains(urlLower, ".mp4") {
				contentType = "video/mp4"
			} else {
				// 保留原始的 application/octet-stream 或者默认为 video/mp4
				// 对于 ijkplayer，最好让其自己嗅探，所以如果是 octet-stream，就保留
				if contentType == "" {
					contentType = "video/mp4" 
				}
			}
			responseHeaders.Set("Content-Type", contentType)
		}

		contentRange := responseHeaders.Get("Content-Range")
		if contentRange != "" {
			matchGroup := regexp.MustCompile(`.*/([0-9]+)`).FindStringSubmatch(contentRange)
			contentSize, _ := strconv.ParseInt(matchGroup[1], 10, 64)

			responseHeaders.Set("Content-Length", strconv.FormatInt(contentSize, 10))
		} else {
			responseHeaders.Set("Content-Length", strconv.FormatInt(resp.Size(), 10))
		}

		acceptRange := responseHeaders.Get("Accept-Ranges")
		if contentRange == "" && acceptRange == "" {
			// 不支持断点续传
			remainingSize := 0
			const maxBufferSize = 128 * 1024 * 1024 // 128MB
			// const maxBufferSize = 1*1024 // 1KB

			// 必须先写入 Header
			responseHeaders.Del("Transfer-Encoding")
			for key, values := range responseHeaders {
				if strings.EqualFold(strings.ToLower(key), "connection") || strings.EqualFold(strings.ToLower(key), "proxy-connection") {
					continue
				}
				w.Header().Set(key, strings.Join(values, ","))
			}
			w.Header().Set("Cache-Control", "public, max-age=31536000")
			w.Header().Del("Pragma")
			w.Header().Del("Expires")
			w.Header().Set("Connection", "keep-alive")
			w.WriteHeader(statusCode)

			buf := make([]byte, 1024*64) // 64KB 缓冲区
			for {
				n, err := resp.RawBody().Read(buf)
				if n > 0 {
					// 写入到客户端前更新未消费的数据大小
					remainingSize += n
					// 写入数据到客户端
					_, writeErr := w.Write(buf[:n])
					if writeErr != nil {
						logrus.Errorf("向客户端写入 Response 失败: %v", writeErr)
						return
					}
				}
				if err != nil {
					if err != io.EOF {
						logrus.Errorf("读取 Response Body 错误: %v", err)
					}
					break
				}
			}
			// 不支持断点续传的流直接结束
			return

		} else {
			// 支持断点续传
			// 缓存前深拷贝一份，避免后续修改 responseHeaders 污染缓存
			cacheHeaders := make(http.Header)
			for k, v := range responseHeaders {
				cacheHeaders[k] = append([]string(nil), v...)
			}
			mediaCache.Set(headersKey, cacheHeaders, 1800*time.Second)
			mediaCache.Set(cacheTimeKey, curTime, 1800*time.Second)
		}

		defer func() {
			if resp != nil && resp.RawBody() != nil {
				logrus.Debugf("resp.RawBody 已关闭")
				resp.RawBody().Close()
			}
		}()
	}

	acceptRange := responseHeaders.Get("Accept-Ranges")
	contentRange := responseHeaders.Get("Content-Range")
	if contentRange == "" && acceptRange == "" {
		// 理论上不可达，因为不支持断点续传的请求不会被缓存
		http.Error(w, "无法处理不支持断点续传的缓存请求", http.StatusInternalServerError)
		return
	} else {
		// 支持断点续传
		var splitSize int64
		var numTasks int64

		contentSize := int64(0)
		matchGroup = regexp.MustCompile(`.*/([0-9]+)`).FindStringSubmatch(contentRange)
		if matchGroup != nil {
			contentSize, _ = strconv.ParseInt(matchGroup[1], 10, 64)
		} else {
			contentSize, _ = strconv.ParseInt(responseHeaders.Get("Content-Length"), 10, 64)
		}

		if rangeEnd == int64(0) || rangeEnd >= contentSize {
			rangeEnd = contentSize - 1
		}
		if rangeStart < contentSize {
			if strThread == "" {
				// 根据文件大小和请求范围自动设置线程数
				numTasks = 1 // 默认单线程
				if rangeEnd-rangeStart > 512*1024*1024 {
					if contentSize < 1*1024*1024*1024 {
						numTasks = 16
					} else if contentSize < 4*1024*1024*1024 {
						numTasks = 32
					} else if contentSize < 16*1024*1024*1024 {
						numTasks = 64
					} else {
						numTasks = 64
					}
				}
			} else {
				numTasks, _ = strconv.ParseInt(strThread, 10, 64)
				if numTasks <= 0 {
					numTasks = 1
				}
				// 限制最大线程数，防止被服务器封禁（尤其是迅雷等网盘）
				if numTasks > 16 {
					logrus.Debugf("请求线程数(%d)过大，限制为16以防止被封禁", numTasks)
					numTasks = 16
				}
			}

			if strSplitSize != "" {
				// 处理带单位的参数，如 256K, 1M 等
				strSplitSize = strings.ToUpper(strSplitSize)
				if strings.HasSuffix(strSplitSize, "K") || strings.HasSuffix(strSplitSize, "KB") {
					valStr := strings.TrimRight(strings.TrimRight(strSplitSize, "B"), "K")
					val, _ := strconv.ParseInt(valStr, 10, 64)
					splitSize = val * 1024
				} else if strings.HasSuffix(strSplitSize, "M") || strings.HasSuffix(strSplitSize, "MB") {
					valStr := strings.TrimRight(strings.TrimRight(strSplitSize, "B"), "M")
					val, _ := strconv.ParseInt(valStr, 10, 64)
					splitSize = val * 1024 * 1024
				} else {
					// 纯数字，如果是256之类的小数字，原版逻辑认为是KB，如果是1024这种，原版逻辑认为是Byte？
					// 按照 README 里的说法，256K 是带K的。纯数字如果是迅雷等传递过来的，往往是 kb 或者 b?
					// 之前的逻辑是 splitSize, _ = strconv.ParseInt(strSplitSize, 10, 64)
					// 这意味着纯数字是 byte 为单位的。
					val, _ := strconv.ParseInt(strSplitSize, 10, 64)
					// 如果值太小，很可能单位是 KB
					if val < 1024*10 {
						splitSize = val * 1024
					} else {
						splitSize = val
					}
				}

				// 动态调整分片大小，结合线程数限制，保障下载速度同时避免过快请求
				if splitSize < 128*1024 {
					// 强制分片大小最小为 128KB，避免请求过于频繁
					splitSizeOriginal := splitSize
					splitSize = 128 * 1024
					logrus.Debugf("splitSize adjusted to min 128KB: original=%d", splitSizeOriginal)
				}
			} else {
				// 如果没有传，默认 2MB (之前是128KB，太小会导致请求过于频繁)
				splitSize = int64(2 * 1024 * 1024)
			}

			logrus.Debugf("Proxy data transfer: thread=%d, splitSize=%d", numTasks, splitSize)

			// 对于 ExoPlayer，我们必须确保返回 Content-Range 时，格式完全正确。
			// 之前我们使用 fmt.Sprintf("bytes %d-%d/%d", rangeStart, rangeEnd, contentSize)
			// 如果 rangeStart == 0 且 rangeEnd == contentSize - 1，有些播放器（特别是ExoPlayer拖拽时）
			// 会因为缓存和重新请求的 Range 发生冲突。
			// 同时，必须确保 Accept-Ranges 存在
			if statusCode == 206 {
				// ExoPlayer 非常依赖于精确的 Range 回复。如果请求的是 bytes=0-，它可能会检查长度是否一致。
				// 有些情况下返回 0-X/Y (X = Y - 1) 会被 ExoPlayer 认为不匹配它的期望，导致重置播放或抛出异常。
				// 因此我们必须确保回复格式完全合规，如果请求没有给结束位置，我们返回到文件末尾。
				responseHeaders.Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", rangeStart, rangeEnd, contentSize))
			} else {
				responseHeaders.Del("Content-Range")
			}
			responseHeaders.Set("Content-Length", strconv.FormatInt(rangeEnd-rangeStart+1, 10))
			responseHeaders.Set("Accept-Ranges", "bytes")

			// 必须清理可能干扰播放器 Range 判断的头
			responseHeaders.Del("Transfer-Encoding") 
			responseHeaders.Del("Content-Encoding")
			
			// 对于 ExoPlayer，我们最好设置一个强缓存标志并且带有 ETag 以支持精确的范围请求
			// 否则 ExoPlayer 在拖拽时可能会发送没有 If-Range 的请求或者放弃使用现有的 Cache
			for key, values := range responseHeaders {
				if strings.EqualFold(strings.ToLower(key), "connection") || strings.EqualFold(strings.ToLower(key), "proxy-connection") {
					continue
				}
				w.Header().Set(key, strings.Join(values, ","))
			}
			w.Header().Set("Cache-Control", "public, max-age=31536000")
			w.Header().Del("Pragma")
			w.Header().Del("Expires")

			w.Header().Set("Connection", "keep-alive")
			w.WriteHeader(statusCode) // 206 for partial content

			rp, wp := io.Pipe()
			emitter := base.NewEmitter(rp, wp)

			defer func() {
				if !emitter.IsClosed() {
					emitter.Close()
					logrus.Debugf("handleGetMethod emitter 已关闭-支持断点续传")
				}
			}()

			go ConcurrentDownload(req.Context(), url, rangeStart, rangeEnd, contentSize, splitSize, numTasks, emitter, req)

			// 响应数据，使用较大的 buffer 提高复制效率
			buf := make([]byte, 128*1024)
			_, err := io.CopyBuffer(w, emitter, buf)
			if err != nil && !strings.Contains(err.Error(), "write on closed pipe") && !strings.Contains(err.Error(), "client disconnected") && !strings.Contains(err.Error(), "forcibly closed") && !errors.Is(err, syscall.EPIPE) && !errors.Is(err, syscall.ECONNRESET) {
				logrus.Debugf("io.Copy error: %v", err)
			}
		} else {
			// Range超出文件大小，返回416错误
			logrus.Debugf("Range超出文件大小，返回416错误. rangeStart: %d, rangeEnd: %d, contentSize: %d", rangeStart, rangeEnd, contentSize)
			statusCode = 416
			w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", contentSize))
			w.WriteHeader(statusCode)
		}
	}
}

func handleOtherMethod(w http.ResponseWriter, req *http.Request) {
	defer req.Body.Close()

	var url string
	query := req.URL.Query()
	url = query.Get("url")
	strForm := query.Get("form")
	strHeader := query.Get("headers")
	strAuth := query.Get("auth")

	// 验证auth参数
	if authKey != "" && strAuth != authKey {
		http.Error(w, "无效的认证参数", http.StatusUnauthorized)
		return
	}

	if url != "" {
		if strForm == "base64" {
			bytesUrl, err := base64.StdEncoding.DecodeString(url)
			if err != nil {
				http.Error(w, fmt.Sprintf("无效的 Base64 Url: %v", err), http.StatusBadRequest)
				return
			}
			url = string(bytesUrl)
		}
	} else {
		http.Error(w, "缺少 url 参数", http.StatusBadRequest)
		return
	}

	// 处理自定义 headers
	var headers map[string]string
	if strHeader != "" {
		if strForm == "base64" {
			bytesStrHeader, err := base64.StdEncoding.DecodeString(strHeader)
			if err != nil {
				http.Error(w, fmt.Sprintf("无效的Base64 Headers: %v", err), http.StatusBadRequest)
				return
			}
			strHeader = string(bytesStrHeader)
		}
		err := json.Unmarshal([]byte(strHeader), &headers)
		if err != nil {
			http.Error(w, fmt.Sprintf("Header Json格式化错误: %v", err), http.StatusInternalServerError)
			return
		}
		for key, value := range headers {
			req.Header.Set(key, value)
		}
	}
	newHeader := make(map[string][]string)
	for name, value := range req.Header {
		if !shouldFilterHeaderName(name) {
			newHeader[name] = value
		}
	}
	// 强制要求服务器不进行 gzip 压缩
	newHeader["Accept-Encoding"] = []string{"identity"}

	// 移除错误的URL参数追加逻辑，直接使用解码后的完整URL

	jar, _ := cookiejar.New(nil)
	cookies := req.Cookies()
	if len(cookies) > 0 {
		// 将 cookies 添加到 cookie jar 中
		u, _ := handleUrl.Parse(req.URL.String())
		jar.SetCookies(u, cookies)
	}

	var reqBody []byte
	// 读取请求体以记录
	if req.Body != nil {
		reqBody, _ = io.ReadAll(req.Body)
	}

	var resp *resty.Response
	var err error
	switch req.Method {
	case http.MethodPost:
		resp, err = base.RestyClient.
			SetTimeout(10 * time.Second).
			SetRetryCount(3).
			SetCookieJar(jar).
			R().
			SetBody(reqBody).
			SetHeaderMultiValues(newHeader).
			Post(url)
	case http.MethodPut:
		resp, err = base.RestyClient.
			SetTimeout(10 * time.Second).
			SetRetryCount(3).
			SetCookieJar(jar).
			R().
			SetBody(reqBody).
			SetHeaderMultiValues(newHeader).
			Put(url)
	case http.MethodOptions:
		resp, err = base.RestyClient.
			SetTimeout(10 * time.Second).
			SetRetryCount(3).
			SetCookieJar(jar).
			R().
			SetHeaderMultiValues(newHeader).
			Options(url)
	case http.MethodDelete:
		resp, err = base.RestyClient.
			SetTimeout(10 * time.Second).
			SetRetryCount(3).
			SetCookieJar(jar).
			R().
			SetHeaderMultiValues(newHeader).
			Delete(url)
	case http.MethodPatch:
		resp, err = base.RestyClient.
			SetTimeout(10 * time.Second).
			SetRetryCount(3).
			SetCookieJar(jar).
			R().
			SetHeaderMultiValues(newHeader).
			Patch(url)
	case http.MethodHead:
		resp, err = base.RestyClient.
			SetTimeout(10 * time.Second).
			SetRetryCount(3).
			SetCookieJar(jar).
			R().
			SetHeaderMultiValues(newHeader).
			Head(url)
	default:
		http.Error(w, fmt.Sprintf("无效的Method: %v", req.Method), http.StatusBadRequest)
	}

	if err != nil {
		http.Error(w, fmt.Sprintf("%v 链接 %v 失败: %v", req.Method, url, err), http.StatusInternalServerError)
		resp = nil
		return
	}
	if resp.StatusCode() < 200 || resp.StatusCode() >= 400 {
		http.Error(w, resp.Status(), resp.StatusCode())
		resp = nil
		return
	}

	// 处理响应
	w.Header().Set("Connection", "close")
	for name, values := range resp.Header() {
		w.Header().Set(name, strings.Join(values, ","))
	}
	w.WriteHeader(resp.StatusCode())
	bodyReader := bytes.NewReader(resp.Body())
	io.Copy(w, bodyReader)
}

func shouldFilterHeaderName(key string) bool {
	if len(strings.TrimSpace(key)) == 0 {
		return false
	}
	key = strings.ToLower(key)
	// 移除对 range 头的过滤，允许 Range 请求正常转发
	return key == "host" || key == "http-client-ip" || key == "remote-addr" || key == "accept-encoding"
}

func main() {
	// 定义 dns 和 debug 命令行参数
	dns := flag.String("dns", "8.8.8.8", "DNS解析 IP:port")
	port := flag.String("port", "5575", "服务器端口")
	debug := flag.Bool("debug", false, "Debug模式")
	auth := flag.String("auth", "", "认证密钥")
	flag.Parse()

	// 忽略 SIGPIPE 信号
	signal.Ignore(syscall.SIGPIPE)

	// 设置日志输出和级别
	logrus.SetOutput(os.Stdout)
	if *debug {
		logrus.SetLevel(logrus.DebugLevel)
		logrus.Info("已开启Debug模式")
	} else {
		logrus.SetLevel(logrus.InfoLevel)
	}
	logrus.Infof("服务器运行在 %s 端口.", *port)

	// 开启Debug
	//logrus.SetLevel(logrus.DebugLevel)

	// 设置全局变量
	authKey = *auth
	base.DnsResolverIP = *dns
	base.InitClient()
	var server = http.Server{
		Addr:    ":" + *port,
		Handler: http.HandlerFunc(handleMethod),
	}
	// server.SetKeepAlivesEnabled(false)
	server.ListenAndServe()
}
