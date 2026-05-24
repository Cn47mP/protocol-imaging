"""
协议映射 · 帧序列录制器
负责连续采集帧、保存帧序列、管理录制状态
"""

import os
import time
from pathlib import Path
from typing import Optional, Callable
from app.capture.window_capture import WindowCapture


class Recorder:
    """连续帧录制器"""

    def __init__(self, capture: WindowCapture):
        self.capture = capture
        self._recording = False
        self._frames: list = []
        self._output_dir: Optional[Path] = None
        self._interval: float = 0.5  # 截图间隔（秒）
        self._on_frame: Optional[Callable] = None  # 帧回调

    @property
    def is_recording(self) -> bool:
        return self._recording

    @property
    def frame_count(self) -> int:
        return len(self._frames)

    def set_interval(self, seconds: float):
        """设置截图间隔，单位秒"""
        self._interval = max(0.1, seconds)

    def set_output_dir(self, path: str):
        """设置帧保存目录"""
        self._output_dir = Path(path)
        self._output_dir.mkdir(parents=True, exist_ok=True)

    def set_frame_callback(self, callback: Callable):
        """设置帧回调函数，每捕获一帧调用一次"""
        self._on_frame = callback

    def start(self):
        """开始录制"""
        if self._recording:
            return
        self._recording = True
        self._frames = []

    def stop(self) -> list:
        """停止录制，返回所有帧的列表"""
        self._recording = False
        return self._frames

    def capture_frame(self) -> Optional[object]:
        """捕获一帧并记录"""
        if not self._recording:
            return None
        img = self.capture.capture()
        if img is None:
            return None
        self._frames.append(img)
        if self._on_frame:
            self._on_frame(len(self._frames), img)
        return img

    def save_frames(self, prefix: str = "frame") -> list[Path]:
        """将录制的帧保存到输出目录，返回文件路径列表"""
        if self._output_dir is None:
            raise RuntimeError("未设置输出目录")
        saved = []
        for i, frame in enumerate(self._frames):
            name = f"{prefix}_{i:04d}.png"
            path = self._output_dir / name
            import cv2
            cv2.imwrite(str(path), frame)
            saved.append(path)
        return saved

    def clear(self):
        """清空帧缓存"""
        self._frames = []
