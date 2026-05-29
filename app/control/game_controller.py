"""
协议映射 · 游戏控制器
通过 Win32 SendInput API 控制终末地游戏视角
参考 MaaFramework SeizeInput.cpp 的 SendInput 实现

终末地基地模式视角控制：
- WASD = 平移视角
- 滚轮 = 缩放
"""

import ctypes
import sys
import time
from ctypes import wintypes

WIN32 = sys.platform == "win32"
if WIN32:
    try:
        import win32gui
        import win32con
        import win32api
    except ImportError:
        WIN32 = False

# --- Win32 SendInput 结构体定义 ---
if WIN32:
    user32 = ctypes.windll.user32

    INPUT_MOUSE = 0
    INPUT_KEYBOARD = 1

    MOUSEEVENTF_MOVE = 0x0001
    MOUSEEVENTF_WHEEL = 0x0800
    MOUSEEVENTF_ABSOLUTE = 0x8000

    class MOUSEINPUT(ctypes.Structure):
        _fields_ = [
            ("dx", wintypes.LONG),
            ("dy", wintypes.LONG),
            ("mouseData", wintypes.DWORD),
            ("dwFlags", wintypes.DWORD),
            ("time", wintypes.DWORD),
            ("dwExtraInfo", ctypes.POINTER(ctypes.c_ulong)),
        ]

    class KEYBDINPUT(ctypes.Structure):
        _fields_ = [
            ("wVk", wintypes.WORD),
            ("wScan", wintypes.WORD),
            ("dwFlags", wintypes.DWORD),
            ("time", wintypes.DWORD),
            ("dwExtraInfo", ctypes.POINTER(ctypes.c_ulong)),
        ]

    class INPUT_UNION(ctypes.Union):
        _fields_ = [("mi", MOUSEINPUT), ("ki", KEYBDINPUT)]

    class INPUT(ctypes.Structure):
        _fields_ = [("type", wintypes.DWORD), ("union", INPUT_UNION)]

    WHEEL_DELTA: int = 120


