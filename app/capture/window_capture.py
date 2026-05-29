"""
协议映射 · 窗口截图模块
使用 mss 捕获指定窗口或屏幕区域
"""

import cv2
import mss
import mss.tools
import numpy as np
from PIL import Image
import sys

# 尝试导入 Windows 相关库
WIN32_AVAILABLE = False
if sys.platform == "win32":
    try:
        import win32gui
        import win32con
        import win32process
        import win32api
        WIN32_AVAILABLE = True
    except ImportError:
        WIN32_AVAILABLE = False


class WindowCapture:
    """窗口截图器，支持选择窗口、游戏窗口自动检测或指定区域"""

    # 终末地游戏窗口标题关键词
    ENDFIELD_WINDOW_TITLES = [
        "Endfield",
        "终末地",
        "Arknights Endfield",
        "明日方舟：终末地",
        "明日方舟终末地"
    ]
    ENDFIELD_PROCESS_NAMES = [
        "Endfield",
        "Endfield.exe",
    ]
    ENDFIELD_CLASS_NAMES = [
        "UnityWndClass",
    ]

    def __init__(self):
        self.sct = mss.mss()
        self._monitor: dict | None = None
        self._hwnd: int | None = None

    def list_monitors(self) -> list[dict]:
        """列出所有可用显示器"""
        return self.sct.monitors

    def set_monitor(self, index: int):
        """选择要捕获的显示器编号（1=全部并集, 2=主屏, 3+=副屏）"""
        monitors = self.sct.monitors
        if 0 < index < len(monitors):
            self._monitor = monitors[index]
            self._hwnd = None
        else:
            raise ValueError(f"显示器编号 {index} 无效，可用: 1..{len(monitors)-1}")

    def set_custom_region(self, left: int, top: int, width: int, height: int):
        """自定义捕获区域（像素坐标）"""
        self._monitor = {"left": left, "top": top, "width": width, "height": height}
        self._hwnd = None

    def auto_detect_game_window(self) -> bool:
        """
        自动检测终末地游戏窗口
        返回: 是否成功找到并设置游戏窗口
        """
        if not WIN32_AVAILABLE:
            return False

        def matches_game_window(hwnd: int) -> bool:
            try:
                title = win32gui.GetWindowText(hwnd)
                class_name = win32gui.GetClassName(hwnd)
                title_lower = title.lower()
                class_lower = class_name.lower()
            except Exception:
                return False

            if any(keyword.lower() in title_lower for keyword in self.ENDFIELD_WINDOW_TITLES):
                return True
            if any(keyword.lower() in class_lower for keyword in self.ENDFIELD_CLASS_NAMES):
                return True

            try:
                _, pid = win32process.GetWindowThreadProcessId(hwnd)
                # QueryFullProcessImageName is available through win32process on Windows.
                handle = win32api.OpenProcess(win32con.PROCESS_QUERY_LIMITED_INFORMATION, False, pid)
                try:
                    process_path = win32process.QueryFullProcessImageName(handle, 0)
                finally:
                    win32api.CloseHandle(handle)
                process_name = process_path.rsplit("\\", 1)[-1].lower()
                return any(name.lower() == process_name for name in self.ENDFIELD_PROCESS_NAMES)
            except Exception:
                return False

        def callback(hwnd, _):
            if self._hwnd is not None:
                return True
            if matches_game_window(hwnd):
                # 检查窗口是否可见且未最小化
                try:
                    is_usable = win32gui.IsWindowVisible(hwnd) and not win32gui.IsIconic(hwnd)
                except Exception:
                    is_usable = False
                if is_usable:
                    self._hwnd = hwnd
            return True

        self._hwnd = None
        try:
            win32gui.EnumWindows(callback, None)
        except Exception:
            if self._hwnd is None:
                return False

        if self._hwnd is not None:
            # 获取客户区（不含标题栏）的屏幕坐标
            left, top, right, bottom = win32gui.GetClientRect(self._hwnd)
            lt = win32gui.ClientToScreen(self._hwnd, (left, top))
            rb = win32gui.ClientToScreen(self._hwnd, (right, bottom))
            self._monitor = {
                "left": lt[0],
                "top": lt[1],
                "width": rb[0] - lt[0],
                "height": rb[1] - lt[1],
            }
            return True

        return False

    def is_game_window_active(self) -> bool:
        """检查当前捕获的窗口是否在前台"""
        if not WIN32_AVAILABLE or self._hwnd is None:
            return False
        return win32gui.GetForegroundWindow() == self._hwnd

    def list_windows(self) -> list[dict]:
        """列出所有可见窗口"""
        if not WIN32_AVAILABLE:
            return []

        windows = []
        def callback(hwnd, _):
            if win32gui.IsWindowVisible(hwnd):
                title = win32gui.GetWindowText(hwnd)
                if title:  # 只列出有标题的窗口
                    windows.append({
                        "hwnd": hwnd,
                        "title": title,
                        "class": win32gui.GetClassName(hwnd)
                    })
            return True

        win32gui.EnumWindows(callback, None)
        return windows

    def set_window_by_hwnd(self, hwnd: int) -> bool:
        """通过窗口句柄设置捕获区域"""
        if not WIN32_AVAILABLE:
            return False
        try:
            left, top, right, bottom = win32gui.GetClientRect(hwnd)
            lt = win32gui.ClientToScreen(hwnd, (left, top))
            rb = win32gui.ClientToScreen(hwnd, (right, bottom))
            self._monitor = {
                "left": lt[0],
                "top": lt[1],
                "width": rb[0] - lt[0],
                "height": rb[1] - lt[1],
            }
            self._hwnd = hwnd
            return True
        except Exception:
            return False

    def capture(self) -> np.ndarray | None:
        """捕获当前选定区域的截图，返回 BGR numpy 数组（OpenCV 格式）"""
        if self._monitor is None:
            return None
        try:
            sct_img = self.sct.grab(self._monitor)
            # mss 返回 BGRA，转为 BGR
            img = np.array(sct_img)
            return cv2.cvtColor(img, cv2.COLOR_BGRA2BGR)
        except Exception:
            return None

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
        info = {
            "left": self._monitor["left"],
            "top": self._monitor["top"],
            "width": self._monitor["width"],
            "height": self._monitor["height"],
        }
        if self._hwnd is not None and WIN32_AVAILABLE:
            info["window_title"] = win32gui.GetWindowText(self._hwnd)
            info["is_active"] = self.is_game_window_active()
        return info

    def release(self):
        self.sct.close()
