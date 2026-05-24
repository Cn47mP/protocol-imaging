# 协议映射

> 集成工业基地全景快照成像

《明日方舟：终末地》PC 端基地俯视图导出工具。通过外部屏幕捕获与图像拼接生成基地全景图，用于查看设备摆放和基地布局。

## 当前状态

项目处于早期原型阶段，当前重点是搭建桌面端工程骨架与图像处理流程。

- 已建立基础模块结构
- 已规划截图、预处理、对齐、拼接、标注、导出流程
- 尚未接入完整可用的真实游戏窗口工作流
- 暂不提供演示素材和完整 Demo

## 使用

```bash
pip install -e .
protocol-imaging
```

也可以直接运行：

```bash
pip install -e .
protocol-imaging
```

也可以直接运行：

```bash
pip install -r requirements.txt
python -m app.main
```

预期流程：

1. 选择游戏窗口
2. 开始采集（手动移动视角覆盖全基地）
3. 停止采集
4. 锚点校正微调
5. 导出全景图

## 开发路线

### 草图阶段

- 区域截图
- 连续采集
- 截图序列保存
- 简单横向 / 纵向拼接
- PNG 导出

### 正稿阶段

- 窗口选择与画面裁切
- 图像预处理
- 特征匹配辅助对齐
- 手动锚点校正
- 基础标注图层

### 定稿阶段

- 项目文件保存 / 加载
- 更稳定的多图拼接
- 标注层编辑
- 导出参数配置
- 完整 GUI 工作流

## 目录约定

```text
samples/
├─ input/   # 示例输入截图；当前仅保留目录说明，不提交游戏截图素材
└─ output/  # 示例导出结果；当前仅保留目录说明，不提交生成图
```

实际采集输出默认不纳入 Git。

## 技术栈

Python · OpenCV · PySide6 · mss · Pillow · numpy

## 项目结构

```text
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
├─ docs/plans/
├─ samples/
│  ├─ input/
│  └─ output/
└─ requirements.txt
```

## 许可

MIT。详见 [LICENSE](LICENSE)。
