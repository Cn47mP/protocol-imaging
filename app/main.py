#!/usr/bin/env python3
"""协议映射 · 入口 — 启动参数 + 全局异常边界"""

from __future__ import annotations

import argparse
import sys
import traceback
import time
from pathlib import Path

# Windows 高 DPI 感知：必须在 QApplication 创建前声明，否则截图和坐标都是逻辑像素
if sys.platform == "win32":
    import ctypes
    try:
        ctypes.windll.shcore.SetProcessDpiAwareness(2)  # Per-Monitor DPI Aware V2
    except Exception:
        pass

from PySide6.QtCore import Qt, QThread, Signal
from PySide6.QtWidgets import QApplication, QMessageBox

from app.ui.main_window import MainWindow
from app.capture.window_capture import WindowCapture
from app.capture.auto_capturer import AutoCapturer, CaptureGrid, CAPTURE_PRESETS
from app.control.game_controller import GameController
from app.image.preprocess import crop_ui
from app.image.stitch import stitch_sequential
from app.export.png_export import export_png


def _exception_hook(exc_type, exc_value, exc_tb):
    """全局未捕获异常 — 弹窗提示 + 日志记录."""
    tb_str = "".join(traceback.format_exception(exc_type, exc_value, exc_tb))
    print(f"[协议映射 · 异常] {tb_str}", file=sys.stderr)

    msg = QMessageBox()
    msg.setIcon(QMessageBox.Icon.Critical)
    msg.setWindowTitle("协议映射 · 异常")
    msg.setText(f"{exc_type.__name__}: {exc_value}")
    msg.setDetailedText(tb_str)
    msg.setStandardButtons(QMessageBox.StandardButton.Ok)
    msg.exec()


def run_auto_mode(
    preset: str = "medium",
    skip_blur: bool = True,
    blur_threshold: float = 100.0,
    use_fusion: bool = True,
    use_openstitching: bool = False,
    output_path: str | None = None
) -> bool:
    """运行 CLI 自动模式：自动采集 + 拼接 + 导出."""
    print(f"[协议映射 · 自动模式] 预设={preset}, 跳过模糊={skip_blur}, 羽化融合={use_fusion}")

    # 初始化组件
    controller = GameController()
    capture = WindowCapture()
    auto_cap = AutoCapturer(controller, capture)

    auto_cap.set_blur_filter(skip_blur, blur_threshold)
    auto_cap.set_log_callback(lambda msg: print(f"[协议映射] {msg}"))
    auto_cap.set_progress_callback(lambda done, total: print(f"[协议映射] 进度: {done}/{total}"))

    # 查找游戏窗口
    if not controller.find_window():
        print("[协议映射 · 错误] 未找到游戏窗口", file=sys.stderr)
        return False
    if not capture.auto_detect_game_window():
        print("[协议映射 · 错误] 未能自动检测游戏窗口", file=sys.stderr)
        return False

    print("[协议映射] 已找到游戏窗口，开始激活...")
    controller.focus_window()
    time.sleep(1.0)

    # 获取配置
    grid = CAPTURE_PRESETS.get(preset, CAPTURE_PRESETS["medium"])

    # 执行采集
    frames = auto_cap.capture_grid(grid)
    if not frames:
        print("[协议映射 · 错误] 没有采集到任何有效帧", file=sys.stderr)
        return False

    print(f"[协议映射] 采集完成，共 {len(frames)} 帧，开始拼接...")

    # 拼接图像
    img_list = [f.image for f in frames]
    stitched, stitched_annotated, _ = stitch_sequential(
        img_list,
        auto_align=True,
        use_fusion=use_fusion,
        use_openstitching=use_openstitching
    )

    if stitched is None:
        print("[协议映射 · 错误] 拼接失败", file=sys.stderr)
        return False

    # 导出
    out_path = output_path or "base_panorama.png"
    out_p = Path(out_path).resolve()

    export_png(stitched, out_p)
    print(f"[协议映射] 成功导出到 {out_p}")
    return True


def main(argv: list[str] | None = None) -> None:
    if argv is None:
        argv = sys.argv[1:]

    parser = argparse.ArgumentParser(
        prog="protocol-imaging",
        description="协议映射 — 终末地基地全景快照成像工具",
    )
    parser.add_argument(
        "--project", "-p",
        type=Path,
        metavar="PATH",
        help="启动后直接加载指定项目 (.json 或项目目录)",
    )
    parser.add_argument(
        "--debug", "-d",
        action="store_true",
        help="启用调试模式（全局异常弹窗）",
    )
    # CLI 自动模式参数
    parser.add_argument(
        "--mode", "-m",
        type=str,
        choices=["gui", "auto"],
        default="gui",
        help="运行模式：gui 图形界面（默认）、auto 自动模式",
    )
    parser.add_argument(
        "--preset",
        type=str,
        choices=["small", "medium", "large", "xlarge"],
        default="medium",
        help="自动采集预设（仅 auto 模式）",
    )
    parser.add_argument(
        "--skip-blur",
        type=float,
        nargs="?",
        const=100.0,
        metavar="THRESHOLD",
        help="跳过模糊帧（可选阈值，默认100.0）",
    )
    parser.add_argument(
        "--use-fusion",
        action="store_true",
        help="启用羽化融合（仅 auto 模式）",
    )
    parser.add_argument(
        "--use-openstitching",
        action="store_true",
        help="使用 OpenStitching（仅 auto 模式）",
    )
    parser.add_argument(
        "--output", "-o",
        type=str,
        metavar="PATH",
        help="输出路径（仅 auto 模式）",
    )

    args = parser.parse_args(argv)

    # 自动模式
    if args.mode == "auto":
        skip_blur = args.skip_blur is not None
        blur_threshold = args.skip_blur if skip_blur else 100.0
        success = run_auto_mode(
            preset=args.preset,
            skip_blur=skip_blur,
            blur_threshold=blur_threshold,
            use_fusion=args.use_fusion,
            use_openstitching=args.use_openstitching,
            output_path=args.output
        )
        sys.exit(0 if success else 1)

    # GUI 模式
    app = QApplication(sys.argv)
    app.setApplicationName("协议映射")
    app.setOrganizationName("终末地工业")

    # 全局异常边界
    if args.debug:
        sys.excepthook = _exception_hook

    window = MainWindow()
    window.show()

    # 自动加载项目
    if args.project:
        path = args.project.resolve()
        if path.exists():
            window._load_project_quiet(path)
        else:
            QMessageBox.warning(
                window, "项目不存在",
                f"指定的项目路径不存在：\n{path}",
            )

    sys.exit(app.exec())


if __name__ == "__main__":
    main()
