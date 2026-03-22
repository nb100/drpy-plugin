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
var enableContentTypeGuess bool

const (
	AppVersion = "V1.0.1 20260322"
)

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

		// 这里有一个关键点：我们需要告诉播放器真实的 Content-Length，
		// 但是我们从网盘下载的速度可能远大于播放器消费的速度。
		// base.Emitter.Write 内部通过 io.Pipe 阻塞写入，
		// 这样可以根据播放器的实际消费能力（网速/解码速度）来背压（backpressure）下载协程，
		// 从而不会无意义地消耗带宽和内存。
		_, err := emitter.Write(buffer)

		// 增加极微小的睡眠，让出 CPU 切片，帮助缓解瞬间高并发写入时播放器读取跟不上的缓冲暴涨
		time.Sleep(1 * time.Millisecond)

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
			currentChunkSize := p.ChunkSize
			// 动态分片：第一个分片强制缩小，以极大降低首包延迟，防止 IjkPlayer 超时
			// 只有当原始 chunkSize 大于 256KB 时，首包才缩减到 256KB
			if startOffset == p.startOffset && currentChunkSize > 256*1024 {
				currentChunkSize = 256 * 1024
			}

			p.NextChunkStartOffset += currentChunkSize
			endOffset := startOffset + currentChunkSize - 1
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
				var finalBody []byte
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

					// 检查数据长度
					body := resp.Body()
					expectedLen := int(chunk.endOffset - chunk.startOffset + 1)

					if resp.StatusCode() == 200 && chunk.startOffset > 0 {
						logrus.Warnf("【警告】请求部分数据 range=%d-%d 但服务器返回 200 OK (全量数据), 丢弃并重试", chunk.startOffset, chunk.endOffset)
						err = fmt.Errorf("server returned 200 instead of 206")
						resp = nil
						select {
						case <-p.Ctx.Done():
							return
						case <-time.After(2 * time.Second):
						}
						continue
					}

					// 严格校验 Content-Range 偏移量，防止 CDN 返回错误的分片数据导致播放器解码卡死
					respContentRange := resp.Header().Get("Content-Range")
					if respContentRange != "" && resp.StatusCode() == 206 {
						expectedPrefix := fmt.Sprintf("bytes %d-", chunk.startOffset)
						if !strings.HasPrefix(respContentRange, expectedPrefix) {
							logrus.Warnf("【致命警告】CDN返回的Range偏移量错误! 期望: %s, 实际: %s. 丢弃并重试以防止播放器画面卡死", expectedPrefix, respContentRange)
							err = fmt.Errorf("invalid content-range: %s", respContentRange)
							resp = nil
							select {
							case <-p.Ctx.Done():
								return
							case <-time.After(1 * time.Second):
							}
							continue
						}
					}

					if len(body) < expectedLen {
						logrus.Warnf("【警告】收到数据长度不足! 请求 range=%d-%d (预期 %d), 实际收到 %d bytes, 丢弃并重试", chunk.startOffset, chunk.endOffset, expectedLen, len(body))
						err = fmt.Errorf("short read: %d < %d", len(body), expectedLen)
						resp = nil
						select {
						case <-p.Ctx.Done():
							return
						case <-time.After(1 * time.Second):
						}
						continue
					} else if len(body) > expectedLen {
						logrus.Debugf("收到数据长度超长 (预期 %d, 实际 %d), 进行截断", expectedLen, len(body))
						finalBody = body[:expectedLen]
					} else {
						finalBody = body
					}

					break
				}

				if err != nil && resp == nil && finalBody == nil {
					logrus.Errorf("处理链接 range=%d-%d 最终失败: %+v", chunk.startOffset, chunk.endOffset, err)
				}

				// 接收数据
				if finalBody != nil {
					chunk.put(finalBody)
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

func guessContentType(url string, contentDisposition string) string {
	var fileName string
	contentDisposition = strings.ToLower(contentDisposition)
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

	contentType := ""
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
	}
	return contentType
}

