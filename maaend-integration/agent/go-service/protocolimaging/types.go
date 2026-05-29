package protocolimaging

import "github.com/bytedance/sonic"

// CapturePreset 预设采集模式
type CapturePreset string

const (
    CapturePresetSmall  CapturePreset = "small"   // 小型基地 2x2
    CapturePresetMedium CapturePreset = "medium"  // 中型基地 3x3
    CapturePresetLarge  CapturePreset = "large"   // 大型基地 4x4
    CapturePresetXLarge CapturePreset = "xlarge"  // 超大基地 5x5
)

// CaptureParams 采集参数
type CaptureParams struct {
    Preset      CapturePreset `json:"preset,omitempty"`
    SkipBlur    bool          `json:"skip_blur,omitempty"`
    BlurThreshold float64      `json:"blur_threshold,omitempty"`
    UseFusion   bool          `json:"use_fusion,omitempty"`
    UseOpenStitching bool      `json:"use_openstitching,omitempty"`
}

// DefaultCaptureParams 默认参数
func DefaultCaptureParams() CaptureParams {
    return CaptureParams{
        Preset:      CapturePresetMedium,
        SkipBlur:    true,
        BlurThreshold: 100.0,
        UseFusion:   true,
        UseOpenStitching: false,
    }
}

// ActionParams 动作参数（来自 Maa Framework）
type ActionParams struct {
    Capture  CaptureParams `json:"capture,omitempty"`
    OutputPath string       `json:"output_path,omitempty"`
}

func (p ActionParams) String() string {
    b, _ := sonic.Marshal(p)
    return string(b)
}
