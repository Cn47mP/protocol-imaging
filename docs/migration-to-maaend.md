# 协议映射 → MaaEnd 迁移路线

> 2026-05-29

## 背景

协议映射最初是独立的 PySide6 桌面应用，目标是对终末地 AIC 基地做全景快照成像。经过 P0/P1 路线的开发，自动检测窗口和 WASD 采集的基础框架已经建立。

但在真实游戏测试中暴露出几个结构性问题：

- PySide6 桌面应用的窗口管理、输入模拟、实时截图每一层都需要和 MaaFramework 的 Win32 控制层做重复实现
- MaaEnd 已经拥有成熟的窗口检测、SendInput 键盘/鼠标、FramePool/DXGI 后台截图、场景跳转等基础设施
- MaaEnd 有 3.1k star 的用户社区和 web 前端分发渠道，独立 pyinstaller exe 分发效率远不如它

因此决定：**将全景成像功能作为 MaaEnd 的一个组件实现，protocol-imaging 仓库转为资产储备和迁移文档载体。**

## 架构对比

| 层次 | protocol-imaging（当前） | MaaEnd（目标） |
|------|--------------------------|----------------|
| 窗口检测 | `window_capture.py` EnumWindows | MaaFramework Win32 控制器 |
| 输入模拟 | `game_controller.py` SendInput | MaaFramework SeizeInput |
| 截图 | `window_capture.py` mss.grab | MaaFramework FramePool/DXGI/GDI |
| 场景管理 | 无 | MaaEnd SceneManager |
| 任务编排 | GUI 按钮直接调用 | Go service 后台调度 |
| 用户界面 | PySide6 桌面窗口 | Web 前端 (TypeScript) |
| 图像处理 | Python OpenCV 管线 | 提取为独立 CLI 工具 |
| 发布 | pyinstaller exe | MaaEnd 内嵌组件 |

## 可复用资产

以下 protocol-imaging 模块可以直接/少量修改后复用到 MaaEnd 组件中：

| 模块 | 文件 | 复用方式 |
|------|------|----------|
| 模糊检测 | `app/image/preprocess.py` → `is_blurry()` | 直接迁移到 MaaEnd Python 组件 |
| UI 移除 | `app/image/preprocess.py` → `remove_ui_elements()` | 需按终末地真实 UI 布局重新校准 |
| ORB 特征匹配 | `app/image/align.py` → `auto_align()` | 直接迁移 |
| 手动锚点校正 | `app/image/align.py` → `manual_align()` | 需适配 web GUI 的锚点 UI |
| 逐帧拼接 | `app/image/stitch.py` → `stitch_sequential()` | 直接迁移 |
| 羽化融合 | `app/image/stitch.py` → `blend_images()` | 直接迁移 |
| OpenStitching 备选 | `app/image/stitch.py` → `stitch_with_openstitching()` | 直接迁移 |
| 网格/标签标注 | `app/image/annotate.py` | 迁移，需 web 端标注 UI |
| PNG 导出 | `app/export/png_export.py` | 直接迁移 |

以下模块由 MaaEnd 替代，不需要迁移：

| 模块 | 文件 | 替代方案 |
|------|------|----------|
| 窗口检测 | `app/capture/window_capture.py` | MaaFramework Win32 控制器 |
| 连续录制 | `app/capture/recorder.py` | Pipeline 截图机制 |
| 自动采集器 | `app/capture/auto_capturer.py` | 新 Pipeline + Go service |
| 游戏控制器 | `app/control/game_controller.py` | MaaFramework SeizeInput |
| 主窗口 UI | `app/ui/main_window.py` | Web 前端 |
| 校准对话框 | `app/ui/widgets/calibration_dialog.py` | Web 前端 |
| 标注覆盖层 | `app/ui/widgets/annotation_overlay.py` | Web 前端 |
| 项目存储 | `app/project/model.py` + `storage.py` | 按需迁移到 MaaEnd 数据格式 |
| 应用入口 | `app/main.py` + `__main__.py` | 不需要 |

## 实施路线

### Phase 1：提取 Python CLI 工具（1-2 天）

**目标**：把图像处理管线打包成独立的命令行工具，输入是截图目录，输出是全景 PNG。

```
panoramic-stitch/
├── stitch.py        # 从 app/image/stitch.py 提取
├── align.py         # 从 app/image/align.py 提取
├── preprocess.py    # 从 app/image/preprocess.py 提取
├── annotate.py      # 从 app/image/annotate.py 提取
├── export.py        # 从 app/export/png_export.py 提取
├── __main__.py      # CLI 入口
└── requirements.txt # opencv-python-headless, numpy, Pillow, stitching
```

