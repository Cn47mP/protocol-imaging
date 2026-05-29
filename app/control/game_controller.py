"""
协议映射 · 游戏控制器
通过 Win32 SendInput API 控制终末地游戏视角（鼠标拖拽 + 滚轮缩放 + WASD）
参考 MaaFramework Win32 Input 方案，使用 SendInput 而非 pyautogui
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
    except ImportError:
        WIN32 = False

# --- Win32 SendInput 结构体定义 ---
if WIN32:
    user32 = ctypes.windll.user32

    INPUT_MOUSE = 0
    INPUT_KEYBOARD = 1

    MOUSEEVENTF_MOVE = 0x0001
    MOUSEEVENTF_LEFTDOWN = 0x0002
    MOUSEEVENTF_LEFTUP = 0x0004
    MOUSEEVENTF_RIGHTDOWN = 0x0008
    MOUSEEVENTF_RIGHTUP = 0x0010
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
        "终末地",
        "Arknights Endfield",
        "明日方舟：终末地",
        "明日方舟终末地",
    ]

    DEFAULT_ZOOM_STEPS: int = 10
    DEFAULT_PAN_DISTANCE: int = 400
    DEFAULT_PAN_DURATION: float = 0.3
    STABILIZE_DELAY: float = 0.3

    def __init__(self):
        self.hwnd: int | None = None

    # ---- 窗口操作 ----

    def find_window(self) -> bool:
        """查找游戏窗口，优先匹配窗口标题"""
        if not WIN32:
            return False
        self.hwnd = None
        for title in self.WINDOW_TITLES:
            hwnd = win32gui.FindWindow(None, title)
            if hwnd and win32gui.IsWindowVisible(hwnd):
                self.hwnd = hwnd
                return True
        return False

    def focus_window(self) -> bool:
        """激活游戏窗口到前台"""
        if not WIN32 or self.hwnd is None:
            return False
        if win32gui.IsIconic(self.hwnd):
            win32gui.ShowWindow(self.hwnd, win32con.SW_RESTORE)
        win32gui.SetForegroundWindow(self.hwnd)
        time.sleep(0.3)
        return True

    def is_active(self) -> bool:
        if not WIN32 or self.hwnd is None:
            return False
        return win32gui.GetForegroundWindow() == self.hwnd

    def get_client_center(self) -> tuple[int, int]:
        """获取游戏窗口客户区中心坐标（屏幕坐标）"""
        if not WIN32 or self.hwnd is None:
            return (0, 0)
        left, top, right, bottom = win32gui.GetClientRect(self.hwnd)
        lt = win32gui.ClientToScreen(self.hwnd, (left, top))
        cx = lt[0] + (right - left) // 2
        cy = lt[1] + (bottom - top) // 2
        return (cx, cy)

    # ---- 鼠标操作 (SendInput) ----

    def _send_mouse_move(self, dx: int, dy: int):
        """相对移动鼠标"""
        inp = INPUT()
        inp.type = INPUT_MOUSE
        inp.union.mi.dx = dx
        inp.union.mi.dy = dy
        inp.union.mi.dwFlags = MOUSEEVENTF_MOVE
        user32.SendInput(1, ctypes.byref(inp), ctypes.sizeof(INPUT))

    def _send_mouse_left_down(self):
        inp = INPUT()
        inp.type = INPUT_MOUSE
        inp.union.mi.dwFlags = MOUSEEVENTF_LEFTDOWN
        user32.SendInput(1, ctypes.byref(inp), ctypes.sizeof(INPUT))

    def _send_mouse_left_up(self):
        inp = INPUT()
        inp.type = INPUT_MOUSE
        inp.union.mi.dwFlags = MOUSEEVENTF_LEFTUP
        user32.SendInput(1, ctypes.byref(inp), ctypes.sizeof(INPUT))

    def _send_mouse_wheel(self, delta: int):
        """delta > 0 向上滚（拉远）, delta < 0 向下滚（拉近）"""
        inp = INPUT()
        inp.type = INPUT_MOUSE
        inp.union.mi.mouseData = delta * WHEEL_DELTA
        inp.union.mi.dwFlags = MOUSEEVENTF_WHEEL
        user32.SendInput(1, ctypes.byref(inp), ctypes.sizeof(INPUT))

    def _send_key_down(self, vk: int):
        inp = INPUT()
        inp.type = INPUT_KEYBOARD
        inp.union.ki.wVk = vk
        user32.SendInput(1, ctypes.byref(inp), ctypes.sizeof(INPUT))

    def _send_key_up(self, vk: int):
        inp = INPUT()
        inp.type = INPUT_KEYBOARD
        inp.union.ki.wVk = vk
        inp.union.ki.dwFlags = 0x0002  # KEYEVENTF_KEYUP
        user32.SendInput(1, ctypes.byref(inp), ctypes.sizeof(INPUT))

    # ---- 视角控制 ----

    def zoom_out(self, steps: int | None = None):
        """滚轮拉远（俯视）"""
        if not WIN32:
            return
        steps = steps or self.DEFAULT_ZOOM_STEPS
        for _ in range(steps):
            self._send_mouse_wheel(-1)
            time.sleep(0.05)

    def zoom_in(self, steps: int | None = None):
        """滚轮拉近"""
        if not WIN32:
            return
        steps = steps or self.DEFAULT_ZOOM_STEPS
        for _ in range(steps):
            self._send_mouse_wheel(1)
            time.sleep(0.05)

    def pan_view(self, dx: int, dy: int, duration: float | None = None):
        """
        鼠标拖拽移动视角
        dx > 0 向右, dy > 0 向下
        终末地：拖拽鼠标反向移动视角（拖右→画面左移→看到右边内容）
        """
        if not WIN32:
            return
        duration = duration if duration is not None else self.DEFAULT_PAN_DURATION
        steps = max(1, int(duration * 60))  # 60fps 平滑移动
        step_dx = dx // steps
        step_dy = dy // steps

        self._send_mouse_left_down()
        for _ in range(steps):
            self._send_mouse_move(step_dx, step_dy)
            time.sleep(duration / steps)
        self._send_mouse_left_up()
        time.sleep(self.STABILIZE_DELAY)

    def pan_right(self, distance: int | None = None):
        d = distance or self.DEFAULT_PAN_DISTANCE
        self.pan_view(d, 0)

    def pan_left(self, distance: int | None = None):
        d = distance or self.DEFAULT_PAN_DISTANCE
        self.pan_view(-d, 0)

    def pan_down(self, distance: int | None = None):
        d = distance or self.DEFAULT_PAN_DISTANCE
        self.pan_view(0, d)

    def pan_up(self, distance: int | None = None):
        d = distance or self.DEFAULT_PAN_DISTANCE
        self.pan_view(0, -d)

    # ---- WASD 移动（备选方案，部分玩家可能用键盘移动视角）----

    def pan_with_key(self, key: str, duration: float = 0.15):
        """用 WASD 短暂移动视角"""
        if not WIN32:
            return
        vk_map = {"W": 0x57, "A": 0x41, "S": 0x53, "D": 0x44}
        vk = vk_map.get(key.upper())
        if vk is None:
            return
        self._send_key_down(vk)
        time.sleep(duration)
        self._send_key_up(vk)
        time.sleep(self.STABILIZE_DELAY)