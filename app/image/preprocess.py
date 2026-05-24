"""
协议映射 · 图像预处理模块
负责裁剪、去噪、亮度归一化等
"""

import cv2
import numpy as np


def crop_ui(frame: np.ndarray, crop_top: int = 0, crop_bottom: int = 0,
            crop_left: int = 0, crop_right: int = 0) -> np.ndarray:
    """裁剪固定 UI 区域"""
    h, w = frame.shape[:2]
    return frame[crop_top:h - crop_bottom, crop_left:w - crop_right]


def to_grayscale(frame: np.ndarray) -> np.ndarray:
    """转为灰度图"""
    if len(frame.shape) == 3:
        return cv2.cvtColor(frame, cv2.COLOR_BGR2GRAY)
    return frame


def normalize_brightness(frame: np.ndarray) -> np.ndarray:
    """亮度归一化"""
    lab = cv2.cvtColor(frame, cv2.COLOR_BGR2LAB)
    lightness, a, b = cv2.split(lab)
    lightness = cv2.equalizeHist(lightness)
    return cv2.cvtColor(cv2.merge([lightness, a, b]), cv2.COLOR_LAB2BGR)


def denoise(frame: np.ndarray, strength: int = 10) -> np.ndarray:
    """轻度去噪"""
    return cv2.fastNlMeansDenoisingColored(frame, None, strength, strength, 7, 21)


def resize_if_large(frame: np.ndarray, max_pixels: int = 4000) -> np.ndarray:
    """如果图像尺寸过大则缩放（提升拼接速度）"""
    h, w = frame.shape[:2]
    if max(h, w) > max_pixels:
        scale = max_pixels / max(h, w)
        new_w = int(w * scale)
        new_h = int(h * scale)
        return cv2.resize(frame, (new_w, new_h), interpolation=cv2.INTER_AREA)
    return frame
