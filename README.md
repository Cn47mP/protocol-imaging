# 协议映射

> 集成工业基地全景快照成像

《明日方舟：终末地》PC 端基地俯视图全景采集与拼接工具。作为 MaaEnd 的子模块运行。

## 架构

```
MaaEnd Pipeline（场景识别 + 流程编排）
  ├─ CharacterControllerPitchDeltaAction 拉远视角
  ├─ ProtocolImagingCaptureGrid (Go Custom Action)
  │    └─ 蛇形网格移动 + MaaFramework 截图
  └─ ProtocolImagingStitch (Go Custom Action)
       └─ 调 Python CLI 拼接
```

- **Pipeline** — 用 JSON 编排采集流程，利用 MaaEnd 内置的 CharacterController
- **Go Service** — 两个 Custom Action：网格采集 + 拼接调用
- **Python CLI** — 纯图像处理：对齐（ORB）+ 拼接（羽化融合）+ 导出

## 作为 MaaEnd 组件使用

1. 将 `maaend-integration/` 中的文件复制到 MaaEnd 仓库对应位置
2. 在 MaaEnd 的 `register.go` 中添加 `protocolimaging.Register()`
3. 重新编译 MaaEnd

在 MaaEnd 界面中选择「📷基地全景图采集」任务，选择网格大小后启动。

## 作为独立 CLI 使用

```bash
pip install -r requirements.txt

# 拼接已有截图
python -m app frames/ --output result.png --use-fusion

# 完整自动采集（需要游戏窗口）
python -m app.main --preset medium --use-fusion --output base_panorama.png
```

### CLI 参数

| 参数 | 说明 |
|------|------|
| `frames_dir` | 截图目录（纯拼接模式） |
| `--preset` / `-p` | 网格预设：small/medium/large/xlarge |
| `--skip-blur [THRESHOLD]` | 跳过模糊帧，可选阈值，默认 100 |
| `--use-fusion` / `-f` | 启用羽化融合拼接 |
| `--use-openstitching` | 备选 OpenStitching 拼接 |
| `--output` / `-o` | 输出路径，默认 `base_panorama.png` |
| `--debug` / `-d` | 启用调试输出 |

## 模块结构

```
protocol-imaging/
├─ app/
│  ├─ cli.py               # 纯拼接 CLI 入口（MaaEnd 调用）
│  ├─ main.py              # 完整自动采集入口（独立使用）
│  ├─ capture/             # 窗口截图 + 自动采集（独立模式用）
│  ├─ control/             # Win32 游戏控制（独立模式用）
│  ├─ image/
│  │  ├─ preprocess.py     # 预处理（模糊检测、UI 移除）
│  │  ├─ align.py          # ORB 特征匹配对齐
│  │  ├─ stitch.py         # 图像拼接 + 羽化融合
│  │  └─ annotate.py       # 标注（网格、标签）
│  ├─ project/             # 项目数据模型 + 存储
│  └─ export/
│     └─ png_export.py     # PNG 导出
├─ maaend-integration/     # MaaEnd 集成文件（参考）
│  └─ agent/go-service/protocolimaging/
├─ docs/                   # 方案与路线文档
└─ requirements.txt
```

Go service 代码已迁移到 MaaEnd 仓库的 `agent/go-service/protocolimaging/`。

## 技术栈

Python · OpenCV · numpy · Pillow · pywin32 · OpenStitching

## 许可

MIT。详见 [LICENSE](LICENSE)。
