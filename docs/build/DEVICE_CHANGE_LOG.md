# L06A 设备变更日志

设备：L06A / AS06 VER0106  
管理地址：`192.168.10.61`

## 2026-09-29 23:28:25 +08:00 — SSH 恢复与在线诊断

### 用户授权

用户明确授权执行此前说明的高权限修复，并要求完整记录以便追踪。授权动作限定为：

1. 使用现有管理令牌临时执行一次受控 shell 修复。
2. 在持久分区创建 `/data/root/.ssh` 并安装镜像内预置公钥。
3. 复制 `/etc/passwd` 到 `/data/etc/passwd`，只把 root home 改为 `/data/root`。
4. 将 `/data/etc/passwd` bind mount 到 `/etc/passwd`，随后重启 Dropbear。
5. 取得 SSH 后清除临时执行内容、恢复原厂 TTS 命令和空的远程 TTS 密钥。
6. 通过 SSH/SCP 安装经过本地测试的代理更新，并读取唤醒、录音与 ASR 日志。

### 目的

- 修复旧 Dropbear 未接受 `/root/.ssh` 符号链接下公钥的问题。
- 避免再次线刷，通过网络安装管理页 token 修复和诊断增强。
- 定位“原厂唤醒灯亮但没有后续识别或响应”的具体阶段。

### 回滚

- 当前 bind mount 可通过重启清除。
- 持久启动项若写入 `/data/init.sh`，删除相关 passwd bind 行后重启即可回滚。
- `/data/etc/passwd` 和 `/data/root` 可保留为审计副本；不修改只读 system1 中的 `/etc/passwd`。
- `boot0/system0` 回滚槽不受本次操作影响。

### 执行记录

- 23:28 前：管理 API 正常；代理版本 `2026.09.29-final3`；ASR、LLM、Music 均已配置。
- 23:28 前：原厂唤醒灯可亮，但代理日志无 `wake source=hotword`。
- 23:28 前：Web 模拟唤醒产生 `wake source=web`，约 3 秒空结果结束，旧版本未报告空录音错误。
- 后续命令、校验值和结果继续追加在本节。

## 2026-09-29 23:28 至 2026-09-30 00:09 +08:00 — 执行结果

### SSH 修复与临时提权清理

- 确认本固件 Dropbear 实际读取 `/etc/dropbear/authorized_keys`，而不是 `/root/.ssh/authorized_keys`。
- 将镜像预置公钥复制到 `/data/etc/dropbear/authorized_keys`，权限设为 `0600`，并 bind mount 到 Dropbear 实际路径。
- Windows 私钥 ACL 已收敛为当前用户、SYSTEM 和 Administrators；密钥登录实测返回 `uid=0(root) gid=0(root)`。
- 已解除临时 `/etc/passwd` bind mount；当前只有 `/etc/shadow` 和 `/etc/dropbear/authorized_keys` 使用持久分区挂载。
- 已清除临时命令载荷与临时 TTS 密钥；ASR、LLM 和 Music 密钥保持原值，远程 TTS 仍为空。
- `/data/init.sh` SHA-256：`90b1b96e2f1beb70f180b15987901b78d334d89928f95501404c895c1ee3c220`。

### 根因与语音输入修复

- 直接执行 `arecord -D mico_record` 得到 `Device or resource busy`；`/proc/asound/card0/pcm3c/sub0/status` 显示 PDM 采集设备由 `mipns-xiaomi` 持有。
- 旧代理使用 ALSA 二次开麦，因此唤醒灯能亮，但没有 PCM 送给豆包 ASR；旧错误路径又把空录音当成正常结束。
- 改为保留原厂 `mipns-xiaomi` 对八通道阵列、唤醒、AEC、波束成形和 VAD 的所有权，代理接管 `/tmp/mico_aivs_lab/usock/speech.usock`，接收原厂前端输出的 16 kHz 单声道 PCM。
- `/etc/init.d/pns` 只在运行时通过 bind mount 覆盖，去掉 `-r opus32`；system1 中的原文件未改写。
- 当前 PNS 状态已验证为 `overlay=yes ... codec=pcm socket=ready`，注册日志为 `vendor=xiaomi codec=pcm16 peer=/tmp/mipns/usock/speech.usock`。
- 增加协议测试，覆盖 REGISTER、STREAM_PREPARE、ASR PCM、STOP_CAPTURE 和 DIALOG_FINISH；`go test ./...`、`go vet ./...` 均通过。

### 代理网络更新

