"""
协议映射 · 锚点标定对话框
用户点选 4 对锚点 → 计算单应性矩阵 → 实时预览叠图效果
支持批量逐帧标定：前后帧导航，自动保存每帧结果
"""

from __future__ import annotations

import cv2
import numpy as np
from PySide6.QtCore import Qt, QPoint, Signal
from PySide6.QtGui import (
    QColor,
    QImage,
    QPainter,
    QPen,
    QPixmap,
)
from PySide6.QtWidgets import (
    QCheckBox,
    QDialog,
    QHBoxLayout,
    QLabel,
    QPushButton,
    QVBoxLayout,
    QWidget,
)

from app.image.align import auto_align, manual_align


# ── 可点击标注的图片控件 ──

class ClickableImageLabel(QLabel):
    """支持点击选点的图片控件"""

    point_added = Signal(int, float, float)  # (point_index, x, y)
    point_removed = Signal()  # 删除最后一个点

    def __init__(self, title: str = "", parent=None):
        super().__init__(parent)
        self._title = title
        self._points: list[tuple[float, float]] = []  # 图像像素坐标
        self._image: np.ndarray | None = None
        self._scale: float = 1.0
        self._offset_x: float = 0.0
        self._offset_y: float = 0.0

        self.setAlignment(Qt.AlignCenter)
        self.setMinimumSize(400, 400)
        self.setFrameShape(QLabel.StyledPanel)
        self.setStyleSheet("background: #1a1d23;")
        self.setMouseTracking(True)

    def set_image(self, image: np.ndarray) -> None:
        """设置显示的图像"""
        self._image = image
        self._points.clear()
        self._update_display()

    def set_points(self, points: list[tuple[float, float]]) -> None:
        """预设锚点"""
        self._points = list(points)
        self._update_display()

    def get_points(self) -> list[tuple[float, float]]:
        return list(self._points)

    def clear_points(self) -> None:
        self._points.clear()
        self._update_display()

    # ── 绘制 ──

    def _update_display(self) -> None:
        if self._image is None:
            self.setText("无图像")
            return

        h, w = self._image.shape[:2]
        view_w = max(self.width() - 12, 1)
        view_h = max(self.height() - 12, 1)
        self._scale = min(view_w / w, view_h / h, 1.0)

        display_w = int(w * self._scale)
        display_h = int(h * self._scale)
        self._offset_x = (view_w - display_w) / 2 + 6
        self._offset_y = (view_h - display_h) / 2 + 6

        display = cv2.resize(self._image, (display_w, display_h))
        rgb = cv2.cvtColor(display, cv2.COLOR_BGR2RGB)
        qimage = QImage(rgb.data, display_w, display_h, display_w * 3, QImage.Format_RGB888).copy()

        # 绘制锚点标记
        pixmap = QPixmap.fromImage(qimage)
        painter = QPainter(pixmap)
        colors = [
            QColor("#ff4444"),  # 红
            QColor("#44ff44"),  # 绿
            QColor("#4488ff"),  # 蓝
            QColor("#ffaa00"),  # 橙
        ]
        for i, (px, py) in enumerate(self._points):
            color = colors[i % len(colors)]
            pen = QPen(color, 2)
            painter.setPen(pen)
            # 十字线
            sx = int(px * self._scale)
            sy = int(py * self._scale)
            r = 10
            painter.drawLine(sx - r, sy, sx + r, sy)
            painter.drawLine(sx, sy - r, sx, sy + r)
            # 圆圈
            pen.setWidth(3)
            painter.setPen(pen)
            painter.drawEllipse(QPoint(sx, sy), r, r)
            # 编号
            painter.setPen(QColor("#ffffff"))
            painter.drawText(sx + r + 4, sy + 4, str(i + 1))

        painter.end()
        self.setPixmap(pixmap)

    # ── 事件 ──

    def mousePressEvent(self, event) -> None:
        if self._image is None:
            return
        if len(self._points) >= 4:
            return

        # 计算图像坐标
        img_x = (event.position().x() - self._offset_x) / self._scale
        img_y = (event.position().y() - self._offset_y) / self._scale

        h, w = self._image.shape[:2]
        img_x = max(0, min(img_x, w - 1))
        img_y = max(0, min(img_y, h - 1))

        idx = len(self._points)
        self._points.append((img_x, img_y))
        self.point_added.emit(idx, img_x, img_y)
        self._update_display()

    def contextMenuEvent(self, event) -> None:
        """右键删除最后一个点"""
        if self._points:
            self._points.pop()
            self.point_removed.emit()
            self._update_display()

    def resizeEvent(self, event) -> None:
        super().resizeEvent(event)
        self._update_display()


# ── 标定对话框 ──

