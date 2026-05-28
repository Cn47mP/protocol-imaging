"""
协议映射 · 锚点标定对话框
用户点选 4 对锚点 → 计算单应性矩阵 → 实时预览叠图效果
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

from app.image.align import manual_align


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
    """锚点标定对话框：源帧 ←→ 底图 四点对齐"""

    MIN_POINTS = 4

    def __init__(
        self,
        src_frame: np.ndarray,
        base_frame: np.ndarray,
        parent=None,
    ):
        super().__init__(parent)
        self.setWindowTitle("锚点标定")
        self.setMinimumSize(1100, 600)

        self._src_frame = src_frame
        self._base_frame = base_frame
        self._homography: np.ndarray | None = None

        self._build_ui()
        self._connect_signals()

        # 加载图像
        self._src_label.set_image(src_frame)
        self._base_label.set_image(base_frame)

    def _build_ui(self) -> None:
        layout = QVBoxLayout(self)

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

        self._status_label = QLabel("请在两侧图上各点选 4 个对应锚点（共 8 处点击）")
        bottom.addWidget(self._status_label)

        bottom.addStretch()

        self._preview_check = QCheckBox("实时叠图预览")
        self._preview_check.setEnabled(False)
        bottom.addWidget(self._preview_check)

        self._clear_btn = QPushButton("清除锚点")
        bottom.addWidget(self._clear_btn)

        self._ok_btn = QPushButton("确定")
        self._ok_btn.setEnabled(False)
        bottom.addWidget(self._ok_btn)

        self._cancel_btn = QPushButton("取消")
        bottom.addWidget(self._cancel_btn)

        layout.addLayout(bottom)

    def _connect_signals(self) -> None:
        self._src_label.point_added.connect(self._on_point_added)
        self._base_label.point_added.connect(self._on_point_added)
        self._src_label.point_removed.connect(self._on_point_removed)
        self._base_label.point_removed.connect(self._on_point_removed)
        self._preview_check.toggled.connect(self._on_preview_toggled)
        self._clear_btn.clicked.connect(self._clear_points)
        self._ok_btn.clicked.connect(self.accept)
        self._cancel_btn.clicked.connect(self.reject)

    # ── 逻辑 ──

    def _on_point_added(self, idx: int, x: float, y: float):
        src_count = len(self._src_label.get_points())
        base_count = len(self._base_label.get_points())
        self._status_label.setText(
            f"源帧：{src_count}/4 · 底图：{base_count}/4  "
            f"（右键撤销上一个点）"
        )
        self._try_compute()

    def _on_point_removed(self):
        src_count = len(self._src_label.get_points())
        base_count = len(self._base_label.get_points())
        self._status_label.setText(
            f"源帧：{src_count}/4 · 底图：{base_count}/4"
        )
        self._homography = None
        self._ok_btn.setEnabled(False)
        self._preview_check.setEnabled(False)
        self._preview_check.setChecked(False)
        # 恢复底图
        self._base_label.set_image(self._base_frame)

    def _clear_points(self) -> None:
        self._src_label.clear_points()
        self._base_label.clear_points()
        self._base_label.set_image(self._base_frame)
        self._homography = None
        self._ok_btn.setEnabled(False)
        self._preview_check.setEnabled(False)
        self._preview_check.setChecked(False)
        self._status_label.setText("请在两侧图上各点选 4 个对应锚点")

    def _try_compute(self) -> None:
        src_pts = self._src_label.get_points()
        base_pts = self._base_label.get_points()

        if len(src_pts) < self.MIN_POINTS or len(base_pts) < self.MIN_POINTS:
            return

        # 使用对应点计算单应性矩阵
        H = manual_align(src_pts, base_pts)
        if H is not None:
            self._homography = H
            self._ok_btn.setEnabled(True)
            self._preview_check.setEnabled(True)
            self._preview_check.setChecked(True)  # 自动开启预览
            self._status_label.setText("锚点标定完成 ✓ — 已开启叠图预览")
        else:
            self._status_label.setText("计算失败，请重新选点")

    def _on_preview_toggled(self, checked: bool) -> None:
        if checked and self._homography is not None:
            self._show_overlay()
        elif not checked:
            self._base_label.set_image(self._base_frame)

    def _show_overlay(self) -> None:
        """将源帧按 homography 变换后叠加到底图上"""
        if self._homography is None:
            return

        h, w = self._base_frame.shape[:2]
        warped = cv2.warpPerspective(self._src_frame, self._homography, (w, h))

        # 50% 透明度叠加
        overlay = cv2.addWeighted(self._base_frame, 0.5, warped, 0.5, 0)
        self._base_label.set_image(overlay)

        # 恢复锚点标记
        self._base_label.set_points(self._base_label.get_points())

    @property
    def homography(self) -> np.ndarray | None:
        return self._homography
