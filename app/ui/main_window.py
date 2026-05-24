"""
协议映射 · 主窗口
"""

import cv2
import numpy as np
from pathlib import Path
from PySide6.QtWidgets import (
    QMainWindow, QWidget, QVBoxLayout, QHBoxLayout,
    QPushButton, QLabel, QListWidget, QSpinBox,
    QFileDialog, QMessageBox, QStatusBar, QGroupBox,
    QSlider, QComboBox,
)
from PySide6.QtCore import Qt, QTimer, Signal
from PySide6.QtGui import QPixmap, QImage

from app.capture.window_capture import WindowCapture
from app.capture.recorder import Recorder


class MainWindow(QMainWindow):
    """协议映射主窗口"""

    def __init__(self):
        super().__init__()
        self.setWindowTitle("协议映射")
        self.setMinimumSize(1200, 800)

        # 核心模块
        self.capture = WindowCapture()
        self.recorder = Recorder(self.capture)

        # 录制定时器
        self._capture_timer = QTimer()
        self._capture_timer.timeout.connect(self._on_capture_tick)

        # 预览定时器（实时显示画面）
        self._preview_timer = QTimer()
        self._preview_timer.timeout.connect(self._on_preview_tick)
        self._preview_active = False

        # 帧数据
        self._frames = []
        self._preview_img = None

        self._build_ui()
        self._populate_monitors()
        self._update_ui_state()

    def _build_ui(self):
        """构建界面"""
        central = QWidget()
        self.setCentralWidget(central)
        main_layout = QHBoxLayout(central)

        # 左侧：控制面板
        left_panel = QVBoxLayout()

        # 1.截图设置
        capture_group = QGroupBox("截图设置")
        cap_layout = QVBoxLayout(capture_group)

        self._monitor_combo = QComboBox()
        cap_layout.addWidget(QLabel("捕获源:"))
        cap_layout.addWidget(self._monitor_combo)

        interval_layout = QHBoxLayout()
        interval_layout.addWidget(QLabel("间隔(秒):"))
        self._interval_spin = QSpinBox()
        self._interval_spin.setRange(1, 30)
        self._interval_spin.setValue(5)
        interval_layout.addWidget(self._interval_spin)
        cap_layout.addLayout(interval_layout)

        left_panel.addWidget(capture_group)

        # 2.录制控制
        record_group = QGroupBox("录制控制")
        rec_layout = QVBoxLayout(record_group)

        self._btn_preview = QPushButton("开启预览")
        self._btn_preview.clicked.connect(self._toggle_preview)
        rec_layout.addWidget(self._btn_preview)

        self._btn_start = QPushButton("开始采集")
        self._btn_start.clicked.connect(self._start_capture)
        rec_layout.addWidget(self._btn_start)

        self._btn_stop = QPushButton("停止采集")
        self._btn_stop.clicked.connect(self._stop_capture)
        rec_layout.addWidget(self._btn_stop)

        self._frame_label = QLabel("已采集: 0 帧")
        rec_layout.addWidget(self._frame_label)

        left_panel.addWidget(record_group)

        # 3.操作
        action_group = QGroupBox("操作")
        action_layout = QVBoxLayout(action_group)

        self._btn_clear = QPushButton("清空帧")
        self._btn_clear.clicked.connect(self._clear_frames)
        action_layout.addWidget(self._btn_clear)

        self._btn_align = QPushButton("锚点标定")
        self._btn_align.clicked.connect(self._open_align)
        action_layout.addWidget(self._btn_align)

        self._btn_export = QPushButton("导出 PNG")
        self._btn_export.clicked.connect(self._export_png)
        action_layout.addWidget(self._btn_export)

        left_panel.addWidget(action_group)
        left_panel.addStretch()

        # 左侧容器
        left_widget = QWidget()
        left_widget.setLayout(left_panel)
        left_widget.setMaximumWidth(250)
        main_layout.addWidget(left_widget)

        # 右侧：预览区
        right_layout = QVBoxLayout()

        # 预览图
        self._preview_label = QLabel("请选择捕获源并开启预览")
        self._preview_label.setAlignment(Qt.AlignCenter)
        self._preview_label.setStyleSheet("border: 1px solid #555; background: #1a1a1a;")
        self._preview_label.setMinimumSize(800, 600)
        right_layout.addWidget(self._preview_label)

        # 帧列表
        self._frame_list = QListWidget()
        self._frame_list.setMaximumHeight(150)
        self._frame_list.currentRowChanged.connect(self._on_frame_selected)
        right_layout.addWidget(QLabel("帧序列:"))
        right_layout.addWidget(self._frame_list)

        main_layout.addLayout(right_layout, 1)

        # 状态栏
        self.statusBar().showMessage("就绪")

    def _populate_monitors(self):
        """填充可用显示器"""
        try:
            monitors = self.capture.list_monitors()
            for i, m in enumerate(monitors):
                if i == 0:
                    name = f"所有显示器 ({m['width']}x{m['height']})"
                else:
                    name = f"显示器 {i} ({m['width']}x{m['height']})"
                self._monitor_combo.addItem(name, i)
        except Exception as e:
            self.statusBar().showMessage(f"获取显示器失败: {e}")

    def _update_ui_state(self):
        """更新界面按钮状态"""
        recording = self.recorder.is_recording
        has_frames = len(self._frames) > 0

        self._btn_preview.setEnabled(not recording)
        self._btn_start.setEnabled(not recording and self._monitor_combo.count() > 0)
        self._btn_stop.setEnabled(recording)
        self._btn_clear.setEnabled(has_frames and not recording)
        self._btn_align.setEnabled(has_frames and not recording)
        self._btn_export.setEnabled(has_frames and not recording)

    # --- 操作 ---

    def _toggle_preview(self):
        """切换预览"""
        if self._preview_active:
            self._preview_timer.stop()
            self._preview_active = False
            self._btn_preview.setText("开启预览")
            self.statusBar().showMessage("预览已关闭")
        else:
            idx = self._monitor_combo.currentData()
            if idx is None:
                return
            self.capture.set_monitor(idx)
            self._preview_timer.start(200)  # 200ms 刷新一次
            self._preview_active = True
            self._btn_preview.setText("关闭预览")
            self.statusBar().showMessage("预览中...")

    def _on_preview_tick(self):
        """预览定时器回调"""
        img = self.capture.capture()
        if img is not None:
            self._show_image(img, scale=True)

    def _start_capture(self):
        """开始采集"""
        idx = self._monitor_combo.currentData()
        if idx is None:
            QMessageBox.warning(self, "提示", "请选择捕获源")
            return

        self.capture.set_monitor(idx)

        # 关闭预览
        if self._preview_active:
            self._toggle_preview()

        interval = self._interval_spin.value() / 10.0  # 转为秒
        self.recorder.set_interval(interval)
        self.recorder.start()

        self._capture_timer.start(int(interval * 1000))
        self._update_ui_state()
        self.statusBar().showMessage("采集中...")

    def _on_capture_tick(self):
        """录制定时器回调"""
        img = self.recorder.capture_frame()
        if img is not None:
            self._frames.append(img)
            self._frame_label.setText(f"已采集: {len(self._frames)} 帧")
            self._frame_list.addItem(f"帧 {len(self._frames):04d}")
            self._frame_list.setCurrentRow(self._frame_list.count() - 1)

    def _stop_capture(self):
        """停止采集"""
        self._capture_timer.stop()
        self.recorder.stop()
        self._update_ui_state()
        self.statusBar().showMessage(f"采集完成，共 {len(self._frames)} 帧")

    def _clear_frames(self):
        """清空所有帧"""
        self._frames.clear()
        self.recorder.clear()
        self._frame_list.clear()
        self._frame_label.setText("已采集: 0 帧")
        self._preview_label.clear()
        self._preview_label.setText("已清空")
        self._update_ui_state()
        self.statusBar().showMessage("已清空")

    def _on_frame_selected(self, row: int):
        """帧列表选中回调"""
        if 0 <= row < len(self._frames):
            self._show_image(self._frames[row], scale=True)

    def _open_align(self):
        """打开锚点标定"""
        if len(self._frames) < 2:
            QMessageBox.warning(self, "提示", "至少需要 2 帧才能对齐")
            return
        QMessageBox.information(self, "锚点标定",
            "选择两张参考帧，分别点击对应位置设置锚点。\n\n"
            "该功能将在下一版本实现完整交互界面。\n"
            "当前推荐使用自动拼接（导出时自动执行）。")

    def _export_png(self):
        """导出全景图：自动拼接并导出 PNG"""
        if not self._frames:
            QMessageBox.warning(self, "提示", "没有可导出的帧")
            return
        if len(self._frames) < 2:
            path, _ = QFileDialog.getSaveFileName(
                self, "导出全景图", "protocol_map.png", "PNG 图片 (*.png)"
            )
            if path:
                import cv2
                cv2.imwrite(path, self._frames[0])
                self.statusBar().showMessage(f"已导出: {path}")
            return

        from app.image.align import auto_align
        from app.image.stitch import stitch_sequential
        import numpy as np

        self.statusBar().showMessage("正在拼接...")
        QMessageBox.information(self, "提示",
            "开始自动拼接，帧数较多时可能需要一些时间。\n\n"
            "当前为自动特征匹配模式，结果可能不完美。\n"
            "锚点校正功能将在下一版本完善。")

        # 逐帧计算单应性
        homographies = [np.eye(3, dtype=np.float64)]  # 第一帧的变换为单位矩阵
        base_img = self._frames[0]

        for i in range(1, len(self._frames)):
            H = auto_align(self._frames[i], base_img)
            if H is None:
                self.statusBar().showMessage(f"第 {i} 帧对齐失败，跳过")
                homographies.append(homographies[-1])
            else:
                homographies.append(H)

        # 拼接
        result = stitch_sequential(self._frames, homographies)

        if result is None or result.size == 0:
            QMessageBox.warning(self, "错误", "拼接失败")
            return

        path, _ = QFileDialog.getSaveFileName(
            self, "导出全景图", "protocol_map.png", "PNG 图片 (*.png)"
        )
        if path:
            import cv2
            cv2.imwrite(path, result)
            self.statusBar().showMessage(f"已导出: {path}")
            self._show_image(result, scale=True)

    # --- 辅助 ---

    def _show_image(self, img: np.ndarray, scale: bool = False):
        """在预览区显示 OpenCV 图像"""
        if img is None:
            return
        h, w = img.shape[:2]
        if scale:
            max_w = self._preview_label.width() - 10
            max_h = self._preview_label.height() - 10
            scale_f = min(max_w / w, max_h / h, 1.0)
            if scale_f < 1.0:
                new_w = int(w * scale_f)
                new_h = int(h * scale_f)
                img = cv2.resize(img, (new_w, new_h))

        rgb = cv2.cvtColor(img, cv2.COLOR_BGR2RGB)
        h, w, ch = rgb.shape
        bytes_per_line = ch * w
        qimg = QImage(rgb.data, w, h, bytes_per_line, QImage.Format_RGB888)
        self._preview_label.setPixmap(QPixmap.fromImage(qimg))

    def closeEvent(self, event):
        """窗口关闭时清理"""
        self._capture_timer.stop()
        self._preview_timer.stop()
        self.capture.release()
        super().closeEvent(event)
