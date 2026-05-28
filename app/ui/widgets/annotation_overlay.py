"""
协议映射 · 标注叠加组件
在全景图上交互式绘制暗管路径和文字标签
"""

from __future__ import annotations

from dataclasses import dataclass, field

import cv2
import numpy as np
from PySide6.QtCore import Qt, QPoint, QRect, Signal
from PySide6.QtGui import (
    QColor,
    QImage,
    QPainter,
    QPen,
    QPixmap,
    QFont,
)
from PySide6.QtWidgets import (
    QInputDialog,
    QLabel,
    QMenu,
)


# ── 管线类型预设 ──

PIPE_PRESETS = {
    "上水": "#4488ff",
    "下水": "#ff8844",
    "电缆": "#ffdd00",
    "气路": "#44dddd",
    "传送带": "#ff44ff",
    "自定义": "#ffffff",
}


@dataclass
class PipeData:
    """管线数据"""
    points: list = None
    color: str = "#4488ff"
    label: str = ""
    thickness: int = 3

    def __post_init__(self):
        if self.points is None:
            self.points = []

    def to_dict(self) -> dict:
        return {
            "points": [[p[0], p[1]] for p in self.points],
            "color": self.color,
            "label": self.label,
            "thickness": self.thickness,
        }

    @classmethod
    def from_dict(cls, d: dict):
        return cls(
            points=[(p[0], p[1]) for p in d.get("points", [])],
            color=d.get("color", "#4488ff"),
            label=d.get("label", ""),
            thickness=d.get("thickness", 3),
        )


@dataclass
class LabelData:
    """文字标签"""
    x: float = 0.0
    y: float = 0.0
    text: str = ""
    color: str = "#44ff44"

    def to_dict(self) -> dict:
        return {"x": self.x, "y": self.y, "text": self.text, "color": self.color}

    @classmethod
    def from_dict(cls, d: dict):
        return cls(x=d["x"], y=d["y"], text=d["text"], color=d.get("color", "#44ff44"))


# ── 标注叠加控件 ──

