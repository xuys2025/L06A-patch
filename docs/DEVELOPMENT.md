# 开发与维护指南

## 开发边界

`assistant-agent/` 继续作为单一 Go 模块，保留当前包结构与业务逻辑。优先按行为编写已有模块测试，待新功能出现明确边界时再拆包。目录整理不代表未来功能计划已经实施。

Shell 构建入口支持 Linux / WSL。PowerShell 设备入口在 Windows 使用。所有主入口根据自身位置找到根目录，可以从其他工作目录调用。设备内部的 `/data`、`/etc` 路径独立于电脑目录。

## 常用命令

从 Windows 项目根目录进入 WSL：

```powershell
wsl -d Ubuntu --cd .
```

随后运行：

```bash
bash scripts/verify/check.sh
VERSION=dev-local bash scripts/build/build_agent.sh
python3 scripts/verify/check_repository.py
```

`check.sh` 执行 Go 测试、`go vet`、Shell 语法和默认 JSON 校验。仓库边界检查以 Git 索引的文件列表为范围；首次引入文件需要先 `git add`。CI 使用相同入口，并交叉编译 ARMv7 程序。

构建产物写入 `assistant-agent/dist/assistant-agent-linux-armv7`，`VERSION` 会写入程序 `--version` 输出。没有显式版本时使用 Git 提交标识及 dirty 状态。当前历史产物保留在该目录中，构建会替换同名产物；希望保留它时指定其他输出：

```bash
XIAOAI_AGENT_BINARY="$PWD/local/build/candidate/assistant-agent-linux-armv7" \
  VERSION=dev-candidate bash scripts/build/build_agent.sh
```

## 路径设置

| 环境变量 | 默认位置 | 适用入口 |
|---|---|---|
| `XIAOAI_LOCAL_DIR` | `<项目>/local` | Bash / PowerShell |
| `XIAOAI_BUILD_DIR` | `<local>/build/L06A_1.88.221` | Bash / PowerShell |
| `XIAOAI_BACKUP_DIR` | `<local>/backups/L06A_1.88.221` | Bash |
| `XIAOAI_ACCESS_DIR` | `<build>/network-access` | Bash / PowerShell |
| `XIAOAI_PATCH_DIR` | `<项目>/external/xiaoai-patch` | Bash 软件包和旧镜像脚本 |
| `XIAOAI_AGENT_BINARY` | `<项目>/assistant-agent/dist/assistant-agent-linux-armv7` | Bash |
| `XIAOAI_GO` | 系统 Go，其次本地 Go 1.27.1 | Go 构建及检查 |
| `VERSION` | Git 提交标识 | Go 构建 |

PowerShell 网络更新通过 `-Binary`、`-PlayerScript` 选择输入，通过 `-SpeakerIP` 选择设备。路径变量使用对应操作系统的绝对路径，Windows 路径不能直接作为 Linux 路径使用。

## 固件构建

混合固件构建依赖 `unsquashfs`、`mksquashfs`、`patch`、`readelf`、Python 3 等 Linux 工具，解包保留设备节点时需要相应权限。输入为原始 system1 镜像、编译好的 ARMv7 助手、overlay 与补丁。

以下材料保存在本地，不由 Git 提供：

- `local/backups/L06A_1.88.221/ORIGINAL_READONLY/mtd5.img`
- `local/build/L06A_1.88.221/network-access/id_rsa_l06a.pub`
- `local/build/L06A_1.88.221/network-access/admin-token.txt`
- `local/build/L06A_1.88.221/network-access/root-password.hash`

本机密码哈希已从原构建脚本迁出，值保持一致。新环境由维护者准备所需凭据；密码哈希文件为单行 crypt SHA-256 / SHA-512 / MD5 格式（`$5$` / `$6$` / `$1$`）。镜像包含管理令牌种子与密码哈希，因此镜像也只保存在 `local/`。

使用单独输出目录可保留历史镜像：

```bash
export XIAOAI_ACCESS_DIR="$PWD/local/build/L06A_1.88.221/network-access"
export XIAOAI_BUILD_DIR="$PWD/local/build/candidate"
bash scripts/build/build_l06a_final_assistant.sh
bash scripts/verify/verify_l06a_final_assistant.sh
```

验证脚本会检查镜像中的助手是否与当前输入程序完全一致。历史 final3 镜像与目前 final14 二进制不一致，不能直接用当前程序验证历史镜像并宣称是同一发布。

旧软件包构建的 WSL 工作副本仍在 `/root/xiaoai-patch`。需要继续原构建时显式指定：

```bash
XIAOAI_PATCH_DIR=/root/xiaoai-patch bash scripts/build/resume_packages_lx06.sh
```

`run_packages_lx06.sh` 会先执行软件包清理；仅恢复构建请使用 `resume_packages_lx06.sh`。旧包构建和环境安装脚本保留原有代理与系统依赖假设，运行前阅读脚本。`scripts/legacy/` 为一次性实验脚本，可能引用旧临时目录，不属于标准验证入口。

## 设备维护

设备操作入口集中在 `scripts/device/`：

| 入口 | 用途 |
|---|---|
| `open_assistant_console.ps1` | 打开带令牌的本地管理界面 |
| `configure_assistant.ps1` | 交互式配置云服务 |
| `update_assistant_over_network.ps1` | 通过 SSH 上传并更新助手和播放器 |
| `flash_system1_final.ps1` | 带固定哈希与回读检查的历史 final3 固件刷写 |
| `runtime/` | 配套的设备端安装、修复与更新脚本 |

仅检查旧刷写材料、无需连接设备：

```powershell
./scripts/device/flash_system1_final.ps1 -PreflightOnly
```

刷写脚本仍锁定历史镜像的 SHA-256 和长度，新构建不会自动获得刷写资格。实际部署或刷写属于另一次明确的设备操作；保留原厂恢复槽与既有回滚流程。

## 文档入口

- [结构调整记录](PROJECT_STRUCTURE.md)
- [设备变更记录](build/DEVICE_CHANGE_LOG.md)
- [下一阶段统一排期](plans/L06A_NEXT_PHASE_SCHEDULE.md)
- [长期记忆计划](plans/L06A_LONG_TERM_MEMORY_PLAN.md)
- [固件重构研究](plans/L06A_FIRMWARE_REFACTOR_RESEARCH.md)
- [上游依赖及恢复方法](../external/README.md)
