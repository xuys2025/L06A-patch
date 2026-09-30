# L06A 自定义语音助手实施计划

## 1. 项目目标

在小米 L06A（AS06 VER0106）上实现以下能力：

1. 保留原厂“**小爱同学**”六麦克风远场唤醒。
2. 唤醒后将语音交给豆包流式 ASR 2.0。
3. 将识别文本发送给可配置的 LLM。
4. 优先复用原厂 TTS，并预留豆包 TTS 后端。
5. 支持唤醒后的连续对话，允许配置等待时间、最大轮数和退出条件。
6. 支持网易云音乐搜索、点播、暂停、继续、切歌和播放列表。
7. 保留原厂系统恢复槽和完整回滚路径。

## 2. 安全边界

- `boot0/system0` 继续作为原厂恢复槽，不写入。
- 所有新镜像仅以原厂 `1.88.221 system1` 备份为基础构建。
- 构建完成后先做离线校验、文件系统校验和依赖检查。
- 再次写入前单独生成镜像哈希、写入范围和回滚说明，等待明确批准。
- 写入时只写 `system1`；写后按镜像实际长度完整回读并核对 SHA-256。

## 3. 推荐总体架构

```text
原厂 mipns-xiaomi / 六麦克风前端
              │
              ├─ “小爱同学”唤醒事件
              │
              ▼
        会话状态机（音箱端）
              │
              ├─ 处理后的 16 kHz 单声道 PCM
              ▼
       豆包流式 ASR 2.0
              │ 最终文本
              ▼
       路由器 / 意图判断
          ┌───┴─────────┐
          │             │
          ▼             ▼
       LLM 对话      网易云音乐控制
          │             │
          ▼             ▼
  原厂 TTS / 豆包 TTS   本机播放器
          │             │
          └──────┬──────┘
                 ▼
              扬声器
```

## 4. 分阶段实施

### 阶段 A：原厂语音链路勘察

- 从原厂 `1.88.221 system1` 备份恢复并分析：
  - `mipns-xiaomi`
  - `pns_ubus_helper`
  - `mibrain_service`
  - `mico_aivs_lab`
  - `/bin/wakeup.sh`
  - `/usr/share/xiaomi` 模型与音频前端配置
- 确认 L06A 上的唤醒事件、处理后 PCM 获取点和原厂 TTS 调用。
- 记录原厂进程、socket、FIFO、ALSA 设备及占用关系。

**完成标准：**“小爱同学”可稳定触发自定义 hook，并能取得一段可识别的干净语音。

### 阶段 B：最小混合固件

- 以原厂 `1.88.221 system1` 为底，不运行删除小米组件的补丁。
- 增加最小维护能力：SSH、可信根证书、启动入口和诊断工具。
- 使用 `/data/init.sh` 加载运行时组件，避免频繁重刷只读 rootfs。
- 所有密钥和用户配置仅保存在 `/data`，不写入固件镜像。

**完成标准：**原厂唤醒、原厂播放、网络和 SSH 同时正常。

### 阶段 C：豆包流式 ASR 2.0

- WebSocket：`wss://openspeech.bytedance.com/api/v3/sauc/bigmodel_async`。
- 输入：16 kHz、16-bit、mono、raw PCM。
- 默认参数：
  - `enable_nonstream=true`
  - `show_utterances=true`
  - `result_type=full`
  - `end_window_size=900`
  - `force_to_speech_time=1000`
- 仅采用最终确定结果；中间结果用于日志和调试。
- API Key 从 `/data/doubao_asr.env` 读取。

**完成标准：**唤醒后首轮语音稳定转成中文文本，无截断、无重复提交。

### 阶段 D：LLM 接入

- 在音箱端部署一个小型 ARM32 `assistant-agent`，由它直接连接云端 LLM；不在 L06A 上运行本地大模型。
- 抽象 OpenAI 兼容的流式接口，配置项包括：
  - API Base
  - API Key
  - Model
  - System Prompt
  - 超时与最大输出长度
