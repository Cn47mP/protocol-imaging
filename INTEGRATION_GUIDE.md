# 协议映射全景图工具 - MaaEnd 集成指南

## 概述

本项目（protocol-imaging）已完全适配 MaaEnd，可以作为独立工具运行，也可以完全集成到 MaaEnd 的任务系统中！

## 方案一：完全集成到 MaaEnd（推荐）

### 1. 复制文件到 MaaEnd 仓库

```bash
# 假设你的 MaaEnd 仓库在 ../MaaEnd
cp -r maaend-integration/agent/go-service/protocolimaging ../MaaEnd/agent/go-service/
cp -r maaend-integration/assets/resource/pipeline/ProtocolImaging.json ../MaaEnd/assets/resource/pipeline/
cp -r maaend-integration/assets/tasks/ProtocolImaging.json ../MaaEnd/assets/tasks/
cp -r . ../MaaEnd/tools/protocolimaging/
```

### 2. 更新 MaaEnd 的 Go 服务注册

编辑 `../MaaEnd/agent/go-service/register.go`：

```go
import (
    // ... 其他导入
    "github.com/MaaXYZ/MaaEnd/agent/go-service/protocolimaging"
)

func registerAll() {
    // ... 其他注册
    protocolimaging.Register() // 添加这一行！
}
```

### 3. 更新 MaaEnd 的界面配置（可选）

如果需要让用户能在 MaaEnd 界面中选择「基地全景图采集」任务，可以编辑界面配置文件（在 `assets` 目录下）。

### 4. 重新编译 MaaEnd

```bash
cd ../MaaEnd
tools/build_and_install.py
```

---

## 方案二：作为独立工具使用

### 安装依赖

```bash
pip install -r requirements.txt
```

### 运行 GUI 模式
```bash
python -m app.main
```

### 运行 CLI 自动模式
```bash
python -m app.main --mode auto --preset medium --skip-blur --use-fusion --output my_base.png
```

---

## MaaEnd 任务使用方式

在 MaaEnd 中选择并运行 `ProtocolImaging` 任务，它会自动：

1. 找到并激活终末地窗口
2. 拉远视角
3. 按照 3x3 网格自动移动和采集
4. 自动拼接
5. 导出到 `base_panorama.png`

---

## 自定义参数

你可以直接修改 `assets/resource/pipeline/ProtocolImaging.json` 中的参数：

```json
{
    "ProtocolImagingStart": {
        "custom_action_param": {
            "capture": {
                "preset": "medium",  // 可选 small/medium/large/xlarge
                "skip_blur": true,
                "blur_threshold": 100.0,
                "use_fusion": true,
                "use_openstitching": false
            }
        }
    }
}
```

---

## 技术细节

### Go 服务 Custom Action 模块

- `types.go`: 定义参数类型和预设
- `action.go`: 核心执行逻辑，调用 Python CLI
- `register.go`: 注册 Custom Action 到 Maa Framework

### Python 模块结构

- `app.control.game_controller`: Win32 API 调用 SendInput（参考 Maa Framework）
- `app.capture.auto_capturer`: 网格采集逻辑
- `app.main`: 入口，支持 GUI 和 CLI 两种模式
