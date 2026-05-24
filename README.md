# 协议映射

> 集成工业基地全景快照成像

《明日方舟：终末地》PC 端基地俯视图导出工具。零注入、零内存读取、零改客户端，仅靠外部屏幕捕获 + 图像拼接生成基地全景图。

## 名称

- **协议** — 终末地工业核心技术体系，与"协议核心""协议传送""协议回收"同源
- **映射** — 通过协议扫描对基地进行完整成像，将实际布局映射为全景图

## 版本

- **草本** — 手工锚点校正，初步成图
- **正本** — 自动匹配，标准成图
- **定本** — 全功能，可存档编辑

## 使用

```bash
pip install -r requirements.txt
python -m app.main
```

1. 选择游戏窗口
2. 开始采集（手动移动视角覆盖全基地）
3. 停止采集
4. 锚点校正微调
5. 导出全景图

## 技术栈

Python · OpenCV · PySide6 · mss · Pillow · numpy

## 项目结构

```
protocol-imaging/
├─ app/
│  ├─ main.py              # 应用入口
│  ├─ ui/main_window.py    # 主窗口 UI
│  ├─ capture/
│  │  ├─ window_capture.py # 窗口截图
│  │  └─ recorder.py       # 连续帧录制
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
├─ tests/
├─ docs/plans/
└─ requirements.txt
```

## 许可

MIT
