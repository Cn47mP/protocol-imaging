# 协议映射

> 集成工业基地全景快照成像

《明日方舟：终末地》PC 端基地俯视图全景采集与拼接工具。通过 Win32 输入模拟控制视角、蛇形网格扫描截图，最终生成一张完整的基地全景图。

**当前形态**：作为 MaaEnd 的一个子模块运行。通过 `python -m app` 以 CLI 模式调用，MaaEnd 的 Go service 负责编排任务。

## 快速开始

### 安装

```bash
pip install -r requirements.txt
```

### 运行

```bash
python -m app --preset medium --use-fusion --output base_panorama.png
```

参数说明：

| 参数 | 说明 |
|------|------|
| `--preset` / `-p` | 网格预设：small/medium/large/xlarge，默认 medium |
| `--skip-blur [THRESHOLD]` | 跳过模糊帧，可选阈值，默认 100 |
| `--use-fusion` / `-f` | 启用羽化融合拼接 |
| `--use-openstitching` | 备选 OpenStitching 拼接 |
| `--output` / `-o` | 输出路径，默认 `base_panorama.png` |
| `--debug` / `-d` | 启用调试输出 |

## 集成 MaaEnd

详见 [`INTEGRATION_GUIDE.md`](INTEGRATION_GUIDE.md) 和 [`docs/migration-to-maaend.md`](docs/migration-to-maaend.md)。

## 模块结构

```
protocol-imaging/
├─ app/
│  ├─ main.py              # CLI 入口
│  ├─ capture/
│  │  ├─ window_capture.py # 窗口截图
│  │  ├─ recorder.py       # 连续帧录制
│  │  └─ auto_capturer.py  # 自动网格采集
│  ├─ control/
│  │  └─ game_controller.py # Win32 游戏视角控制 (SendInput)
│  ├─ image/
│  │  ├─ preprocess.py     # 预处理（模糊检测、UI 移除）
│  │  ├─ align.py          # 特征匹配 / 手动锚点
│  │  ├─ stitch.py         # 图像拼接 + 羽化融合
│  │  └─ annotate.py       # 标注（网格、标签）
│  ├─ project/
│  │  ├─ model.py          # 项目数据模型
│  │  └─ storage.py        # 项目保存/加载
│  └─ export/
│     └─ png_export.py     # PNG 导出
├─ maaend-integration/     # MaaEnd 集成文件
│  ├─ agent/go-service/protocolimaging/
│  └─ assets/
├─ docs/                   # 方案与路线文档
├─ INTEGRATION_GUIDE.md    # MaaEnd 集成指南
└─ requirements.txt
```

## 技术栈

Python · OpenCV · mss · Pillow · numpy · pywin32 · OpenStitching

## 路线

| 阶段 | 状态 | 说明 |
|------|------|------|
| P0 自动采集 | 已合入 | 窗口检测、WASD 视角控制、蛇形网格、预设选择 |
| P1 体验优化 | 已合入 | 模糊检测、羽化融合、OpenStitching 备选 |
| P2 MaaEnd 集成 | 进行中 | Go service + Pipeline + Web 前端面板 |

## 许可

MIT。详见 [LICENSE](LICENSE)。