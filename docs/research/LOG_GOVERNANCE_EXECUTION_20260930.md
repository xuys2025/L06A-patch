# L06A 日志治理执行记录

时间：2026-09-30 02:56–03:03，Asia/Hong_Kong。授权：用户明确表示“日志我同意，你现在就可以治理”。

状态：**已通过网络部署，容量及真实轮转即时验收通过。未重启音箱；实际冷启动和 24 小时自然使用观察尚未完成。** 没有新增后台监控或定时任务，沿用原有 cron。

## 1. 结果

| 指标 | 安装前 03:00:50 | 安装、真实轮转后 03:02:13 |
|---|---:|---:|
| `/data` 可用量 | 1,584 KiB，1.55 MiB | **3,040 KiB，2.97 MiB** |
| `/data` 使用率 | 88% | **76%** |
| `/data/log` 的 `du` | 2,000 KiB | **452 KiB** |
| 历史日志预算 | 2,048 KiB | **512 KiB** |

即时净增加可用空间 **1,456 KiB，约 1.42 MiB**，已计入治理脚本和本次新归档。最初调查快照是可用 1,468 KiB / 89%，采样之间会因 UBIFS 回收等发生变化，因此收益以前后相邻快照计算。

保留设备最新日志；淘汰的历史记录已完整归档到电脑。未修改助手二进制、播放器脚本、语音配置或任何系统/启动分区。

## 2. 备份

目录：[network-backups/log-governance-20260930](../../local/build/L06A_1.88.221/network-backups/log-governance-20260930)。

- `before.tar.gz`：第一次历史日志、原轮转脚本、启动与日志配置备份。
  - SHA-256：`28242355aac7886b946c08d5005f28570f58c7b7dea51927ee07ddb5194dd777`。
- `pre-install.tar.gz`：紧邻部署前再次保存 `/data/log`、`/data/init.sh`、原 `easy_logcut`、cron/日志服务配置及当前内存日志快照。
  - SHA-256：`52ecbda8c6a50c1e620fdcdb856f02b0c61026505557cc05d4667d416ac8317c`。
- 两次下载均核对设备与本机 SHA-256 一致；保留 `post-install-init.sh` 供追溯。
- 设备 `/tmp` 的上传包、测试脚本、临时备份已清理；电脑备份保留。

## 3. 实现与生效路径

代码目录：[log-governance](../../local/build/L06A_1.88.221/network-access/log-governance)。

设备安装：

```text
/data/assistant/log-governance/easy_logcut.sh
/data/assistant/log-governance/enable-log-governance.sh
/data/assistant/log-governance/rollback.sh
/data/assistant/log-governance/init.sh.before
```

`/data/init.sh` 顶部增加日志启动钩子，将新脚本 bind mount 到只读根目录的 `/usr/sbin/easy_logcut`。这样现有每 5 分钟 cron 和关机前轮转均调用相同策略；不需要修改只读固件，也不重启语音服务。原只读文件通过卸载覆盖即可恢复。

启动钩子来自当前 `/etc/init.d/assistant-agent` 对 `/data/init.sh` 的调用。已验证钩子语法、卸载后重建覆盖、重复调用不叠加挂载；**尚未通过整机冷启动验证**。后续若重写 `/data/init.sh`（例如运行旧 `finish_ssh_repair.sh`），必须保留或重新安装这一行。

### 轮转行为

1. 以锁避免定时/关机调用并发；保留原 1 MiB 内存日志触发阈值。
2. 将活动日志在 `/tmp/log` 中改名为 pending，再建立新的活动文件。
3. 对实际持有 pending 文件的 syslog-ng 工作进程发 SIGHUP，等待确认文件描述符释放；不向监督进程发信号。
4. 在 `/tmp` 压缩和校验；源文件不会因为压缩失败被清空。
5. 先从旧到新淘汰已过保留预算的归档，为新文件及目录块余量腾空间，再复制、比较、校验并同步新归档。
6. 新归档提交成功后才移除 pending 源文件。失败时 pending 留在内存，下一次调用先重试；活动日志继续接受新记录。

