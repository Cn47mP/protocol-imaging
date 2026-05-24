"""
协议映射 · 主窗口框架
"""

from __future__ import annotations

from pathlib import Path

import cv2
import numpy as np
from PySide6.QtCore import Qt, QTimer
from PySide6.QtGui import QAction, QImage, QPixmap
from PySide6.QtWidgets import (
    QComboBox,
    QFileDialog,
    QFormLayout,
    QFrame,
    QGroupBox,
    QHBoxLayout,
    QLabel,
    QListWidget,
    QMainWindow,
    QMessageBox,
    QPushButton,
    QSpinBox,
    QSplitter,
    QStatusBar,
    QTabWidget,
    QTextEdit,
    QVBoxLayout,
    QWidget,
)

from app.capture.recorder import Recorder
from app.capture.window_capture import WindowCapture
from app.export.png_export import export_png
from app.image.align import auto_align
from app.image.stitch import stitch_sequential


class MainWindow(QMainWindow):
    """协议映射主窗口。"""

    def __init__(self) -> None:
        super().__init__()
        self.setWindowTitle("协议映射")
        self.setMinimumSize(1280, 820)

        self.capture = WindowCapture()
        self.recorder = Recorder(self.capture)

        self._frames: list[np.ndarray] = []
        self._stitched_image: np.ndarray | None = None
        self._project_path: Path | None = None
        self._preview_active = False

        self._capture_timer = QTimer(self)
        self._capture_timer.timeout.connect(self._on_capture_tick)

        self._preview_timer = QTimer(self)
        self._preview_timer.timeout.connect(self._on_preview_tick)

        self._build_actions()
        self._build_ui()
        self._populate_monitors()
        self._update_ui_state()
        self._log("就绪：请选择捕获源。")

    # --- UI 构建 ---

    def _build_actions(self) -> None:
        file_menu = self.menuBar().addMenu("文件")

        self._act_new = QAction("新建项目", self)
        self._act_new.triggered.connect(self._new_project)
        file_menu.addAction(self._act_new)

        self._act_open = QAction("打开项目...", self)
        self._act_open.triggered.connect(self._open_project_placeholder)
        file_menu.addAction(self._act_open)

        self._act_save = QAction("保存项目...", self)
        self._act_save.triggered.connect(self._save_project_placeholder)
        file_menu.addAction(self._act_save)

        file_menu.addSeparator()

        self._act_export = QAction("导出 PNG...", self)
        self._act_export.triggered.connect(self._export_png)
        file_menu.addAction(self._act_export)

        view_menu = self.menuBar().addMenu("视图")
        self._act_preview = QAction("切换预览", self)
        self._act_preview.triggered.connect(self._toggle_preview)
        view_menu.addAction(self._act_preview)

    def _build_ui(self) -> None:
        root = QSplitter(Qt.Horizontal)
        self.setCentralWidget(root)

        root.addWidget(self._build_left_panel())
        root.addWidget(self._build_workspace())
        root.setSizes([300, 980])

        status = QStatusBar()
        self.setStatusBar(status)
        self._status_label = QLabel("就绪")
        status.addPermanentWidget(self._status_label)

    def _build_left_panel(self) -> QWidget:
        panel = QWidget()
        panel.setMaximumWidth(340)
        layout = QVBoxLayout(panel)

        capture_group = QGroupBox("采集")
        capture_layout = QFormLayout(capture_group)
        self._monitor_combo = QComboBox()
        capture_layout.addRow("捕获源", self._monitor_combo)

        self._interval_spin = QSpinBox()
        self._interval_spin.setRange(1, 300)
        self._interval_spin.setValue(5)
        self._interval_spin.setSuffix(" 秒")
        capture_layout.addRow("采集间隔", self._interval_spin)

        self._btn_preview = QPushButton("开启预览")
        self._btn_preview.clicked.connect(self._toggle_preview)
        capture_layout.addRow(self._btn_preview)

        self._btn_start = QPushButton("开始采集")
        self._btn_start.clicked.connect(self._start_capture)
        capture_layout.addRow(self._btn_start)

        self._btn_stop = QPushButton("停止采集")
        self._btn_stop.clicked.connect(self._stop_capture)
        capture_layout.addRow(self._btn_stop)

        layout.addWidget(capture_group)

        frames_group = QGroupBox("帧序列")
        frames_layout = QVBoxLayout(frames_group)
        self._frame_label = QLabel("已采集：0 帧")
        frames_layout.addWidget(self._frame_label)
        self._frame_list = QListWidget()
        self._frame_list.currentRowChanged.connect(self._on_frame_selected)
        frames_layout.addWidget(self._frame_list)
        self._btn_clear = QPushButton("清空帧")
        self._btn_clear.clicked.connect(self._clear_frames)
        frames_layout.addWidget(self._btn_clear)
        layout.addWidget(frames_group, 1)

        pipeline_group = QGroupBox("处理流程")
        pipeline_layout = QVBoxLayout(pipeline_group)
        self._btn_align = QPushButton("锚点标定")
        self._btn_align.clicked.connect(self._open_align_placeholder)
        pipeline_layout.addWidget(self._btn_align)
        self._btn_stitch = QPushButton("生成全景图")
        self._btn_stitch.clicked.connect(self._stitch_frames)
        pipeline_layout.addWidget(self._btn_stitch)
        self._btn_export = QPushButton("导出 PNG")
        self._btn_export.clicked.connect(self._export_png)
        pipeline_layout.addWidget(self._btn_export)
        layout.addWidget(pipeline_group)

        layout.addStretch()
        return panel

    def _build_workspace(self) -> QWidget:
        workspace = QWidget()
        layout = QVBoxLayout(workspace)

        self._tabs = QTabWidget()
        self._tabs.addTab(self._build_preview_tab(), "画面预览")
        self._tabs.addTab(self._build_stitch_tab(), "全景结果")
        self._tabs.addTab(self._build_log_tab(), "日志")
        layout.addWidget(self._tabs)
        return workspace

    def _build_preview_tab(self) -> QWidget:
        tab = QWidget()
        layout = QVBoxLayout(tab)
        self._preview_label = QLabel("请选择捕获源并开启预览")
        self._preview_label.setAlignment(Qt.AlignCenter)
        self._preview_label.setMinimumSize(820, 560)
        self._preview_label.setFrameShape(QFrame.StyledPanel)
        self._preview_label.setStyleSheet("background: #15171a; color: #b8c0cc;")
        layout.addWidget(self._preview_label)
        return tab

    def _build_stitch_tab(self) -> QWidget:
        tab = QWidget()
        layout = QVBoxLayout(tab)
        self._result_label = QLabel("生成全景图后显示结果")
        self._result_label.setAlignment(Qt.AlignCenter)
        self._result_label.setMinimumSize(820, 560)
        self._result_label.setFrameShape(QFrame.StyledPanel)
        self._result_label.setStyleSheet("background: #101216; color: #b8c0cc;")
        layout.addWidget(self._result_label)
        return tab

    def _build_log_tab(self) -> QWidget:
        tab = QWidget()
        layout = QVBoxLayout(tab)
        self._log_box = QTextEdit()
        self._log_box.setReadOnly(True)
        layout.addWidget(self._log_box)
        return tab

    # --- 项目操作 ---

    def _new_project(self) -> None:
        if self.recorder.is_recording:
            return
        self._frames.clear()
        self.recorder.clear()
        self._stitched_image = None
        self._project_path = None
        self._frame_list.clear()
        self._frame_label.setText("已采集：0 帧")
        self._preview_label.setText("请选择捕获源并开启预览")
        self._result_label.setText("生成全景图后显示结果")
        self._update_ui_state()
        self._log("已新建空项目。")

    def _open_project_placeholder(self) -> None:
        QMessageBox.information(self, "打开项目", "项目文件保存 / 加载将在后续步骤接入。")

    def _save_project_placeholder(self) -> None:
        QMessageBox.information(self, "保存项目", "项目文件保存 / 加载将在后续步骤接入。")

    # --- 采集 ---

    def _populate_monitors(self) -> None:
        try:
            self._monitor_combo.clear()
            for index, monitor in enumerate(self.capture.list_monitors()):
                label = "全部显示器" if index == 0 else f"显示器 {index}"
                self._monitor_combo.addItem(
                    f"{label}（{monitor['width']}×{monitor['height']}）",
                    index,
                )
        except Exception as exc:  # noqa: BLE001
            self._log(f"获取显示器失败：{exc}")

    def _toggle_preview(self) -> None:
        if self._preview_active:
            self._preview_timer.stop()
            self._preview_active = False
            self._btn_preview.setText("开启预览")
            self._log("预览已关闭。")
        else:
            if not self._apply_monitor_selection():
                return
            self._preview_timer.start(250)
            self._preview_active = True
            self._btn_preview.setText("关闭预览")
            self._log("预览已开启。")
        self._update_ui_state()

    def _start_capture(self) -> None:
        if not self._apply_monitor_selection():
            return
        if self._preview_active:
            self._toggle_preview()

        interval_ms = self._interval_spin.value() * 1000
        self.recorder.set_interval(float(self._interval_spin.value()))
        self.recorder.start()
        self._capture_timer.start(interval_ms)
        self._log("开始采集。")
        self._update_ui_state()

    def _stop_capture(self) -> None:
        self._capture_timer.stop()
        self.recorder.stop()
        self._log(f"采集停止，共 {len(self._frames)} 帧。")
        self._update_ui_state()

    def _on_preview_tick(self) -> None:
        frame = self.capture.capture()
        if frame is not None:
            self._show_image(frame, self._preview_label)

    def _on_capture_tick(self) -> None:
        frame = self.recorder.capture_frame()
        if frame is None:
            return
        self._frames.append(frame)
        self._frame_label.setText(f"已采集：{len(self._frames)} 帧")
        self._frame_list.addItem(f"帧 {len(self._frames):04d}")
        self._frame_list.setCurrentRow(self._frame_list.count() - 1)
        self._log(f"采集帧 {len(self._frames):04d}。")
        self._update_ui_state()

    def _clear_frames(self) -> None:
        self._frames.clear()
        self.recorder.clear()
        self._stitched_image = None
        self._frame_list.clear()
        self._frame_label.setText("已采集：0 帧")
        self._preview_label.setText("帧序列已清空")
        self._result_label.setText("生成全景图后显示结果")
        self._log("已清空帧序列。")
        self._update_ui_state()

    def _on_frame_selected(self, row: int) -> None:
        if 0 <= row < len(self._frames):
            self._show_image(self._frames[row], self._preview_label)
            self._tabs.setCurrentIndex(0)

    # --- 拼接与导出 ---

    def _open_align_placeholder(self) -> None:
        QMessageBox.information(self, "锚点标定", "锚点标定面板将在后续步骤实现。")

    def _stitch_frames(self) -> None:
        if not self._frames:
            QMessageBox.warning(self, "提示", "没有可拼接的帧。")
            return
        if len(self._frames) == 1:
            self._stitched_image = self._frames[0].copy()
        else:
            self._log("开始自动拼接。")
            base = self._frames[0]
            homographies = [np.eye(3, dtype=np.float64)]
            for index, frame in enumerate(self._frames[1:], start=2):
                matrix = auto_align(frame, base)
                if matrix is None:
                    self._log(f"帧 {index:04d} 自动对齐失败，沿用上一帧位置。")
                    matrix = homographies[-1]
                homographies.append(matrix)
            self._stitched_image = stitch_sequential(self._frames, homographies)

        self._show_image(self._stitched_image, self._result_label)
        self._tabs.setCurrentIndex(1)
        self._log("全景图已生成。")
        self._update_ui_state()

    def _export_png(self) -> None:
        if self._stitched_image is None:
            if self._frames:
                self._stitch_frames()
            else:
                QMessageBox.warning(self, "提示", "没有可导出的图像。")
                return

        path, _ = QFileDialog.getSaveFileName(
            self,
            "导出 PNG",
            "protocol_map.png",
            "PNG 图片 (*.png)",
        )
        if not path:
            return
        export_png(self._stitched_image, path)
        self._log(f"已导出：{path}")

    # --- 辅助 ---

    def _apply_monitor_selection(self) -> bool:
        index = self._monitor_combo.currentData()
        if index is None:
            QMessageBox.warning(self, "提示", "请选择捕获源。")
            return False
        try:
            self.capture.set_monitor(index)
        except Exception as exc:  # noqa: BLE001
            QMessageBox.warning(self, "捕获源错误", str(exc))
            return False
        return True

    def _show_image(self, image: np.ndarray | None, label: QLabel) -> None:
        if image is None:
            return
        view_w = max(label.width() - 16, 1)
        view_h = max(label.height() - 16, 1)
        h, w = image.shape[:2]
        scale = min(view_w / w, view_h / h, 1.0)
        display = image
        if scale < 1.0:
            display = cv2.resize(image, (int(w * scale), int(h * scale)))

        rgb = cv2.cvtColor(display, cv2.COLOR_BGR2RGB)
        qimage = QImage(
            rgb.data,
            rgb.shape[1],
            rgb.shape[0],
            rgb.shape[1] * rgb.shape[2],
            QImage.Format_RGB888,
        ).copy()
        label.setPixmap(QPixmap.fromImage(qimage))

    def _update_ui_state(self) -> None:
        recording = self.recorder.is_recording
        has_frames = bool(self._frames)
        has_result = self._stitched_image is not None

        self._btn_preview.setEnabled(not recording)
        self._btn_start.setEnabled(not recording and self._monitor_combo.count() > 0)
        self._btn_stop.setEnabled(recording)
        self._btn_clear.setEnabled(has_frames and not recording)
        self._btn_align.setEnabled(has_frames and not recording)
        self._btn_stitch.setEnabled(has_frames and not recording)
        self._btn_export.setEnabled((has_frames or has_result) and not recording)
        self._status_label.setText("采集中" if recording else "就绪")

    def _log(self, message: str) -> None:
        self.statusBar().showMessage(message)
        self._log_box.append(message)

    def closeEvent(self, event) -> None:  # noqa: N802
        self._capture_timer.stop()
        self._preview_timer.stop()
        self.capture.release()
        super().closeEvent(event)