- 支持 SSE 流式响应和按中文句子切分。
- 为音乐、音量、停止等本地指令设置优先路由，减少不必要的 LLM 调用。
- 对话历史保存在内存或 `/tmp`，不把隐私写入固件。
- 密钥放在 `/data/assistant/secrets.env`，权限设为 `0600`；固件镜像中只放配置模板。
- 云端 LLM 模式不需要电脑或 Home Assistant 常驻；若以后改用本地 Ollama/vLLM，才需要 NAS/电脑提供模型服务。

**完成标准：**ASR 文本可进入指定 LLM，首句响应能够尽早开始播报。

### 阶段 E：TTS

- `TTS_ENGINE=native`：优先调用原厂 `mibrain text_to_speech`。
- `TTS_ENGINE=doubao`：预留豆包流式 TTS。
- 增加超时、失败回退和按句播放队列。
- 避免 TTS 播放音频重新进入 ASR；需要验证参考声道与回声抑制。

**完成标准：**LLM 长回答可以分句连续播放，失败时不会卡死会话。

### 阶段 F：连续对话（超级小爱式）

- 音箱端维护会话状态机：
  - `IDLE`
  - `WAKE`
  - `LISTENING`
  - `THINKING`
  - `SPEAKING`
  - `FOLLOWUP_WAIT`
- 可配置参数：
  - `FOLLOWUP_ENABLED`
  - `FOLLOWUP_TIMEOUT_SEC=10`：TTS 结束后，等待用户开始说话的时间。
  - `FOLLOWUP_MAX_TURNS=8`：一次唤醒后的最大问答轮数。
  - `UTTERANCE_END_SILENCE_MS=900`：说话过程中，连续静音多久判定一句结束。
  - `SESSION_MAX_SEC=120`：整次连续会话的最长保护时间。
  - `BARGE_IN_ENABLED=0`：第一版关闭播放中打断。
  - `EXIT_PHRASES=退出,结束对话,不用了,再见`
- 第一版在 TTS 播放结束后重新监听。
- 播放中打断（barge-in）作为后续增强，需要单独验证回声消除和并发采集。

**完成标准：**只唤醒一次即可进行多轮问答；超时、静音或“退出”指令会安全结束会话。

### 阶段 G：网易云音乐

- 第一版采用已验证的 **MusicFree 官方版音源 API**。音箱端 `assistant-agent` 直接完成网易云搜索、播放地址解析和本地队列控制，不要求 Home Assistant、电脑或 NAS 常驻。
- 只移植所需协议，不在 L06A 上运行第三方 JavaScript 插件：搜索走网易云搜索接口；播放时向音源服务的 `/api/music/url` 请求 URL，并在 `X-API-Key` 请求头中传入密钥。
- 密钥统一从 `/data/assistant/secrets.env` 读取，文件权限设为 `0600`；不得写入 system1、源码、构建日志或诊断输出。
- 2026-09-29 实测 5 首歌曲的 `128k` 地址全部成功；同一有效歌曲的 `320k`、`flac`、`flac24bit`、`hires`、`atmos`、`master` 均返回地址。音频 CDN 支持 HTTP Range，`128k` 样本返回 `206 Partial Content` 和 `audio/mpeg`。
- 播放器在开始播放前按音质能力选档；解析失败、单曲源异常或 URL 失效时，降级音质后重试一次，再跳过并播报原因。
- Music Assistant 2.9+ 与本地 NetEase API 保留为后备路线，用于第三方音源服务不可用、密钥失效或以后需要账号歌单同步的情况。
- `go-musicfox/netease-music` 和网易云官方 `@music163/ncm-cli` 继续作为实现参考，不直接移植完整播放器到 L06A。
- 实现本地音乐意图：搜索、播放、暂停、继续、上一首、下一首、停止、音量。
- 对 API 返回的失败、地区或版权不可播曲目直接跳过并给出语音提示。

