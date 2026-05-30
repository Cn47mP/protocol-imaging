#!/usr/bin/env python3
"""协议映射 · CLI 入口 — MaaEnd 集成的全景快照成像工具"""

from __future__ import annotations

import argparse
import logging
import sys
import time
from pathlib import Path

import numpy as np

from app.capture.window_capture import WindowCapture
from app.capture.auto_capturer import AutoCapturer, CaptureGrid, CAPTURE_PRESETS
from app.control.game_controller import GameController
from app.image.align import auto_align
from app.image.stitch import stitch_sequential, stitch_with_openstitching
from app.export.png_export import export_png

LOG_PATH = Path("logs/protocol-imaging.log")


def _setup_logging() -> None:
    LOG_PATH.parent.mkdir(parents=True, exist_ok=True)
    logging.basicConfig(
        filename=LOG_PATH,
        level=logging.INFO,
        format="%(asctime)s %(levelname)s %(name)s: %(message)s",
        encoding="utf-8",
    )


def run_auto_mode(
    preset: str = "medium",
    skip_blur: bool = True,
    blur_threshold: float = 100.0,
    use_fusion: bool = True,
    use_openstitching: bool = False,
    output_path: str | None = None,
) -> bool:
    """CLI 自动模式：自动采集 + 拼接 + 导出."""
    print(f"[协议映射] 预设={preset} 跳过模糊={skip_blur} 羽化融合={use_fusion}")

    controller = GameController()
    capture = WindowCapture()
    auto_cap = AutoCapturer(controller, capture)

    auto_cap.set_blur_filter(skip_blur, blur_threshold)
    auto_cap.set_log_callback(lambda msg: print(f"[协议映射] {msg}"))
    auto_cap.set_progress_callback(lambda done, total: print(f"[协议映射] 进度: {done}/{total}"))

    if not controller.find_window():
        print("[协议映射 · 错误] 未找到游戏窗口", file=sys.stderr)
        return False
    if not capture.auto_detect_game_window():
        print("[协议映射 · 错误] 未能自动检测游戏窗口", file=sys.stderr)
        return False

    print("[协议映射] 已找到游戏窗口，激活...")
    controller.focus_window()
    time.sleep(1.0)

    grid = CAPTURE_PRESETS.get(preset, CAPTURE_PRESETS["medium"])
    frames = auto_cap.capture_grid(grid)
    if not frames:
        print("[协议映射 · 错误] 没有采集到任何有效帧", file=sys.stderr)
        return False

    print(f"[协议映射] 采集完成，共 {len(frames)} 帧，拼接中...")

    img_list = [f.image for f in frames]

    if use_openstitching:
        stitched = stitch_with_openstitching(img_list)
    else:
        # 计算相邻帧的累积单应性矩阵
        homographies = [np.eye(3, dtype=np.float64)]  # 第一帧为单位矩阵
        for i in range(1, len(img_list)):
            H = auto_align(img_list[i], img_list[0])
            if H is None:
                print(f"[协议映射 · 警告] 第 {i} 帧对齐失败，使用平移近似", file=sys.stderr)
                H = np.eye(3, dtype=np.float64)
            homographies.append(H)
        stitched = stitch_sequential(img_list, homographies, use_blend=use_fusion)

    if stitched is None:
        print("[协议映射 · 错误] 拼接失败", file=sys.stderr)
        return False

    out_path = output_path or "base_panorama.png"
    out_p = Path(out_path).resolve()
    export_png(stitched, out_p)
    print(f"[协议映射] 成功导出到 {out_p}")
    return True


def main(argv: list[str] | None = None) -> None:
    _setup_logging()
    logging.info("Starting protocol-imaging")

    if argv is None:
        argv = sys.argv[1:]

    parser = argparse.ArgumentParser(
        prog="protocol-imaging",
        description="协议映射 — 终末地基地全景快照成像工具 (MaaEnd 集成)",
    )
    parser.add_argument(
        "--preset", "-p",
        type=str,
        choices=["small", "medium", "large", "xlarge"],
        default="medium",
        help="自动采集预设（默认 medium）",
    )
    parser.add_argument(
        "--skip-blur",
        type=float,
        nargs="?",
        const=100.0,
        metavar="THRESHOLD",
        help="跳过模糊帧（可选阈值，默认 100）",
    )
    parser.add_argument(
        "--use-fusion", "-f",
        action="store_true",
        help="启用羽化融合",
    )
    parser.add_argument(
        "--use-openstitching",
        action="store_true",
        help="使用 OpenStitching 备选拼接",
    )
    parser.add_argument(
        "--output", "-o",
        type=str,
        metavar="PATH",
        help="输出路径（默认 base_panorama.png）",
    )
    parser.add_argument(
        "--debug", "-d",
        action="store_true",
        help="启用调试输出",
    )

    args = parser.parse_args(argv)

    if args.debug:
        logging.getLogger().setLevel(logging.DEBUG)

    skip_blur = args.skip_blur is not None
    blur_threshold = args.skip_blur if skip_blur else 100.0
    success = run_auto_mode(
        preset=args.preset,
        skip_blur=skip_blur,
        blur_threshold=blur_threshold,
        use_fusion=args.use_fusion,
        use_openstitching=args.use_openstitching,
        output_path=args.output,
    )
    sys.exit(0 if success else 1)


if __name__ == "__main__":
    main()