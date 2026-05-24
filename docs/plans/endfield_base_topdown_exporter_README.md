# 协议映射

> 集成工业基地全景快照成像

《明日方舟：终末地》PC 端基地俯视图导出工具。

## 名称释义

- **映射**：通过协议扫描对基地进行完整成像，将实际布局映射为全景图

## 工具简介

一个用于《明日方舟：终末地》PC 端基地俯视图导出的外部工具。从游戏画面中采集基地截图，自动或半自动拼接成一张完整的基地全景图，用于查看设备摆放和基地布局。

## 快速开始

### 环境要求

- Windows 10 / 11
- Python 3.10+
- 游戏窗口运行中

### 安装

```bash
git clone https://github.com/Cn47mP/protocol-imaging.git
cd protocol-imaging
pip install -r requirements.txt
```

### 使用

```bash
python -m app.main
```

1. 选择游戏窗口
2. 点击"开始采集"
3. 手动控制游戏视角移动，覆盖整个基地
4. 点击"停止采集"
5. 使用锚点校正功能微调错位
6. 导出全景图

## 目标

- 采集游戏窗口画面
- 连续截图并保存
- 将多张视角图拼接成总览图
- 导出高清 PNG
- 支持基础标注

## 非目标

- 不做进程注入
- 不做内存读取
- 不修改客户端文件
- 不自动操作游戏
- 不追求一开始就全自动无误拼接

## 核心思路

1. **截图层**：从窗口外部采集画面
2. **对齐层**：对相邻截图进行位置校正
3. **拼接层**：合成为一张大图
4. **导出层**：输出 PNG，并可叠加标注

## 技术栈

- Python
- OpenCV
- PySide6 / PyQt
- mss 或 dxcam
- Pillow
- numpy

## 项目结构

```text
protocol-imaging/
├─ app/
│  ├─ main.py
│  ├─ ui/
│  │  ├─ main_window.py
│  │  └─ widgets/
│  ├─ capture/
│  │  ├─ window_capture.py
│  │  └─ recorder.py
│  ├─ image/
│  │  ├─ preprocess.py
│  │  ├─ align.py
│  │  ├─ stitch.py
│  │  └─ annotate.py
│  ├─ project/
│  │  ├─ model.py
│  │  └─ storage.py
│  └─ export/
│     └─ png_export.py
├─ tests/
├─ docs/
└─ requirements.txt
```

## 开发顺序

1. 窗口截图
2. 截图预览
3. 保存帧序列
4. 4 点标定
5. 单张拼接
6. 多张拼接
7. PNG 导出
8. 项目保存
9. 自动匹配
10. 标注层

## 验收标准

- 能稳定抓取游戏窗口
- 能生成完整基地全景图
- 导出的 PNG 可正常放大查看
- 图中设备位置基本正确

## 许可证

MIT
