# 项目协作规则

- 默认在当前 Agent 完成工作；不能仅为提速、并行检查或增加信心派生子 Agent。
- 只有确实无法独立完成、且已尝试合理的本地检查、推理与测试后，才可委派一个必要且有界的子任务。派生前向用户简要说明必要性。
- 每个子 Agent 必须显式使用 `gpt-5.6-luna`；无法保证该型号时不派生。嵌套子 Agent 同样受此限制；有界任务完成后立即停止或释放。
- 从 README 和 docs/DEVELOPMENT.md 了解入口；Go 主模块位于 assistant-agent/。
- 修改源码后运行适用检查；标准入口为 `bash scripts/verify/check.sh` 和 `python3 scripts/verify/check_repository.py`。
- 主机路径通过 scripts/lib/paths.sh 或 paths.ps1 取得，避免写死电脑项目位置。
- local/、assistant-agent/dist/ 和 external/ 下的独立工作副本不进入主仓库。凭据与原厂固件只保存在本地。
- 保留 external/xiaoai-patch 的独立历史与现有修改；需要修改时同时维护版本清单及对应补丁记录。
- 目录、文档和普通代码任务使用离线验证。设备部署、刷写或录音需属于用户明确授权的任务范围。
- 原厂恢复槽、镜像大小、哈希与回读检查沿用已有维护流程。