- `2026.09.29-final5-native-pcm` 首次部署 SHA-256：`9931242ce10a191e559ab2ae752017d4a3434c6ed62c119debfbc6b9a9d7f9f5`。
- final5 首次实测发现设备的 `ubus` 位于 `/bin/ubus`，不是 `/usr/bin/ubus`；未打开收音，没有改动用户密钥。
- 修正后的 `2026.09.29-final6-native-pcm` SHA-256：`a58cdbdc5840cad4c24735f5ba42d9fc1f69b72ae8575e63d9e4e38637f2966c`。
- 常规原子更新因 `/data` 只有 13.2 MiB，复制 `.new` 时空间不足；旧 final5 未被替换，PNS 自动恢复。失败留下的半截 `/data/assistant/bin/assistant-agent.new` 已清除。
- 随后使用 `/tmp/assistant-agent.rollback` 保存旧二进制，释放目标文件空间后写入 final6；PID、speech socket、PCM PNS 和 SHA-256 全部通过后才删除内存回滚副本。
- 当前运行版本：`2026.09.29-final6-native-pcm`。

### 原厂 TTS 恢复

- 修正 `/usr/share/libubox/jshn.sh` 与 `set -u` 不兼容导致的 `JSON_PREFIX: parameter not set`。
- 确认 `mibrain text_to_speech` 仍返回 `code: -1`，原因为最终镜像删除了 `/usr/bin/mico_aivs_lab` 和 `/usr/lib/libaivs_sdk.so`；原厂授权接口同时返回 `code: -1`。
- 从本项目保存的原始 system1 解包目录取回同版组件，压缩后持久化到 `/data/assistant/native-tts/`，每次启动解压到 `/tmp/assistant-native/`，不再占用 system1。
- `mico_aivs_lab.gz` SHA-256：`d828503fa196332677082041520da3089d57b0d238c04b920e475191466ed497`。
- `libaivs_sdk.so.gz` SHA-256：`96fea3c95fdf8632b3668534135a13eedb2ebcfae7d24d04e01fac664516ee65`。
- 代理的 speech socket 先 self bind mount 保护；恢复的 `mico_aivs_lab` 只取得 `common.usock` 供原厂 TTS 使用，无法替换代理的 speech socket，也不接管 ASR。
- 当前 TTS 命令：`/data/assistant/native-tts.sh`；测试句“语音链路测试成功”返回 `{"ok":true}` 并实机播出。

### 实机闭环验证

- Web 触发测试：PCM 收音成功，豆包识别“介绍一下你自己。”，LLM 返回 93 个中文字符，原厂 TTS 正常播报。
- 连续对话：播报结束后生成新的 follow-up turn，10 秒无语音时由原厂 VAD 结束该轮。
- 真实唤醒词测试：日志出现 `wake source=hotword`；随后正确识别“你是什么模型？”，LLM 回答且原厂 TTS 播报，`last_error` 为空。
- ASR、LLM、Music 配置状态均为 `true`；远程 TTS 为 `false`。

### 当前持久文件与回滚

- `/data/assistant/bin/assistant-agent`：final6 代理。
- `/data/assistant/enable-native-speech.sh`：PCM PNS 切换与 `restore` 回滚。
- `/data/assistant/enable-native-tts-runtime.sh`：原厂 TTS 运行时启动与 `restore` 回滚。
- `/data/assistant/native-tts.sh`：原厂 TTS 调用脚本。
- `/data/assistant/native-tts/*.gz`：同版原厂 AIVS 组件压缩副本。
- `/data/init.sh`：开机恢复 Dropbear 公钥、PCM PNS 和原厂 TTS 运行时。
- 语音输入回滚：执行 `/data/assistant/enable-native-speech.sh restore`。
- TTS 运行时回滚：执行 `/data/assistant/enable-native-tts-runtime.sh restore`，并把 `native_tts_command` 改回所需实现。
- 完整固件回滚仍可使用未修改的 `boot0/system0`。
- 持久分区当前约 94% 使用，剩余约 712 KiB；后续代理更新必须继续使用 `low_space_update.sh`，不能依赖同时保存两个 9.5 MiB 二进制的普通原子更新方式。

## 2026-09-30 00:10 至 00:22 +08:00 — 间歇性静默退出修复

### 现象与证据