**完成标准：**语音点歌能够解析曲目并由音箱播放，控制命令稳定，登录凭证不写入固件。

### 阶段 H：验证与发布

- 安静、远场、噪声和音乐播放场景下测试唤醒。
- ASR 测试首字截断、方言、网络抖动和静音判停。
- 连续对话测试超时、最大轮数、TTS 回声和异常恢复。
- 网易云测试登录失效、VIP/版权不可播、URL 过期和切歌。
- 生成构建报告、SHA-256、回读验证方案和恢复步骤。

## 5. 研究清单

- [x] LLM 可以由音箱端直接连接；云端 LLM 不需要常驻局域网服务。
- [ ] L06A 原厂处理后 PCM 的稳定获取点。
- [x] 原厂固件包含连续对话、`ExpectSpeech`、多轮重开麦克风和 ASR socket；仍需实机确认调用参数和最终文本出口。
- [ ] 播放中打断所需的回声消除能力。
- [ ] 原厂 TTS 在保留最小服务集时能否独立工作。
- [x] 网易云现成方案已比较；MusicFree 官方版音源的清单、网易云脚本、搜索、播放 URL、音质和 Range 播放已验证，确定为首选直连方案。

## 6. 当前决策

- 唤醒：原厂“**小爱同学**”。
- ASR：豆包流式语音识别模型 2.0，开启句末二遍识别。
- TTS：原厂优先，豆包作为可切换后端。
- 音乐：MusicFree 官方版音源 API 直连；Music Assistant 作为后备。
- 原厂恢复槽：保留 `boot0/system0`。
- 新开发与验证目标：仅 `system1`。

## 7. 研究结论（2026-09-29）

### 7.1 LLM：可以接入

原厂 `1.88.221 system1` 已包含 `/usr/bin/curl`、CA 证书和网络栈，具备发起 HTTPS 请求的基础。参考项目 `open-speaker-llm/xiaomi_ai_llm` 也验证了老款小爱音箱可以在设备端直连 DeepSeek、MiniMax、Claude 和 OpenAI 类接口，并用 SSE 流式返回；它只实测 S12A，因此这里只复用架构，不直接照搬二进制或固件补丁。

推荐实现：

```text
L06A assistant-agent
    ├─ 豆包 ASR WebSocket
    ├─ OpenAI 兼容 LLM HTTPS/SSE
    ├─ 原厂 mibrain TTS 或豆包 TTS
    └─ 局域网 Music Assistant 控制接口
```

结论：

- 使用云端 LLM 时，音箱可以独立运行，Home Assistant 和电脑都不是语音主链路的必需组件。
- L06A 只运行协议客户端和状态机，不适合本机加载大模型权重。
- 如果 API 不兼容 OpenAI 协议，只需增加 provider 适配器，不改语音状态机。
- 第一版先完整收集一句 LLM 文本再交给原厂 TTS；稳定后再做“LLM 流式分句 → TTS 队列”，缩短首句延迟。

### 7.2 连续对话：L06A 原厂栈已有基础

对原厂镜像的静态检查发现：

- `/bin/wakeup.sh` 已有 `multirounds` 和 `continous_md_end` 分支。
- `mipns-xiaomi` 包含 `continuous dialog, reopen mic`、`ExpectSpeech`、`multirounds`，并连接 `/tmp/mico_aivs_lab/usock/speech.usock`。
- `mico_aivs_lab` 包含 `Dialog.TurnOnContinuousDialog`、`EnterTemporaryContinuousDialog`、`ExitContinuousDialog`、`TurnOffContinuousDialog`，还会读写 `/data/mipns/dialog_continuous`。
- 原厂链路已有六麦克风加一路参考声道，具备做回声抑制的硬件输入条件。

