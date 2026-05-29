package protocolimaging

import (
    "context"
    "fmt"
    "os"
    "os/exec"
    "path/filepath"
    "runtime"
    "strings"
    "time"

    maa "github.com/MaaXYZ/maa-framework-go/v4"
    "github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/pienv"
    "github.com/bytedance/sonic"
    "github.com/rs/zerolog/log"
)

const (
    protocolImagingCaptureActionName = "ProtocolImagingCapture"
    protocolImagingStitchActionName  = "ProtocolImagingStitch"
)

// ProtocolImagingCaptureAction 执行自动采集动作
type ProtocolImagingCaptureAction struct{}

func (a *ProtocolImagingCaptureAction) Run(ctx context.Context, param []byte) (bool, []byte) {
    log.Info().
        Str("action", protocolImagingCaptureActionName).
        Str("param", string(param)).
        Msg("Executing protocol imaging capture")

    var params ActionParams
    if err := sonic.Unmarshal(param, &params); err != nil {
        params = ActionParams{
            Capture: DefaultCaptureParams(),
        }
    }

    // 查找 Python 可执行文件
    pythonBin, err := findPython()
    if err != nil {
        log.Error().Err(err).Msg("Failed to find Python executable")
        return false, []byte(err.Error())
    }

    // 查找工具目录
    toolPath, err := findProtocolImagingToolPath()
    if err != nil {
        log.Error().Err(err).Msg("Failed to find protocol imaging tool path")
        return false, []byte(err.Error())
    }

    // 准备运行命令
    args := []string{"-m", "app.main", "--mode", "auto", "--preset", string(params.Capture.Preset)}
    if params.Capture.SkipBlur {
        args = append(args, "--skip-blur", fmt.Sprintf("%.2f", params.Capture.BlurThreshold))
    }
    if params.Capture.UseFusion {
        args = append(args, "--use-fusion")
    }
    if params.Capture.UseOpenStitching {
        args = append(args, "--use-openstitching")
    }
    if params.OutputPath != "" {
        args = append(args, "--output", params.OutputPath)
    }

    cmd := exec.CommandContext(ctx, pythonBin, args...)
    cmd.Dir = toolPath
    cmd.Stdout = os.Stdout
    cmd.Stderr = os.Stderr

    log.Info().
        Str("python", pythonBin).
        Str("tool_dir", toolPath).
        Str("cmd", strings.Join(args, " ")).
        Msg("Starting protocol imaging tool")

    start := time.Now()
    if err := cmd.Start(); err != nil {
        log.Error().Err(err).Msg("Failed to start protocol imaging tool")
        return false, []byte(err.Error())
    }

    if err := cmd.Wait(); err != nil {
        log.Error().Err(err).Dur("duration", time.Since(start)).Msg("Protocol imaging tool failed")
        return false, []byte(err.Error())
    }

    log.Info().
        Dur("duration", time.Since(start)).
        Msg("Protocol imaging capture completed successfully")
    return true, []byte("{}")
}

// ProtocolImagingStitchAction 执行拼接动作（如果单独需要）
type ProtocolImagingStitchAction struct{}

func (a *ProtocolImagingStitchAction) Run(ctx context.Context, param []byte) (bool, []byte) {
    log.Info().
        Str("action", protocolImagingStitchActionName).
        Str("param", string(param)).
        Msg("Executing protocol imaging stitch")

    // 拼接通常在采集后自动完成，所以这里主要是一个占位符
    return true, []byte(`{"status": "ok"}`)
}

func findPython() (string, error) {
    candidates := []string{"python3", "python"}
    if runtime.GOOS == "windows" {
        candidates = []string{"pythonw.exe", "python.exe", "py.exe"}
    }
    for _, bin := range candidates {
        if p, err := exec.LookPath(bin); err == nil {
            return p, nil
        }
    }
    return "", fmt.Errorf("could not find Python executable")
}

func findProtocolImagingToolPath() (string, error) {
    // 可能的位置
    candidates := []string{
        filepath.Join(getCwd(), "tools", "protocolimaging"),
        filepath.Join(getCwd(), "..", "tools", "protocolimaging"),
    }
    if userDir, err := os.UserHomeDir(); err == nil {
        candidates = append(candidates, filepath.Join(userDir, ".maa", "tools", "protocolimaging"))
    }
    // 也可以尝试从 pienv 或者环境变量找
    if piPath := pienv.ProtocolImagingPath(); piPath != "" {
        candidates = append(candidates, piPath)
    }
    for _, p := range candidates {
        if _, err := os.Stat(p); err == nil {
            return p, nil
        }
    }
    return "", fmt.Errorf("could not find protocol imaging tool")
}

func getCwd() string {
    cwd, _ := os.Getwd()
    return cwd
}