- 用户报告已经在唤醒词后说出问题，但灯随后熄灭且没有回答；这里的“空 PCM”表示代理没有收到原厂前端转交的数据，并不表示用户没有说话。
- “停止”在 00:10:06 已被豆包正确识别，但 `/usr/libexec/assistant/playerctl.sh` 因 `jshn.sh` 与 `set -u` 不兼容而报 `JSON_PREFIX: parameter not set`。
- 00:10:46 的失败轮次复现了核心竞态：连续监听 turn 19 正在传输时检测到新的唤醒词；原厂 PNS 先取消 turn 19，再建立 turn 20。旧会话协程随后发送迟到的 `DialogFinish`，在 turn 20 建立约 6 ms 后将其关闭，因而 turn 20 没有向代理产生 PCM。
- 同一机制也解释了此前 turn 16/17 的相邻空流，不是用户未发声，也不是豆包 ASR 返回空文本。

### 修复

- 将修正后的播放与音量脚本持久化为 `/data/assistant/playerctl.sh` 和 `/data/assistant/volume.sh`，去掉 `set -u`；配置已切换到这两个路径，`playerctl.sh stop` 实机返回成功。
- 原生语音轮次增加代际校验：已取消轮次或 ID 早于最新轮次时，其 `StopCapture`、`ExpectSpeech` 和 `DialogFinish` 全部丢弃，不能控制替代它的新轮次。
- 首个原生收音轮次若仍异常产生零 PCM，原厂 TTS 提示“我没听清，请再说一遍”，并只自动重试一次；连续对话等待轮次无人继续说话时按正常结束处理，不再污染 `last_error`。
- 增加 `TestCancelledTurnCannotStopReplacement`，复现“旧轮次取消、新轮次建立、旧轮次迟到控制”的顺序，断言不会再发出控制包；空 PCM 分类测试同时通过。
- `go test ./...` 与 `go vet ./...` 通过。
- 低空间更新脚本和普通网络更新脚本现会先停止并解除原厂 TTS 的 speech socket 保护，更新后按 PCM PNS、原厂 TTS 的顺序恢复；失败回滚路径同样恢复两者。

### 部署与验证

- 当前版本：`2026.09.30-final7-turn-race`。
- 二进制 SHA-256：`e420ec4eb3d4c9bc3d55c1a1d45f6099921754f0492f27062cd9076ba30a9dfc`。
- 低空间部署返回：`LOW_SPACE_UPDATE_OK`；部署时旧二进制保存在 `/tmp`，健康检查通过后删除。
- 部署后状态：代理 `IDLE` 且 `last_error` 为空；PCM PNS 为 `codec=pcm socket=ready`；原厂 TTS 为 `common=ready speech_protected=yes init_overlay=yes`；speech/common 两个 socket 均存在。
- `/data` 当前约 97% 使用，剩余约 384 KiB；继续禁止普通双副本原子更新，必须使用带 `/tmp` 回滚的低空间更新脚本。

### 回滚

- `low_space_update.sh` 在安装或健康检查失败时自动从 `/tmp/assistant-agent.rollback` 恢复上一版本并重启 PCM PNS 与原厂 TTS。
- 若需手工回退 final7，必须先把上一版本二进制放入 `/tmp/assistant-agent.new`，再用同一低空间脚本部署；不要在 `/data` 同时复制两个代理二进制。

## 2026-09-30 00:30 至 00:43 +08:00 — 退出工具、灯效状态与语音打断

### 用户要求

- “退下”等结束表达应可靠关闭连续监听；模型需要能理解自然结束语义并主动调用退出工具。
- 设备处于聆听状态时必须有持续、清楚的 LED 反馈；若无法开启聆听灯，不允许把该轮语音交给 ASR。
- 思考阶段使用 RGB 跑马灯。
- 正在播报回答时再次喊“小爱同学”，立即停止旧回答并进入新一轮聆听。

### 实现

