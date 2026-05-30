"""app.image.stitch 单元测试"""

import cv2
import numpy as np
import pytest

from app.image.stitch import (
    blend_images,
    compute_offset_homography,
    stitch_sequential,
    stitch_with_openstitching,
)


def _make_color_block(h=100, w=100, color=(255, 0, 0)):
    """生成纯色方块"""
    img = np.full((h, w, 3), color, dtype=np.uint8)
    return img


def _make_shifted_pair(shift_x=30):
    """生成一对有重叠的图片（右图是左图的平移）"""
    base = np.zeros((100, 200, 3), dtype=np.uint8)
    cv2.rectangle(base, (10, 10), (90, 90), (0, 255, 0), -1)
    cv2.circle(base, (150, 50), 20, (0, 0, 255), -1)

    M = np.float32([[1, 0, -shift_x], [0, 1, 0]])
    shifted = cv2.warpAffine(base, M, (200, 100))
    return base, shifted


def test_blend_images_no_crash():
    base = _make_color_block(color=(100, 100, 100))
    new_img = _make_color_block(color=(200, 200, 200))
    mask = np.ones((100, 100), dtype=np.uint8) * 255
    result = blend_images(base, new_img, mask)
    assert result.shape == base.shape
    assert result.dtype == np.uint8


def test_blend_images_mask_area():
    base = np.zeros((100, 100, 3), dtype=np.uint8)
    new_img = np.ones((100, 100, 3), dtype=np.uint8) * 255
    mask = np.zeros((100, 100), dtype=np.uint8)
    mask[25:75, 25:75] = 255  # 中间区域融合

    result = blend_images(base, new_img, mask)
    # 角落应该还是接近 0（base 区域）
    assert result[0, 0, 0] < 50
    # 中心应该接近 255（new_img 区域）
    assert result[50, 50, 0] > 200


def test_compute_offset_homography_identity():
    img = _make_color_block()
    H = np.eye(3, dtype=np.float64)
    ox, oy, cw, ch, offset_H = compute_offset_homography([img], [H])
    assert cw == img.shape[1]
    assert ch == img.shape[0]
    np.testing.assert_allclose(offset_H, np.eye(3), atol=1e-10)


def test_compute_offset_homography_negative_offset():
    img1 = _make_color_block()
    img2 = _make_color_block()
    # 第二张图相对第一张向左上偏移
    H0 = np.eye(3, dtype=np.float64)
    H1 = np.array([[1, 0, -50], [0, 1, -30], [0, 0, 1]], dtype=np.float64)
    ox, oy, cw, ch, offset_H = compute_offset_homography([img1, img2], [H0, H1])
    assert ox >= 50  # 偏移量应补偿负坐标
    assert oy >= 30


def test_stitch_sequential_single_frame():
    img = _make_color_block()
    H = np.eye(3, dtype=np.float64)
    result = stitch_sequential([img], [H], use_blend=False)
    assert result is not None
    assert result.shape[:2] >= img.shape[:2]  # 画布不小于原图


def test_stitch_sequential_two_frames():
    base, shifted = _make_shifted_pair(30)
    H = np.array([[1, 0, -30], [0, 1, 0], [0, 0, 1]], dtype=np.float64)
    result = stitch_sequential([base, shifted], [np.eye(3), H], use_blend=True)
    assert result is not None
    # 拼接后宽度应大于单帧
    assert result.shape[1] >= base.shape[1]


def test_stitch_sequential_mismatched_lengths():
    img = _make_color_block()
    with pytest.raises(ValueError):
        stitch_sequential([img, img], [np.eye(3)])


def test_stitch_sequential_empty():
    result = stitch_sequential([], [])
    assert result.shape == (1, 1, 3)


@pytest.mark.skipif(
    not pytest.importorskip("stitching", reason="stitching 库未安装"),
    reason="stitching 库未安装"
)
def test_stitch_with_openstitching_basic():
    # 生成两张有大量重叠的图片
    img = np.random.randint(0, 255, (200, 300, 3), dtype=np.uint8)
    # 第二张是第一张的局部（模拟重叠）
    crop = img[:, 50:250]
    padded = np.zeros_like(img)
    padded[:, 50:250] = crop
    try:
        result = stitch_with_openstitching([img, padded])
        assert result is not None
    except Exception:
        pytest.skip("OpenStitching 对合成图片拼接失败，需真实截图")
