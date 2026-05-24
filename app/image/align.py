"""
协议映射 · 图像对齐模块
支持自动特征匹配对齐和手动锚点校正
"""


import cv2
import numpy as np


def detect_features(img: np.ndarray, max_features: int = 2000) -> tuple:
    """检测 ORB 特征点与描述子"""
    gray = cv2.cvtColor(img, cv2.COLOR_BGR2GRAY) if len(img.shape) == 3 else img
    orb = cv2.ORB_create(nfeatures=max_features)
    keypoints, descriptors = orb.detectAndCompute(gray, None)
    return keypoints, descriptors


def match_features(desc1, desc2, ratio_thresh: float = 0.75) -> list:
    """特征点匹配"""
    if desc1 is None or desc2 is None:
        return []
    bf = cv2.BFMatcher(cv2.NORM_HAMMING, crossCheck=False)
    matches = bf.knnMatch(desc1, desc2, k=2)
    # Lowe's ratio test
    good = []
    for m, n in matches:
        if m.distance < ratio_thresh * n.distance:
            good.append(m)
    return good


def estimate_homography(kp1, kp2, matches, reproj_thresh: float = 5.0) -> np.ndarray | None:
    """估算单应性矩阵"""
    if len(matches) < 4:
        return None
    src_pts = np.float32([kp1[m.queryIdx].pt for m in matches]).reshape(-1, 1, 2)
    dst_pts = np.float32([kp2[m.trainIdx].pt for m in matches]).reshape(-1, 1, 2)
    H, mask = cv2.findHomography(src_pts, dst_pts, cv2.RANSAC, reproj_thresh)
    return H


def auto_align(img_src: np.ndarray, img_dst: np.ndarray,
               min_matches: int = 10) -> np.ndarray | None:
    """自动对齐：返回 src 到 dst 的单应性矩阵"""
    kp1, desc1 = detect_features(img_src)
    kp2, desc2 = detect_features(img_dst)

    if desc1 is None or desc2 is None:
        return None

    matches = match_features(desc1, desc2)
    if len(matches) < min_matches:
        return None

    H = estimate_homography(kp1, kp2, matches)
    return H


def manual_align(points_src: list[tuple[float, float]],
                 points_dst: list[tuple[float, float]]) -> np.ndarray | None:
    """手动锚点校正：根据 4+ 对应点计算透视变换"""
    if len(points_src) < 4 or len(points_dst) < 4:
        return None
    src = np.float32(points_src).reshape(-1, 1, 2)
    dst = np.float32(points_dst).reshape(-1, 1, 2)
    H, _ = cv2.findHomography(src, dst)
    return H
