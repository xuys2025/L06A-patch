# 2026-09-30 项目目录与 Git 整理

本次建立项目根仓库并整理已有材料；助手 Go 业务代码保持原样，设备发布状态仍以历史变更记录为准。

## 迁移表

| 原位置 | 新位置 |
|---|---|
| `assistant-agent/` | 保持，继续作为 Go 模块 |
| `image-overlay/` | `firmware/overlay/` |
| `patches-final/` | `firmware/patches/final/` |
| 根目录 LX06 补丁与兼容脚本 | `firmware/patches/compat/` |
| 根目录 `*-package.mk`、YAJL 补丁 | `firmware/packages/<包名>/` |
| 构建 / 检查 / 安装脚本 | `scripts/build/`、`scripts/verify/`、`scripts/setup/` |
| build 下 PowerShell、network-access 下 Shell | `scripts/device/`、`scripts/device/runtime/` |
| 一次性 Python 兼容性与代理实验 | `scripts/legacy/` |
| 根目录计划 | `docs/plans/` |
| build 下 Markdown 报告 | `docs/build/` |
| analysis 下 Markdown 报告 | `docs/research/` |
| analysis 下 Go / Python 工具 | `tools/diagnostics/` |
| `cmake-compat/` | `tools/cmake-compat/` |
| `xiaoai-patch/` | `external/xiaoai-patch/`，保留原 `.git` 与工作树修改 |
| `L06A_1.88.221_backup/` | `local/backups/L06A_1.88.221/` |
| `L06A_1.88.221_build/` 剩余文件 | `local/build/L06A_1.88.221/` |
| `L06A_1.88.221_analysis/` 剩余文件 | `local/analysis/L06A_1.88.221/` |
| `toolchains/`、`reference/`、`vendor/`、`tmp/` | `local/` 下同名目录 |
| Amlogic 刷机工具 | `local/tools/Amlogic_Flash_Tool_v6.0.0/` |
| Home Assistant 镜像、WSL 安装包 | `local/downloads/` |
| WSL / Windows 环境日志与状态 | `local/environment/` |
| Home Assistant compose | `deploy/home-assistant/compose.yaml` |

90 项移动记录保存在本地 `local/layout-migration.json`，原始关键文件的哈希清单保存在 `local/integrity-before.json`。目录移动使用同盘重命名，未删除固件、备份或第三方仓库。SSH 私钥保持原 ACL，未读取其内容。

## 结构决策

- Go 业务包规模尚可，保留单模块，避免为整理目录引入无关业务改写。
- 构建输入、部署脚本和研究报告进入版本管理；产物、原厂固件、设备信息和工具下载归入 `local/`。
- 主机脚本使用统一路径模块；Linux / Windows 路径由各自环境推导，可通过环境变量覆盖。
- 镜像构建中的 root 密码哈希移入被忽略的本地文件；真实 API Key、SSH 密钥、令牌不加入 Git。
- Linux 脚本、overlay、补丁和源码使用 LF，避免 Windows 检出产生 Shell 兼容问题。
- 历史报告更新了主要路径和链接，历史版本、判断和发布指纹保留。报告中的裸文件名需结合相应 `local/build/` 或 `local/analysis/` 目录查找。

## 验证范围

本次实际完成的离线检查：

| 检查 | 结果 |
|---|---|
| Go 测试、`go vet`、Shell 语法、默认配置 JSON | 通过 |
| Linux ARMv7 编译 | 通过，静态链接；验证产物单独存于 `local/build/verification/` |
| 主仓库文件边界、凭据特征及 Markdown 链接 | 118 个文件，0 个错误；源码约 501 KiB |
| PowerShell 语法 | 通过 |
| 历史 final3 镜像与刷写工具的离线预检 | SHA-256 与镜像长度均通过 |
| 未修改的源码、备份、镜像及相关材料 | 97 个文件 SHA-256 一致，0 个差异；另有 9 个报告/主机脚本按计划更新路径 |
| Windows / WSL 上游修改快照 | 两个补丁均可在锁定基准上通过应用检查 |

Go 初次下载依赖时默认代理超时；本次验证通过进程级 `GOPROXY=https://goproxy.cn,direct` 获取公开依赖后完成，没有修改用户的全局 Go 代理配置。SSH 私钥因已有 ACL 不读取内容，也不加入哈希清单。

完整固件重新组装和设备部署另按维护流程进行；本次不改变音箱上的程序或分区。