class GameController:
    """通过 Win32 API 控制终末地游戏视角和操作"""

    WINDOW_TITLES = [
        "Endfield",
        "终末地",
        "Arknights Endfield",
        "明日方舟：终末地",
        "明日方舟终末地",
    ]
    WINDOW_CLASSES = [
        "UnityWndClass",
    ]

    DEFAULT_ZOOM_STEPS: int = 10
    DEFAULT_PAN_DURATION: float = 0.35
    STABILIZE_DELAY: float = 0.4

    # WASD 虚拟键码
    VK_W = 0x57
    VK_A = 0x41
    VK_S = 0x53
    VK_D = 0x44

    def __init__(self):
        self.hwnd: int | None = None

    # ---- 窗口操作 ----

    def find_window(self) -> bool:
        """查找游戏窗口。"""
        if not WIN32:
            return False
        self.hwnd = None

        def callback(hwnd, _):
            if self.hwnd is not None:
                return True
            try:
                if not win32gui.IsWindowVisible(hwnd) or win32gui.IsIconic(hwnd):
                    return True
                title = win32gui.GetWindowText(hwnd).lower()
                class_name = win32gui.GetClassName(hwnd).lower()
            except Exception:
                return True

            title_match = any(k.lower() in title for k in self.WINDOW_TITLES)
            class_match = any(k.lower() in class_name for k in self.WINDOW_CLASSES)
            if title_match or class_match:
                self.hwnd = hwnd
            return True

        try:
            win32gui.EnumWindows(callback, None)
        except Exception:
            return self.hwnd is not None
        return self.hwnd is not None

    def focus_window(self) -> bool:
        """激活游戏窗口到前台"""
        if not WIN32 or self.hwnd is None:
            return False
        try:
            if win32gui.IsIconic(self.hwnd):
                win32gui.ShowWindow(self.hwnd, win32con.SW_RESTORE)
            win32gui.SetForegroundWindow(self.hwnd)
            time.sleep(0.5)
        except Exception:
            return False
        return True

    def is_active(self) -> bool:
        if not WIN32 or self.hwnd is None:
            return False
        return win32gui.GetForegroundWindow() == self.hwnd

    def ensure_game_foreground(self) -> bool:
        if not self.is_active():
            return self.focus_window()
        return True

    def _move_cursor_to_center(self):
        """将鼠标移到游戏窗口客户区中心（屏幕坐标）"""
        if not WIN32 or self.hwnd is None:
            return
        try:
            left, top, right, bottom = win32gui.GetClientRect(self.hwnd)
            lt = win32gui.ClientToScreen(self.hwnd, (left, top))
            cx = lt[0] + (right - left) // 2
            cy = lt[1] + (bottom - top) // 2
            if cx > 0 and cy > 0:
                user32.SetCursorPos(cx, cy)
                time.sleep(0.05)
        except Exception:
            pass

    # ---- 键盘操作 (MaaFramework SeizeInput 方式) ----
    # 同时填 wVk + wScan，dwFlags=0 (key_down) / KEYEVENTF_KEYUP (key_up)
    # 不加 KEYEVENTF_SCANCODE

    def _send_key_down(self, vk: int):
        scan = user32.MapVirtualKeyW(vk, 0)  # MAPVK_VK_TO_VSC = 0
        inp = INPUT()
        inp.type = INPUT_KEYBOARD
        inp.union.ki.wVk = vk
        inp.union.ki.wScan = scan
        user32.SendInput(1, ctypes.byref(inp), ctypes.sizeof(INPUT))

    def _send_key_up(self, vk: int):
        scan = user32.MapVirtualKeyW(vk, 0)
        inp = INPUT()
        inp.type = INPUT_KEYBOARD
        inp.union.ki.wVk = vk
        inp.union.ki.wScan = scan
        inp.union.ki.dwFlags = 0x0002  # KEYEVENTF_KEYUP
        user32.SendInput(1, ctypes.byref(inp), ctypes.sizeof(INPUT))

    # ---- 鼠标操作 (SendInput) ----

    def _send_mouse_wheel(self, delta: int):
        """delta < 0 向上滚（拉远）, delta > 0 向下滚（拉近）"""
        inp = INPUT()
        inp.type = INPUT_MOUSE
        inp.union.mi.mouseData = delta * WHEEL_DELTA
        inp.union.mi.dwFlags = MOUSEEVENTF_WHEEL
        user32.SendInput(1, ctypes.byref(inp), ctypes.sizeof(INPUT))

    # ---- 视角控制 ----

    def zoom_out(self, steps: int | None = None):
        """滚轮拉远（俯视）"""
        if not WIN32:
            return
        self.ensure_game_foreground()
        self._move_cursor_to_center()
        steps = steps or self.DEFAULT_ZOOM_STEPS
        for _ in range(steps):
            self._send_mouse_wheel(-1)
            time.sleep(0.08)

    def zoom_in(self, steps: int | None = None):
        """滚轮拉近"""
        if not WIN32:
            return
        self.ensure_game_foreground()
        self._move_cursor_to_center()
        steps = steps or self.DEFAULT_ZOOM_STEPS
        for _ in range(steps):
            self._send_mouse_wheel(1)
            time.sleep(0.08)

    def pan_view(self, vk: int, duration: float | None = None):
        """
        WASD 平移视角
        vk = VK_A / VK_D / VK_W / VK_S
        """
        if not WIN32:
            return
        self.ensure_game_foreground()
        self._move_cursor_to_center()

        duration = duration if duration is not None else self.DEFAULT_PAN_DURATION
        self._send_key_down(vk)
        time.sleep(duration)
        self._send_key_up(vk)
        time.sleep(self.STABILIZE_DELAY)

    def pan_right(self, duration: float | None = None):
        self.pan_view(self.VK_D, duration)

    def pan_left(self, duration: float | None = None):
        self.pan_view(self.VK_A, duration)

    def pan_down(self, duration: float | None = None):
        self.pan_view(self.VK_S, duration)

    def pan_up(self, duration: float | None = None):
        self.pan_view(self.VK_W, duration)