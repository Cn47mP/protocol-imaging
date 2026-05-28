"""
协议映射 · 图像拼接模块
将多张对齐后的图像合并成一张大图
"""


import cv2
import numpy as np


def warp_and_merge(img: np.ndarray, H: np.ndarray,
                   canvas_size: tuple[int, int]) -> np.ndarray:
    """根据单应性矩阵将图像变换到画布上"""
    warped = cv2.warpPerspective(img, H, canvas_size)
    return warped


def compute_canvas_size(images: list[np.ndarray],
                        homographies: list[np.ndarray]) -> tuple[int, int]:
    """计算拼接画布的尺寸"""
    if not images or not homographies:
        return (0, 0)

    # 初始画布大小 = 第一张图
    h, w = images[0].shape[:2]
    corners = np.float32([[0, 0], [w, 0], [w, h], [0, h]]).reshape(-1, 1, 2)

    all_corners = [corners]
    for H in homographies[1:]:
        transformed = cv2.perspectiveTransform(corners, H)
        all_corners.append(transformed)

    all_corners = np.vstack(all_corners)
    x_min, y_min = all_corners.min(axis=0).ravel()
    x_max, y_max = all_corners.max(axis=0).ravel()

    # 偏移量
    canvas_w = int(np.ceil(x_max - x_min))
    canvas_h = int(np.ceil(y_max - y_min))
    return (canvas_w, canvas_h)


def stitch_sequential(images: list[np.ndarray],
                      homographies: list[np.ndarray]) -> np.ndarray:
    """逐帧拼接：将图像按顺序拼接到第一张图的坐标系"""
    if len(images) != len(homographies):
        raise ValueError("图像数量和单应性矩阵数量不匹配")
    if len(images) == 0:
        return np.zeros((1, 1, 3), dtype=np.uint8)

    # 偏移矩阵：将坐标平移到非负区域（同时得到画布尺寸）
    _, _, canvas_w, canvas_h, offset_H = compute_offset_homography(images, homographies)

    # 逐帧拼接
    result = None
    for i, (img, H) in enumerate(zip(images, homographies)):
        # 先变换到全景坐标系，再平移到正区域
        full_H = offset_H @ H if i > 0 else offset_H

        warped = cv2.warpPerspective(img, full_H, (canvas_w, canvas_h))

        if result is None:
            result = warped
        else:
            # 简单叠加（重叠区域后面那张覆盖前面）
            mask = np.all(warped > 0, axis=2)
            result[mask] = warped[mask]

    return result


def compute_offset_homography(
    images: list[np.ndarray],
    homographies: list[np.ndarray]
) -> tuple:
    """计算偏移矩阵使所有图像落在正坐标区域"""
    if not images or not homographies:
        return (0, 0, 0, 0, None)

    h, w = images[0].shape[:2]
    corners = np.float32([[0, 0], [w, 0], [w, h], [0, h]]).reshape(-1, 1, 2)

    all_corners = [corners]
    for H in homographies[1:]:
        transformed = cv2.perspectiveTransform(corners, H)
        all_corners.append(transformed)

    all_corners = np.vstack(all_corners)
    x_min, y_min = all_corners.min(axis=0).ravel()
    x_max, y_max = all_corners.max(axis=0).ravel()

    offset_x = int(np.floor(-x_min))
    offset_y = int(np.floor(-y_min))
    canvas_w = int(np.ceil(x_max - x_min))
    canvas_h = int(np.ceil(y_max - y_min))

    offset_H = np.array([
        [1, 0, offset_x],
        [0, 1, offset_y],
        [0, 0, 1]
    ], dtype=np.float64)

    return offset_x, offset_y, canvas_w, canvas_h, offset_H
