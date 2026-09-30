# L06A USB-only 单槽试刷方案

## 已确认前提

- 设备：L06A，PCB `AS06 VER0106`。
- Micro-USB WorldCup 下载模式连续三次冷启动捕获成功。
- 原厂 OTA 日志证明槽位配对：
  - `boot0/mtd2 + system0/mtd4 = 1.94.13`，当前可用回滚槽。
  - `boot1/mtd3 + system1/mtd5 = 1.88.221`，待试刷槽。
- 定制镜像从原始 `system1/mtd5` 的 1.88.221 构建，继续使用未修改的 `boot1/mtd3`。
- 镜像：`L06A_1.88.221_xiaoai-patch_20260929.squashfs`
- 长度：`0x2354000`（37,044,224 bytes）。
- SHA-256：`6344a856efa4b1893200aedfcfa1430de3b2430e3ab0ea47f884e0d4c1d0fcfd`

## 阶段 A：只写非活动 system1

1. 冷启动捕获 WorldCup 模式并再次执行 `identify`。
2. 只执行：

   ```powershell
   .\update.exe partition system1 "D:\xiaoai\local\build\L06A_1.88.221\L06A_1.88.221_xiaoai-patch_20260929.squashfs"
   ```

3. 不修改 bootloader、TPL、boot0、boot1、system0 或 data。
4. 写入过程中保持 12V 和 USB 稳定；不得断电或拔线。

## 阶段 B：写后回读校验

回读恰好与镜像相同的长度：

```powershell
.\update.exe mread store system1 normal 0x2354000 "D:\xiaoai\local\build\L06A_1.88.221\system1-after-flash-first-0x2354000.img"
```

计算 SHA-256。只有结果严格等于：

```text
6344a856efa4b1893200aedfcfa1430de3b2430e3ab0ea47f884e0d4c1d0fcfd
```

才允许进入阶段 C。若不一致，立即停止；保持 `boot_part=boot0`，继续从原厂 1.94.13 启动。

## 阶段 C：切换到 boot1

回读完全一致后才执行：

```powershell
.\update.exe bulkcmd "setenv boot_part boot1"
.\update.exe bulkcmd "setenv boot_failcnt 2"
.\update.exe bulkcmd "saveenv"
```

随后先断开 12V，退出下载模式，再冷启动测试。`boot1` 本身不重刷。

## USB 回滚

如果定制系统不能正常启动，重新冷启动捕获 WorldCup 模式后执行：

```powershell
.\update.exe bulkcmd "setenv boot_part boot0"
.\update.exe bulkcmd "setenv boot_failcnt 1"
.\update.exe bulkcmd "saveenv"
```

然后断开 12V，再冷启动。原厂 `boot0 + system0 = 1.94.13` 在整个试刷过程中保持不变。

## 停止条件

出现任一情况即停止，不继续切槽：

- `identify` 未稳定成功；
- `partition` 没有明确报告 `mwrite success`；
- USB 或 12V 中途掉线；
- 回读文件长度不是 37,044,224 bytes；
- 回读 SHA-256 不匹配；
- 工具输出与本方案预期不一致。

所有 NAND 和 U-Boot 环境写入必须在用户明确批准进入写入阶段后进行。
