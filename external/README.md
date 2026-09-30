# 上游依赖

主仓库管理自有代码、固件 overlay 和适配补丁。`external/xiaoai-patch` 保留独立 Git 历史，整个工作副本被主仓库忽略。这样不会把构建缓存或嵌套 `.git` 误当作主仓库内容，也不会用未发布提交创建无法克隆的 submodule。

基准版本记录在 [dependencies.lock.json](dependencies.lock.json)。现有两个工作副本均基于 `fb070495955668f825700bbd988d5443e9bcadb3`，但修改不同：

| 工作副本 | 位置 | 保存的修改 |
|---|---|---|
| Windows | `external/xiaoai-patch` | [windows-local.patch](../firmware/upstream/windows-local.patch)：Wi-Fi 脚本及 shairport-sync 配方 |
| WSL | `/root/xiaoai-patch`，未移动 | [wsl-local.patch](../firmware/upstream/wsl-local.patch)：软件包配方、固件兼容补丁、未追踪脚本 |

两个补丁分别相对基准提交生成，均通过 `git apply --cached --check`。它们是不同工作副本的快照，包含重叠文件，不要连续应用在同一工作树。`firmware/packages/` 保存根目录原有的自定义配方，是后续适配的可编辑入口；修改后需同步到选定上游工作副本并重新验证。

## 新环境恢复

现有目录已经存在时无需重建。新环境在主仓库根目录运行：

```bash
git clone https://github.com/duhow/xiaoai-patch.git external/xiaoai-patch
git -C external/xiaoai-patch checkout -b l06a-local fb070495955668f825700bbd988d5443e9bcadb3
git -C external/xiaoai-patch apply ../../firmware/upstream/windows-local.patch
```

WSL 软件包构建建议使用 Linux 文件系统中的独立克隆，以保留符号链接和执行权限；检出相同基准，应用 `wsl-local.patch`，再将 `XIAOAI_PATCH_DIR` 指向它。固件与构建缓存需自行准备。

后续修改上游工作副本时，在该仓库内检查和提交，同时更新主仓库记录的基准及补丁。主仓库的 `git status` 不显示独立仓库的内部修改。参考项目与下载的厂商示例仅用于研究，分别保存在 `local/reference/`、`local/vendor/`。
