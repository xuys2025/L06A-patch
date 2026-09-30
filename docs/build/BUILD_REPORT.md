# L06A 1.88.221 xiaoai-patch 构建报告

## 结论

已从本机只读备份的 `system1`（`mtd5.img`）构建出 L06A/LX06 定制 SquashFS 镜像。依赖包和最终镜像均已重新解包验证，未发现 x86/x86_64 ELF 或 Python 3.14 构建残留。

初版镜像已写入实体音箱的非活动 `system1`，回读哈希完全一致，并通过 `boot1` 正常播放开机提示音。首次配网测试暴露了 1.88.221 的配置文件路径兼容问题；已生成并完整验证修复镜像，尚待重新写入 `system1`。

## 输入来源

| 项目 | 值 |
|---|---|
| 原始分区 | `system1` / `mtd5.img` |
| 原始字节数 | 41,943,040 (`0x2800000`) |
| 原始 SHA-256 | `276da7ca8ff63dd5495fb1bc80bbf6608bc90f37a9dc720e228c21ae68f58c22` |
| 原始 ROM | `1.88.221` |
| 原始硬件标识 | `LX06` |
| PCB 丝印 | `AS06 VER0106` |
| 整机型号 | `L06A` |
| xiaoai-patch commit | `fb070495955668f825700bbd988d5443e9bcadb3` |

## 输出镜像

| 项目 | 值 |
|---|---|
| 文件 | `L06A_1.88.221_xiaoai-patch_20260929.squashfs` |
| 文件字节数 | 37,044,224 |
| SHA-256 | `6344a856efa4b1893200aedfcfa1430de3b2430e3ab0ea47f884e0d4c1d0fcfd` |
| MD5 | `834bbc6fb5f06e1ea3f95cbf98158f0a` |
| 格式 | SquashFS 4.0, XZ, 131,072-byte block |
| SquashFS 数据长度 | 37,042,452 bytes |
| 分区上限 | 41,943,040 bytes |
| 剩余空间 | 4,898,816 bytes |

最终版本元数据保留了来源版本：`LINUX=1.88.221`、`ROOTFS=1.88.221`，并把构建渠道标为 `custom`。

### Wi-Fi 配网修复镜像

| 项目 | 值 |
|---|---|
| 文件 | `L06A_1.88.221_xiaoai-patch_20260929_wifi-fix.squashfs` |
| 文件字节数 | 37,044,224 |
| SHA-256 | `a77ae242a49d2fc5eac120df3b6c3ed8922642376c0aba666662dba264fadfdc` |
| MD5 | `95465a071ea0e184108ebdef89f93091` |
| 分区上限 | 41,943,040 bytes |
| 剩余空间 | 4,898,816 bytes |

修复内容：

- 当固件无线服务使用 `/data/wifi/wpa.conf` 时，Improv 配网脚本写入同一文件。初版错误写入未被服务读取的 `wpa_supplicant.conf`，导致配网必然超时。
- 管理 API 启动等待增加 30 秒上限，防止 Wi-Fi 已成功连接时 Improv 页面仍无限停留在 `Provision`。

### Wi-Fi + DHCP 最终修复镜像

| 项目 | 值 |
|---|---|
| 文件 | `L06A_1.88.221_xiaoai-patch_20260929_wifi-dhcp-fix.squashfs` |
| 文件字节数 | 37,044,224 |
| SHA-256 | `ae840a768e9b3799a82a6cbf24b669177e6f05f15cfdb22435cbe0cbd0232259` |
| MD5 | `4db984cc5865b241a541068a7095a55f` |
| 分区上限 | 41,943,040 bytes |
| 剩余空间 | 4,898,816 bytes |

第二次实体测试证明 Wi-Fi 已完成认证，但 DHCP 没有启动。1.88.221 原厂由已停用的 Xiaomi 网络组件响应连接事件并启动 `dhcpc`；无线 init 脚本本身没有这一动作。最终修复镜像增加：

