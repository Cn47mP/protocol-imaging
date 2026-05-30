"""集成测试：用合成图片测试完整的 align → stitch → export 流程

不依赖游戏窗口，纯 Python + OpenCV 验证管线可用性。
"""

import cv2
import numpy as np
from pathlib import Path

from app.image.align import auto_align
from app.image.stitch import stitch_sequential
from app.export.png_export import export_png


def _make_synthetic_grid(rows=2, cols=3, cell_size=200, overlap=40):
    """
    生成一组模拟蛇形网格采集的合成截图。
    每张图是大场景的一个平移裁剪，相邻帧有 overlap 像素的重叠。
    """
    # 生成一张大场景
    scene_h = rows * (cell_size - overlap) + overlap
    scene_w = cols * (cell_size - overlap) + overlap
    scene = np.zeros((scene_h, scene_w, 3), dtype=np.uint8)

    # 画网格和特征点让 ORB 能匹配
    for y in range(0, scene_h, 40):
        cv2.line(scene, (0, y), (scene_w, y), (80, 80, 80), 1)
    for x in range(0, scene_w, 40):
        cv2.line(scene, (x, 0), (x, scene_h), (80, 80, 80), 1)
    for cy in range(50, scene_h, 120):
        for cx in range(50, scene_w, 120):
            cv2.circle(scene, (cx, cy), 15, (0, 200, 0), -1)
            cv2.rectangle(scene, (cx - 8, cy - 8), (cx + 8, cy + 8), (0, 0, 255), 2)

    # 蛇形裁剪
    frames = []
    step_y = cell_size - overlap
    step_x = cell_size - overlap
    for r in range(rows):
        cols_range = range(cols) if r % 2 == 0 else range(cols - 1, -1, -1)
        for c in cols_range:
            y0 = r * step_y
            x0 = c * step_x
            crop = scene[y0:y0 + cell_size, x0:x0 + cell_size]
            frames.append(crop.copy())

    return frames


def test_full_pipeline_align_stitch_export(tmp_path):
    """端到端：合成截图 → 对齐 → 拼接 → 导出 PNG"""
    frames = _make_synthetic_grid(rows=2, cols=3, cell_size=200, overlap=40)
    assert len(frames) == 6

    # 计算 homographies（每帧相对第一帧）
    homographies = [np.eye(3, dtype=np.float64)]
    for i in range(1, len(frames)):
        H = auto_align(frames[i], frames[0])
        assert H is not None, f"第 {i} 帧对齐失败"
        homographies.append(H)

    # 拼接
    result = stitch_sequential(frames, homographies, use_blend=True)
    assert result is not None
    assert result.shape[0] > 0
    assert result.shape[1] > 0

    # 导出
    out_path = tmp_path / "panorama.png"
    export_png(result, out_path)
    assert out_path.exists()

    # 验证导出的图可以读取
    loaded = cv2.imread(str(out_path))
    assert loaded is not None
    assert loaded.shape == result.shape


def test_pipeline_with_alignment_failures(tmp_path):
    """部分帧对齐失败时，管线应优雅处理（用单位矩阵兜底）"""
    frames = _make_synthetic_grid(rows=1, cols=2, cell_size=200, overlap=40)

    # 第一帧正常，第二帧故意用一张完全不同的图
    frames[1] = np.ones((200, 200, 3), dtype=np.uint8) * 255

    homographies = [np.eye(3, dtype=np.float64)]
    H = auto_align(frames[1], frames[0])
    # 对齐可能失败
    if H is None:
        H = np.eye(3, dtype=np.float64)
    homographies.append(H)

    # 拼接不应崩溃
    result = stitch_sequential(frames, homographies, use_blend=True)
    assert result is not None


def test_pipeline_single_frame(tmp_path):
    """单帧不应崩溃"""
    frames = _make_synthetic_grid(rows=1, cols=1, cell_size=200, overlap=0)
    assert len(frames) == 1

    H = np.eye(3, dtype=np.float64)
    result = stitch_sequential(frames, [H], use_blend=False)
    assert result is not None

    out_path = tmp_path / "single.png"
    export_png(result, out_path)
    assert out_path.exists()
