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

## 使用方式

1. 将 `maaend-integration/` 中的文件复制到 MaaEnd 仓库对应位置
2. 在 MaaEnd 的 `register.go` 中添加 `protocolimaging.Register()`
3. 重新编译 MaaEnd

在 MaaEnd 界面中选择「📷基地全景图采集」任务，选择网格大小后启动。

详细集成步骤请参阅 [INTEGRATION_GUIDE.md](INTEGRATION_GUIDE.md)。

## 模块结构

```
protocol-imaging/
├─ app/
│  ├─ cli.py               # 纯拼接 CLI 入口（Go Service 调用）
│  ├─ image/
│  │  ├─ preprocess.py     # 预处理（模糊检测、UI 移除）
│  │  ├─ align.py          # ORB 特征匹配对齐
│  │  ├─ stitch.py         # 图像拼接 + 羽化融合
│  │  └─ annotate.py       # 标注（网格、标签）
│  ├─ project/             # 项目数据模型 + 存储
│  └─ export/
│     └─ png_export.py     # PNG 导出
├─ maaend-integration/     # MaaEnd 集成文件
│  ├─ agent/go-service/protocolimaging/
│  └─ assets/              # Pipeline、Task、国际化
├─ tests/                  # 测试
├─ docs/                   # 方案与路线文档
└─ requirements.txt
```

## 技术栈

Python · OpenCV · numpy · Pillow

## 许可

MIT。详见 [LICENSE](LICENSE)。
