import numpy as np

from app.image.preprocess import crop_ui, resize_if_large, to_grayscale


def test_crop_ui_removes_edges():
    frame = np.zeros((10, 12, 3), dtype=np.uint8)

    cropped = crop_ui(frame, crop_top=1, crop_bottom=2, crop_left=3, crop_right=4)

    assert cropped.shape == (7, 5, 3)


def test_to_grayscale_converts_color_image():
    frame = np.zeros((4, 5, 3), dtype=np.uint8)

    gray = to_grayscale(frame)

    assert gray.shape == (4, 5)


def test_resize_if_large_keeps_small_image():
    frame = np.zeros((20, 30, 3), dtype=np.uint8)

    resized = resize_if_large(frame, max_pixels=100)

    assert resized is frame


def test_resize_if_large_scales_down_large_image():
    frame = np.zeros((20, 40, 3), dtype=np.uint8)

    resized = resize_if_large(frame, max_pixels=10)

    assert resized.shape[:2] == (5, 10)
