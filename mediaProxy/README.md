# MediaProxy

一个高性能的多媒体代理转发服务，基于Go语言开发，支持多线程并发下载和流式传输。

## 功能特性

- 🚀 **高性能并发下载**: 支持多线程分片下载，显著提升下载速度
- 🔄 **流式传输**: 实时转发数据流，无需等待完整下载
- 🛡️ **防SNI阻断**: 支持Base64编码URL和Header，绕过网络限制
- 📦 **智能缓存**: 内置4小时缓存机制，减少重复请求
- 🌐 **自定义DNS**: 支持指定DNS服务器，提升解析速度
- 📱 **Web界面**: 提供简洁的404页面和状态展示
- 🔧 **灵活配置**: 支持动态调整线程数、分片大小等参数
- 🔐 **安全认证**: 支持自定义认证密钥，保护API访问安全

## 快速开始

### 运行程序
```bash
# 使用默认配置运行
./mediaProxy

# 使用自定义端口和认证密钥
./mediaProxy -port 57574 -auth "mySecretKey"

# 开启调试模式
./mediaProxy -debug -auth "mySecretKey"
```

### 基本用法
```bash
# GET请求示例（使用默认auth参数）
curl "http://localhost:57574/?url=https://example.com/video.mp4&auth=drpys"

# GET请求示例（使用自定义auth参数）
curl "http://localhost:57574/?url=https://example.com/video.mp4&auth=mySecretKey"

# POST请求示例
curl -X POST "http://localhost:57574/" \
  -d "url=https://example.com/video.mp4&thread=4&form=base64&auth=drpys"
```

<table>
  <thead>
    <tr>
      <th style="text-align:center;">参数</th>
      <th style="text-align:center;">描述</th>
      <th style="text-align:center;">默认值</th>
      <th style="text-align:center;">示例</th>
    </tr>
  </thead>
  <tbody>
    <tr>
      <td style="text-align:center;">debug</td>
      <td style="text-align:center;">进入调试模式</td>
      <td style="text-align:center;">false</td>
      <td style="text-align:center;">-debug</td>
    </tr>
    <tr>
      <td style="text-align:center;">port</td>
      <td style="text-align:center;">指定程序端口</td>
      <td style="text-align:center;">57574</td>
      <td style="text-align:center;">-port 57574</td>
    </tr>
    <tr>
      <td style="text-align:center;">dns</td>
      <td style="text-align:center;">指定DNS服务器</td>
      <td style="text-align:center;">8.8.8.8</td>
      <td style="text-align:center;">-dns 127.0.0.1:5335</td>
    </tr>
    <tr>
      <td style="text-align:center;">auth</td>
      <td style="text-align:center;">认证密钥，用于API访问验证</td>
      <td style="text-align:center;">drpys</td>
      <td style="text-align:center;">-auth "mykey123"</td>
    </tr>
  </tbody>
</table>

## 安装说明

### 从源码编译

#### 方法1: 使用构建脚本（推荐）
```bash
# Linux/macOS
chmod +x build.sh
./build.sh --all                    # 构建所有平台
./build.sh --platform linux/amd64   # 构建指定平台

# Windows
build.bat                           # 构建所有平台
build.bat -p windows/amd64          # 构建指定平台
```

#### 方法2: 使用Makefile
```bash
make build          # 构建当前平台
make build-all      # 构建所有平台
make quickstart     # 快速开始
make help           # 查看所有命令
```

#### 方法3: 手动编译
```bash
# 克隆项目
git clone <repository-url>
cd mediaProxy

# 安装依赖
go mod tidy

# 编译（体积优化）
go build -ldflags="-s -w" -trimpath -o mediaProxy

# 运行
./mediaProxy
```

### 使用预编译二进制文件
从 [Releases](../../releases) 页面下载对应平台的二进制文件。

### Docker部署
```bash
# 使用docker-compose（推荐）
docker-compose up -d

# 或直接使用Docker
docker build -t mediaproxy .
docker run -p 57574:57574 mediaproxy
```

## 使用示例

### 1. 基础代理下载
```bash
# 代理下载一个视频文件（使用默认auth）
curl "http://localhost:57574/?url=https://example.com/video.mp4&auth=drpys" -o video.mp4

# 使用自定义auth参数
curl "http://localhost:57574/?url=https://example.com/video.mp4&auth=mySecretKey" -o video.mp4
```

### 2. 多线程下载
```bash
# 使用8个线程并发下载
curl "http://localhost:57574/?url=https://example.com/largefile.zip&thread=8&auth=drpys"
```

