# L06A 自定义语音助手

面向小米 L06A / Amlogic A113X 的 Go 语音助手及混合固件工程。复用原厂唤醒、语音和播放能力，接入流式 ASR、可配置 LLM、音乐与设备管理界面。

当前开发状态以 [设备变更记录](docs/build/DEVICE_CHANGE_LOG.md) 为准；下一阶段工作见 [统一排期](docs/plans/L06A_NEXT_PHASE_SCHEDULE.md)。历史固件报告与当前 Go 源码对应不同发布版本，构建前应核对版本和 SHA-256。

## 目录

| 路径 | 用途 | Git 管理 |
|---|---|---|
| `assistant-agent/` | Go 模块、业务代码和测试 | 是，排除 `dist/` |
| `firmware/overlay/` | 镜像文件、启动脚本及默认配置 | 是 |
| `firmware/patches/` | 最终固件补丁和 LX06 兼容补丁 | 是 |
| `firmware/packages/` | 自定义软件包构建配方 | 是 |
| `firmware/upstream/` | 两个现有上游工作副本的修改快照 | 是 |
| `scripts/` | 构建、验证、环境准备和设备维护入口 | 是 |
| `tools/` | CMake 兼容包装及离线诊断源码 | 是 |
| `docs/` | 开发指南、计划、构建记录与研究结论 | 是 |
| `deploy/` | 可选 Home Assistant 部署配置 | 是 |
| `external/` | 上游仓库版本清单和独立工作副本 | 仅清单及说明 |
| `local/` | 固件、备份、设备凭据、工具链及调查原始材料 | 否 |

## 开发与验证

Linux / WSL 下使用 Go 1.22 或以上版本。现有工作区的 Go 1.27.1 工具链位于 `local/toolchains/`，脚本会在系统没有 Go 时使用它。新克隆应自行安装 Go；依赖由 `assistant-agent/go.mod` 和 `go.sum` 管理。

```bash
# 在项目根目录执行：测试、静态检查、Shell 语法及配置格式校验。
bash scripts/verify/check.sh

# 输出 Linux ARMv7 静态程序到 assistant-agent/dist/。
VERSION=dev-local bash scripts/build/build_agent.sh

# 检查 Git 已追踪文件中的本地数据、二进制、凭据及文档链接。
python3 scripts/verify/check_repository.py
```

源码检查与编译不需要原厂固件、密钥或连接音箱。若依赖下载受网络限制，使用自己可访问的 `GOPROXY` 或代理环境变量。完整步骤、可覆盖路径和设备维护入口见 [开发指南](docs/DEVELOPMENT.md)。

## Git 工作方式

根目录是主仓库，默认分支 `main`。上游 `xiaoai-patch` 保持独立 Git 历史，版本与本地修改说明见 [上游依赖](external/README.md)。主仓库暂未设置远端，GitHub Actions 配置将在以后配置远端并推送后运行。

```bash
git status --short
git switch -c feature/your-change
# 修改并运行上述检查后，选择具体文件暂存。
git add assistant-agent scripts docs
git diff --cached
git commit -m "Describe the change"
```

`local/` 和 `assistant-agent/dist/` 不会进入版本历史；设备密钥、令牌和 root 密码哈希也有单独忽略规则。Git 不能替代原厂分区备份，请独立备份 `local/backups/` 和设备凭据。

此次目录迁移说明见 [结构调整记录](docs/PROJECT_STRUCTURE.md)。
