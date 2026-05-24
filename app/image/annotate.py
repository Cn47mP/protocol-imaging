"""
协议映射 · 标注模块
在总图上叠加图层标记
"""


import cv2
import numpy as np


def draw_grid(img: np.ndarray, grid_size: int = 100,
              color: tuple[int, int, int] = (100, 100, 100),
              thickness: int = 1) -> np.ndarray:
    """绘制网格参考线"""
    result = img.copy()
    h, w = result.shape[:2]
    for x in range(0, w, grid_size):
        cv2.line(result, (x, 0), (x, h), color, thickness)
    for y in range(0, h, grid_size):
        cv2.line(result, (0, y), (w, y), color, thickness)
    return result


def draw_label(img: np.ndarray, text: str, position: tuple[int, int],
               font_scale: float = 0.6, color: tuple[int, int, int] = (0, 255, 0),
               thickness: int = 2) -> np.ndarray:
    """在图中添加文字标注"""
    result = img.copy()
    font = cv2.FONT_HERSHEY_SIMPLEX
    # 文字背景
    (tw, th), _ = cv2.getTextSize(text, font, font_scale, thickness)
    x, y = position
    cv2.rectangle(result, (x, y - th - 4), (x + tw + 4, y + 2),
                  (0, 0, 0), -1)
    cv2.putText(result, text, (x + 2, y - 2),
                font, font_scale, color, thickness)
    return result


def draw_bounding_box(img: np.ndarray, x: int, y: int, w: int, h: int,
                      color: tuple[int, int, int] = (0, 255, 0),
                      thickness: int = 2) -> np.ndarray:
    """绘制矩形框"""
    result = img.copy()
    cv2.rectangle(result, (x, y), (x + w, y + h), color, thickness)
    return result