func handleMethod(w http.ResponseWriter, req *http.Request) {
	switch req.Method {
	case http.MethodGet, http.MethodHead:
		// 处理 GET 和 HEAD 请求
		logrus.Info("正在 GET/HEAD 请求")
		// 检查查询参数是否为空
		if req.URL.RawQuery == "" {
			if req.Method == http.MethodGet {
				indexContent, err := indexHTML.ReadFile("static/index.html")
				if err == nil {
					w.Header().Set("Content-Type", "text/html; charset=utf-8")
					w.Write(indexContent)
				} else {
					w.Header().Set("Content-Type", "text/plain; charset=utf-8")
					w.Write([]byte(fmt.Sprintf("欢迎使用drpyS专用多线程媒体代理服务，由道长于2026年开发\n版本: %s", AppVersion)))
				}
			}
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
	if strHeader == "" {
		strHeader = query.Get("header")
	}
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
	// ExoPlayer 请求时可能会带上一些额外的控制头，这里我们保留必要的，但要确保我们以原样拉取

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
	var rangeStart, rangeEnd = int64(0), int64(-1)
	var isSuffixRange bool
	var suffixLength int64
	var isExactRange bool
	var originalRequestRange string

	requestRange := req.Header.Get("Range")
	originalRequestRange = requestRange

	if requestRange != "" {
		suffixRegex := regexp.MustCompile(`bytes= *-([0-9]+)`)
		rangeRegex := regexp.MustCompile(`bytes= *([0-9]+) *- *([0-9]*)`)

		if suffixMatch := suffixRegex.FindStringSubmatch(requestRange); suffixMatch != nil {
			statusCode = 206
			isSuffixRange = true
			suffixLength, _ = strconv.ParseInt(suffixMatch[1], 10, 64)
		} else if matchGroup := rangeRegex.FindStringSubmatch(requestRange); matchGroup != nil {
			statusCode = 206
			rangeStart, _ = strconv.ParseInt(matchGroup[1], 10, 64)
			if len(matchGroup) > 2 && matchGroup[2] != "" {
				rangeEnd, _ = strconv.ParseInt(matchGroup[2], 10, 64)
				isExactRange = true
			} else {
				// 将其标记为一个特殊的 -1，表示到文件末尾
				rangeEnd = -1
			}
		} else {
			statusCode = 200
			rangeStart = 0
			rangeEnd = -1
		}
	} else {
		statusCode = 200
		rangeStart = 0
		rangeEnd = -1
	}

	// 提前处理 Content-Type 以防影响缓存逻辑
	// 注意：缓存查询等其他逻辑保留

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

		contentType := responseHeaders.Get("Content-Type")
		if contentType == "" || contentType == "application/octet-stream" {
			if enableContentTypeGuess {
				guessedType := guessContentType(url, responseHeaders.Get("Content-Disposition"))
				if guessedType != "" {
					responseHeaders.Set("Content-Type", guessedType)
				} else {
					responseHeaders.Del("Content-Type")
				}
			} else {
				// 默认不启用：不要强行根据 URL 猜测 Content-Type，因为网盘的 fext=mp4 可能是假的（实际上是 mkv 等）。
				// 强行设置为 video/mp4 会导致 MPV/ffmpeg 强制使用 mp4 解码器，从而在拖拽时因为索引不匹配而卡死！
				// 删除 Content-Type 让所有播放器（Exo/Ijk/MPV）强制进行真实的二进制嗅探 (Sniffing)。
				responseHeaders.Del("Content-Type")
			}
		}

		contentRange := responseHeaders.Get("Content-Range")
		if contentRange != "" {
			matchGroup := regexp.MustCompile(`.*/([0-9]+)`).FindStringSubmatch(contentRange)
			contentSize, _ := strconv.ParseInt(matchGroup[1], 10, 64)

			// 检查是否受限于网盘试看(如迅雷 rg=0-82432800)参数
			parsedUrl, errUrl := handleUrl.Parse(url)
			if errUrl == nil {
				rg := parsedUrl.Query().Get("rg")
				if rg != "" {
					rgMatch := regexp.MustCompile(`[0-9]+-([0-9]+)`).FindStringSubmatch(rg)
					if rgMatch != nil {
						rgEnd, _ := strconv.ParseInt(rgMatch[1], 10, 64)
						if rgEnd > 0 && rgEnd < contentSize {
							contentSize = rgEnd + 1
							logrus.Debugf("检测到 URL 包含试看范围限制 rg=%s，将文件总大小修正为: %d", rg, contentSize)

							// 同步修改 responseHeaders 中的 Content-Range，防止后续重新解析出旧的 contentSize
							oldContentRange := responseHeaders.Get("Content-Range")
							if oldContentRange != "" {
								newContentRange := regexp.MustCompile(`/([0-9]+)`).ReplaceAllString(oldContentRange, fmt.Sprintf("/%d", contentSize))
								responseHeaders.Set("Content-Range", newContentRange)
							}
						}
					}
				}
			}

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

			if req.Method == http.MethodHead {
				return
			}

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
	if contentRange == "" && acceptRange == "" && responseHeaders.Get("Content-Length") == "" {
		// 理论上不可达，因为不支持断点续传的请求不会被缓存
		http.Error(w, "无法处理不支持断点续传的缓存请求", http.StatusInternalServerError)
		return
	} else {
		// 支持断点续传
		var splitSize int64
		var numTasks int64

		contentSize := int64(0)
		matchGroup := regexp.MustCompile(`.*/([0-9]+)`).FindStringSubmatch(contentRange)
		if matchGroup != nil {
			contentSize, _ = strconv.ParseInt(matchGroup[1], 10, 64)
		} else {
			contentSize, _ = strconv.ParseInt(responseHeaders.Get("Content-Length"), 10, 64)
		}

		if isSuffixRange {
			rangeStart = contentSize - suffixLength
			if rangeStart < 0 {
				rangeStart = 0
			}
			rangeEnd = contentSize - 1
		} else {
			if rangeEnd == -1 || rangeEnd >= contentSize {
				rangeEnd = contentSize - 1
			}
			if rangeStart < 0 {
				rangeStart = 0
			}
		}

		if rangeStart <= rangeEnd && rangeStart < contentSize {
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
				if numTasks > 32 {
					logrus.Debugf("请求线程数(%d)过大，限制为32以防止被封禁", numTasks)
					numTasks = 32
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
				} else if strings.HasSuffix(strSplitSize, "B") {
					valStr := strings.TrimRight(strSplitSize, "B")
					val, _ := strconv.ParseInt(valStr, 10, 64)
					splitSize = val
				} else {
					// 纯数字，默认单位为 KB
					val, _ := strconv.ParseInt(strSplitSize, 10, 64)
					splitSize = val * 1024
				}

				// 根据媒体资源代理的常识设置合理的上下限
				// 上限：最大 10MB，避免单次 HTTP 请求过长导致超时或占用过多内存
				maxSplitSize := int64(10 * 1024 * 1024)
				// 下限：最小 32KB，避免分片过小导致频繁发起 HTTP 请求（如果用户需要更小，则不建议）
				minSplitSize := int64(32 * 1024)

				if splitSize > maxSplitSize {
					logrus.Debugf("splitSize 超过上限 %d，强制调整为 %d", splitSize, maxSplitSize)
					splitSize = maxSplitSize
				} else if splitSize < minSplitSize {
					logrus.Debugf("splitSize 过小 %d，强制调整为最小 %d", splitSize, minSplitSize)
					splitSize = minSplitSize
				}
			} else {
				// 如果没有传，默认 128KB
				splitSize = int64(128 * 1024)
			}

			logrus.Debugf("Proxy data transfer: thread=%d, splitSize=%d", numTasks, splitSize)

			// ExoPlayer 兼容性核心：如果请求中有 Range 且要求部分数据，必须返回 206
			if requestRange != "" || statusCode == 206 {
				statusCode = 206
				// 注意：如果请求头中的 Range 只有起点而没有终点 (例如 bytes=12345-)，
				// 在 HTTP/1.1 标准中，服务器返回的 Content-Range 应该是 `bytes 12345-EOF/TOTAL`

				// ExoPlayer 等播放器在拖拽时，如果获取到的 Content-Length 和它期望的不一致，会重置连接。
				// 当我们拦截并修改了 Range 后，必须确保返回正确的 Content-Length 和 Content-Range
				contentLength := rangeEnd - rangeStart + 1
				if contentLength < 0 {
					contentLength = 0
				}

				// 如果是 suffixRange 或者 rangeStart = 0 且没有指定 end 的情况，可能某些播放器需要特定的返回
				if isSuffixRange {
					responseHeaders.Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", rangeStart, rangeEnd, contentSize))
				} else if originalRequestRange == "bytes=0-" {
					// 对于原始请求就是 bytes=0- 的情况，我们应该返回完整的 Content-Range
					responseHeaders.Set("Content-Range", fmt.Sprintf("bytes 0-%d/%d", contentSize-1, contentSize))
				} else if !isExactRange {
					// 如果没有明确的结束位置，按照原始请求的逻辑可能需要保留开放结尾或者我们已经修正了 rangeEnd
					responseHeaders.Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", rangeStart, rangeEnd, contentSize))
				} else {
					responseHeaders.Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", rangeStart, rangeEnd, contentSize))
				}
				responseHeaders.Set("Content-Length", strconv.FormatInt(contentLength, 10))
			} else {
				statusCode = 200
				responseHeaders.Del("Content-Range")
				responseHeaders.Set("Content-Length", strconv.FormatInt(contentSize, 10))
			}

			responseHeaders.Set("Accept-Ranges", "bytes")

			// 先设置响应头，再开始数据传输
			responseHeaders.Del("Transfer-Encoding") // 避免播放器因为存在 Chunked 而拒绝解析 Content-Length
			responseHeaders.Del("Content-Encoding")

			for key, values := range responseHeaders {
				if strings.EqualFold(strings.ToLower(key), "connection") || strings.EqualFold(strings.ToLower(key), "proxy-connection") {
					continue
				}
				w.Header().Set(key, strings.Join(values, ","))
			}

			// 强制设置缓存头，让播放器尽可能缓存已下载的数据，实现秒切回已缓冲位置
			w.Header().Set("Cache-Control", "public, max-age=31536000")
			w.Header().Set("Connection", "keep-alive")
			w.WriteHeader(statusCode) // 206 for partial content

			if req.Method == http.MethodHead {
				return
			}

			// ExoPlayer 会高频拉取，我们需要限制向播放器吐出数据的速度，防止 ExoPlayer 贪婪拉取耗尽内存或带宽
			// io.CopyBuffer 默认会全速复制，这会导致即使播放器不需要那么多数据，也会被强制塞满 TCP 缓冲区
			rp, wp := io.Pipe()
			emitter := base.NewEmitter(rp, wp)

			defer func() {
				if !emitter.IsClosed() {
					emitter.Close()
					logrus.Debugf("handleGetMethod emitter 已关闭-支持断点续传")
				}
			}()

			go ConcurrentDownload(req.Context(), url, rangeStart, rangeEnd, contentSize, splitSize, numTasks, emitter, req)

			// 响应数据，使用较小的 buffer 降低每次复制的吞吐量，配合背压防止 ExoPlayer 贪婪拉取导致带宽暴走
			buf := make([]byte, 32*1024) // 减小 buffer 强制限制单次搬运速度
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
	return key == "host" || key == "http-client-ip" || key == "remote-addr" || key == "accept-encoding" || key == "if-range"
}

func main() {
	// 定义命令行参数
	dns := flag.String("dns", "8.8.8.8", "DNS解析 IP:port")
	port := flag.String("port", "5575", "服务器端口")
	debug := flag.Bool("debug", false, "Debug模式")
	auth := flag.String("auth", "", "认证密钥")
	guessType := flag.Bool("guess-type", false, "是否根据URL强制猜测并设置 Content-Type (可能导致 MPV 等播放器拖拽失败，默认不启用)")

	// 帮助和版本信息
	showHelp := flag.Bool("h", false, "显示帮助信息")
	showHelpLong := flag.Bool("help", false, "显示帮助信息")
	showVersion := flag.Bool("v", false, "显示版本信息")
	showVersionLong := flag.Bool("version", false, "显示版本信息")

	// 自定义 Usage
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "drpyS专用多线程媒体代理服务 %s\n\n", AppVersion)
		fmt.Fprintf(os.Stderr, "用法:\n")
		fmt.Fprintf(os.Stderr, "  %s [参数]\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "参数列表:\n")
		flag.PrintDefaults()
	}

	flag.Parse()

	if *showHelp || *showHelpLong {
		flag.Usage()
		return
	}

	if *showVersion || *showVersionLong {
		fmt.Printf("drpyS专用多线程媒体代理服务 %s\n", AppVersion)
		return
	}

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

	// 设置全局变量
	authKey = *auth
	enableContentTypeGuess = *guessType
	base.DnsResolverIP = *dns
	base.InitClient()
	var server = http.Server{
		Addr:    ":" + *port,
		Handler: http.HandlerFunc(handleMethod),
	}
	// server.SetKeepAlivesEnabled(false)
	server.ListenAndServe()
}
