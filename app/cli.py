#!/usr/bin/env python3
"""协议映射 · 纯图像处理 CLI — MaaEnd Go Service 调用入口

用法:
    python -m app frames_dir/ --output result.png --use-fusion

功能:
    1. 读取目录中的截图（按文件名排序）
    2. 可选模糊检测跳过
    3. 计算 homographies（ORB 特征匹配）
    4. 拼接（羽化融合）
    5. 导出 PNG
"""

from __future__ import annotations

import argparse
import logging
import sys
from pathlib import Path

import cv2
import numpy as np

from app.image.align import auto_align
from app.image.preprocess import is_blurry
from app.image.stitch import stitch_sequential
from app.export.png_export import export_png

LOG_PATH = Path("logs/protocol-imaging.log")


def _setup_logging(debug: bool = False) -> None:
    LOG_PATH.parent.mkdir(parents=True, exist_ok=True)
    logging.basicConfig(
        filename=LOG_PATH,
        level=logging.DEBUG if debug else logging.INFO,
        format="%(asctime)s %(levelname)s %(name)s: %(message)s",
        encoding="utf-8",
    )


def load_frames(frames_dir: Path) -> list[np.ndarray]:
    """从目录加载截图，按文件名排序"""
    extensions = {".png", ".jpg", ".jpeg", ".bmp"}
    files = sorted(
        f for f in frames_dir.iterdir()
        if f.suffix.lower() in extensions
    )
    frames = []
    for f in files:
        img = cv2.imread(str(f))
        if img is not None:
            frames.append(img)
            logging.info("Loaded frame: %s (%dx%d)", f.name, img.shape[1], img.shape[0])
        else:
            logging.warning("Failed to load frame: %s", f)
    return frames


def filter_blurry(frames: list[np.ndarray], threshold: float) -> list[np.ndarray]:
    """过滤模糊帧"""
    filtered = []
    for i, frame in enumerate(frames):
        blur, val = is_blurry(frame, threshold)
        if blur:
            logging.info("Frame %d skipped (blurry: %.1f < %.1f)", i, val, threshold)
            print(f"[协议映射] 跳过模糊帧 {i} (模糊值={val:.1f})", file=sys.stderr)
        else:
            filtered.append(frame)
    return filtered


def run_stitch(
    frames_dir: str,
    output_path: str = "base_panorama.png",
    use_fusion: bool = True,
    skip_blur: bool = True,
    blur_threshold: float = 100.0,
) -> bool:
    """纯图像处理流程：加载 → 过滤 → 对齐 → 拼接 → 导出"""
    frames_path = Path(frames_dir)
    if not frames_path.is_dir():
        print(f"[协议映射 · 错误] 目录不存在: {frames_dir}", file=sys.stderr)
        return False

    print(f"[协议映射] 从 {frames_dir} 加载截图...")
    frames = load_frames(frames_path)
    if not frames:
        print("[协议映射 · 错误] 没有找到有效截图", file=sys.stderr)
        return False
    print(f"[协议映射] 加载了 {len(frames)} 帧")

    if skip_blur:
        frames = filter_blurry(frames, blur_threshold)
        if not frames:
            print("[协议映射 · 错误] 过滤后没有剩余帧", file=sys.stderr)
            return False
        print(f"[协议映射] 过滤后剩余 {len(frames)} 帧")

    print("[协议映射] 开始拼接...")

    # 计算 homographies（每帧相对第一帧）
    homographies = [np.eye(3, dtype=np.float64)]
    for i in range(1, len(frames)):
        H = auto_align(frames[i], frames[0])
        if H is None:
            print(f"[协议映射 · 警告] 第 {i} 帧对齐失败，使用单位矩阵", file=sys.stderr)
            H = np.eye(3, dtype=np.float64)
        homographies.append(H)
    stitched = stitch_sequential(frames, homographies, use_blend=use_fusion)

    if stitched is None:
        print("[协议映射 · 错误] 拼接失败", file=sys.stderr)
        return False

    out_p = Path(output_path).resolve()
    export_png(stitched, out_p)
    print(f"[协议映射] 成功导出到 {out_p}")
    return True


def main(argv: list[str] | None = None) -> None:
    if argv is None:
        argv = sys.argv[1:]

    parser = argparse.ArgumentParser(
        prog="protocol-imaging",
        description="协议映射 — 纯图像处理 CLI（对齐 + 拼接 + 导出）",
    )
    parser.add_argument(
        "frames_dir",
        type=str,
        help="截图目录路径",
    )
    parser.add_argument(
        "--output", "-o",
        type=str,
        default="base_panorama.png",
        help="输出路径（默认 base_panorama.png）",
    )
    parser.add_argument(
        "--use-fusion", "-f",
        action="store_true",
        help="启用羽化融合",
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
        "--debug", "-d",
        action="store_true",
        help="启用调试输出",
    )

    args = parser.parse_args(argv)
    _setup_logging(args.debug)

    skip_blur = args.skip_blur is not None
    blur_threshold = args.skip_blur if skip_blur else 100.0

    success = run_stitch(
        frames_dir=args.frames_dir,
        output_path=args.output,
        use_fusion=args.use_fusion,
        skip_blur=skip_blur,
        blur_threshold=blur_threshold,
    )
    sys.exit(0 if success else 1)
