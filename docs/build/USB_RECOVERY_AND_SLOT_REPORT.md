# L06A USB 救援与活动槽确认

## Micro-USB 下载模式重复性

在音箱每次完全断开 12V、Micro-USB 保持连接的条件下，连续进行了三次冷启动 `update.exe identify` 捕获：

| 次数 | 捕获时间（Asia/Hong_Kong） | 轮询次数 | 返回结果 |
|---:|---|---:|---|
| 1 | 2026-09-29 19:25:08 | 75 | `AmlUsbIdentifyHost` / `0-7-0-16` |
| 2 | 2026-09-29 19:25:55 | 74 | `AmlUsbIdentifyHost` / `0-7-0-16` |
| 3 | 2026-09-29 19:26:34 | 64 | `AmlUsbIdentifyHost` / `0-7-0-16` |

三次均成功，未执行分区写入、环境变量写入或擦除。由此确认：只要 bootloader/TPL 不被修改，当前 Micro-USB WorldCup 下载模式可作为系统分区实验的基本恢复入口。

该接口并不等于串口控制台。此前运行只读 `bulkcmd "printenv"` 时，工具仅返回命令成功，没有返回环境变量正文；原厂 1.88.221 的 `release` 启动脚本也会关闭正常启动后的 ADB/RNDIS。

## data 备份离线分析

使用只读 UBI 解析工具成功从 `mtd6.img` 提取 219 个文件。UBIFS 参数包括：

- Min I/O：2048
- LEB：126,976 bytes
- LEB count：134

这也证明先前读取的 `0x13A0000` 范围包含可解析的完整文件系统元数据；它仍不能单独证明 NAND 物理分区终点或 OOB/坏块布局。

## 活动槽证据

离线 data 内容给出了完整升级时间线：

1. 2026-09-28 20:09，系统日志持续报告运行版本 `1.88.221`。
2. 20:12，日志记录收到 `1.94.13` OTA，并明确记录：
   - 当前版本 `1.88.221`
   - 正在升级到 `1.94.13`
   - `Burning /dev/mtd2 kernel Block`
   - `Burning /dev/mtd4 rootfs Block`
3. 20:13，下一次启动日志、`miio_helper`、AIVS 和其他服务持续报告运行版本 `1.94.13`。
4. data 中 `/data/mico/version` 与 `/data/mico/system.cfg` 均为 `1.94.13`。
5. 两个原厂 rootfs 的启动脚本都会在每次 L06A 启动时，把当前 `/usr/share/mico/version` 和 `system.cfg` 复制到 `/data/mico/`。
6. 备份对应关系：`mtd4/system0=1.94.13`，`mtd5/system1=1.88.221`。

因此可确认：**备份时最后一次成功运行的是 system0（1.94.13），system1（1.88.221）是非活动系统槽。** 此后只进行了 USB `identify`/读取，没有执行会切换 `boot_part` 的命令，因此当前槽状态没有被本次操作改变。

## boot/system 配对确认

两个原厂 boot 镜像均为 Android boot image。离线提取并解压内核后得到：

| 启动槽 | 分区 | Linux | 内核编译时间 |
|---|---|---|---|
| boot0 | `mtd2` | `4.9.61` | 2025-09-16 09:56:25 |
| boot1 | `mtd3` | `4.9.61` | 2025-07-01 12:16:10 |

OTA 日志同时记录从 `1.88.221` 升级到 `1.94.13` 时写入 `mtd2` 和 `mtd4`。因此槽位配对可直接确定为：

- `boot0/mtd2` + `system0/mtd4` = OTA 后的 `1.94.13`
- `boot1/mtd3` + `system1/mtd5` = 保留下来的 `1.88.221`

定制镜像以原始 `system1/mtd5` 的 `1.88.221` 为底包，并计划继续使用未修改的 `boot1/mtd3`，版本与槽位配对一致。

## USB-only 写入方案的边界

若用户明确批准进入写入阶段，计划只涉及：

1. 仅写 `system1`，不写 bootloader、TPL、boot0、boot1、system0 或 data。
2. 写入后先从 `system1` 回读镜像长度 `0x2354000`，SHA-256 必须等于 `6344a856efa4b1893200aedfcfa1430de3b2430e3ab0ea47f884e0d4c1d0fcfd`。
3. 回读一致后才允许把 `boot_part` 切换为 `boot1` 并保存环境。
4. 回滚时通过已验证的 USB 下载模式把 `boot_part` 恢复为 `boot0`；`system0=1.94.13` 始终保留。

上述步骤包含 NAND 和 U-Boot 环境写入，必须在用户明确批准后执行。