- 每次已配置的无线服务启动时显式重启 `dhcpc` 与 `odhcp6c`，重启后同样有效。
- Improv 配网脚本等待取得 IPv4 地址后才播放成功提示并返回管理 URL。

## 依赖包

| 项目 | 值 |
|---|---|
| 文件 | `bin-20260929-07138.tar.gz` |
| 字节数 | 36,582,119 |
| SHA-256 | `c8e3c53172992a22c19889086695a9d4f427a6015be7b250714a92b01e293e02` |

为当前构建环境修正了 CMake 兼容、已失效的上游下载地址、MPD/Boost、Shairport Sync/GLib 生成代码，以及 `core_api` 的 Python 3.9 依赖解析。`core_api` 的可选加速模块使用纯 Python 回退，避免把构建主机的 x86_64 扩展装入 ARM 镜像。

## 1.88.221 兼容处理

上游补丁包含多个历史固件版本的候选 hunk。针对 1.88.221，额外落地并验证了：

- L06A 配置在 RAM 中合成，避免每次启动反复写 `/data/mico`。
- 启用 `bluealsa-aplay`，并继续停用原厂私有蓝牙进程。
- 联网启动后异步执行 NTP 校时。
- 移除 Wi-Fi 配置流程中对已经停用或删除的原厂服务的残余重启调用。
- 修正上游 LX06 `sysfixtime` 补丁的 hunk 长度错误。
- 修正 Improv Wi-Fi 与 1.88.221 无线服务使用不同配置文件的问题。

## 成品验证

- 成品镜像可由 `unsquashfs` 正常识别并完整解包。
- 修改过的启动脚本通过 Bash 语法检查。
- 所有用户空间关键程序均为 32-bit ARM：`mpd`、`shairport-sync`、`upmpdcli`、`improv-wifi`、`python3.9`。
- 关键程序声明的动态库均能在成品根文件系统中找到。
- 没有 x86 或 x86_64 ELF。
- 没有 `cpython-314` 文件。
- Python 3.9 下已导入 Flask、Flask-APScheduler、requests、Wyoming、PyYAML、charset-normalizer、MarkupSafe 等依赖。
- 54 个 AArch64 可重定位 ELF 全部位于原厂 `/lib/modules/4.9.61/`，属于原厂 64-bit 内核模块；用户空间仍为 32-bit ARM。

详细结果见 `image-verification.log`、`image-260929-1914-file-report.txt` 和依赖包对应报告。

## 构建日志说明

`build-patch-lx06.log` 中仍可见部分 `FAILED`/`can't find file to patch`。这是项目把不同历史固件的候选 hunk 放在同一补丁文件中、逐个尝试造成的日志；与 1.88.221 对应的功能已按成品内容逐项核对。最终干净构建中没有 `malformed patch`，版本专用兼容补丁的四个目标文件均成功应用。

## 安全状态与下一步

- NAND 分区写入：**已按用户明确授权仅写入 system1；初版写后回读哈希完全一致**
- U-Boot 环境写入：**已按用户明确授权设置 `boot_part=boot1` 并 `saveenv`**
- 固件降级：**未执行**
- 当前活动 A/B 槽：**boot1/system1；已完成一次实体冷启动**
- 已知两个系统槽：`system0=1.94.13`，`system1=1.88.221`

Micro-USB WorldCup 下载模式已连续三次冷启动捕获成功，可在不改 bootloader/TPL/boot 分区的前提下作为 system 槽实验的基本恢复入口。活动槽的完整证据和 USB-only 方案见 `USB_RECOVERY_AND_SLOT_REPORT.md`。

后续修复继续限定为仅重写 `system1`，写后回读新镜像长度并核对 SHA-256；不再修改 U-Boot 环境。`boot0/system0=1.94.13` 保持原样作为回滚槽。