class CalibrationDialog(QDialog):
    """锚点标定对话框：源帧 ←→ 底图 四点对齐，支持批量逐帧标定"""

    MIN_POINTS = 4

    def __init__(
        self,
        src_frames: list[np.ndarray],       # 全部帧
        base_frame: np.ndarray,             # 底图（第一帧）
        homographies: list[np.ndarray],     # 当前所有 homography
        start_index: int = 0,               # 起始帧索引
        parent=None,
    ):
        super().__init__(parent)
        self.setWindowTitle("锚点标定")
        self.setMinimumSize(1100, 600)

        self._all_frames = src_frames
        self._base_frame = base_frame
        self._homographies = [H.copy() for H in homographies]  # 可变副本
        self._current_idx = start_index
        self._current_homography: np.ndarray | None = None
        self._src_points: list[tuple[float, float]] = []
        self._base_points: list[tuple[float, float]] = []

        self._build_ui()
        self._connect_signals()
        self._load_frame(self._current_idx)

    def _build_ui(self) -> None:
        layout = QVBoxLayout(self)

        # 顶部导航
        nav = QHBoxLayout()
        self._prev_btn = QPushButton("◀ 上一帧")
        nav.addWidget(self._prev_btn)
        self._frame_label = QLabel("帧 0000 / 0000")
        self._frame_label.setAlignment(Qt.AlignCenter)
        nav.addWidget(self._frame_label)
        self._next_btn = QPushButton("下一帧 ▶")
        nav.addWidget(self._next_btn)
        nav.addStretch()
        self._auto_btn = QPushButton("自动对齐")
        self._auto_btn.setToolTip("尝试自动特征匹配对齐，失败则保持手动模式")
        nav.addWidget(self._auto_btn)
        layout.addLayout(nav)

        # 双图区域
        images_layout = QHBoxLayout()

        left = QVBoxLayout()
        left.addWidget(QLabel("源帧（单击选点，右键撤销）"))
        self._src_label = ClickableImageLabel("源帧")
        left.addWidget(self._src_label, 1)
        images_layout.addLayout(left)

        right = QVBoxLayout()
        right.addWidget(QLabel("底图 / 参考帧（单击选点，右键撤销）"))
        self._base_label = ClickableImageLabel("底图")
        right.addWidget(self._base_label, 1)
        images_layout.addLayout(right)

        layout.addLayout(images_layout, 1)

        # 底部控制
        bottom = QHBoxLayout()

        self._status_label = QLabel("请在两侧图上各点选 4 个对应锚点")
        bottom.addWidget(self._status_label)

        bottom.addStretch()

        self._preview_check = QCheckBox("实时叠图预览")
        self._preview_check.setEnabled(False)
        bottom.addWidget(self._preview_check)

        self._clear_btn = QPushButton("清除锚点")
        bottom.addWidget(self._clear_btn)

        self._apply_btn = QPushButton("应用标定")
        self._apply_btn.setEnabled(False)
        bottom.addWidget(self._apply_btn)

        self._ok_btn = QPushButton("完成")
        bottom.addWidget(self._ok_btn)

        layout.addLayout(bottom)

    def _connect_signals(self) -> None:
        self._src_label.point_added.connect(self._on_point_added)
        self._base_label.point_added.connect(self._on_point_added)
        self._src_label.point_removed.connect(self._on_point_removed)
        self._base_label.point_removed.connect(self._on_point_removed)
        self._preview_check.toggled.connect(self._on_preview_toggled)
        self._clear_btn.clicked.connect(self._clear_points)
        self._apply_btn.clicked.connect(self._apply_calibration)
        self._auto_btn.clicked.connect(self._try_auto_align)
        self._prev_btn.clicked.connect(self._go_prev)
        self._next_btn.clicked.connect(self._go_next)
        self._ok_btn.clicked.connect(self.accept)

    # ── 帧导航 ──

    def _load_frame(self, index: int) -> None:
        """加载第 index 帧进入标定界面"""
        self._current_idx = index
        frame = self._all_frames[index]
        self._src_label.set_image(frame)
        self._base_label.set_image(self._base_frame)

        self._current_homography = None
        self._src_points = []
        self._base_points = []

        # 检查是否已有标定数据——尝试从 homography 反推
        H = self._homographies[index]
        is_identity = np.allclose(H, np.eye(3), atol=1e-6)
        if not is_identity:
            self._current_homography = H
            self._apply_btn.setEnabled(True)
            self._preview_check.setEnabled(True)
            self._preview_check.setChecked(True)
            self._status_label.setText(f"帧 {index:04d} — 已有标定数据 ✓")
            self._show_overlay()
        else:
            self._preview_check.setChecked(False)
            self._preview_check.setEnabled(False)
            self._apply_btn.setEnabled(False)
            self._status_label.setText(f"帧 {index:04d} — 请在两侧图上各点选 4 个对应锚点")

        self._frame_label.setText(f"帧 {index:04d} / {len(self._all_frames):04d}")
        self._prev_btn.setEnabled(index > 0)
        self._next_btn.setEnabled(index < len(self._all_frames) - 1)

    def _go_prev(self) -> None:
        if self._current_idx > 0:
            # 如果有未应用的标定，提示
            if self._current_homography is not None:
                self._homographies[self._current_idx] = self._current_homography
            self._load_frame(self._current_idx - 1)

    def _go_next(self) -> None:
        if self._current_idx < len(self._all_frames) - 1:
            if self._current_homography is not None:
                self._homographies[self._current_idx] = self._current_homography
            self._load_frame(self._current_idx + 1)

    # ── 逻辑 ──

    def _on_point_added(self, idx: int, x: float, y: float):
        # 同步两侧点数
        src_count = len(self._src_label.get_points())
        base_count = len(self._base_label.get_points())
        self._src_points = self._src_label.get_points()
        self._base_points = self._base_label.get_points()
        self._status_label.setText(
            f"源帧：{src_count}/4 · 底图：{base_count}/4  "
            f"（右键撤销上一个点）"
        )
        self._try_compute()

    def _on_point_removed(self):
        self._src_points = self._src_label.get_points()
        self._base_points = self._base_label.get_points()
        src_count = len(self._src_points)
        base_count = len(self._base_points)
        self._status_label.setText(
            f"源帧：{src_count}/4 · 底图：{base_count}/4"
        )
        self._current_homography = None
        self._apply_btn.setEnabled(False)
        self._preview_check.setEnabled(False)
        self._preview_check.setChecked(False)
        self._base_label.set_image(self._base_frame)

    def _clear_points(self) -> None:
        self._src_label.clear_points()
        self._base_label.clear_points()
        self._base_label.set_image(self._base_frame)
        self._src_points = []
        self._base_points = []
        self._current_homography = None
        self._apply_btn.setEnabled(False)
        self._preview_check.setEnabled(False)
        self._preview_check.setChecked(False)
        self._status_label.setText("请在两侧图上各点选 4 个对应锚点")

    def _try_compute(self) -> None:
        if len(self._src_points) < self.MIN_POINTS or len(self._base_points) < self.MIN_POINTS:
            return

        H = manual_align(self._src_points, self._base_points)
        if H is not None:
            self._current_homography = H
            self._apply_btn.setEnabled(True)
            self._preview_check.setEnabled(True)
            self._preview_check.setChecked(True)
            self._status_label.setText("锚点标定完成 ✓ — 已开启叠图预览，点击「应用标定」确认")
        else:
            self._status_label.setText("计算失败，请重新选点")

    def _try_auto_align(self) -> None:
        """尝试自动特征匹配"""
        frame = self._all_frames[self._current_idx]
        H = auto_align(frame, self._base_frame)
        if H is not None:
            self._current_homography = H
            self._apply_btn.setEnabled(True)
            self._preview_check.setEnabled(True)
            self._preview_check.setChecked(True)
            self._show_overlay()
            self._status_label.setText(f"自动对齐成功 ✓ — 点击「应用标定」确认")
        else:
            self._status_label.setText("自动对齐失败，请手动选点")

    def _apply_calibration(self) -> None:
        """保存当前帧标定结果"""
        if self._current_homography is not None:
            self._homographies[self._current_idx] = self._current_homography
            self._apply_btn.setEnabled(False)
            self._status_label.setText(f"帧 {self._current_idx:04d} 标定已应用 ✓")
            # 自动跳到下一帧
            if self._current_idx < len(self._all_frames) - 1:
                self._go_next()

    def _on_preview_toggled(self, checked: bool) -> None:
        if checked and self._current_homography is not None:
            self._show_overlay()
        elif not checked:
            self._base_label.set_image(self._base_frame)

    def _show_overlay(self) -> None:
        """将源帧按 homography 变换后叠加到底图上"""
        frame = self._all_frames[self._current_idx]
        H = self._current_homography
        if H is None:
            return

        h, w = self._base_frame.shape[:2]
        warped = cv2.warpPerspective(frame, H, (w, h))

        # 50% 透明度叠加
        overlay = cv2.addWeighted(self._base_frame, 0.5, warped, 0.5, 0)
        self._base_label.set_image(overlay)

        # 恢复锚点标记
        self._base_label.set_points(self._base_label.get_points())

    # ── 公共接口 ──

    @property
    def homography(self) -> np.ndarray | None:
        """当前帧的单应性矩阵"""
        return self._current_homography

    @property
    def all_homographies(self) -> list[np.ndarray]:
        """全部帧的单应性矩阵"""
        # 提交当前未应用的
        if self._current_homography is not None:
            self._homographies[self._current_idx] = self._current_homography
        return self._homographies

    @property
    def current_index(self) -> int:
        return self._current_idx