这比从 ALSA 强行抢麦克风更有希望。实施顺序为：

1. 用 bind mount 替换 `/bin/wakeup.sh` 为记录型 hook，只记事件，不改变原厂行为。
2. 打开原厂连续对话后，记录 `pnshelper`、AIVS 指令、`speech.usock` 和最终 `RecognizeResult`。
3. 如果能稳定取得原厂最终文本，连续追问直接复用原厂 ASR-only 会话。
4. 如果只能取得处理后 PCM，则把这段 PCM 送豆包流式 ASR 2.0。
5. 状态机自己的 10 秒 watchdog 负责可配置的追问窗口；即使原厂内部超时不能修改，也能在上层可靠结束会话。

第一版的“连续监听”定义为 **TTS 播放结束后自动重新开麦**。播放过程中说“停”即可打断属于全双工 barge-in，需要验证参考声道、AEC 和播放器并发，单独作为第二版。

### 7.3 网易云音乐：可以接入，首选音箱直连音源 API

| 方案 | 当前情况 | L06A 适配判断 | 结论 |
|---|---|---|---|
| MusicFree 官方版音源 API | 已验证网易云搜索、歌词、歌单和播放 URL 接口；密钥允许 `128k` 到 `master` 档位 | 只需在 `assistant-agent` 增加轻量适配器，音箱可独立搜索和直放 CDN 音频 | **第一选择** |
| Music Assistant 2.9+ NetEase provider | 已有搜索、歌单、推荐、歌词、扫码登录和多音质；依赖 NeteaseCloudMusicApi 兼容服务 | 适合第三方音源失效时切换，或以后需要账号歌单同步 | 服务端后备 |
| 网易云官方 `@music163/ncm-cli` | 需开放平台 AppId/privateKey、Node.js 18+、登录；播放端依赖 mpv | 适合装在 NAS/电脑，不适合原厂 ARMv7 小系统 | 服务端备选 |
| go-musicfox | 持续维护，发布 Linux ARM 构建，并把网易云逻辑拆成 `netease-music` Go 包 | 可提取 SDK 做无界面 ARM resolver；完整 TUI 和播放器依赖较重 | 设备直连后备 |
| XiaoMusic | 已于 2026-06 归档，后继 Songloft 偏向自托管本地音乐和插件 | 可以参考小爱音箱控制方式，但不作为新的网易云主链路 | 不选作主方案 |

已验证的 MusicFree 官方版订阅返回网易云插件版本 `8.4+9b28cdffd1c7`。静态检查表明：搜索、专辑、歌手、歌单、歌词和排行榜主要调用网易云接口；真正的播放地址由音源服务使用 API Key 解析。插件依赖 `axios`、`crypto-js`、`qs`、`big-integer`、`dayjs` 和 `cheerio`，因此设备端应实现最小原生适配器，不应为了运行插件而加入整套 Node.js 依赖。

音乐链路建议：

```text
语音文本 → 本地音乐意图路由
         → 网易云搜索接口取得歌曲 ID
         → MusicFree 音源 API 解析播放 URL
         → 本机队列与播放器直接播放 CDN 音频
```

这条首版链路不需要额外局域网服务。它依赖第三方云端音源和用户密钥，所以适配器必须把 `403`、`429`、`5xx`、超时、空 URL 和单曲不可播分开处理。实测一个旧歌曲 ID 返回 `500`，而随后 5 个有效搜索结果均成功，说明不能把单曲解析失败误判成密钥整体失效。

## 8. 推荐实施顺序

1. **混合固件基线**：恢复原厂语音组件，同时保留 SSH、`/data/init.sh`、可信根证书和局域网播放器。
2. **只读事件探针**：验证“小爱同学”hook、原厂最终文本、原厂 TTS、连续对话入口和处理后 PCM。
3. **单轮闭环**：原厂唤醒 → 豆包 ASR → LLM → 原厂 TTS。
4. **免唤醒多轮**：加入 10 秒追问窗口、8 轮上限和 120 秒会话上限。
5. **网易云点播**：实现 MusicFree 音源 API 适配器、本地播放队列和音质降级，先完成搜索/播放/暂停/切歌。
6. **体验增强**：LLM 流式分句 TTS、豆包 TTS 备份、播放中打断、灯效反馈。

