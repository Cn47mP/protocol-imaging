package protocolimaging

import (
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const component = "protocolimaging"

// ── CaptureGrid Action ──

// CaptureGridAction 蛇形网格采集：在每个网格点截图并保存到目录
type CaptureGridAction struct{}

var _ maa.CustomActionRunner = &CaptureGridAction{}

func (a *CaptureGridAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	params := DefaultCaptureGridParams()
	if arg != nil && arg.CustomActionParam != "" {
		if err := json.Unmarshal([]byte(arg.CustomActionParam), &params); err != nil {
			log.Error().Err(err).Str("component", component).Msg("CaptureGrid: failed to parse params")
			return false
		}
	}

	// 创建输出目录
	if err := os.MkdirAll(params.OutputDir, 0755); err != nil {
		log.Error().Err(err).Str("component", component).Msg("CaptureGrid: failed to create output dir")
		return false
	}

	ctrl := ctx.GetTasker().GetController()
	if ctrl == nil {
		log.Error().Str("component", component).Msg("CaptureGrid: nil controller")
		return false
	}

	totalFrames := params.Rows * params.Cols
	captured := 0

	log.Info().
		Str("component", component).
		Int("rows", params.Rows).
		Int("cols", params.Cols).
		Int("total", totalFrames).
		Msg("CaptureGrid: starting grid capture")

	for row := 0; row < params.Rows; row++ {
		// 蛇形：偶数行从左到右，奇数行从右到左
		cols := make([]int, params.Cols)
		for i := range cols {
			if row%2 == 0 {
				cols[i] = i
			} else {
				cols[i] = params.Cols - 1 - i
			}
		}

		for _, col := range cols {
			// 截图
			ctrl.PostScreencap().Wait()
			img, err := ctrl.CacheImage()
			if err != nil || img == nil {
				log.Error().Err(err).Str("component", component).
					Int("row", row).Int("col", col).
					Msg("CaptureGrid: screenshot failed")
				return false
			}

			// 保存截图
			filename := fmt.Sprintf("frame_%02d_%02d.png", row, col)
			outPath := filepath.Join(params.OutputDir, filename)
			if err := saveImage(img, outPath); err != nil {
				log.Error().Err(err).Str("component", component).
					Str("path", outPath).
					Msg("CaptureGrid: save failed")
				return false
			}

			captured++
			log.Info().
				Str("component", component).
				Int("row", row).Int("col", col).
				Int("captured", captured).Int("total", totalFrames).
				Msg("CaptureGrid: frame captured")

			// 移动到下一个网格点（除了最后一个）
			if captured < totalFrames {
				if row%2 == 0 && col < params.Cols-1 || row%2 == 1 && col > 0 {
					// 水平移动：通过 CharacterControllerYawDeltaAction 旋转视角
					yawRight(ctx, params.PanDuration)
				} else if row < params.Rows-1 {
					// 垂直移动：通过 CharacterControllerForwardAxisAction 后退
					moveBackward(ctx, params.PanDuration)
				}
				// 等待稳定
				time.Sleep(400 * time.Millisecond)
			}
		}
	}

	log.Info().
		Str("component", component).
		Int("captured", captured).
		Str("output_dir", params.OutputDir).
		Msg("CaptureGrid: completed")

	return true
}

// yawRight 通过 CharacterControllerYawDeltaAction 向右旋转视角
func yawRight(ctx *maa.Context, duration int) {
	// delta 单位是度，内部乘以 2 得到像素偏移
	// 正值向右旋转
	override := map[string]any{
		"CharacterControllerYawDeltaAction": map[string]any{
			"delta": 30,
		},
	}
	ctx.RunAction("CharacterControllerYawDeltaAction",
		maa.Rect{0, 0, 0, 0}, "", override)
}

// moveBackward 通过 CharacterControllerForwardAxisAction 向后移动
func moveBackward(ctx *maa.Context, duration int) {
	// axis * 100ms = 移动时长，负值向后
	override := map[string]any{
		"CharacterControllerForwardAxisAction": map[string]any{
			"axis": -3,
		},
	}
	ctx.RunAction("CharacterControllerForwardAxisAction",
		maa.Rect{0, 0, 0, 0}, "", override)
}

// saveImage 保存 image.Image 到 PNG 文件
func saveImage(img image.Image, path string) error {
	if img == nil {
		return fmt.Errorf("image is nil")
	}
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create file: %w", err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		return fmt.Errorf("encode png: %w", err)
	}
	return nil
}

// ── Stitch Action ──

// StitchAction 调用 Python CLI 拼接截图
type StitchAction struct{}

var _ maa.CustomActionRunner = &StitchAction{}

func (a *StitchAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	params := DefaultStitchParams()
	if arg != nil && arg.CustomActionParam != "" {
		if err := json.Unmarshal([]byte(arg.CustomActionParam), &params); err != nil {
			log.Error().Err(err).Str("component", component).Msg("Stitch: failed to parse params")
			return false
		}
	}

	pythonBin, err := findPython()
	if err != nil {
		log.Error().Err(err).Str("component", component).Msg("Stitch: python not found")
		return false
	}

	toolPath, err := findProtocolImagingToolPath()
	if err != nil {
		log.Error().Err(err).Str("component", component).Msg("Stitch: tool path not found")
		return false
	}

	// 构建 CLI 参数
	args := []string{"-m", "app", params.FramesDir, "--output", params.OutputPath}
	if params.UseFusion {
		args = append(args, "--use-fusion")
	}
	if params.SkipBlur {
		args = append(args, "--skip-blur", fmt.Sprintf("%.1f", params.BlurThreshold))
	}

	cmd := exec.Command(pythonBin, args...)
	cmd.Dir = toolPath
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	log.Info().
		Str("component", component).
		Str("python", pythonBin).
		Str("tool_dir", toolPath).
		Str("cmd", fmt.Sprintf("%v", args)).
		Msg("Stitch: starting")

	start := time.Now()
	if err := cmd.Run(); err != nil {
		log.Error().Err(err).Str("component", component).
			Dur("duration", time.Since(start)).
			Msg("Stitch: failed")
		return false
	}

	log.Info().
		Str("component", component).
		Dur("duration", time.Since(start)).
		Str("output", params.OutputPath).
		Msg("Stitch: completed")
	return true
}

func findPython() (string, error) {
	candidates := []string{"python3", "python"}
	if runtime.GOOS == "windows" {
		candidates = []string{"python.exe", "pythonw.exe", "py.exe"}
	}
	for _, bin := range candidates {
		if p, err := exec.LookPath(bin); err == nil {
			return p, nil
		}
	}
	return "", fmt.Errorf("python executable not found")
}

func findProtocolImagingToolPath() (string, error) {
	candidates := []string{
		filepath.Join(getCwd(), "tools", "protocolimaging"),
		filepath.Join(getCwd(), "..", "tools", "protocolimaging"),
	}
	if userDir, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates, filepath.Join(userDir, ".maa", "tools", "protocolimaging"))
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	return "", fmt.Errorf("protocol imaging tool not found in: %v", candidates)
}

func getCwd() string {
	cwd, _ := os.Getwd()
	return cwd
}
