# 协议映射

> 集成工业基地全景快照成像

《明日方舟：终末地》PC 端基地俯视图导出工具。通过外部屏幕捕获与图像拼接生成基地全景图，用于查看设备摆放和基地布局。

## 当前状态

项目处于可运行原型阶段，P0/P1 路线中的核心自动采集能力已经合入，当前重点转为真实游戏场景校准和稳定性验证。

- 已建立 PySide6 桌面端和单文件 exe 打包流程
- 已支持显示器/窗口截图、连续采集、帧序列管理、项目保存/加载
- 已支持自动检测终末地窗口、Win32 输入模拟、蛇形网格自动采集
- 已支持模糊检测、特征匹配/手动锚点校正、羽化融合拼接、PNG 导出
- 已支持基础标注图层（管线、标签）
- 仍需用真实 AIC 基地校准单帧覆盖格数、移动步长和不同分辨率下的稳定性

## 环境要求

- **Python 3.10+**（需已加入系统 PATH）
- Windows 10/11

## 安装与启动

### 1. 安装依赖

```bash
pip install -r requirements.txt
```

主要依赖：

| 包 | 最低版本 | 用途 |
|---|---|---|
| PySide6 | ≥6.5.0 | GUI 框架 |
| opencv-python-headless | ≥4.8.0 | 图像处理与特征匹配 |
| numpy | ≥1.24.0 | 数组计算 |
| Pillow | ≥10.0.0 | 图像读写 |
| mss | ≥9.0.0 | 屏幕截图 |
| pywin32 | ≥306 | Windows 窗口检测与前台状态检查 |
| stitching | ≥0.4.0 | OpenStitching 备选拼接方案 |

### 2. 启动

**方式一：双击启动**

- `run.bat` — 正常启动（自动检测 Python、自动安装依赖）
- `run_debug.bat` — 调试模式（带日志输出，窗口不自动关闭）

**方式二：命令行启动**

```bash
python -m app
```

### 3. 打包为 exe（可选）

```bash
pip install pyinstaller
build.bat
```

生成的 `dist/protocol-imaging.exe` 为单文件可执行程序，无需 Python 环境即可运行。

### 4. 开发模式安装（可选）

```bash
pip install -e .
protocol-imaging
```

或双击 `run.bat`（自动检测 Python / 安装依赖）。

预期流程：

1. 自动检测终末地游戏窗口，或手动选择显示器/区域
2. 选择小/中/大/超大网格预设并开始自动采集
3. 工具拉远视角，按蛇形路径移动并截图，自动跳过模糊帧
4. 对采集帧执行自动特征匹配，必要时用手动锚点微调
5. 使用羽化融合生成全景图，并按需添加管线/标签标注
6. 保存项目或导出 PNG

## 开发路线

### P0：自动采集（已合入）

- 游戏窗口自动检测：`WindowCapture.auto_detect_game_window()`
- Win32 游戏控制器：`GameController` 使用 `SendInput` 执行鼠标拖拽、滚轮缩放、WASD 备选输入
- 自动采集流程：`AutoCapturer` 支持蛇形网格扫描、小/中/大/超大预设、进度回调、取消操作
- GUI 集成：自动检测游戏窗口、自动采集按钮、进度显示、后台线程执行

### P1：体验优化（已合入，待实测调参）

- 运动模糊检测：Laplacian 方差低于阈值时跳过帧
- 羽化融合：重叠区域使用高斯渐变混合，减少硬接缝
- 前台检测：采集前确认游戏窗口状态
- OpenStitching 备选：在 ORB/手动锚点不稳定时可尝试 `stitching` 包
- 网格特征检测草案：已加入 `detect_floor_grid()`，仍需真实截图验证

### P2：下一阶段

- 校准工具：记录单帧可见格数、推荐网格行列数、移动步长和重叠率
- 自动采集参数化：允许用户保存不同分辨率/相机高度/基地规模的采集配置
- 稳定性策略：窗口遮挡、游戏未前台、拖拽失败、拼接失败时给出可恢复提示
- 标注体验增强：标注编辑、删除、吸附、导入/导出图层
- 发布流程完善：每次合入功能后自动构建、校验、上传 release 资产

## 目录约定

```text
samples/
├─ input/   # 示例输入截图；当前仅保留目录说明，不提交游戏截图素材
└─ output/  # 示例导出结果；当前仅保留目录说明，不提交生成图
```

实际采集输出默认不纳入 Git。

## 技术栈

Python · OpenCV · PySide6 · mss · Pillow · numpy · pywin32 · OpenStitching

## 项目结构

```text
protocol-imaging/
├─ app/
│  ├─ main.py              # 应用入口
│  ├─ ui/main_window.py    # 主窗口 UI
│  ├─ capture/
│  │  ├─ window_capture.py # 窗口截图
│  │  ├─ recorder.py       # 连续帧录制
│  │  └─ auto_capturer.py  # 自动网格采集
│  ├─ control/
│  │  └─ game_controller.py # Win32 游戏视角控制
│  ├─ image/
│  │  ├─ preprocess.py     # 预处理（去噪、归一化）
│  │  ├─ align.py          # 特征匹配 / 手动锚点
│  │  ├─ stitch.py         # 图像拼接
│  │  └─ annotate.py       # 标注（网格、标签）
│  ├─ project/
│  │  ├─ model.py          # 项目数据模型
│  │  └─ storage.py        # 项目保存/加载
│  └─ export/
│     └─ png_export.py     # PNG 导出
├─ docs/plans/
├─ samples/
│  ├─ input/
│  └─ output/
└─ requirements.txt
```

## 许可

MIT。详见 [LICENSE](LICENSE)。