## 9. 下一阶段的验收门槛

- 不写设备时先完成混合镜像构建、SquashFS 校验和原厂组件依赖检查。
- 首次实机镜像必须先验证原厂唤醒、原厂 TTS、Wi-Fi、SSH 和回滚槽。
- 连续对话必须在 10 秒静音、最大轮数、网络断开和 TTS 失败时都能回到 `IDLE`。
- 音乐播放必须验证 MPD/upmpdcli 与原厂播放器不会同时占用 ALSA。
- 所有密钥、网易云 Cookie 和对话历史均留在 `/data` 或局域网服务端，不进入 system1 镜像；MusicFree API Key 也不得出现在崩溃日志和调试页面。

## 10. 当前体验规则与排期（2026-09-30）

### 下一阶段总排期

新增 [语音流程改进与实施排期](L06A_VOICE_IMPROVEMENT_ROADMAP.md)，根据 final14 现场日志和当前代码排为三批：

1. **P0，第一批**：先补按 session/turn 关联的诊断，修复打断时新旧会话交接、ASR 音频/最终结果收尾、错误反馈及聆听灯生命周期。
2. **P1，第二批**：自然控制表达与退出意图区分、旧音量脚本同步、推荐音乐的搜索/选择/播放流程、工具结果与短期上下文。
3. **P1/P2，第三批**：短答与分句 TTS、播报任务取消、连续追问策略、分阶段耗时和状态展示。

以上为待实施排期，本次只分析与写文档，设备仍运行 final14。每批通过自动化和集中现场验收后再推进下一批，使用网络更新。

以下状态灯映射作为固定产品规则，后续功能修改不得互换：

| 状态 | 灯效 | 状态 |
|---|---|---|
| 聆听、连续追问等待 | 18 像素常亮青色；首帧写入成功后才允许收音 | 已部署，待用户手动唤醒验收 |
| 思考、等待 LLM | 明亮 RGB 环形跑马灯 | 已部署，待用户手动唤醒验收 |
| 播报 | 原厂播报灯效 3 | 已部署 |
| 空闲、音乐播放 | 熄灭自定义状态灯 | 已部署 |

音乐体验任务按以下顺序执行：

1. **P0，已完成**：指定歌手和歌曲时优先原唱、精确歌手，并优先尝试 QQ 音乐来源；周杰伦《稻香》已实机验证为 QQ 音乐 320k。
2. **P0，final14 修复会话后回溯，待现场验收**：final13 已证明 MUSIC 类型下 seek 生效，但用户发现原厂在会话期间只是压低音乐、仍向前播放，助手收尾强制回到唤醒点。final14 收尾核对原曲：仍播放则只恢复音量，保留的暂停对象则原地继续，不重开 URL 或 seek；不同曲目/无效状态不重播旧歌。实机静音 duck/继续测试已确认无音频重开和 seek，且 9→21 恢复原音量。详见 `L06A_1.88.221_analysis/PLAYBACK_LED_ROOT_CAUSE.md`。
3. **P1，后续增强**：若固件以后找到可读的真实播放位置接口，用播放器位置替代墙钟估算，消除网络缓冲造成的少量进度偏差。

设备短指令按以下顺序恢复：

1. **P1，本地快速通道**：音量设为数值、增大/减小音量、静音/取消静音、播放/暂停/继续、上一首/下一首/停止、查询当前音量和歌曲。高频且确定的控制直接在设备执行，避免网络和 LLM 延迟。
   - final13 收尾已确认设备旧 `volume.sh` 的 `set -eu` 与 jshn 不兼容；本地虽已改 `set -e`，设备仍未同步。该任务需一起部署脚本修正；原厂 UBus 调音量已验证可用。