- 新增 OpenAI 兼容函数工具 `end_conversation`。工具描述覆盖结束、退出、退下、休息、停止继续聆听和“今天聊到这里”等语义；收到工具调用后立即结束当前连续对话，不生成普通回答。
- 保留本地快速退出通道，默认退出词表追加“退下”；规范化逻辑接受标点、句尾语气词以及“好了/请/你/可以”等常见前后缀，例如“退下吧”“好了，请你退下吧”“可以退下了”。
- 旧配置在加载时自动补齐新的默认退出词，不要求用户重新保存管理页；设备运行时词表已验证包含 `退下`。
- 原生 speech.usock 在回复 `STREAM_PREPARE_RESPONSE` 之前同步调用聆听灯准备钩子。只有原厂 LED 服务成功开启 effect 1 后才确认语音流；失败时拒绝该轮准备，不向豆包 ASR 开放 PCM。
- 聆听期间每秒重新确认 effect 1，降低其他原厂灯效事件抢占后无指示灯继续收音的风险；连续追问使用同一隐私约束。
- 思考阶段停止原厂 effect 1/2/3/11，直接通过 L06A AW20054 的 18 像素 `led_rgb` 节点绘制带两级拖尾的多色环形跑马灯；进入下一状态时先取消动画并清空像素。
- 播报阶段使用原厂 effect 3，空闲/退出阶段关闭 effect 1/2/3/11。
- 新唤醒在语音流确认前取消旧会话上下文、终止直接播放原厂 TTS 文件的 `miplayer`，并调用持久化播放器控制脚本停止 common 通道；随后才开启聆听灯和新流。
- 原厂 TTS 命令改为绑定会话 context；被新唤醒取消时返回 `context.Canceled`，不会把正常打断写成会话错误。

### 测试与部署

- 新增退出语句规范化和 `end_conversation` 工具识别测试；原有原生协议竞态测试继续通过。
- `go test ./...`、`go vet ./...` 通过。
- 当前版本：`2026.09.30-final8-led-barge-tools`。
- 二进制 SHA-256：`e4d81366d38ac729f81433fe67fc51833720fcaf2a186568e59c279baab210b9`。
- 低空间部署返回 `LOW_SPACE_UPDATE_OK`；PCM PNS 与原厂 TTS 均成功恢复。
- 使用不在固定退出词表中的“我准备睡觉了，你先休息，不要再继续听了”通过真实已配置 LLM 测试；API 返回空回答且成功，设备日志明确记录 `LLM tool=end_conversation`，最终状态为 `IDLE`、`last_error` 为空。
- LED 服务未记录运行错误；模型工具测试期间思考态完成后已清除 effect 1/2/3/11。

### 说明与回滚

- 检查时原厂夜间模式时段为 `23:00–08:00`；原厂配置会影响 effect 1/3 的亮度。思考跑马灯直接写 RGB 像素，亮度由代理内颜色值限制。
- 回滚仍使用 `/tmp` 中的上一版本二进制配合 `low_space_update.sh`；final8 不改写只读 system1，也不改变 boot0/system0。

## 2026-09-30 00:48 至 01:14 +08:00 — 聚合音乐、当前歌曲状态与聆听跑马灯

### 根因与修复

- 实机日志确认用户说“我要听五月天的知足”时，final8 没有匹配本地音乐前缀，LLM 只回答“这就给你放”，但当时没有音乐工具可执行，因此没有真正播放。
- 补齐“我要听、帮我放”等常用表达，并新增 LLM 工具 `play_music`、`music_control`、`get_current_music`；自然语言音乐请求必须调用工具，不能只口头承诺。
- 按用户提供的 MusicFree 插件清单核对真实协议：网易云、QQ音乐、酷我分别搜索，按“歌名 + 歌手”合并为带多个来源的候选；首选 320k，失败后尝试 128k，再切换来源。
- 播放器控制脚本在下达 URL 后轮询 `player_get_play_status`；只有原生 mediaplayer 进入 status=1 才判定成功。新播放前停止旧 common 播放并解除 wakeup 压低状态，避免把旧歌曲仍在播放误判为新歌曲成功。
- 内存中保存当前歌曲、歌手、来源、音质、队列下标和 playing/paused/stopped 状态；“这是什么歌、谁唱的、现在播放什么”可直接回答，LLM 系统上下文也包含当前音乐状态。
- 聆听状态改为 AW20054 的青蓝色双跑马灯，60ms 更新；第一帧在允许 PCM 采集前同步写入，写入失败继续触发隐私保护并拒绝收音。思考状态改用独立的原厂 effect 2，避免与聆听混淆。

### 测试与部署

