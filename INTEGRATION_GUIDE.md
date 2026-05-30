# 协议映射 — MaaEnd 集成指南

## 架构概览

协议映射采用三层架构，遵循 MaaEnd 的 "Pipeline 管流程，Go 管难点" 原则：

```
Pipeline JSON — 流程编排
  ├─ CharacterControllerPitchDeltaAction 拉远视角
  ├─ ProtocolImagingCaptureGrid → Go Custom Action（网格采集）
  └─ ProtocolImagingStitch → Go Custom Action（调 Python 拼接）

Go Service — 采集 + 调用
  ├─ CaptureGridAction: CharacterController 移动 + MaaFramework 截图
  └─ StitchAction: subprocess → python -m app frames/ --output result.py

Python CLI — 纯图像处理
  └─ 读取截图 → ORB 对齐 → 羽化融合拼接 → 导出 PNG
```

## 集成步骤

### 1. 将 Go Service 添加到 MaaEnd

```bash
# 复制 Go service 代码
cp -r maaend-integration/agent/go-service/protocolimaging/ \
  ../MaaEnd/agent/go-service/protocolimaging/
```

在 `../MaaEnd/agent/go-service/register.go` 中添加：

```go
import "github.com/MaaXYZ/MaaEnd/agent/go-service/protocolimaging"

func registerAll() {
    // ... 其他注册
    protocolimaging.Register()
}
```

### 2. 将 Pipeline 和 Task JSON 添加到 MaaEnd

```bash
# Pipeline 定义
cp maaend-integration/assets/resource/pipeline/ProtocolImaging.json \
  ../MaaEnd/assets/resource/pipeline/

# Task 定义
cp maaend-integration/assets/tasks/ProtocolImaging.json \
  ../MaaEnd/assets/tasks/
```

### 3. 添加国际化文案

在 `../MaaEnd/assets/locales/interface/zh_cn.json` 和 `en_us.json` 中添加 `task.ProtocolImaging.*` 条目（详见 `maaend-integration/assets/locales/`）。

### 4. 将 Python 工具放到 MaaEnd 的 tools 目录

```bash
# 将整个 protocol-imaging 仓库放到 MaaEnd 的 tools 目录
cp -r . ../MaaEnd/tools/protocolimaging/

# 安装 Python 依赖
cd ../MaaEnd/tools/protocolimaging
pip install -r requirements.txt
```

### 5. 在 interface.json 中注册任务

在 `../MaaEnd/assets/interface.json` 的 `import` 数组中添加：

```json
"tasks/ProtocolImaging.json"
```

### 6. 重新编译 MaaEnd

```bash
cd ../MaaEnd
python tools/build_and_install.py
```

## Pipeline 节点说明

| 节点 | 类型 | 说明 |
|------|------|------|
| `ProtocolImagingStart` | 入口 | 流程起点 |
| `ProtocolImagingZoomOut` | CharacterControllerPitchDeltaAction | 拉远视角到俯视角度 |
| `ProtocolImagingCaptureGrid` | Custom Action (Go) | 蛇形网格采集截图到目录 |
| `ProtocolImagingStitch` | Custom Action (Go) | 调 Python CLI 拼接截图 |
| `ProtocolImagingEnd` | 终点 | 流程结束 |

## 用户选项

| 选项 | 类型 | 默认 | 说明 |
|------|------|------|------|
| 网格大小 | select | Medium (3×3) | Small/Large/XLarge |
| 羽化融合 | switch | Yes | 高斯羽化融合减少接缝 |

## 需要实测校准的参数

| 参数 | 当前值 | 说明 |
|------|--------|------|
| `pitch_delta` | 60 | 拉远视角角度，需实测 |
| `pan_duration` | 350ms | 每步移动时长，需实测 |
| 移动步长 | 由 CharacterController 决定 | `ForwardAxisAction` 的 axis × 100ms |
