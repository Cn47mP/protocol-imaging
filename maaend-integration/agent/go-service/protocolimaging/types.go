package protocolimaging

// CaptureGridParams 网格采集参数
type CaptureGridParams struct {
	Rows        int    `json:"rows"`         // 网格行数
	Cols        int    `json:"cols"`         // 网格列数
	PanDuration int    `json:"pan_duration"` // 每步移动时长 (ms)
	ZoomSteps   int    `json:"zoom_steps"`   // 拉远视角的滚轮步数
	OutputDir   string `json:"output_dir"`   // 截图输出目录
	SkipBlur    bool   `json:"skip_blur"`    // 跳过模糊帧
	BlurThreshold float64 `json:"blur_threshold"` // 模糊阈值
}

// DefaultCaptureGridParams 默认网格采集参数（3×3 medium 预设）
func DefaultCaptureGridParams() CaptureGridParams {
	return CaptureGridParams{
		Rows:          3,
		Cols:          3,
		PanDuration:   350,
		ZoomSteps:     10,
		OutputDir:     "pi_frames",
		SkipBlur:      true,
		BlurThreshold: 100.0,
	}
}

// StitchParams 拼接参数
type StitchParams struct {
	FramesDir       string `json:"frames_dir"`       // 截图目录
	OutputPath      string `json:"output_path"`      // 输出路径
	UseFusion       bool   `json:"use_fusion"`       // 羽化融合
	SkipBlur        bool   `json:"skip_blur"`        // 跳过模糊帧
	BlurThreshold   float64 `json:"blur_threshold"`  // 模糊阈值
	UseOpenStitching bool  `json:"use_openstitching"` // 使用 OpenStitching
}

// DefaultStitchParams 默认拼接参数
func DefaultStitchParams() StitchParams {
	return StitchParams{
		FramesDir:     "pi_frames",
		OutputPath:    "base_panorama.png",
		UseFusion:     true,
		SkipBlur:      true,
		BlurThreshold: 100.0,
	}
}