- 新增三平台同曲合并、跨平台/跨音质回退、音乐工具参数、当前歌曲问法和实机失败原句测试；`go test ./...`、`go vet ./...`、两个 shell 脚本语法检查均通过。
- 当前版本：`2026.09.30-final9-music-aggregate-listen-led`。
- 二进制 SHA-256：`6b9a1150a2d6291994330b2d2bdfb1f4557ea0831980232c142c05371d206abf`。
- 播放控制脚本 SHA-256：`9dd31b8b1153f1254bfd04a73ac48754f3c8ddccbbfe6450c59f5a2fbad14bd9`。
- `low_space_update.sh` 和 `update_assistant_over_network.ps1` 现将二进制与播放器脚本作为同一事务更新；任一步失败会从 `/tmp` 同时恢复 final8、旧播放器脚本、PCM PNS 和原厂 TTS。
- 网络部署返回 `LOW_SPACE_UPDATE_OK`；版本、配置、PCM socket 和播放器脚本检查通过。部署后 `/data` 约 87% 使用，约 1.6 MiB 可用。
- 实机重放“我要听五月天的知足”成功：返回“正在播放五月天的知足，来源网易云音乐，音质320k”；随后“这是什么歌”正确回答，停止播放成功。
- 实机自然说法“能不能找五月天的知足给我听”成功，日志记录 `LLM tool=play_music`，播放、当前歌曲查询和停止全部通过，`last_error` 为空。
- 未远程触发 LED 唤醒测试：自动审批拒绝在没有专项授权时为观察灯效开启麦克风并发送环境音。用户选择下次手动正常唤醒时观察聆听跑马灯。

### 回滚

- 继续使用 `network-access/low_space_update.sh`。更新期间上一版二进制和播放器脚本只保存在 `/tmp`；成功健康检查后删除，失败时自动恢复。

## 2026-09-30 01:14 至 01:44 +08:00 — 原唱优先、音乐断点续播与灯效规则修正

### 原唱和来源选择

- 将点歌请求解析成歌手、歌名和版本。明确指定歌手时，精确歌手和精确歌名排在最前；翻唱、DJ、伴奏、纯音乐、白噪音、片段及其他改编版本降权。
- 同一原唱候选同时存在多个平台时，解析顺序调整为 QQ 音乐、网易云、酷我；单个平台不可播时继续跨平台和跨音质回退。
- LLM 的 `play_music` 工具增加结构化 `artist`、`title`、`version` 参数；模型负责理解自然语言，设备端仍负责实时可播性验证和播放器控制。
- `2026.09.30-final10-original-resume-led` 已实机验证“我要听周杰伦的稻香”返回“来源QQ音乐，音质320k”。final10 二进制 SHA-256：`c570c491f25b1479aabc6d7422feb28161ff2e11aaecdb297063211891094f6a`；播放器脚本 SHA-256：`66a2e016830d4e262a54dea7c8796afbb0a3089dd31f134f9992d169dae30a12`。

### 网络歌曲不能原地继续的根因

- 实机确认网络直链执行 `player_play_operation play` 后播放器仍保持 status=0；原厂 `player_wakeup start/stop` 同样不能恢复这类 URL。因此普通 pause/resume 和原厂唤醒恢复都不适用于第三方直链。
- 原厂 `mediaplayer` 二进制包含 `player_set_positon`、`offset_in_ms` 和 `startPosition`，但公开状态接口不返回当前位置。
- 新实现把当前播放 URL 仅保存在进程内存，同时记录墙钟估算的毫秒位置。唤醒时冻结位置，回答结束后重新打开同一 URL，再调用 `player_set_positon` 跳回中断点。
- 若缓存 URL 已过期，先按原平台和音质重新解析；失败后再按聚合来源重新选择可播地址。用户在问答中执行暂停、停止、切歌或另点一首会递增修订号，旧会话结束时不会错误恢复旧歌。
- URL 字段禁止 JSON 序列化，不写入状态、日志或配置文件。

### 固定 LED 映射

- 聆听和连续追问等待：18 像素常亮青色 `(0,190,230)`，每秒重写一次；第一帧失败时继续拒绝收音。
- 思考：六色 RGB 环形跑马灯，75 ms 一帧。
- 播报：原厂 effect 3；空闲和音乐状态关闭自定义状态灯。
- 该映射已写入实施计划，后续不得再次把聆听和思考效果互换。

### 测试、部署和实机结果

- `go test ./...`、`go vet ./...` 和 `playerctl.sh` 语法检查通过；新增中断位置冻结、过期修订号不得恢复、指定歌手原唱与 QQ 来源优先测试。
- 当前版本：`2026.09.30-final11-seek-resume-led`。
- 二进制 SHA-256：`5362e733acd996141484326e0ae1b19b9df836fa21a89293b490d7efb91ff4b9`。
- 播放器脚本 SHA-256：`55bc05b79cfb1499724dda7b6435dcd9efbb8905ed8891f620cb4a285bb2aff1`。
- 网络部署返回 `LOW_SPACE_UPDATE_OK`；配置、PCM socket 和原厂 TTS 健康检查均通过，没有线刷。
- 不启用麦克风的实机测试依次完成：播放周杰伦《稻香》、等待 6 秒、暂停、等待 2 秒、继续、检查原厂播放器 status=1、停止。代理日志完整记录 play/pause/resume/stop，`last_error` 为空。
- 初次测试时 Windows PowerShell 未绕过用户的本地代理，局域网请求被代理返回 502；设备的 `192.168.10.61:8090/health` 始终返回 200。改用 `-NoProxy` 后测试成功，未发生设备进程崩溃。
- 完整的“音乐中手动唤醒、提问、回答后恢复”和两种灯效仍需用户现场听觉/视觉验收；自动化测试没有远程开启麦克风。

