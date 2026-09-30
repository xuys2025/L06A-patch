# L06A 音乐断点恢复与灯环闪烁分析

更新日期：2026-09-30。固件 1.88.221，硬件 L06A / AS06 VER0106。

## 验收状态

- 用户已确认 final12 的方言识别问题解决。
- 用户确认 final12 音乐中断后仍从头播放，断点恢复未通过验收。
- 用户确认 final12 跑马灯有改善，但仍闪烁且不够平滑。

## 证据标准

`ubus` 命令退出码 0、业务返回 `code=0` 或播放状态 `status=1`，都不能独立证明播放位置正确。后续需检查真实位置、解码器 seek 或对已知测试音频的读取偏移。不得把“接受了带 offset 的请求”记作“已实现断点续播”。

## 原厂播放器：已证实

- `mediaplayer` 动态符号保留了 `get_curtrack_position`（0x19e64）、`get_curtrack_duration`（0x19dc8）、`seek_curtrack_to`（0x1a7e8）。可从同版本 ELF 分析其行为。
- `seek_curtrack_to` 的 125 返回值涵盖当前曲目指针为空，以及曲目不可 seek 的分支；126 是位置不在有效范围内。因此不能仅由 125 推断唯一原因。
- `get_curtrack_position` 调用播放器对象虚表 +0x28 取得真实进度，原厂内部已有真实位置能力。是否可通过现有 UBus 上下文取出，继续核查。
- final12 把 offset 放进 `player_play_music` 的 `payload.audio_items[].stream.offset_in_ms` 和外层 `startOffset`，但用户现场证实未生效。需要追踪该接口的分支和底层播放器行为。

## AW20054 灯驱动：已证实

分析对象：`original-system1-rootfs/lib/modules/4.9.61/aw20054.ko`；完整带 relocation 注释反汇编见 `aw20054-disasm.txt`。

- 实机 `led_rgb` 的文档只接受 `像素编号 RGB整数`，编号 0–17。
- 对应处理函数位于模块 `.text+0x5a0`，只解析两个 token，不能写入多行实现整帧更新。
- 每次更新通过 `i2c_smbus_write_byte_data` 选择页 0xC1，再分开写该像素三个通道。8 位输入右移两位，实际颜色控制是 6 位精度。
- `led_fade` 写处理函数位于 `.text+0x410`，只接受 `r/g/b 数值`，选择页 0xC2 后更新整圈对应色通道。它不是完整帧或任意渐变动画接口。
- `led_rgb` 在内核动画标志已置位时会取消定时器并清空灯环，然后进入逐像素写入。这是需进一步排查的闪烁路径；尚未证明用户闪烁一定由该标志触发。
- 当前 Go 动画每 75 ms 移动一个完整像素，约 13.3 FPS，颜色是六段阶跃变化。final12 只减少写入量，没有做时间插值；因此视觉平滑度仍受帧率和色块跳变限制。

## 待验证

1. 用可控的静音测试音频确认原厂播放器对 offset 的实际使用，定位 `common` 类型或 URL 解码器是否绕过 seek。
2. 检查原厂 `ledserver` 是否在自定义动画期间同时写灯，造成写入竞争或清屏。
3. 评估保留同一播放对象暂停/恢复，或改用能报告真实位置并可靠 seek 的播放接口。
4. 修正动画帧率、插值和控制权后，记录机器可测的帧耗时与现场视觉验收。

## 分析工具

- `inspect_elf.py`：本地只读 ELF 符号、字符串及 ARM/ARM64 反汇编工具；对内核模块附加 relocation 注释。
- 原厂文件保持只读；分析输出保存在本目录。

## 02:18：音乐定位根因确认

### 缺失 audio_type 导致绕过 seek

- `parse_audio_player` 的 `.text 0x2c400–0x2c514` 解析 `payload.audio_type`，字符串 `MUSIC` 映射到类型 3；缺失时映射到 0。最终在 `0x2d140` 写入曲目对象 +0xA0。
- `is_playing_cp_media`（0x19480）检查该类型，类型 0 不属于可定位的内容曲目。
- `handle_play_prepared_event` 在 `0x1b80c` 先检查 `is_playing_cp_media`。类型 0 直接进入播放，不执行 offset；类型 3 才执行 `0x1b9a4` 读取 +0x690，然后在 `0x1b9b0` 调用 `seek_curtrack_to`。
- 因此 final12 外层参数、返回码和播放状态都正常，仍然必定从头播放。缺少 `audio_type: MUSIC` 是已证实的直接原因。

### 严格实机验证

使用本地生成的 65 秒静音 WAV：单声道、16 kHz、16 bit，头部 44 字节，每秒 PCM 32000 字节。

补齐 `audio_type: MUSIC` 后：