设备 syslog-ng 版本为 3.0.5。采用 SIGHUP 重新打开文件的方式，并已在实机核对文件描述符与轮转前后标记；没有停止日志服务。方法依据：[syslog-ng 官方日志轮转说明](https://syslog-ng.github.io/admin-guide/170_Best_practices_and_examples/004_Configuring_log_rotation)。

单个 gzip 最多 480 KiB；极端不可压缩/过大日志只保留最近尾部，使新归档也能进入 512 KiB 总预算。该保留策略会主动淘汰最旧内容，不承诺保存无限历史。未知非托管文件占满日志目录时，脚本保留源日志并失败退出，不自动删除那些文件。

pending 位于 RAM，断电/重启后不保留；失败保护是对压缩和写入错误的改进，不能解释为任何故障下都不丢日志。日志保持每 5 分钟检查，不引入密集重试。

## 4. 验证

### 隔离测试

本地 WSL 先通过基础测试，随后在设备独立 `/tmp/l06a-logcut-test.*` 目录验证原厂 BusyBox。最后一次设备测试还模拟了写出半截文件后失败。

- 超限时按旧到新删除，保留最新归档。
- 正常轮转后归档可解压，源文件在成功提交后移除。
- gzip 失败：源文件保留，恢复后下一次调用可完成。
- 持久复制部分写入后失败：清除残缺归档，源文件保留，可重试。
- 日志写入者未释放文件：源文件保留，新活动日志不被覆盖；恢复后成功。
- 1.2 MiB 不可压缩数据：归档控制在预算内，最后的标记保留。
- 未达到触发阈值不轮转。

### 实机验收

- 写入轮转前标记，强制一次真实轮转；在新 `messages.0.gz` 内找到标记。
- 轮转后写入另一标记，在新的活动文件找到；没有残留 pending 文件。
- 本次 187,164 bytes 源日志完整保留，生成 22,194 bytes gzip；本次没有截尾。
- syslog-ng PID 前后均为 `1368 1367`；工作进程 FD 指向新 `/tmp/log/messages`。
- 原只读 `easy_logcut` 在解除覆盖后哈希与备份一致；再次执行启动 helper 成功；重复执行成功。
- 回退的钩子删除表达式生成的文件，与原 `/data/init.sh` 字节完全一致。没有在设备执行完整回退。
- 助手 `/health=ok`，版本 final14，状态 `IDLE`，`last_error` 为空。
- 原厂 PCM `socket=ready`；TTS `common=ready speech_protected=yes init_overlay=yes`。
- 没有远程触发麦克风采集，也没有用真实分区写满来测试。

### 指纹

| 文件 | SHA-256 |
|---|---|
| 新 easy_logcut.sh | `c0048ad05cdbd7cda01e4c49e7bf3b134bb7f3e48d790aadef18ab49b0101b4f` |
| enable-log-governance.sh | `14c51efeff7b1264ce7936f583fd3b3e805e2259a608ee34942adb46da52122a` |
| 最终 rollback.sh | `0c64fab9d58e1826837f459bb4f64dd70712287d918034c4f51271073f3ff9dc` |
| 安装后 /data/init.sh | `c1e1d97d2fe5dbb9f03065740724f63a35d7af2562008e2992482b1930cff8c3` |
| 助手（未变） | `89dd2aaea5199a278f20149f74aed16b2613d49540c37f9ba0a2fdcb1adbd205` |
| 播放器脚本（未变） | `ae692ca2644db4071db9da19ce4cfbebfcd57723c7bb0416f6674ca9e12e9eb4` |

## 5. 回退与后续观察

必要时在音箱执行：

```sh
/data/assistant/log-governance/rollback.sh
```

它只移除本次启动钩子并卸载 `easy_logcut` 覆盖，恢复原厂轮转。旧日志继续保存在电脑，不默认复制回设备占满空间。若存在失败留下的 pending，应先归档到电脑后再决定如何处理；回退脚本不会删除它。

即时验收已满足 S1 空闲至少 2.5 MiB 的目标。还需在下次正常使用/重启时核对挂载和日志限额，并完成原计划的 24 小时自然使用观察；本轮没有宣称这两项已完成，也没有设置自动提醒。长期记忆原定 4 MiB 上线门槛仍未达到，S2/S3 不在本次实施范围。

证据文件：

- [部署前备份与隔离测试](../../local/analysis/L06A_1.88.221/log-governance-pre-install-20260930.txt)
- [安装结果](../../local/analysis/L06A_1.88.221/log-governance-install-20260930.txt)
- [真实轮转和重新绑定验证](../../local/analysis/L06A_1.88.221/log-governance-live-verification-20260930.txt)
- [回退表达式验证](../../local/analysis/L06A_1.88.221/log-governance-rollback-check-20260930.txt)
- [收尾容量和语音通道](../../local/analysis/L06A_1.88.221/log-governance-final-status-20260930.txt)
