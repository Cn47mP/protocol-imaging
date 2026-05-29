"""
协议映射 · 主窗口框架
"""

from __future__ import annotations

import logging
import traceback
from pathlib import Path

import cv2
import numpy as np
from PySide6.QtCore import Qt, QTimer, QThread, Signal
from PySide6.QtGui import QAction, QColor, QGuiApplication, QImage, QPixmap
from PySide6.QtWidgets import (
    QCheckBox,
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
from app.capture.auto_capturer import AutoCapturer, CaptureGrid, CAPTURE_PRESETS
from app.control.game_controller import GameController
from app.export.png_export import export_png
from app.image.align import auto_align, manual_align
from app.image.annotate import draw_grid
from app.image.preprocess import crop_ui, normalize_brightness, is_blurry
from app.image.stitch import stitch_sequential, STITCHING_AVAILABLE
from app.ui.widgets.annotation_overlay import AnnotationOverlay, PipeData, LabelData, PIPE_PRESETS
from app.project.model import FrameInfo, Project
from app.project.storage import ProjectStorage


class AutoCaptureWorker(QThread):
    """后台线程执行自动化网格采集"""
    progress = Signal(int, int)
    log = Signal(str)
    finished = Signal(list)  # list[CapturedFrame]
    error = Signal(str)

    def __init__(self, auto_capturer: AutoCapturer, grid: CaptureGrid):
        super().__init__()
        self._capturer = auto_capturer
        self._grid = grid

    def run(self):
        self._capturer.set_progress_callback(
            lambda done, total: self.progress.emit(done, total)
        )
        self._capturer.set_log_callback(lambda msg: self.log.emit(msg))
        try:
            frames = self._capturer.capture_grid(self._grid)
            self.finished.emit(frames)
        except Exception as exc:
            logging.exception("Auto capture worker failed")
            traceback.print_exc()
            self.error.emit(str(exc))


class MainWindow(QMainWindow):
    """协议映射主窗口。"""

    def __init__(self) -> None:
        super().__init__()
        self.setWindowTitle("协议映射")
        self.setMinimumSize(1280, 820)

        self.capture = WindowCapture()
        self.recorder = Recorder(self.capture)
        self.controller = GameController()
        self.auto_capturer = AutoCapturer(self.controller, self.capture)

        self._frames: list[np.ndarray] = []
        self._homographies: list[np.ndarray] = []
        self._stitched_image: np.ndarray | None = None
        self._project_path: Path | None = None
        self._preview_active = False
        self._auto_active = False
        self._auto_grid: CaptureGrid = CAPTURE_PRESETS["medium"]
        self._auto_worker: AutoCaptureWorker | None = None
        # 预处理参数
        self._crop_top: int = 0
        self._crop_bottom: int = 0
        self._crop_left: int = 0
        self._crop_right: int = 0
        self._normalize_enabled: bool = False
        self._grid_enabled: bool = False
        self._annotation_pipes: list = []
        self._annotation_labels: list = []
        # 新功能参数
        self._skip_blurry_frames: bool = True  # 是否跳过模糊帧
        self._blur_threshold: float = 100.0  # 模糊判断阈值
        self._use_blend: bool = True  # 是否使用羽化融合
        self._skipped_frames_count: int = 0  # 跳过的模糊帧数量

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
        self._act_open.triggered.connect(self._open_project)
        file_menu.addAction(self._act_open)

        self._act_save = QAction("保存项目", self)
        self._act_save.triggered.connect(self._save_project)
        file_menu.addAction(self._act_save)

        self._act_save_as = QAction("另存为...", self)
        self._act_save_as.triggered.connect(self._save_project_as)
        file_menu.addAction(self._act_save_as)

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

        self._btn_auto_detect = QPushButton("自动检测游戏窗口")
        self._btn_auto_detect.clicked.connect(self._auto_detect_game_window)
        capture_layout.addRow(self._btn_auto_detect)

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

        auto_group = QGroupBox("自动采集（键盘控制）")
        auto_layout = QFormLayout(auto_group)

        preset_row = QHBoxLayout()
        self._auto_preset_combo = QComboBox()
        self._auto_preset_combo.addItems(["小型基地 (2×2)", "中型基地 (3×3)", "大型基地 (4×4)", "超大基地 (5×5)"])
        self._auto_preset_combo.currentIndexChanged.connect(self._on_auto_preset_changed)
        preset_row.addWidget(self._auto_preset_combo)
        auto_layout.addRow("预设", preset_row)

        btn_row = QHBoxLayout()
        self._btn_auto_start = QPushButton("一键自动采集")
        self._btn_auto_start.clicked.connect(self._start_auto_capture)
        btn_row.addWidget(self._btn_auto_start)

        self._btn_auto_cancel = QPushButton("取消")
        self._btn_auto_cancel.clicked.connect(self._cancel_auto_capture)
        self._btn_auto_cancel.setEnabled(False)
        btn_row.addWidget(self._btn_auto_cancel)
        auto_layout.addRow(btn_row)

        self._auto_progress_label = QLabel("")
        auto_layout.addRow(self._auto_progress_label)

        layout.addWidget(auto_group)

        preprocess_group = QGroupBox("预处理")
        preprocess_layout = QFormLayout(preprocess_group)

        crop_row = QHBoxLayout()
        self._crop_top_spin = QSpinBox()
        self._crop_top_spin.setRange(0, 500)
        self._crop_top_spin.setPrefix("上 ")
        self._crop_top_spin.setSuffix("px")
        self._crop_top_spin.valueChanged.connect(self._on_crop_changed)
        crop_row.addWidget(self._crop_top_spin)

        self._crop_bottom_spin = QSpinBox()
        self._crop_bottom_spin.setRange(0, 500)
        self._crop_bottom_spin.setPrefix("下 ")
        self._crop_bottom_spin.setSuffix("px")
        self._crop_bottom_spin.valueChanged.connect(self._on_crop_changed)
        crop_row.addWidget(self._crop_bottom_spin)
        preprocess_layout.addRow("裁剪", crop_row)

        crop_row2 = QHBoxLayout()
        self._crop_left_spin = QSpinBox()
        self._crop_left_spin.setRange(0, 500)
        self._crop_left_spin.setPrefix("左 ")
        self._crop_left_spin.setSuffix("px")
        self._crop_left_spin.valueChanged.connect(self._on_crop_changed)
        crop_row2.addWidget(self._crop_left_spin)

        self._crop_right_spin = QSpinBox()
        self._crop_right_spin.setRange(0, 500)
        self._crop_right_spin.setPrefix("右 ")
        self._crop_right_spin.setSuffix("px")
        self._crop_right_spin.valueChanged.connect(self._on_crop_changed)
        crop_row2.addWidget(self._crop_right_spin)
        preprocess_layout.addRow("", crop_row2)

        self._normalize_check = QCheckBox("亮度归一化")
        self._normalize_check.toggled.connect(self._on_normalize_toggled)
        preprocess_layout.addRow(self._normalize_check)

        self._skip_blurry_check = QCheckBox("跳过模糊帧")
        self._skip_blurry_check.setChecked(True)
        self._skip_blurry_check.toggled.connect(self._on_skip_blurry_toggled)
        preprocess_layout.addRow(self._skip_blurry_check)

        self._blur_threshold_spin = QSpinBox()
        self._blur_threshold_spin.setRange(10, 500)
        self._blur_threshold_spin.setValue(100)
        self._blur_threshold_spin.setSuffix(" (值越低越严格)")
        self._blur_threshold_spin.valueChanged.connect(self._on_blur_threshold_changed)
        preprocess_layout.addRow("模糊阈值", self._blur_threshold_spin)

        self._use_blend_check = QCheckBox("羽化融合拼接")
        self._use_blend_check.setChecked(True)
        self._use_blend_check.toggled.connect(self._on_use_blend_toggled)
        preprocess_layout.addRow(self._use_blend_check)

        layout.addWidget(preprocess_group)

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
        self._btn_align.clicked.connect(self._open_calibration)
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

        # 标注工具栏
        toolbar = QHBoxLayout()

        self._grid_check = QCheckBox("网格参考线")
        self._grid_check.toggled.connect(self._on_grid_toggled)
        toolbar.addWidget(self._grid_check)

        toolbar.addWidget(QLabel("管线类型："))

        self._pipe_type_combo = QComboBox()
        self._pipe_type_combo.addItems(list(PIPE_PRESETS.keys()))
        self._pipe_type_combo.setCurrentText("上水")
        self._pipe_type_combo.currentTextChanged.connect(self._on_pipe_type_changed)
        toolbar.addWidget(self._pipe_type_combo)

        toolbar.addStretch()

        self._btn_clear_annotations = QPushButton("清除标注")
        self._btn_clear_annotations.clicked.connect(self._clear_annotations)
        toolbar.addWidget(self._btn_clear_annotations)

        layout.addLayout(toolbar)

        # 标注叠加层
        self._result_label = AnnotationOverlay()
        self._result_label.setText("生成全景图后显示结果")
        self._result_label.setMinimumSize(820, 560)
        self._result_label.setStyleSheet("background: #101216; color: #b8c0cc;")
        self._result_label.data_changed.connect(self._on_annotation_changed)
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
        self._homographies.clear()
        self.recorder.clear()
        self._stitched_image = None
        self._project_path = None
        self._frame_list.clear()
        self._frame_label.setText("已采集：0 帧")
        self._preview_label.setText("请选择捕获源并开启预览")
        self._result_label.setText("生成全景图后显示结果")
        self._update_window_title()
        self._update_ui_state()
        self._log("已新建空项目。")

    def _open_project(self) -> None:
        if self.recorder.is_recording:
            return

        path, _ = QFileDialog.getOpenFileName(
            self,
            "打开项目",
            "",
            "协议映射项目 (project.json);;所有文件 (*)",
        )
        if not path:
            return

        try:
            project_dir = str(Path(path).parent)
            storage = ProjectStorage(project_dir)
            project, frames = storage.load()

            self._frames = frames
            self._homographies = [
                np.array(fi.homography, dtype=np.float64)
                if fi.homography else np.eye(3, dtype=np.float64)
                for fi in project.frames
            ]
            self._stitched_image = None
            self._project_path = Path(project_dir)
            self.recorder.clear()

            self._frame_list.clear()
            for fi in project.frames:
                self._frame_list.addItem(f"帧 {fi.index:04d}")
            self._frame_label.setText(f"已采集：{len(self._frames)} 帧")

            # 显示第一帧
            if self._frames:
                self._show_image(self._frames[0], self._preview_label)
                self._tabs.setCurrentIndex(0)
                self._frame_list.setCurrentRow(0)

            self._update_window_title()
            self._update_ui_state()
            self._log(f"已打开项目：{project.name}（{len(self._frames)} 帧）")

            # 恢复标注数据
            from dataclasses import asdict as _asdict
            self._annotation_labels = [_asdict(l) for l in project.labels]
            self._annotation_pipes = [_asdict(p) for p in project.pipes]

            # 如果有 homography，自动生成全景预览
            if self._homographies:
                self._stitch_from_homographies()

        except Exception as exc:
            QMessageBox.warning(self, "打开失败", f"无法打开项目：{exc}")
            self._log(f"打开项目失败：{exc}")

    def _save_project(self) -> None:
        if not self._frames:
            QMessageBox.warning(self, "提示", "没有可保存的数据。")
            return

        if self._project_path is None:
            self._save_project_as()
            return

        self._do_save(self._project_path)

    def _save_project_as(self) -> None:
        if not self._frames:
            QMessageBox.warning(self, "提示", "没有可保存的数据。")
            return

        path = QFileDialog.getExistingDirectory(self, "选择项目保存目录")
        if not path:
            return

        self._do_save(Path(path))

    def _do_save(self, project_dir: Path) -> None:
        try:
            # 构建 Project 模型
            project = Project(name=project_dir.name)
            for i, (frame, H) in enumerate(zip(self._frames, self._homographies)):
                h_list = H.tolist() if H is not None else None
                project.frames.append(FrameInfo(
                    path="",
                    index=i,
                    width=frame.shape[1],
                    height=frame.shape[0],
                    homography=h_list,
                ))

            if self._stitched_image is not None:
                project.canvas_width = self._stitched_image.shape[1]
                project.canvas_height = self._stitched_image.shape[0]

            # 保存标注
            from app.project.model import MapLabel, PipeSegment
            project.labels = [
                MapLabel(**l) for l in self._annotation_labels
            ]
            project.pipes = [
                PipeSegment(**p) for p in self._annotation_pipes
            ]

            storage = ProjectStorage(str(project_dir))
            storage.save(project, self._frames)

            self._project_path = project_dir
            self._update_window_title()
            self._log(f"项目已保存：{project_dir}")
        except Exception as exc:
            QMessageBox.warning(self, "保存失败", f"无法保存项目：{exc}")
            self._log(f"保存项目失败：{exc}")

    def _update_window_title(self) -> None:
        if self._project_path:
            self.setWindowTitle(f"协议映射 — {self._project_path.name}")
        else:
            self.setWindowTitle("协议映射")

    # --- 采集 ---

    def _populate_monitors(self) -> None:
        """使用 QScreen 获取物理像素分辨率，考虑 DPI 缩放。"""
        try:
            self._monitor_combo.clear()
            self._monitor_regions: list[dict] = []

            app = QGuiApplication.instance()
            if app is None:
                self._log("Qt 应用未初始化，无法获取显示器信息")
                return

            screens = app.screens()

            # 全部显示器（虚拟桌面）— 使用主屏 DPR 换算物理像素
            primary_dpr = app.primaryScreen().devicePixelRatio()
            all_geo = app.primaryScreen().virtualGeometry()
            phys_w = int(all_geo.width() * primary_dpr)
            phys_h = int(all_geo.height() * primary_dpr)
            phys_x = int(all_geo.x() * primary_dpr)
            phys_y = int(all_geo.y() * primary_dpr)
            all_region = {
                "left": phys_x, "top": phys_y,
                "width": phys_w, "height": phys_h,
            }
            self._monitor_regions.append(all_region)
            self._monitor_combo.addItem(
                f"全部显示器（{phys_w}×{phys_h}）",
                0,
            )

            # 逐个屏幕
            for i, screen in enumerate(screens, start=1):
                geo = screen.geometry()
                dpr = screen.devicePixelRatio()
                pw = int(geo.width() * dpr)
                ph = int(geo.height() * dpr)
                px = int(geo.x() * dpr)
                py = int(geo.y() * dpr)
                region = {"left": px, "top": py, "width": pw, "height": ph}
                self._monitor_regions.append(region)
                self._monitor_combo.addItem(
                    f"显示器 {i}（{pw}×{ph}，{int(dpr*100)}%）",
                    i,
                )
        except Exception as exc:
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
        """开始采集，重置跳过计数"""
        self._skipped_frames_count = 0
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
        msg = f"采集停止，共 {len(self._frames)} 帧"
        if self._skipped_frames_count > 0:
            msg += f" (已跳过 {self._skipped_frames_count} 个模糊帧)"
        self._log(msg + "。")
        self._update_ui_state()

    def _on_preview_tick(self) -> None:
        try:
            frame = self.capture.capture()
        except Exception:
            return
        if frame is not None:
            self._show_image(frame, self._preview_label)

    def _on_capture_tick(self) -> None:
        """采集一帧，增加模糊检测"""
        try:
            frame = self.recorder.capture_frame()
        except Exception:
            return
        if frame is None:
            return

        # 模糊检测
        if self._skip_blurry_frames:
            is_blur, blur_val = is_blurry(frame, self._blur_threshold)
            if is_blur:
                self._skipped_frames_count += 1
                self._log(f"跳过模糊帧 (值: {blur_val:.1f}, 已跳过: {self._skipped_frames_count})")
                return

        # 正常处理
        frame = self._preprocess_frame(frame)
        self._frames.append(frame)
        self._homographies.append(np.eye(3, dtype=np.float64))  # 默认单位矩阵
        self._frame_label.setText(f"已采集：{len(self._frames)} 帧")
        self._frame_list.addItem(f"帧 {len(self._frames):04d}")
        self._frame_list.setCurrentRow(self._frame_list.count() - 1)
        self._log(f"采集帧 {len(self._frames):04d}。")
        self._update_ui_state()
        self._refresh_frame_list_status()

    def _clear_frames(self) -> None:
        self._frames.clear()
        self._homographies.clear()
        self.recorder.clear()
        self._stitched_image = None
        self._frame_list.clear()
        self._frame_label.setText("已采集：0 帧")
        self._preview_label.setText("帧序列已清空")
        self._result_label.setText("生成全景图后显示结果")
        self._log("已清空帧序列。")
        self._update_ui_state()

    def _on_crop_changed(self) -> None:
        self._crop_top = self._crop_top_spin.value()
        self._crop_bottom = self._crop_bottom_spin.value()
        self._crop_left = self._crop_left_spin.value()
        self._crop_right = self._crop_right_spin.value()

    def _on_normalize_toggled(self, checked: bool) -> None:
        self._normalize_enabled = checked

    def _on_skip_blurry_toggled(self, checked: bool) -> None:
        self._skip_blurry_frames = checked

    def _on_blur_threshold_changed(self, value: int) -> None:
        self._blur_threshold = float(value)

    def _on_use_blend_toggled(self, checked: bool) -> None:
        self._use_blend = checked

    def _on_grid_toggled(self, checked: bool) -> None:
        self._grid_enabled = checked
        if self._stitched_image is not None:
            self._show_stitched_result()

    def _auto_detect_game_window(self) -> None:
        """自动检测游戏窗口"""
        success = self.capture.auto_detect_game_window()
        if success:
            info = self.capture.get_region_info()
            self._log(f"已检测到游戏窗口: {info.get('window_title', '未知')}")
            if self._preview_active:
                self._toggle_preview()
        else:
            self._log("未找到游戏窗口，请确保游戏正在运行")

    def _on_auto_preset_changed(self, index: int) -> None:
        keys = ["small", "medium", "large", "xlarge"]
        if 0 <= index < len(keys):
            self._auto_grid = CAPTURE_PRESETS[keys[index]]

    def _start_auto_capture(self) -> None:
        """启动自动网格采集（后台线程）"""
        # 先尝试自动检测游戏窗口
        if not self.capture.auto_detect_game_window():
            self._log("尝试游戏窗口检测...")
            if not self._apply_monitor_selection():
                return

        self._frames = []
        self._homographies = []
        self._stitched_image = None
        self._skipped_frames_count = 0
        self._frame_list.clear()
        self._frame_label.setText("准备中...")

        self._auto_active = True
        self._btn_auto_start.setEnabled(False)
        self._btn_auto_cancel.setEnabled(True)
        self._update_ui_state()

        self._auto_worker = AutoCaptureWorker(self.auto_capturer, self._auto_grid)
        self._auto_worker.progress.connect(self._on_auto_progress)
        self._auto_worker.log.connect(self._log)
        self._auto_worker.finished.connect(self._on_auto_finished)
        self._auto_worker.error.connect(self._on_auto_error)
        self._auto_worker.start()
        self._log(f"自动采集已启动：{self._auto_grid.rows}×{self._auto_grid.cols} 网格")

    def _cancel_auto_capture(self) -> None:
        self.auto_capturer.cancel()
        self._log("正在取消自动采集...")

    def _on_auto_progress(self, done: int, total: int) -> None:
        self._auto_progress_label.setText(f"进度：{done}/{total}")
        self.statusBar().showMessage(f"自动采集中... {done}/{total}")

    def _on_auto_finished(self, frames: list) -> None:
        self._auto_active = False
        self._auto_worker = None
        self._btn_auto_start.setEnabled(True)
        self._btn_auto_cancel.setEnabled(False)
        self._auto_progress_label.setText("")
        self._update_ui_state()

        if not frames:
            self._log("采集被取消或未采集到任何帧")
            return

        self._frames = [f.image for f in frames]
        self._homographies = [np.eye(3, dtype=np.float64) for _ in frames]

        self._frame_list.clear()
        for i, f in enumerate(frames):
            self._frame_list.addItem(f"帧 {i:04d} ({f.row},{f.col})")
        self._frame_label.setText(f"已采集：{len(self._frames)} 帧")

        if self._frames:
            self._show_image(self._frames[0], self._preview_label)
            self._tabs.setCurrentIndex(0)

        self._log(f"自动采集完成：{len(self._frames)} 帧")
        self._log("请点击「生成全景图」进行拼接")

    def _on_auto_error(self, msg: str) -> None:
        self._auto_active = False
        self._auto_worker = None
        self._btn_auto_start.setEnabled(True)
        self._btn_auto_cancel.setEnabled(False)
        self._auto_progress_label.setText("")
        self._update_ui_state()
        self._log(f"自动采集错误：{msg}")
        QMessageBox.warning(self, "采集失败", msg)

    def _preprocess_frame(self, frame):
        result = frame
        if any([self._crop_top, self._crop_bottom, self._crop_left, self._crop_right]):
            result = crop_ui(result, self._crop_top, self._crop_bottom,
                            self._crop_left, self._crop_right)
        if self._normalize_enabled:
            result = normalize_brightness(result)
        return result

    def _on_frame_selected(self, row: int) -> None:
        if 0 <= row < len(self._frames):
            self._show_image(self._frames[row], self._preview_label)
            self._tabs.setCurrentIndex(0)

    def _refresh_frame_list_status(self) -> None:
        """刷新帧列表：已标定的帧显示绿色"""
        for i in range(self._frame_list.count()):
            item = self._frame_list.item(i)
            if i < len(self._homographies):
                is_calibrated = not np.allclose(
                    self._homographies[i], np.eye(3), atol=1e-6
                )
                if is_calibrated:
                    item.setForeground(QColor("#44cc44"))
                    item.setText(f"帧 {i:04d} ✓")
                else:
                    item.setForeground(QColor("#b8c0cc"))
                    item.setText(f"帧 {i:04d}")

    # --- 锚点标定 ---

    def _open_calibration(self) -> None:
        if not self._frames:
            QMessageBox.warning(self, "提示", "没有可标定的帧。")
            return

        from app.ui.widgets.calibration_dialog import CalibrationDialog

        src_idx = max(0, self._frame_list.currentRow())
        base_frame = self._frames[0]

        dlg = CalibrationDialog(
            src_frames=self._frames,
            base_frame=base_frame,
            homographies=self._homographies,
            start_index=src_idx,
            parent=self,
        )
        if dlg.exec():
            self._homographies = dlg.all_homographies
            self._stitched_image = None
            self._result_label.setText("标定完成，请重新生成全景图")
            self._refresh_frame_list_status()
            calibrated = sum(
                1 for H in self._homographies
                if not np.allclose(H, np.eye(3), atol=1e-6)
            )
            self._log(f"锚点标定完成：{calibrated}/{len(self._homographies)} 帧已标定。")
            self._update_ui_state()

    # --- 拼接与导出 ---

    def _stitch_frames(self) -> None:
        if not self._frames:
            QMessageBox.warning(self, "提示", "没有可拼接的帧。")
            return

        self._stitch_from_homographies()

    def _stitch_from_homographies(self) -> None:
        """使用已有的 homographies 拼接帧，未标定帧自动尝试对齐"""
        if not self._frames:
            return

        if len(self._frames) == 1:
            self._stitched_image = self._frames[0].copy()
        else:
            self._log("开始拼接。")
            # 自动补全未标定帧
            base = self._frames[0]
            auto_count = 0
            for i, (frame, H) in enumerate(zip(self._frames, self._homographies)):
                if np.allclose(H, np.eye(3), atol=1e-6) and i > 0:
                    auto_H = auto_align(frame, base)
                    if auto_H is not None:
                        self._homographies[i] = auto_H
                        auto_count += 1

            if auto_count:
                self._log(f"自动对齐：{auto_count} 帧")
                self._refresh_frame_list_status()

            try:
                self._stitched_image = stitch_sequential(
                    self._frames, 
                    self._homographies,
                    use_blend=self._use_blend
                )
            except Exception as exc:
                QMessageBox.warning(self, "拼接失败", f"{exc}")
                self._log(f"拼接失败：{exc}")
                return

        self._show_stitched_result()
        self._tabs.setCurrentIndex(1)
        cal_count = sum(1 for H in self._homographies if not np.allclose(H, np.eye(3), atol=1e-6))
        self._log(f"全景图已生成（{cal_count}/{len(self._frames)} 帧已对齐）。")
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
        # 获取含标注的图像（_show_stitched_result 已加载标注数据）
        display = self._result_label.get_annotated_image()
        if display is None:
            display = self._stitched_image
        if self._grid_enabled:
            display = draw_grid(display, grid_size=200)
        export_png(display, path)
        self._log(f"已导出：{path}")

    # --- 辅助 ---

    def _apply_monitor_selection(self) -> bool:
        index = self._monitor_combo.currentData()
        if index is None or index >= len(self._monitor_regions):
            QMessageBox.warning(self, "提示", "请选择捕获源。")
            return False
        try:
            region = self._monitor_regions[index]
            self.capture.set_custom_region(
                region["left"], region["top"],
                region["width"], region["height"],
            )
        except Exception as exc:
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

    def _show_stitched_result(self) -> None:
        if self._stitched_image is None:
            return
        display = self._stitched_image
        if self._grid_enabled:
            display = draw_grid(display, grid_size=200)
        self._result_label.set_image(display)
        # 恢复已有标注
        pipes = [PipeData.from_dict(p.to_dict() if hasattr(p, 'to_dict') else p)
                 for p in self._annotation_pipes]
        labels = [LabelData.from_dict(l.to_dict() if hasattr(l, 'to_dict') else l)
                  for l in self._annotation_labels]
        self._result_label.load_data(pipes, labels)

    def _on_pipe_type_changed(self, text: str) -> None:
        self._result_label.set_pipe_preset(text)

    def _on_annotation_changed(self) -> None:
        pipes = self._result_label.get_pipes()
        labels = self._result_label.get_labels()
        self._annotation_pipes = [p.to_dict() for p in pipes]
        self._annotation_labels = [l.to_dict() for l in labels]

    def _clear_annotations(self) -> None:
        self._result_label.clear_all()
        self._annotation_pipes = []
        self._annotation_labels = []

    def _update_ui_state(self) -> None:
        recording = self.recorder.is_recording
        has_frames = bool(self._frames)
        has_result = self._stitched_image is not None
        busy = recording or self._auto_active

        self._btn_preview.setEnabled(not busy)
        self._btn_start.setEnabled(not busy and self._monitor_combo.count() > 0)
        self._btn_stop.setEnabled(recording)
        self._btn_auto_start.setEnabled(not busy)
        self._btn_auto_cancel.setEnabled(self._auto_active)
        self._btn_clear.setEnabled(has_frames and not busy)
        self._btn_align.setEnabled(has_frames and not busy)
        self._btn_stitch.setEnabled(has_frames and not busy)
        self._btn_export.setEnabled((has_frames or has_result) and not busy)
        self._act_save.setEnabled(has_frames and not busy)
        self._act_save_as.setEnabled(has_frames and not busy)

        if self._auto_active:
            self._status_label.setText("自动采集中...")
        elif recording:
            self._status_label.setText("采集中")
        else:
            self._status_label.setText("就绪")

    def _log(self, message: str) -> None:
        self.statusBar().showMessage(message)
        self._log_box.append(message)

    def closeEvent(self, event) -> None:  # noqa: N802
        self._capture_timer.stop()
        self._preview_timer.stop()
        if self._auto_worker and self._auto_worker.isRunning():
            self.auto_capturer.cancel()
            self._auto_worker.wait(3000)
        self.capture.release()
        super().closeEvent(event)

    def _load_project_quiet(self, path: Path) -> None:
        """静默加载项目（启动时 --project 参数调用），失败弹窗."""
        try:
            # 支持 project.json 路径或项目目录
            if path.is_file():
                project_dir = path.parent
            else:
                project_dir = path
            storage = ProjectStorage(str(project_dir))
            project, frames = storage.load()

            self._frames = frames
            self._homographies = [
                np.array(fi.homography, dtype=np.float64)
                if fi.homography else np.eye(3, dtype=np.float64)
                for fi in project.frames
            ]
            self._stitched_image = None
            self._project_path = project_dir
            self.recorder.clear()

            self._frame_list.clear()
            for fi in project.frames:
                self._frame_list.addItem(f"帧 {fi.index:04d}")
            self._frame_label.setText(f"已采集：{len(self._frames)} 帧")

            if self._frames:
                self._show_image(self._frames[0], self._preview_label)
                self._tabs.setCurrentIndex(0)
                self._frame_list.setCurrentRow(0)

            self._update_window_title()
            self._update_ui_state()
            self._log(f"已加载项目：{project.name}（{len(self._frames)} 帧）")

            # 恢复标注
            from dataclasses import asdict as _asdict
            self._annotation_labels = [_asdict(l) for l in project.labels]
            self._annotation_pipes = [_asdict(p) for p in project.pipes]

        except Exception as exc:
            from PySide6.QtWidgets import QMessageBox
            QMessageBox.warning(
                self, "加载失败",
                f"无法加载项目：\n{exc}",
            )