### 回滚

- `low_space_update.sh` 会在安装或健康检查失败时同时恢复上一版代理与播放器脚本；本次没有修改只读 system1、boot0 或 system0。

## 2026-09-30 01:45 至 02:15 +08:00 — 结构化断点恢复、方言识别与跑马灯减闪

### 对 final11 结论的纠正

- final11 的无麦克风测试只确认了恢复命令后 `status=1`，没有检查 `player_set_positon` 的业务返回码；用户现场发现歌曲仍从头播放。
- 重新单独调用后确认：第三方直链经 `player_play_url` 启动时，`player_set_positon` 对 20 和 20000 两种位置均返回 `code=125`。原厂二进制分析表明该码表示当前没有可定位的曲目对象，所以 final11 的直链重开再 seek 方案必然失败。
- 原厂 `player_play_music` 接口可接收带 `payload.audio_items` 的结构化曲目，其中包含 `audio_id`、`stream.url`、`offset_in_ms` 和 `duration_in_ms`；以 30 秒偏移提交后返回 `code=0`，播放器进入 `status=1`。

### 实现

- 音乐搜索结果新增总时长：网易云读取毫秒 `duration`，QQ 音乐读取秒 `interval`，酷我解析 `DURATION`。同曲跨平台合并时会补齐缺失时长。
- 初次播放和中断恢复统一改用 `player_play_music`。恢复请求同时提交歌曲 ID、总时长以及 `offset_in_ms/startOffset`，脚本解析并强制检查 UBus JSON 中的 `code=0`；不再调用已证实无效的 `player_set_positon`。
- 位置仍按设备端墙钟估算；若已接近已知歌曲结尾，恢复点限制到结尾前一秒。缓存 URL 失效时继续沿用原平台、音质及聚合来源回退。
- 豆包实时 ASR 请求新增 `enable_lid=true`。项目提供的接口文档表明该参数默认关闭，开启后支持中英文及上海话、闽南语、四川话/西南官话、陕西话/中原官话、粤语等方言识别和标签返回。
- 思考跑马灯保留 75 ms 节拍和六色分段，但相邻帧只更新六个发生颜色变化的边界像素；此前每帧顺序重写全部 18 个像素，容易产生肉眼可见的局部刷新。
- 固定映射保持为：聆听常亮青色、思考 RGB 跑马灯、播报原厂 effect 3、空闲和音乐关闭自定义灯。

### 后续排期

- P1：恢复本机高频短指令，包括设置/增减音量、静音、播放控制、当前音量和歌曲查询；确定动作走本地快速通道，自然说法通过受限 `device_control` 工具转换参数。
- P1：用同一组普通话和方言句子对 `enable_lid` 做现场 A/B。若仍弱于豆包 App，保存相同的原始 PCM，继续区分远场前端、噪声/回声、VAD 截断和 App 端到端实时语音模型的差异。

### 测试与部署

- `go test ./...`、`go vet ./...` 和 `playerctl.sh` shell 语法检查通过；新增音乐时长解析/合并及跑马灯相邻帧只变化六个像素的测试。
- 部署前在设备 `/tmp` 运行新脚本：结构化 0 秒播放成功；30 秒偏移播放成功且三秒后 `status=1`，随后立即停止并删除临时脚本。该测试确认接口和参数被原厂播放器接受；实际中断后听觉位置仍由用户手动唤醒验收。
- 当前版本：`2026.09.30-final12-track-offset-lid-led`。
- 二进制 SHA-256：`31a29dc5ed548d6ddd329da6b03e0b2162f4ef63caa24ce0ee6f1fd95930a79c`。
- 播放器脚本 SHA-256：`3e8f6c23cce03901a9a4a891e5eefcc37b3e4d516947c0f96f2187c8744842b0`。
- 网络部署返回 `LOW_SPACE_UPDATE_OK`；设备回读 SHA 完全一致。最终状态：HTTP health 为 `ok`，代理版本正确，状态 `IDLE`，`last_error` 为空，ASR/LLM/音乐均已配置；PCM PNS `socket=ready`，原厂 TTS `common=ready speech_protected=yes init_overlay=yes`。

