"""
协议映射 · PNG 导出模块
"""

import cv2
import numpy as np
from pathlib import Path
from typing import Optional


def export_png(image: np.ndarray, output_path: str, quality: int = 95) -> str:
    """导出 PNG 图片"""
    path = Path(output_path)
    path.parent.mkdir(parents=True, exist_ok=True)
    cv2.imwrite(str(path), image, [cv2.IMWRITE_PNG_COMPRESSION, 100 - quality])
    return str(path)


def export_with_annotations(image: np.ndarray, output_path: str,
                            grid: bool = False, grid_size: int = 100,
                            labels: list = None) -> str:
    """导出含标注的 PNG"""
    from app.image.annotate import draw_grid, draw_label

    result = image.copy()

    if grid:
        result = draw_grid(result, grid_size)

    if labels:
        for text, pos in labels:
            result = draw_label(result, text, pos)

    return export_png(result, output_path)