- `player_get_context` 返回 `audio_meta.audio_id=silent-test`、`audio_type=MUSIC`。
- 在 `play_at(..., 30000, ...)` 后，底层解码线程真实执行 `_llseek(8, 960044, [960044], SEEK_SET)=0`，等于 `44 + 30 * 32000`。
- 随后 `player_set_positon(position=40000)` 返回 0，底层执行 `_llseek(8,1280044,[1280044],SEEK_SET)=0`，等于 `44 + 40 * 32000`。
- 原始证据已回收到本目录 `seek-type.trace`，可搜索 `960044` 和 `1280044`。

另发现外层 `duration` 用于播放停止计划，不能用歌曲时长替代；final12 填入曲目时长使 `timed_shutoff=1`。修正为 0、只在 `stream.duration_in_ms` 填总时长后，实测 `timed_shutoff=0`。

### 调试过程中原厂缺陷

缺失 MUSIC 类型时调用 `player_get_latest_playlist` 导致原厂播放器空指针 SIGSEGV，procd 自动恢复。该方法从后续检查中移除；原厂 ELF 未修改，播放器已恢复。该事件不能归因于网络或助手主进程崩溃。

## 灯效的确定代码问题与实机性能

- final12 的 `keepListeningCyan` 和 `runRGBMarquee` 在取消时清空 18 像素，新状态的 start 方法又清空 18 像素，随后才显示首帧。两种自定义状态之间必然出现黑帧。原代码还在每次转换时调用原厂 shut，这些操作在自定义状态之间没有必要。
- final12 每 75 ms 把三个像素宽的纯色色块整格推进，没有插值。只更新六个边界像素能减少 I/O，但不能消除颜色阶跃，也不能提高 13.3 FPS 帧率。
- 通过固定打开的同一个 sysfs 文件描述符连续写入整帧，200 帧/5 秒测试：平均写入 8.420777 ms，最大 9.360125 ms，超过 25 ms 的帧数为 0。测试工具 `ledsysfsprobe.go` 不初始化麦克风或音频，结束清灯。
- 同时跟踪 `ledserver` 的 write 系统调用，5 秒内没有写入，`/tmp/led-probe-native.trace` 为 0 行。这只排除了该次空闲诊断期间的原厂抢写，不能泛化为所有原厂事件都不会抢写。

### 采用的实现

- 保留原厂 sysfs 驱动和电流/亮度寄存器设置；每段自定义灯效复用一个文件描述符。
- 40 FPS（25 ms），六种颜色之间做连续插值，按单调时钟相位计算，3.6 秒转一圈。丢帧不会积累动画时间偏差。
- 聆听/思考切换停止旧动画但不清屏、不重复操作原厂效果；新状态直接覆盖。进入空闲或播报时才释放自定义灯。
- 聆听仍是青色 `(0,190,230)`，同步首帧成功后才允许收音；首帧失败向上层报错。
- 新增 `--led-test`，只展示 1 秒青色、4 秒 RGB，再熄灭；不建立录音、ASR、TTS 或 HTTP 服务。

### 未采用的实验

- 曾尝试 `/dev/i2c-0` 的 I2C_RDWR 连续寄存器更新以减少事务数，但 54 通道回读校验失败（row 1 / col 2 期望 1，读到 0）。改为逐地址读取后仍不匹配。
- 这只能说明本次实验未验证该访问方法，尚不足以断言芯片完全不支持连续写入。原始工具保留为 `ledprobe.go`；该路径不会进入生产版本。
- 实机已有 sysfs 性能足以满足 40 FPS，因此采用经过测试的原厂接口。没有更换内核驱动，也没有修改芯片电流。

### 一手参考

- Awinic AW20054 产品页：https://www.awinic.com/en/productDetail/AW20054QNR
- Linux 上游 AW200xx 驱动：https://raw.githubusercontent.com/torvalds/linux/master/drivers/leds/leds-aw200xx.c
- Linux I2C userspace ABI：https://www.kernel.org/doc/html/latest/i2c/dev-interface.html

## 后续验收边界

音乐 seek 的解码器偏移已有实证；完整的音乐中手动唤醒、回答后恢复仍需现场验收。位置仍使用墙钟估算，网络缓冲会造成小幅进度偏差。LED 代码连续性和 I/O 时限可自动测量，但肉眼是否足够流畅仍以用户反馈为准。方言问题按用户确认记为已解决。

## final13 候选版本与网络部署后的验证

