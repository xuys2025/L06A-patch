# L06A 混合语音助手最终构建报告

## 目标设备

- 整机：小米 L06A
- 主板：AS06 VER0106
- SoC：Amlogic A113X / ARMv7
- 固件基线：原厂 `1.88.221 system1` 只读备份
- 启动配对：保留原厂 `boot1`，仅更新 `system1`
- 回滚槽：`boot0/system0 = 1.94.13`，未修改

## 最终产物

| 项目 | 值 |
|---|---|
| 镜像 | `L06A_1.88.221_hybrid-assistant_final.squashfs` |
| 长度 | 40,402,944 bytes / `0x2688000` |
| system1 上限 | 41,943,040 bytes / `0x2800000` |
| 剩余余量 | 1,540,096 bytes |
| SHA-256 | `2ba23f9bdebe5e94f8dbfaa75ba8a8b19643a644abbce4a2153436057d1b0801` |
| MD5 | `07013963b0ab4f58eff1675c5ec644d6` |
| 代理程序版本 | `2026.09.29-final3` |
| 代理 SHA-256 | `d781e0781777d8649ffee6294217cffabd446ee17b9ec6a17b8ad3da7ba4419c` |

## 已实现

- 保留原厂“小爱同学”六麦克风本地唤醒，并把 `WuW` 系列事件接入自定义代理。
- 从 `pcm.mico_record` 采集 16 kHz、16-bit、单声道 PCM，接入豆包大模型流式 ASR 2.0。
- 接入可配置的 OpenAI 兼容 LLM，支持 SSE 响应和内存对话历史。
- 默认调用原厂 `mibrain text_to_speech`；管理页可切换远程 TTS。
- 连续对话默认等待 10 秒、最多 8 轮、整轮最多 120 秒；这些值可在网页修改。
- 网易云搜索与 MusicFree 播放 URL 解析，支持音质降级、不可播跳过、本地队列和常用播放控制。
- Web 管理页、健康检查、状态、日志、文字测试、朗读测试、模拟唤醒和在线配置。
- Dropbear SSH、公钥登录、持久 root shadow、`/data/init.sh` 与原子网络升级。
- Wi-Fi 关联后显式重启 DHCP/IPv6 DHCP，保留上一版已验证的联网修复。

## 固件取舍

保留：

- `mipns-xiaomi`、`pns_ubus_helper` 和原厂唤醒模型
- `mibrain_service` 与原厂 TTS 调用链
- `mediaplayer`、`miplayer`、ALSA 配置和蓝牙所需共享库
- 原厂 CA、网络栈和 `/data` 持久分区

移除：

- 已禁用、且仅供自身使用的 `mico_aivs_lab`
- 仅被上述程序引用的 `libaivs_sdk.so`

这样可以避免原厂云 ASR 与豆包 ASR 同时抢占会话，并为分区增加约 1.1 MiB 压缩余量。原厂 TTS 使用的 `libaivs-message-util.so` 和本地唤醒需要的库仍在。

## 离线验证结果

- `go test ./...`：通过
- `go vet ./...`：通过
- ARM 产物：ELF 32-bit ARM EABI5，静态链接，无动态依赖
- SquashFS 4.0 / XZ：有效
- 独立完整解包：通过
- `/dev/console` 设备节点：保留
- 所有新增 shell：语法检查通过
- 唤醒、DHCP、SSH、持久化和禁用云 ASR 的断言：通过
- 镜像内 MusicFree API Key：未发现
- 镜像内 SSH 私钥：未发现
- 镜像长度严格小于 `system1`：通过

## 一次线刷流程

音箱进入 USB 调试模式后，在 PowerShell 7 运行：

```powershell
pwsh -File D:\xiaoai\scripts\device\flash_system1_final.ps1
```

脚本会先持续运行 `update.exe identify` 最多 120 秒。使用时先接好 USB、保持 12V 断电，启动脚本并看到等待提示后再接通 12V；捕获到 WorldCup USB 调试模式后才继续。随后脚本校验工具和镜像哈希，只写 `system1`，回读 `0x2688000` 并核对同一 SHA-256。脚本不会修改 bootloader、TPL、boot0、boot1、system0、data 或 U-Boot 环境。

## 首次启动和网络调试

正常启动后，先从路由器 DHCP 列表确认音箱 IP。若仍为 `192.168.10.61`：

```powershell
pwsh -File D:\xiaoai\scripts\device\open_assistant_console.ps1
```

管理页中填写：

1. 豆包 ASR API Key；Resource ID 默认已设为 `volc.seedasr.sauc.duration`。
2. LLM Base URL、Model 和 API Key。
3. MusicFree API Key。
4. 保持 `TTS Engine = native`，先测试原厂语音；若不可用，再配置远程 TTS。

管理令牌位于 `network-access/admin-token.txt`；SSH 私钥位于 `network-access/id_rsa_l06a`。镜像内只有对应公钥。公钥指纹为 `SHA256:CDjEpv89ogibyeZNThQ2ByKYcrx22UDyV+/XII+z5uA`。

后续代理更新使用：

```powershell
pwsh -File D:\xiaoai\scripts\device\update_assistant_over_network.ps1
```

该脚本通过 SCP 上传，设备端先校验版本、配置和 SHA-256，再原子替换 `/data/assistant/bin/assistant-agent` 并重启服务，不需要重新线刷。

## 实机待验收项

- 原厂远场唤醒在恢复原厂 PNS 后的实际灵敏度
- `mico_record` 与 PNS 并发时的采集质量
- 当前设备注册状态下原厂 TTS 是否仍可取回音频
- 豆包 Key、用户选择的 LLM 和第三方音源 Key 的真实调用
- 长时间连续对话中的回声和首字截断

播放中打断仍未启用。当前连续对话会在 TTS 播放完成后重新监听，避免音箱把自己的播报再次送入 ASR。
