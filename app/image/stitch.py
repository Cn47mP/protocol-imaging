"""
协议映射 · 图像拼接模块
将多张对齐后的图像合并成一张大图
"""


import cv2
import numpy as np


def blend_images(base: np.ndarray, new_img: np.ndarray, mask: np.ndarray,
                blur_kernel_size: int = 15) -> np.ndarray:
    """
    羽化融合重叠区域
    参数:
        base: 基础图像
        new_img: 新图像
        mask: 新图像的有效区域掩码
        blur_kernel_size: 模糊核大小
    """
    # 确保 mask 是单通道
    if len(mask.shape) == 3:
        mask = mask[:, :, 0]
    
    # 高斯模糊掩码
    mask_blurred = cv2.GaussianBlur(mask.astype(np.float32), 
                                    (blur_kernel_size, blur_kernel_size), 0)
    mask_blurred = mask_blurred / 255.0
    
    # 扩展为 3 通道
    if len(base.shape) == 3:
        mask_3ch = np.stack([mask_blurred] * 3, axis=2)
    else:
        mask_3ch = mask_blurred
    
    # 羽化融合
    blended = base.astype(np.float32) * (1 - mask_3ch) + new_img.astype(np.float32) * mask_3ch
    return blended.astype(np.uint8)


def warp_and_merge(img: np.ndarray, H: np.ndarray,
                   canvas_size: tuple[int, int]) -> np.ndarray:
    """根据单应性矩阵将图像变换到画布上"""
    warped = cv2.warpPerspective(img, H, canvas_size)
    return warped


def stitch_sequential(images: list[np.ndarray],
                      homographies: list[np.ndarray],
                      use_blend: bool = True) -> np.ndarray:
    """
    逐帧拼接：将图像按顺序拼接到第一张图的坐标系
    参数:
        images: 图像列表
        homographies: 单应性矩阵列表
        use_blend: 是否使用羽化融合
    """
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
            if use_blend:
                # 羽化融合
                mask = np.any(warped > 0, axis=2).astype(np.uint8) * 255
                result = blend_images(result, warped, mask)
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