### 回滚

- 本次仍只更新 `/data` 中代理和播放器脚本，不写 system1、boot0 或 system0。失败时由 `low_space_update.sh` 从 `/tmp` 同事务恢复 final11 和旧脚本。

## 2026-09-30 02:30 +08:00 — final13：修复 MUSIC 类型与灯效连续性

### 授权与范围

- 沿用用户明确的网络调试/修复授权。本次仅部署 `/data` 助手和播放控制脚本，没有线刷或写入启动/系统分区。
- 用户要求确定的代码结论落盘：新增持续分析文档 `../L06A_1.88.221_analysis/PLAYBACK_LED_ROOT_CAUSE.md`，保留反汇编位置、原始 trace、采用及未采用方案。
- 用户已确认方言问题解决，本次不调整 ASR。

### 对旧结论的纠正与实现

- final12 的返回 `code=0/status=1` 不证明发生 seek。遗漏 `payload.audio_type=MUSIC` 导致原厂播放器将其识别为普通音频，在 prepared 分支绕过定位。补齐类型和 `needs_loadmore=false`，播放成功检查同时核对 `audio_meta.audio_id` 与 `audio_type`。
- 外层 `duration` 是停止计划而非歌曲时长；现置 0。歌曲总时长仍放在 `stream.duration_in_ms`。
- 此前把 `seek_curtrack_to` 的 125 唯一解释为“无曲目对象”不完整：该返回值还包含曲目类型不可 seek 的分支。详细证据见分析文档。
- 灯环改为 40 FPS、六色插值、单调时钟相位、3.6 秒一圈；复用一个 sysfs FD，聆听/思考间不清屏。聆听青色同步首帧的收音门槛保留，进入空闲时清灯。
- 实验性的直接 I2C 连续写入未通过回读，未纳入生产。继续使用原厂内核灯驱动，不修改芯片电流。

### 验证与证据

- `go test ./...`、`go vet ./...`、播放脚本 `sh -n` 通过。回归测试覆盖完整旋转周期/颜色连续性、聆听思考转换不清屏、首帧失败拒绝收音。
- 65 秒静音 WAV：本地 30 秒真实 seek 到 `960044 = 44 + 30*32000`；独立 40 秒 seek 到 `1280044`。最终脚本复测 30 秒仍准确，身份/类型检查通过、`timed_shutoff=0`。
- 仅绑定回环地址的 HTTP 服务记录先 `bytes=0-`、再 `bytes=960044-`，证明网络解码器确实定位。测试服务结束退出。
- sysfs 200 帧基准平均 8.421 ms、最大 9.360 ms，无超过 25 ms 的帧。候选程序 4 秒 RGB 测试记录 161 帧、间隔平均 25.092 ms、带 strace 最大 44.470 ms；中途没有清屏，原厂 ledserver 未写灯。
- 本轮没有远程打开麦克风。完整唤醒提问后的续播和肉眼流畅度仍待用户验收；进度依旧按墙钟估算，可能有缓冲造成的小幅误差。
- 调试缺失 MUSIC 类型的原厂 `player_get_latest_playlist` 时触发过播放器空指针崩溃，procd 已自动恢复。该接口已从测试路径移除，未修改原厂 ELF。

### 部署结果

- 版本：`2026.09.30-final13-music-seek-smooth-led`。
- 二进制 SHA-256：`3731bb033d689e4f75248c41cde0a3214bbe50edfda590dadb515705af481581`。
- 播放脚本 SHA-256：`850ccfc7c3422814dc44268d90ae2ce485b07a8254b52bc4c5580d41aac8b716`。
- `update_assistant_over_network.ps1` 返回 `LOW_SPACE_UPDATE_OK`，安装时设备 SHA 校验通过；版本、配置、PCM socket 和原厂 TTS 通道检查通过。
- HTTP health=`ok`，状态=`IDLE`，`last_error` 为空，ASR/LLM/音乐配置均就绪。
- 同事务回滚机制保留。无需用户重新刷机或重新填写服务配置。
- 收尾再次回读两文件 SHA 与上述值完全一致；测试播放已停止，音量恢复为调试前的 20。本轮临时测试二进制、脚本和静音 WAV 已从设备 `/tmp` 清理，原始证据保存在电脑分析目录。

