# screenshot-plat

Windows 屏幕截图识别工具。程序在电脑后台运行，可通过手机网页按钮或电脑全局热键触发截图；截图交给 SiliconFlow 视觉模型识别后，手机端会实时显示题目、答案与解析。

日常使用不需要打开两个终端窗口：完成一次初始化后，双击 `start-hidden.vbs` 即可同时启动服务端和截图客户端。

## 工作流程

1. 手机点击“截图并识别”，或电脑按下 `F8`。
2. 电脑截取主显示器画面，并通过本机 TCP 连接交给服务端。
3. 服务端调用 SiliconFlow 视觉模型分析截图。
4. 服务端只把题目、答案、模型和错误等文字推送到手机，不向手机发送截图。

## 使用条件

- Windows 电脑。
- Go 1.22 或更高版本，仅首次构建或修改代码后需要。
- 可用的 SiliconFlow API Key，以及支持图片输入的视觉模型。
- 电脑和手机连接同一个局域网。电脑使用网线、手机使用同一路由器的 Wi-Fi 也可以。

## 首次配置

在项目根目录打开 PowerShell，创建 `bin` 目录并复制配置模板：

```powershell
New-Item -ItemType Directory -Force bin | Out-Null
Copy-Item screensot-server\config.json.example bin\config.json
```

打开 `bin/config.json`，至少修改以下内容：

```json
{
  "models": [
    "Qwen/Qwen3-VL-32B-Instruct"
  ],
  "siliconflow_base_url": "https://api.siliconflow.cn",
  "siliconflow_api_key": "填写你的 SiliconFlow API Key",
  "vision_timeout_seconds": 120,
  "vision_max_tokens": 4096,
  "vision_max_retries": 2,
  "vision_retry_delay_ms": 2000,
  "mobile_access_token": "替换为你自己的随机字符串",
  "template_path": "web/result.html"
}
```

注意：

- `models` 中必须填写支持图片输入的视觉模型。纯文本模型会返回“not a VLM”错误。
- 不要把真实 API Key 提交到 Git；`bin/config.json` 已被 `.gitignore` 忽略。
- `mobile_access_token` 用于限制手机页面访问，请改成不容易猜到的随机字符串。

## 首次构建

在项目根目录依次执行：

```powershell
cd screensot-server
go build -ldflags "-H=windowsgui" -o ..\bin\screenshot-server.exe ./cmd/server

cd ..\screenshot
go build -ldflags "-H=windowsgui" -o ..\bin\screenshot-client.exe ./cmd/client

cd ..
```

构建成功后，`bin` 目录应包含：

```text
bin/
├─ config.json
├─ screenshot-server.exe
└─ screenshot-client.exe
```

## 日常启动

1. 双击项目根目录的 `start-hidden.vbs`。
2. 服务端和客户端会在后台启动，电脑桌面不会出现终端窗口。
3. 手机浏览器打开局域网页面。
4. 点击手机页面上的“截图并识别”，或在电脑上按 `F8`。
5. 等待手机页面实时显示识别结果。

`F8` 被系统功能键占用时，可以使用备用热键 `Ctrl+Alt+S`。

需要停止程序时，双击 `stop-hidden.vbs`。

## 手机访问地址

启动后，服务端会把可用地址写入：

```text
bin/logs/server.log
```

查找以 `Mobile page:` 开头的记录，例如：

```text
Mobile page: http://192.168.1.10:8848/mobile?token=你的访问令牌
```

如果日志中出现多个地址，应选择电脑当前网卡的局域网 IPv4 地址，不要使用 `127.0.0.1`。也可以执行 `ipconfig` 查看电脑以太网适配器的 IPv4 地址，然后按下面格式访问：

```text
http://电脑IPv4地址:8848/mobile?token=bin/config.json中的mobile_access_token
```

## 修改配置

后台程序优先读取 `bin/config.json`。更换模型、API Key、超时或重试参数后，不需要重新构建：

1. 双击 `stop-hidden.vbs`。
2. 修改 `bin/config.json`。
3. 双击 `start-hidden.vbs`。

只有修改 Go 源码后才需要重新执行构建命令。

主要配置项：

- `models`：SiliconFlow 视觉模型列表。
- `siliconflow_api_key`：SiliconFlow API Key。
- `vision_timeout_seconds`：一次分析的总超时秒数，默认 `120`。
- `vision_max_tokens`：模型最大输出 token，默认 `4096`。
- `vision_max_retries`：限流、服务繁忙或空结果时的最大重试次数，默认 `2`。
- `vision_retry_delay_ms`：首次重试等待时间，默认 `2000` 毫秒。
- `mobile_access_token`：手机页面访问令牌。

## 日志与排错

后台日志位于：

- `bin/logs/server.log`
- `bin/logs/client.log`

常见问题：

- 手机打不开页面：确认手机和电脑处于同一局域网，并允许 Windows 防火墙访问 TCP 端口 `8848`。
- 手机按钮没有反应：检查 `client.log` 是否出现“已连接到服务器”，并检查 `server.log` 是否出现 `TCP client connected`。
- 提示 `not a VLM`：当前模型不支持图片输入，请在 `bin/config.json` 中改用视觉模型并重启。
- 提示 HTTP 429：SiliconFlow 当前限流或服务繁忙，程序会按配置自动重试，也可以稍后再试或更换模型。
- 识别超时：适当增大 `vision_timeout_seconds`，然后重启程序。
- 修改配置没有生效：确认修改的是 `bin/config.json`，并在修改后停止、重新启动后台程序。

## 项目结构

```text
.
├─ start-hidden.vbs            # 无窗口启动服务端和客户端
├─ stop-hidden.vbs             # 停止后台程序
├─ screenshot/                 # Windows 截图客户端、全局热键和 TCP 通信
├─ screensot-server/           # HTTP/SSE 页面、截图接收和 AI 分析
└─ bin/                        # 本地配置、构建产物和日志，不提交到 Git
```

## 开发验证

修改代码后，在两个模块根目录分别执行：

```powershell
go fmt ./...
go test ./...
go vet ./...
```

然后重新构建 `screenshot-server.exe` 和 `screenshot-client.exe`。
