"""app.image.annotate 单元测试"""

import numpy as np

from app.image.annotate import draw_bounding_box, draw_grid, draw_label


def _blank(w=200, h=150):
    return np.zeros((h, w, 3), dtype=np.uint8)


def test_draw_grid_lines_present():
    img = _blank()
    result = draw_grid(img, grid_size=50)
    # 网格线应修改像素值
    assert not np.array_equal(result, img)


def test_draw_grid_output_shape():
    img = _blank()
    result = draw_grid(img)
    assert result.shape == img.shape


def test_draw_grid_does_not_mutate_input():
    img = _blank()
    original = img.copy()
    draw_grid(img, grid_size=30)
    np.testing.assert_array_equal(img, original)


def test_draw_label_output_shape():
    img = _blank()
    result = draw_label(img, "测试标签", (10, 50))
    assert result.shape == img.shape


def test_draw_label_modifies_image():
    img = _blank()
    result = draw_label(img, "Hello", (10, 30))
    assert not np.array_equal(result, img)


def test_draw_bounding_box_output_shape():
    img = _blank()
    result = draw_bounding_box(img, 20, 20, 80, 60)
    assert result.shape == img.shape


def test_draw_bounding_box_modifies_image():
    img = _blank()
    result = draw_bounding_box(img, 10, 10, 50, 50)
    assert not np.array_equal(result, img)