## 2026-09-30 02:41 +08:00 — final14：回答后保留低音量播放的实时进度

### 用户问题与根因

- 用户确认音乐在唤醒/回答时只是压低音量、持续播放；回复完毕却回到唤醒时间点。
- final13 的自动收尾仍无条件通过 `play_at` 重建曲目并 seek 至唤醒检查点。seek 已正确，但它不应该在原曲仍播放时发生。
- 分析与实测证据已追加到 `../L06A_1.88.221_analysis/PLAYBACK_LED_ROOT_CAUSE.md`。

### 修复

- 自动会话收尾改用 `finish_interruption AUDIO_ID`。核对原厂曲目 ID/类型，播放状态 1 只执行 `player_wakeup(stop)` 恢复音量；保留对象的状态 2 原地继续。两者都不重开 URL、不 stop、不 seek。
- 原厂已换曲时不抢回旧歌；上下文读取/业务返回错误时不回退到强制重播。用户已暂停、停止或切歌导致修订号变化时，过期会话不执行恢复。
- 保留 duck 期间经过的播放时间，用于之后暂停/继续的估算位置；语音中明确“继续”也走保留对象路径。
- 本轮未修改 LED、ASR 和服务配置。

### 验证

- `go test ./...`、`go vet ./...`、播放脚本语法检查均通过。
- 静音实机测试：同曲 `status=1` 时音量 21→9，自动收尾返回 continued 并恢复 21；真正暂停后返回 resumed；错误 ID 返回 replaced，未影响当前曲目。
- 底层跟踪统计为 0 次音频重新打开、0 次 seek，覆盖 duck、收尾、暂停、原地继续和错误 ID 保护。证据：`final14-retained.trace`、`final14-retained-summary.json`、`final14-native-check.txt`。
- 本轮没有远程开启麦克风；完整语音问答仍待用户现场确认。

### 部署与回退

- 局域网更新成功，返回 `LOW_SPACE_UPDATE_OK`；无需线刷。版本 `2026.09.30-final14-preserve-ducked-playback`。
- 二进制 SHA-256：`89dd2aaea5199a278f20149f74aed16b2613d49540c37f9ba0a2fdcb1adbd205`。
- 播放脚本 SHA-256：`ae692ca2644db4071db9da19ce4cfbebfcd57723c7bb0416f6674ca9e12e9eb4`。
- 设备再次回读两文件指纹一致，HTTP health=ok，状态 IDLE，last_error 为空；PCM socket ready，原厂 TTS common ready。
- 更新前将 final13 两文件保存到 `network-backups/final13/`，SHA 与 final13 发布记录一致。低空间更新事务仍自带失败回滚。
- 静音测试已停止，本轮临时脚本/WAV 已清理。保留用户本轮开始时的音量 21。

## 2026-09-30 03:01 +08:00 — 日志限额与失败保护

- 用户明确授权立即治理日志。旧 `/data/log`、轮转与启动配置、当前内存日志快照已归档至 `network-backups/log-governance-20260930/`，下载哈希一致。
- 安装 `/data/assistant/log-governance/`，由 `/data/init.sh` 建立对 `/usr/sbin/easy_logcut` 的绑定覆盖，现有 cron 与关机轮转共同生效。上限从 2,048 KiB 降为 512 KiB。
- 日志在 RAM 改名后让 syslog-ng 重新打开活动文件；RAM 压缩/校验，写入前腾出归档预算，持久写入校验和同步后才移除源。压缩、部分写入和重新打开失败均保留源以便重试；异常大归档按限额保留最新尾部。
- 本地及设备隔离测试通过；实机轮转前后标记均找到，syslog PID 未变且继续写入。覆盖解除/重新绑定/重复调用通过，完整冷启动及 24 小时自然使用观察待完成。
- 安装前可用 1,584 KiB，收尾可用 **3,040 KiB（2.97 MiB），使用率 76%**；`/data/log` 452 KiB。相邻快照净释放约 1.42 MiB。
- 助手仍为 final14，二进制与播放器 SHA 不变，health=ok / IDLE / last_error 为空；PCM socket 和原厂 TTS ready。没有重启音箱、触发麦克风或写入系统/启动分区。
- 回退入口 `/data/assistant/log-governance/rollback.sh`：删除本次钩子并卸载覆盖；旧日志继续保存在电脑，不自动写回设备。
- 详细实现、限制、哈希与证据见 `../L06A_1.88.221_analysis/LOG_GOVERNANCE_EXECUTION_20260930.md`；源脚本位于 `network-access/log-governance/`。