2. **P1，LLM 工具通道**：向模型暴露同一组 `device_control` 动作，使“声音小一点”“先别唱了”等自然说法能转换成受限参数；设备端负责范围校验、状态保存和执行结果。
3. **P2，原厂指令盘点**：从原厂资源和日志整理可在当前离线/自定义链路恢复的短指令，逐项加入回归清单。

跑马灯与 ASR 排期：

1. **P0，final13 已部署，待视觉验收**：用户确认 final12 仍闪烁；改为 40 FPS 连续颜色插值，3.6 秒一圈，复用原厂 sysfs 文件描述符。取消聆听/思考之间的清屏和重复原厂 shut；保持“聆听常亮青色、思考 RGB 跑马灯”。实机 4 秒段记录 161 帧，中途无清屏；详细耗时和 trace 已落盘。
2. **已解决，用户确认**：final12 开启豆包 ASR `enable_lid=true` 后，用户已明确确认方言问题解决。保留当前实时流式 2.0 配置。
3. **P0，打断后音频交接**：02:42 与 02:43 的两次播报打断后出现 45000081 音频包超时，已记录到独立故障分析，纳入第一批排期。该任务处理会话/音频生命周期；方言识别配置继续保持已验收状态。

语义职责保持为：LLM 负责把自然语言转换成结构化的歌手、歌名、版本和控制意图；设备端工具负责实时搜索、版权/可播性验证、来源回退、播放器状态和断点恢复。

## 10. 参考项目

- `open-speaker-llm/xiaomi_ai_llm`：https://github.com/open-speaker-llm/xiaomi_ai_llm
- Music Assistant NetEase provider：https://github.com/music-assistant/server/tree/dev/music_assistant/providers/neteasecloudmusic
- 网易云官方 Agent Skills / `ncm-cli`：https://github.com/NetEase/skills
- `go-musicfox`：https://github.com/go-musicfox/go-musicfox
- XiaoMusic（已归档）：https://github.com/hanxi/xiaomusic
- Songloft：https://github.com/songloft-org/songloft

## 11. 已完成的离线实现（2026-09-29）

- 已构建 ARMv7 静态 `assistant-agent`，实现豆包 ASR 2.0 WebSocket、OpenAI 兼容 LLM、原厂 TTS、可选远程 TTS、连续会话状态机和局域网管理页。
- 已将原厂 `wakeup.sh` 的“小爱同学”事件接到自定义代理；会话结束主动调用原厂 `ready` 链路收尾灯效和麦克风状态。
- 已实现网易云搜索、MusicFree URL 解析、`320k → 128k` 降级、不可播结果跳过、本地十首队列、暂停、继续、上一首、下一首、停止和音量。
- 已保留 `mipns-xiaomi`、`mibrain_service`、原厂播放器和所需共享库；已删除禁用且无其他引用的 `mico_aivs_lab` 独立云 ASR 运行时与 `libaivs_sdk.so`。
- 已启用 Dropbear、持久 SSH 公钥、持久配置、Web 管理令牌、`/data/init.sh` 和校验后的网络原子更新。
- 已生成最终镜像 `L06A_1.88.221_hybrid-assistant_final.squashfs`，独立解包检查通过：40,402,944 bytes，SHA-256 `2ba23f9bdebe5e94f8dbfaa75ba8a8b19643a644abbce4a2153436057d1b0801`，距 `system1` 终点余 1,540,096 bytes。
- 尚需一次实机刷写和现场验收：原厂远场唤醒、`mico_record` 并发采集、原厂 TTS 账号可用性、CDN 播放和连续对话回声表现。上述项目均可通过管理页和 SSH 调试；代理程序后续更新不需要再次线刷。
