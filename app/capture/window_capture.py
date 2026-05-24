"""
协议映射 · 窗口截图模块
使用 mss 捕获指定窗口或屏幕区域
"""

import cv2
import mss
import mss.tools
import numpy as np
from PIL import Image


class WindowCapture:
    """窗口截图器，支持选择窗口或指定区域"""

    def __init__(self):
        self.sct = mss.mss()
        self._monitor: dict | None = None

    def list_monitors(self) -> list[dict]:
        """列出所有可用显示器"""
        return self.sct.monitors

    def set_monitor(self, index: int):
        """选择要捕获的显示器编号（1=全部并集, 2=主屏, 3+=副屏）"""
        monitors = self.sct.monitors
        if 0 < index < len(monitors):
            self._monitor = monitors[index]
        else:
            raise ValueError(f"显示器编号 {index} 无效，可用: 1..{len(monitors)-1}")

    def set_custom_region(self, left: int, top: int, width: int, height: int):
        """自定义捕获区域（像素坐标）"""
        self._monitor = {"left": left, "top": top, "width": width, "height": height}

    def capture(self) -> np.ndarray | None:
        """捕获当前选定区域的截图，返回 BGR numpy 数组（OpenCV 格式）"""
        if self._monitor is None:
            return None
        sct_img = self.sct.grab(self._monitor)
        # mss 返回 BGRA，转为 BGR
        img = np.array(sct_img)
        return cv2.cvtColor(img, cv2.COLOR_BGRA2BGR)

    def capture_pil(self) -> Image.Image | None:
        """捕获当前选定区域的截图，返回 PIL Image"""
        bgr = self.capture()
        if bgr is None:
            return None
        rgb = cv2.cvtColor(bgr, cv2.COLOR_BGR2RGB)
        return Image.fromarray(rgb)

    def get_region_info(self) -> dict:
        """返回当前捕获区域的信息"""
        if self._monitor is None:
            return {"status": "未设置"}
        return {
            "left": self._monitor["left"],
            "top": self._monitor["top"],
            "width": self._monitor["width"],
            "height": self._monitor["height"],
        }

    def release(self):
        self.sct.close()
