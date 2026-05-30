"""app.image.align 单元测试"""

import cv2
import numpy as np

from app.image.align import auto_align, detect_features, estimate_homography, manual_align, match_features


def _make_grid_image(size=(200, 200), cell=20):
    """生成一张棋盘格图片作为特征丰富的测试素材"""
    img = np.zeros((*size, 3), dtype=np.uint8)
    for y in range(0, size[0], cell):
        for x in range(0, size[1], cell):
            if (x // cell + y // cell) % 2 == 0:
                img[y:y + cell, x:x + cell] = (200, 200, 200)
    # 加一些圆形特征
    for pos in [(50, 50), (150, 50), (50, 150), (150, 150)]:
        cv2.circle(img, pos, 10, (0, 0, 255), -1)
    return img


def test_detect_features_returns_keypoints():
    img = _make_grid_image()
    kps, descs = detect_features(img)
    assert len(kps) > 0
    assert descs is not None
    assert descs.shape[0] == len(kps)


def test_auto_align_identical_images():
    img = _make_grid_image()
    H = auto_align(img, img)
    assert H is not None
    # 对同一张图对齐，H 应接近单位矩阵
    np.testing.assert_allclose(H, np.eye(3), atol=1.0)


def test_auto_align_shifted_image():
    img = _make_grid_image()
    # 向右平移 20 像素
    M = np.float32([[1, 0, 20], [0, 1, 0]])
    shifted = cv2.warpAffine(img, M, (img.shape[1], img.shape[0]))

    H = auto_align(shifted, img)
    assert H is not None
    # H 应该近似一个平移矩阵
    assert abs(H[0, 2] - (-20)) < 10  # x 方向偏移约 -20
    assert abs(H[1, 2]) < 10  # y 方向偏移约 0


def test_auto_align_no_match():
    # 一张白图 vs 一张黑图，不应匹配
    white = np.ones((200, 200, 3), dtype=np.uint8) * 255
    black = np.zeros((200, 200, 3), dtype=np.uint8)
    H = auto_align(white, black)
    assert H is None


def test_manual_align_four_points():
    src = [(0, 0), (100, 0), (100, 100), (0, 100)]
    dst = [(10, 10), (110, 10), (110, 110), (10, 110)]
    H = manual_align(src, dst)
    assert H is not None
    assert H.shape == (3, 3)


def test_manual_align_insufficient_points():
    src = [(0, 0), (100, 0)]
    dst = [(10, 10), (110, 10)]
    H = manual_align(src, dst)
    assert H is None