### 3. 使用自定义Header
```bash
# 添加认证Header
curl "http://localhost:57574/" \
  -d 'url=https://api.example.com/file.mp4' \
  -d 'headers={"Authorization":"Bearer token123","User-Agent":"CustomAgent"}' \
  -d 'auth=drpys'
```

### 4. Base64编码防SNI阻断
```bash
# 对URL和Header进行Base64编码
URL_B64=$(echo -n "https://blocked-site.com/video.mp4" | base64)
HEADER_B64=$(echo -n '{"Referer":"https://example.com"}' | base64)

curl "http://localhost:57574/?form=base64&url=$URL_B64&headers=$HEADER_B64&auth=drpys"
```

### 5. 自定义分片大小
```bash
# 设置每个分片为256KB
curl "http://localhost:57574/?url=https://example.com/file.zip&size=256K&auth=drpys"
```

## 项目架构

```
mediaProxy/
├── proxy.go           # 主程序入口和核心代理逻辑
├── base/              # 基础组件包
│   ├── client.go      # HTTP客户端配置和初始化
│   └── emitter.go     # 数据流发射器，用于流式传输
├── static/            # 静态资源
│   └── index.html     # Web界面（404页面）
├── go.mod             # Go模块依赖
└── README.md          # 项目文档
```

### 核心组件

- **ProxyDownloadStruct**: 并发下载管理器，负责分片下载和数据合并
- **Chunk**: 数据分片结构，管理下载的数据块
- **Emitter**: 流式数据发射器，实现实时数据传输
- **Client**: HTTP客户端封装，支持自定义DNS和代理配置

## 性能优化

- **并发下载**: 自动根据文件大小调整线程数和分片大小
- **内存管理**: 使用缓冲池减少内存分配开销
- **连接复用**: 复用HTTP连接减少握手时间
- **智能缓存**: 缓存热点资源，避免重复下载

## 注意事项

1. **合法使用**: 请确保遵守相关法律法规和网站服务条款
2. **资源限制**: 高并发可能对目标服务器造成压力，请合理设置线程数
3. **网络环境**: 某些网络环境可能需要配置代理或自定义DNS
4. **文件大小**: 超大文件下载建议适当增加缓存时间和分片大小
5. **安全认证**: 建议在生产环境中使用强密码作为auth参数，避免使用默认值

## 贡献指南

欢迎提交Issue和Pull Request来改进项目。

## 许可证

本项目基于开源许可证发布，具体请查看LICENSE文件。

## 致谢

感谢Panda Groove大佬的原始代码贡献。

## 链接参数
headers和url可进行base64编码，以避免sni阻断
<table>
  <thead>
    <tr>
      <th style="text-align:center;">参数</th>
      <th style="text-align:center;">类型</th>
      <th style="text-align:center;">描述</th>
      <th style="text-align:center;">默认</th>
    </tr>
  </thead>
  <tbody>
    <tr>
      <td style="text-align:center;">size</td>
      <td style="text-align:center;">可选</td>
      <td style="text-align:center;">单线程下载数据大小，可动态调节</td>
      <td style="text-align:center;">128K，线程数小于4时，为 2048/线程数 K</td>
    </tr>
    <tr>
      <td style="text-align:center;">thread</td>
      <td style="text-align:center;">可选</td>
      <td style="text-align:center;">并发线程数</td>
      <td style="text-align:center;">动态调节</td>
    </tr>
    <tr>
      <td style="text-align:center;">form</td>
      <td style="text-align:center;">可选</td>
      <td style="text-align:center;">URL与headers编码方式，可指定为<code>base64</code>，防止某些SNI阻断，默认<code>urlcode</code>编码</td>
      <td style="text-align:center;">urlcode</td>
    </tr>
    <tr>
      <td style="text-align:center;">headers</td>
      <td style="text-align:center;">可选</td>
      <td style="text-align:center;">POST或GET所用的headers，采用JSON格式</td>
      <td style="text-align:center;"><code>{"User-Agent": "Mozilla/5.0 (Windows NT 10.0; WOW64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/86.0.4240.198 Safari/537.36"}</code></td>
    </tr>
    <tr>
      <td style="text-align:center;">url</td>
      <td style="text-align:center;">必要</td>
      <td style="text-align:center;">POST或GET的目标地址</td>
      <td style="text-align:center;">无</td>
    </tr>
    <tr>
      <td style="text-align:center;">auth</td>
      <td style="text-align:center;">必要</td>
      <td style="text-align:center;">API访问认证密钥，必须与服务器启动时设置的auth参数一致</td>
      <td style="text-align:center;">drpys</td>
    </tr>
  </tbody>
</table>
