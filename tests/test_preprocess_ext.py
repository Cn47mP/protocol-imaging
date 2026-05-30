"""app.image.preprocess 扩展测试（补充 test_preprocess.py 中未覆盖的函数）"""

import cv2
import numpy as np

from app.image.preprocess import (
    denoise,
    detect_motion_blur,
    is_blurry,
    normalize_brightness,
    remove_ui_elements,
)


def _make_test_image(h=100, w=100, noise=0):
    img = np.full((h, w, 3), 128, dtype=np.uint8)
    cv2.rectangle(img, (20, 20), (80, 80), (200, 200, 200), -1)
    if noise > 0:
        img = img.astype(np.int16) + np.random.randint(-noise, noise, img.shape, dtype=np.int16)
        img = np.clip(img, 0, 255).astype(np.uint8)
    return img


def _make_blurry_image(h=100, w=100):
    img = np.full((h, w, 3), 128, dtype=np.uint8)
    cv2.rectangle(img, (20, 20), (80, 80), (200, 200, 200), -1)
    return cv2.GaussianBlur(img, (25, 25), 10)


def test_detect_motion_blur_sharp():
    img = _make_test_image()
    val = detect_motion_blur(img)
    assert val > 0


def test_detect_motion_blur_blurry():
    sharp = _make_test_image()
    blurry = _make_blurry_image()
    assert detect_motion_blur(blurry) < detect_motion_blur(sharp)


def test_is_blurry_returns_tuple():
    img = _make_test_image()
    result = is_blurry(img)
    assert isinstance(result, tuple)
    assert len(result) == 2
    assert result[0] in (True, False, np.True_, np.False_)
    assert isinstance(float(result[1]), float)


def test_is_blurry_detects_blur():
    blurry = _make_blurry_image()
    is_blur, val = is_blurry(blurry, threshold=500)
    assert is_blur == True  # noqa: E712 — numpy bool comparison


def test_is_blurry_clear_image():
    sharp = _make_test_image()
    is_blur, val = is_blurry(sharp, threshold=10)
    assert is_blur == False  # noqa: E712 — numpy bool comparison


def test_remove_ui_elements_returns_tuple():
    img = _make_test_image()
    result, mask = remove_ui_elements(img)
    assert result.shape == img.shape
    assert mask.shape == img.shape[:2]


def test_remove_ui_elements_custom_regions():
    img = _make_test_image(h=200, w=200)
    regions = [(0, 50, 0, 50)]  # 左上角 50x50
    result, mask = remove_ui_elements(img, ui_regions=regions)
    # 被 mask 的区域应为 0
    assert mask[25, 25] == 0
    # 未被 mask 的区域应为 255
    assert mask[150, 150] == 255


def test_normalize_brightness_no_crash():
    img = _make_test_image()
    result = normalize_brightness(img)
    assert result.shape == img.shape
    assert result.dtype == np.uint8


def test_denoise_no_crash():
    img = _make_test_image(noise=10)
    result = denoise(img, strength=5)
    assert result.shape == img.shape
    assert result.dtype == np.uint8
