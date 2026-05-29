"""
协议映射 · 自动采集器
网格化扫描基地：自动控制视角移动 + 截图，最后拼接全景图
"""

import time
from collections.abc import Callable
from dataclasses import dataclass, field

import numpy as np

from app.capture.window_capture import WindowCapture
from app.control.game_controller import GameController
from app.image.preprocess import is_blurry


@dataclass
class CaptureGrid:
    """采集网格配置"""
    rows: int = 3
    cols: int = 3
    overlap: float = 0.3
    pan_distance: int = 400
    zoom_steps: int = 10


@dataclass
class CapturedFrame:
    """采集到的单帧数据"""
    image: np.ndarray
    row: int
    col: int
    seq: int
    timestamp: float


# 预设模式
CAPTURE_PRESETS: dict[str, CaptureGrid] = {
    "small": CaptureGrid(rows=2, cols=2, pan_distance=500, zoom_steps=8),
    "medium": CaptureGrid(rows=3, cols=3, pan_distance=400, zoom_steps=10),
    "large": CaptureGrid(rows=4, cols=4, pan_distance=350, zoom_steps=12),
    "xlarge": CaptureGrid(rows=5, cols=5, pan_distance=300, zoom_steps=15),
}


class AutoCapturer:
    """自动控制 + 截图采集 + 拼接"""

    def __init__(self, controller: GameController, capture: WindowCapture):
        self.controller = controller
        self.capture = capture
        self._frames: list[CapturedFrame] = []
        self._cancelled: bool = False
        self._progress_callback: Callable | None = None
        self._log_callback: Callable | None = None
        self._skip_blur: bool = True
        self._blur_threshold: float = 100.0
        self._blur_skipped: int = 0

    @property
    def frames(self) -> list[CapturedFrame]:
        return self._frames

    @property
    def frame_count(self) -> int:
        return len(self._frames)

    def set_progress_callback(self, cb: Callable):
        self._progress_callback = cb

    def set_log_callback(self, cb: Callable):
        self._log_callback = cb

    def set_blur_filter(self, enabled: bool, threshold: float = 100.0):
        self._skip_blur = enabled
        self._blur_threshold = threshold

    def cancel(self):
        self._cancelled = True

    def _log(self, msg: str):
        if self._log_callback:
            self._log_callback(msg)

    def _notify_progress(self, done: int, total: int):
        if self._progress_callback:
            self._progress_callback(done, total)

    def capture_grid(self, grid: CaptureGrid) -> list[CapturedFrame]:
        """执行网格化自动采集"""
        self._frames = []
        self._cancelled = False
        self._blur_skipped = 0
        total = grid.rows * grid.cols

        self._log("查找游戏窗口...")
        if not self.controller.find_window():
            raise RuntimeError("找不到游戏窗口，请确保游戏正在运行")

        self._log("激活游戏窗口...")
        self.controller.focus_window()
        time.sleep(0.5)

        self._log(f"拉远视角（{grid.zoom_steps} 步）...")
        self.controller.zoom_out(grid.zoom_steps)
        time.sleep(1.0)

        self._log(f"开始采集：{grid.rows}×{grid.cols} 网格...")
        self._notify_progress(0, total)

        seq = 0
        for row in range(grid.rows):
            for _col_in_row in range(grid.cols):
                if self._cancelled:
                    self._log("采集已取消")
                    return self._frames

                # 蛇形扫描：偶数行左→右，奇数行右→左
                col = _col_in_row if row % 2 == 0 else (grid.cols - 1 - _col_in_row)

                self._log(f"截图 ({row},{col}) [{seq + 1}/{total}]")
                frame_img = self.capture.capture()

                if frame_img is not None:
                    if self._skip_blur:
                        is_blur, blur_val = is_blurry(frame_img, self._blur_threshold)
                        if is_blur:
                            self._blur_skipped += 1
                            self._log(f"  模糊跳过 (值:{blur_val:.1f})")
                        else:
                            self._frames.append(CapturedFrame(
                                image=frame_img,
                                row=row, col=col,
                                seq=seq, timestamp=time.time(),
                            ))
                            seq += 1
                    else:
                        self._frames.append(CapturedFrame(
                            image=frame_img,
                            row=row, col=col,
                            seq=seq, timestamp=time.time(),
                        ))
                        seq += 1

                self._notify_progress(row * grid.cols + _col_in_row + 1, total)

                # 横向移动（每行内）
                if _col_in_row < grid.cols - 1:
                    direction = "right" if row % 2 == 0 else "left"
                    self._log(f"  移动 → {direction}")
                    if direction == "right":
                        self.controller.pan_right(grid.pan_distance)
                    else:
                        self.controller.pan_left(grid.pan_distance)

            # 纵向移动（每行结束后）
            if row < grid.rows - 1:
                self._log("  移动 ↓ ")
                self.controller.pan_down(grid.pan_distance)

        self._log(f"采集完成：{len(self._frames)} 帧")
        if self._blur_skipped > 0:
            self._log(f"  跳过 {self._blur_skipped} 个模糊帧")

        return self._frames

    def get_ordered_images(self) -> list[np.ndarray]:
        """返回按采集顺序排列的图像列表"""
        return [f.image for f in self._frames]