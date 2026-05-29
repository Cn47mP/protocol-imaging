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


def remove_ui_elements(frame: np.ndarray,
                    ui_regions: list[tuple[int, int, int, int]] | None = None) -> tuple[np.ndarray, np.ndarray]:
    """
    检测并去除游戏常见UI元素
    返回: (处理后的图像, UI掩码)
    """
    h, w = frame.shape[:2]
    mask = np.ones(frame.shape[:2], dtype=np.uint8) * 255

    if ui_regions is None:
        ui_regions = [
            # 默认终末地常见UI区域 (top, bottom, left, right)
            (0, int(h*0.15), 0, int(w*0.2)),  # 左上角小地图
            (int(h*0.9), h, 0, w),  # 底部操作栏
            (0, int(h*0.1), int(w*0.85), w),  # 右上角状态
        ]

    for top, bottom, left, right in ui_regions:
        mask[top:bottom, left:right] = 0

    # 应用掩码
    result = cv2.bitwise_and(frame, frame, mask=mask)
    return result, mask


def detect_motion_blur(frame: np.ndarray) -> float:
    """
    检测运动模糊程度
    返回: 拉普拉斯方差值（值越低越模糊）
    建议阈值: <100 表示明显模糊, <50 非常模糊
    """
    gray = cv2.cvtColor(frame, cv2.COLOR_BGR2GRAY)
    laplacian_var = cv2.Laplacian(gray, cv2.CV_64F).var()
    return laplacian_var


def is_blurry(frame: np.ndarray, threshold: float = 100.0) -> tuple[bool, float]:
    """
    判断图像是否模糊
    返回: (是否模糊, 模糊值)
    """
    blur_value = detect_motion_blur(frame)
    return blur_value < threshold, blur_value


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


def detect_floor_grid(img: np.ndarray) -> list[tuple[float, float]]:
    """
    检测基地网格地板的交叉点作为强特征点
    比通用ORB更稳定
    """
    gray = cv2.cvtColor(img, cv2.COLOR_BGR2GRAY)
    # 边缘检测找网格线
    edges = cv2.Canny(gray, 50, 150, apertureSize=3)
    
    # 检测直线
    lines = cv2.HoughLinesP(edges, 1, np.pi/180, threshold=50, minLineLength=50, maxLineGap=10)
    
    grid_points = []
    if lines is None:
        return grid_points
    
    # 分离水平线和垂直线
    horizontal_lines = []
    vertical_lines = []
    
    for line in lines:
        x1, y1, x2, y2 = line[0]
        angle = np.arctan2(y2 - y1, x2 - x1) * 180 / np.pi
        if abs(angle) < 10 or abs(angle) > 170:
            horizontal_lines.append(line[0])
        elif abs(angle - 90) < 10 or abs(angle + 90) < 10:
            vertical_lines.append(line[0])
    
    # 找交点
    for h_line in horizontal_lines:
        hx1, hy1, hx2, hy2 = h_line
        for v_line in vertical_lines:
            vx1, vy1, vx2, vy2 = v_line
            # 计算两条直线的交点
            pt = _line_intersection(hx1, hy1, hx2, hy2, vx1, vy1, vx2, vy2)
            if pt is not None:
                grid_points.append(pt)
    
    return grid_points


def _line_intersection(x1, y1, x2, y2, x3, y3, x4, y4):
    """计算两条直线的交点"""
    den = (x1 - x2) * (y3 - y4) - (y1 - y2) * (x3 - x4)
    if den == 0:
        return None
    t = ((x1 - x3) * (y3 - y4) - (y1 - y3) * (x3 - x4)) / den
    u = -((x1 - x2) * (y1 - y3) - (y1 - y2) * (x1 - x3)) / den
    if 0 <= t <= 1 and 0 <= u <= 1:
        x = x1 + t * (x2 - x1)
        y = y1 + t * (y2 - y1)
        return (float(x), float(y))
    return None