- 最终播放器脚本再次通过本地静音 WAV 30 秒定位测试；`seek-final13.trace` 记录真实 `_llseek(...,960044,...)=0`。上下文确认 `audio_id=final13-silent`、`audio_type=MUSIC`、`timed_shutoff=0`。
- 又用仅绑定 `127.0.0.1:18090` 的 HTTP 测试服务验证网络解码器。`seek-http.log` 记录初次 `Range: bytes=0-`，随后精确发出 `Range: bytes=960044-`。它证明定位也作用于网络读取，而不只是本地文件。服务和播放均在测试结束关闭。
- 生产候选二进制运行 `--led-test`：4 秒 RGB 段共 161 帧，首像素写入间隔平均 25.092 ms；带 strace 的最大间隔 44.470 ms（包括启动与调度开销）。共 2952 次像素写入，最后 18 次为退出熄灯；此前没有全黑像素写入。同期原厂 `ledserver` 的 write 跟踪为空。
- 原始日志：`led-final13-agent.trace`、`led-final13-native.trace`；汇总：`led-final13-summary.json`。统计已合并 strace 的 unfinished/resumed 记录，避免漏计并误报丢帧。
- 已部署版本 `2026.09.30-final13-music-seek-smooth-led`；健康接口 `ok`，状态 `IDLE`，`last_error` 为空。完整版本指纹与改动审计见 `../build/DEVICE_CHANGE_LOG.md`。

## 收尾发现：音量脚本版本差异（并入短指令 P1）

- 设备 `/usr/libexec/assistant/volume.sh` 仍为 `set -eu`，而本地 overlay 已为 `set -e`。设备直接调用该脚本报 `JSON_PREFIX: parameter not set`：`jshn.sh` 与 nounset 不兼容。
- 本轮更新事务只包含助手二进制与 playerctl，没有替换此旧音量脚本；不要把“本地文件已修正”等同于“设备版本已更新”。
- 收尾直接调用原厂 `player_set_volume` 成功返回 `code=0`，上下文确认恢复至 20。原厂音量接口本身可用。
- 该版本差异纳入原厂短指令恢复任务，部署时须同步音量脚本并覆盖实机测试。本轮音乐定位/灯效验收不依赖该脚本。

## final13 现场反馈：回复后回溯至唤醒点

用户确认唤醒与回答期间音乐一直在低音量播放，回答结束后却跳回唤醒时的位置。代码已确认：

- `BeginInterruption()` 无条件冻结助手的估算位置，但没有真正暂停原厂音乐。
- `startNativeSession()` 收尾无条件调用 `ResumeInterrupted()`；该方法直接进入 `resumeCurrent()`，通过 `play_at` 停止当前曲目、重建播放对象并 seek 至冻结点。
- 因此 final13 修好的 seek 在原厂 duck（压低音量但继续播放）场景中反而把已经向前播放的音乐拉回。仅仅证明 seek 准确，不能证明会话收尾逻辑正确。
- 修复方向：收尾先核对原厂当前曲目和状态。原曲仍播放时，只结束 duck，不 stop/play/seek，并把会话期间经过的时间计入估算进度；真正暂停时优先原对象继续。不得把状态查询失败直接当成允许重播。

### final14 实现与实机结果

- 新增播放脚本 `finish_interruption AUDIO_ID`，核对原厂 `audio_meta.audio_id/audio_type`。`status=1` 只调用 `player_wakeup(stop)` 恢复音量；`status=2` 对保留的同一曲目执行原地 play。两条路径均不提交 URL、不调用 stop 或 seek。
- 实测本固件 `pause` 与 `stop` 都返回 `status=2`，因此不能将它唯一解释为“曲目对象已销毁”。助手内用户暂停/停止会改变修订号，过期会话不得重新恢复。不同曲目返回 `replaced`，不抢回旧歌；无效上下文返回错误，不进行破坏性的重播回退。
- `BeginInterruption()` 保存检查点和时间；原曲继续播放时，收尾把 duck 期间的时间加回位置。语音中主动暂停也计入截至暂停的时间；语音中说“继续”沿用同样的保留对象路径。
- 实机静音 WAV 从 10 秒开始，进入 duck 后 `status=1, volume=9, origin_volume=21`；调用新收尾返回 `continued`，原曲 ID 保持不变，`volume=21`。
- 真正 pause 后调用收尾返回 `resumed`，恢复至 `status=1`。传入错误曲目 ID 返回 `replaced`，原曲继续播放且未被替换。
- 上述过程开始跟踪前已完成初始播放；`final14-retained.trace` 内没有音频文件重新打开记录，也没有 `lseek/_llseek`。对应状态输出保存于 `final14-native-check.txt`。没有打开麦克风。
- 自动化回归覆盖：会话期间时间累加、过期 token 不接触播放器、非法/缺失状态不重播、原地继续、不同曲目保护、显式“继续”不 seek、暂停检查点不回溯。
- final14 的直接修复目标是去掉错误的会话后重播。此前的 seek 能力保留给明确暂停后的手动恢复；灯效和 ASR 本轮不修改。
- 02:41 已通过网络部署 `2026.09.30-final14-preserve-ducked-playback`，两文件 SHA 回读一致、health=ok、IDLE、无 last_error；发布指纹与本地 final13 回退备份位置见设备变更记录。