class AnnotationOverlay(QLabel):
    """全景图标注叠加层：左键画管线，右键完成/菜单，Shift+左键放标签"""

    data_changed = Signal()

    def __init__(self, parent=None):
        super().__init__(parent)
        self._image = None
        self._pipes = []
        self._labels = []
        self._pipe_preset = "上水"
        self._current_pipe = None
        self._preview_point = None

        self._scale = 1.0
        self._offset_x = 0.0
        self._offset_y = 0.0
        self._display_w = 0
        self._display_h = 0

        self.setAlignment(Qt.AlignCenter)
        self.setMinimumSize(400, 400)
        self.setMouseTracking(True)
        self.setContextMenuPolicy(Qt.CustomContextMenu)
        self.customContextMenuRequested.connect(self._on_context_menu)

    # ── 公共接口 ──

    def set_image(self, image):
        self._image = image
        self._redraw()

    def set_pipe_preset(self, name):
        if name in PIPE_PRESETS:
            self._pipe_preset = name

    def get_pipes(self):
        self._finish_current_pipe()
        return self._pipes

    def get_labels(self):
        return self._labels

    def load_data(self, pipes, labels):
        self._pipes = pipes
        self._labels = labels
        self._redraw()

    def clear_all(self):
        self._pipes = []
        self._labels = []
        self._current_pipe = None
        self._redraw()
        self.data_changed.emit()

    def get_annotated_image(self):
        """返回含标注的渲染图像"""
        if self._image is None:
            return None
        result = self._image.copy()
        self._render_annotations_cv(result)
        return result

    # ── 绘制 ──

    def _redraw(self):
        if self._image is None:
            self.setText("无图像")
            return

        h, w = self._image.shape[:2]
        view_w = max(self.width() - 12, 1)
        view_h = max(self.height() - 12, 1)
        self._scale = min(view_w / w, view_h / h, 1.0)

        self._display_w = int(w * self._scale)
        self._display_h = int(h * self._scale)
        self._offset_x = (view_w - self._display_w) / 2 + 6
        self._offset_y = (view_h - self._display_h) / 2 + 6

        display = cv2.resize(self._image, (self._display_w, self._display_h))
        rgb = cv2.cvtColor(display, cv2.COLOR_BGR2RGB)
        qimage = QImage(rgb.data, self._display_w, self._display_h,
                        self._display_w * 3, QImage.Format_RGB888).copy()

        pixmap = QPixmap.fromImage(qimage)
        painter = QPainter(pixmap)
        painter.setRenderHint(QPainter.Antialiasing)

        # 绘制已完成的管线
        for pipe in self._pipes:
            self._draw_pipe(painter, pipe, self._scale)

        # 绘制当前正在画的管线
        if self._current_pipe and self._current_pipe.points:
            self._draw_pipe(painter, self._current_pipe, self._scale)
            # 预览线
            if self._preview_point:
                last = self._current_pipe.points[-1]
                sx = int(last[0] * self._scale)
                sy = int(last[1] * self._scale)
                pen = QPen(QColor(255, 255, 255, 100), 1)
                pen.setStyle(Qt.DashLine)
                painter.setPen(pen)
                painter.drawLine(sx, sy, self._preview_point.x(), self._preview_point.y())

        # 绘制标签
        font = QFont("Microsoft YaHei", 10)
        painter.setFont(font)
        for lbl in self._labels:
            color = QColor(lbl.color)
            painter.setPen(QPen(color, 2))
            sx = int(lbl.x * self._scale)
            sy = int(lbl.y * self._scale)
            text_rect = painter.boundingRect(QRect(), Qt.AlignLeft, lbl.text)
            text_rect.moveTo(sx + 6, sy - text_rect.height() // 2)
            painter.fillRect(text_rect.adjusted(-2, -2, 2, 2), QColor(0, 0, 0, 180))
            painter.drawText(sx + 6, sy + 4, lbl.text)
            painter.setBrush(color)
            painter.drawEllipse(QPoint(sx, sy), 3, 3)

        painter.end()
        self.setPixmap(pixmap)

    def _draw_pipe(self, painter, pipe, scale):
        if len(pipe.points) < 2:
            return
        color = QColor(pipe.color)
        pen = QPen(color, pipe.thickness)
        pen.setCapStyle(Qt.RoundCap)
        pen.setJoinStyle(Qt.RoundJoin)
        painter.setPen(pen)

        scaled = [(int(p[0] * scale), int(p[1] * scale)) for p in pipe.points]
        for i in range(len(scaled) - 1):
            painter.drawLine(scaled[i][0], scaled[i][1],
                            scaled[i + 1][0], scaled[i + 1][1])

        # 顶点标记
        painter.setPen(QPen(color, 1))
        painter.setBrush(color)
        for px, py in scaled:
            painter.drawEllipse(QPoint(px, py), 4, 4)

        # 标签文字
        if pipe.label:
            mid = scaled[len(scaled) // 2]
            painter.setPen(QPen(QColor("#ffffff")))
            painter.drawText(mid[0] + 8, mid[1] - 4, pipe.label)

    def _render_annotations_cv(self, image):
        """在 numpy 图像上渲染标注（用于导出）"""
        for pipe in self._pipes:
            if len(pipe.points) < 2:
                continue
            color_hex = pipe.color.lstrip("#")
            bgr = (
                int(color_hex[4:6], 16) if len(color_hex) >= 6 else 255,
                int(color_hex[2:4], 16) if len(color_hex) >= 4 else 255,
                int(color_hex[0:2], 16) if len(color_hex) >= 2 else 255,
            )
            pts = np.int32([[(int(p[0]), int(p[1])) for p in pipe.points]])
            cv2.polylines(image, pts, False, bgr, pipe.thickness, cv2.LINE_AA)
            if pipe.label:
                mid = pipe.points[len(pipe.points) // 2]
                cv2.putText(image, pipe.label, (int(mid[0]) + 8, int(mid[1])),
                           cv2.FONT_HERSHEY_SIMPLEX, 0.5, (255, 255, 255), 1)

        for lbl in self._labels:
            color_hex = lbl.color.lstrip("#")
            bgr = (
                int(color_hex[4:6], 16) if len(color_hex) >= 6 else 0,
                int(color_hex[2:4], 16) if len(color_hex) >= 4 else 255,
                int(color_hex[0:2], 16) if len(color_hex) >= 2 else 0,
            )
            cv2.putText(image, lbl.text, (int(lbl.x) + 6, int(lbl.y)),
                       cv2.FONT_HERSHEY_SIMPLEX, 0.5, bgr, 1)
            cv2.circle(image, (int(lbl.x), int(lbl.y)), 3, bgr, -1)

    # ── 坐标转换 ──

    def _to_image_coords(self, widget_x, widget_y):
        img_x = (widget_x - self._offset_x) / self._scale
        img_y = (widget_y - self._offset_y) / self._scale
        if self._image is not None:
            h, w = self._image.shape[:2]
            img_x = max(0, min(img_x, w - 1))
            img_y = max(0, min(img_y, h - 1))
        return img_x, img_y

    # ── 鼠标事件 ──

    def mousePressEvent(self, event):
        if self._image is None:
            return

        img_x, img_y = self._to_image_coords(
            event.position().x(), event.position().y()
        )

        # Shift+左键：放置标签
        if event.modifiers() & Qt.ShiftModifier and event.button() == Qt.LeftButton:
            text, ok = QInputDialog.getText(self, "添加标签", "标签文字：")
            if ok and text.strip():
                self._labels.append(LabelData(
                    x=img_x, y=img_y,
                    text=text.strip(),
                    color=PIPE_PRESETS.get(self._pipe_preset, "#44ff44"),
                ))
                self.data_changed.emit()
                self._redraw()
            return

        # 左键：添加管线顶点
        if event.button() == Qt.LeftButton:
            if self._current_pipe is None:
                color = PIPE_PRESETS.get(self._pipe_preset, "#4488ff")
                self._current_pipe = PipeData(
                    color=color,
                    label=self._pipe_preset if self._pipe_preset != "自定义" else "",
                )
            self._current_pipe.points.append((img_x, img_y))
            self._redraw()

        # 右键：完成当前管线
        elif event.button() == Qt.RightButton:
            self._finish_current_pipe()

    def mouseMoveEvent(self, event):
        if self._current_pipe and self._current_pipe.points:
            img_x, img_y = self._to_image_coords(
                event.position().x(), event.position().y()
            )
            self._preview_point = QPoint(int(img_x * self._scale), int(img_y * self._scale))
            self._redraw()

    def mouseDoubleClickEvent(self, event):
        """双击完成当前管线"""
        if event.button() == Qt.LeftButton:
            self._finish_current_pipe()

    def _finish_current_pipe(self):
        if self._current_pipe and len(self._current_pipe.points) >= 2:
            self._pipes.append(self._current_pipe)
            self.data_changed.emit()
        self._current_pipe = None
        self._preview_point = None
        self._redraw()

    # ── 右键菜单 ──

    def _on_context_menu(self, pos):
        menu = QMenu(self)

        pipe_menu = menu.addMenu("管线类型")
        for name in PIPE_PRESETS:
            action = pipe_menu.addAction(f"  {name}")
            action.triggered.connect(lambda checked, n=name: self._set_preset(n))

        menu.addSeparator()

        undo_action = menu.addAction("撤销上一个顶点")
        undo_action.triggered.connect(self._undo_last_point)

        undo_pipe_action = menu.addAction("删除最后一条管线")
        undo_pipe_action.triggered.connect(self._undo_last_pipe)

        menu.addSeparator()

        clear_action = menu.addAction("清除全部标注")
        clear_action.triggered.connect(self.clear_all)

        menu.exec(self.mapToGlobal(pos))

    def _set_preset(self, name):
        self._pipe_preset = name

    def _undo_last_point(self):
        if self._current_pipe and self._current_pipe.points:
            self._current_pipe.points.pop()
            if not self._current_pipe.points:
                self._current_pipe = None
            self._redraw()
        elif self._pipes:
            self._pipes[-1].points.pop()
            if not self._pipes[-1].points:
                self._pipes.pop()
            self.data_changed.emit()
            self._redraw()

    def _undo_last_pipe(self):
        if self._current_pipe:
            self._current_pipe = None
        elif self._pipes:
            self._pipes.pop()
        self.data_changed.emit()
        self._redraw()

    def resizeEvent(self, event):
        super().resizeEvent(event)
        self._redraw()
