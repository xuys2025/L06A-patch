# 2026-09-30 02:43：周杰伦歌曲介绍后，推荐请求未响应

## 调查范围

用户要求读取刚才的日志，解释“介绍周杰伦歌曲后让助手推荐一首，助手却结束，没有播放”的失败原因。本轮只读取日志和分析代码，未修改程序或部署固件，未打开麦克风。

原始证据：`dialogue-diagnostic-20260930-024444.json`。当前版本：`2026.09.30-final14-preserve-ducked-playback`。

## 已确认的时间线（+08:00）

| 时间 | 事件 | 含义 |
|---|---|---|
| 02:42:59 | turn 6，手动唤醒 | 介绍请求开始 |
| 02:43:03 | `recognized: 介绍一下周杰伦有哪些经典歌曲？` | 这句话识别成功 |
| 02:43:05 | `LLM response chars=111` | 模型已生成回答；日志未保存回答全文 |
| 02:43:22 | `barge-in stopped current answer` | 再次唤醒打断原回答 |
| 02:43:23 | turn 7，`prepared` / `wake` | 新一轮会话已建立 |
| 02:43:31 | `ASR code 45000081: [Timeout waiting next packet] waiting next packet timeout: 8.000000 seconds, session has ended` | ASR 端连续 8 秒未等到下一音频包，结束会话 |
| 02:43:38–41 | turn 8 取消，turn 9 新唤醒 | 另一次重试/唤醒 |
| 02:43:47 | `recognized: 我要听周杰伦的中国风歌曲，请你帮我选一首。` | 后续完整请求识别成功 |
| 02:43:48 | `music action=play result=正在播放周杰伦的发如雪，来源QQ音乐，音质320k` | 后续点播成功 |

## 可作出的结论

1. 与用户描述最吻合的是 turn 6 → turn 7。turn 7 没有任何 `recognized`、LLM 结果、音乐工具或播放失败记录，因此失败点在音频/ASR阶段，尚未进入推荐和音乐播放阶段。
2. 日志没有 `LLM tool=end_conversation`，不能把此次结束归因于模型自主选择退下。
3. 服务端报的是等待音频包超时，并不证明用户没说话，也不等于模型没理解推荐意图。当前日志没有每轮 PCM 接收/发送包数，不能唯一确定是原厂停止送音、助手转发阻塞还是网络发送异常。
4. 02:42:40 打断前一次回答后的 turn 4，也在 02:42:48 报相同的 8 秒超时。两次都发生在打断播报后，是会话交接路径值得优先检查的证据。
5. 代码将此错误视为一般 ASR 错误，调用 `Finish(false)` 后返回，最终收尾到 IDLE。`isNativeEmptyAudioError` 的一次提示/重试没有覆盖这个错误，所以用户看到结束，却没有明确的失败反馈。

## 已确认的代码顺序风险，尚待复现验证因果

相关文件：`assistant-agent/native_speech.go`、`agent.go`、`asr.go`。

- `handleUpward(upStreamPrepare)` 首先执行 `s.beforePrepare(...)`，完成后才取消旧 `s.current`、递增 `s.nextID` 并建立新 turn。
- `PrepareNativeSpeechTurn()` 在 beforePrepare 内先取消旧回答上下文，然后执行打断播放、灯效转换等外部操作。这段时间里旧会话的收尾可并发运行。
- 旧 TTS 被取消后，旧 `runNativeVoiceSession` 可能调用旧 turn 的 `Finish(false)`；`sendTurnDownward` 的过期保护依赖 `s.nextID`，而此时它尚未递增，因此旧控制仍可能通过。
- 这确实存在“已收到新 prepare，但旧会话控制仍被当作当前控制”的窗口。它与两次打断后无音频超时相符，但现有日志没有控制包发送/PCM 包计数，尚不能将它写成此次超时的唯一已证实根因。

## 后续修复重点

- 收到新 prepare 后，先使旧 turn/session 失效，再取消旧回答；旧会话不得结束新一轮收音或覆盖新一轮灯效。
- 日志增加每轮 PCM 首包时刻、接收/转发字节数、最后音频包时间和控制包所属 turn，不记录原始音频或密钥。
- 对有唤醒但音频链路超时的情况给出明确处理，避免静默回到 IDLE；重试仍需遵守聆听灯反馈与当前会话所有权。
- 这属于打断后的会话/音频交接问题，与已确认解决的方言识别能力不是同一项。

## 日志限制

设备没有 `logread` 命令。本轮保留了助手 API 的原始日志；没有拿到原厂更底层的同时间段 PCM 传输记录。没有凭缺失日志假定用户没有说话。