CLI 用法：

```bash
python -m panoramic-stitch frames/ --output result.png --blend --blur-threshold 100
```

**交付物**：一个可被 MaaEnd 通过 subprocess 调用的独立 Python 包。

### Phase 2：编写 MaaFramework Pipeline（2-3 天）

**目标**：用 Pipeline JSON 定义自动网格采集流程。

参考 MaaEnd 现有的高级组件文档：

- `CharacterController` — 已有 WASD 移动和视角旋转能力，可以直接复用
- 新增一个 Custom 动作节点 `PanoramicStitchAction` 调用 Phase 1 的 CLI

Pipeline 流程设计：

```json
{
  "全景采集": {
    "recognition": "TemplateMatch",
    "template": "aic_base_screen.png",
    "action": "DoNothing",
    "next": ["拉远视角"]
  },
  "拉远视角": {
    "recognition": "DirectHit",
    "action": "Custom",
    "custom_action": "PanoramicZoomOut",
    "next": ["网格采集"]
  },
  "网格采集": {
    "recognition": "DirectHit",
    "action": "Custom",
    "custom_action": "PanoramicGridCapture",
    "custom_action_param": { "rows": 4, "cols": 4, "pan_duration": 0.35 },
    "next": ["拼接导出"]
  },
  "拼接导出": {
    "recognition": "DirectHit",
    "action": "Custom",
    "custom_action": "PanoramicStitchAction",
    "custom_action_param": { "output_dir": "~/panoramic_output/" }
  }
}
```

**关键 Custom 动作**：

1. `PanoramicZoomOut` — 连续滚轮拉远（复用现有 SendInput 的 scroll 能力）
2. `PanoramicGridCapture` — 蛇形网格 WASD + 截图（复用 CharacterController 的 WASD）
3. `PanoramicStitchAction` — 调用 Phase 1 的 CLI 拼接工具

**交付物**：一个可工作的大地图采集 Pipeline JSON 配置。

### Phase 3：编写 Go Service（1-2 天）

**目标**：参照 MaaEnd 现有 Go service（如 `autoecofarm`、`autosell`）的模式，创建 `panoramic-capture` service。

目录结构：

```
agent/go-service/panoramiccapture/
├── main.go          # service 入口和生命周期
├── task.go          # 任务编排逻辑
├── config.go        # 用户配置（网格大小、输出路径等）
└── pipeline.json    # Phase 2 的 Pipeline 配置
```

**交付物**：MaaEnd 中可注册和调度的全景采集 service。

### Phase 4：Web GUI 集成（2-3 天）

**目标**：在 MaaEnd 的 web 前端添加全景采集入口。

参照 MaaEnd 现有功能面板（如"自动采集"、"生态农场"）的模式：

1. 左侧导航新增"基地全景"入口
2. 面板内容：
   - 网格大小选择：小(2×2) / 中(3×3) / 大(4×4) / 超大(5×5)
   - 重叠率调节
   - 输出目录选择
   - 一键启动 + 进度显示
   - 完成后预览缩略图 + 下载按钮
3. 状态反馈：运行中 / 完成 / 失败原因

**交付物**：Web 前端可用的全景采集功能。

### Phase 5：测试与校准（持续）

1. 用真实 AIC 截图校准单帧覆盖格数和移动步长
2. 测试不同分辨率下的网格参数
3. 验证拼接质量（ORB vs OpenStitching）
4. 收集用户反馈

## 与 MaaEnd 社区的协作

1. **先在 MaaEnd 的开发 QQ 群（1072587329）或 GitHub Discussion 发起提案**，说明全景成像功能的定位和价值
2. **提交 Phase 1 的 CLI 工具**作为独立组件，MaaEnd 可以直接 pip install
3. **基于 CLI 工具提交 Phase 2-4 的 PR**，按 MaaEnd 的编码规范操作
4. **不删除 protocol-imaging 仓库**，改为此迁移计划 + 历史资产存档

## 不做的

- 不继续维护 PySide6 桌面版（被 MaaEnd web 前端替代）
- 不继续维护 pyinstaller exe 分发（被 MaaEnd 内嵌替代）
- 不继续在 protocol-imaging 仓库开发采集/控制功能（由 MaaEnd 替代）