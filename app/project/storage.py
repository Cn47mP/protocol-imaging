"""
协议映射 · 项目持久化
项目保存为目录：<project_dir>/
    project.json          — 项目元数据
    frames/               — 帧 PNG 文件
"""

from __future__ import annotations

import shutil
from pathlib import Path

import cv2
import numpy as np

from app.project.model import FrameInfo, Project


class ProjectStorage:
    """项目文件管理器，以目录方式组织项目"""

    METADATA_FILE = "project.json"
    FRAMES_DIR = "frames"

    def __init__(self, project_dir: str):
        self.project_dir = Path(project_dir)

    # ── 目录管理 ──

    def init_dir(self) -> Path:
        """创建项目目录结构"""
        self.project_dir.mkdir(parents=True, exist_ok=True)
        (self.project_dir / self.FRAMES_DIR).mkdir(exist_ok=True)
        return self.project_dir

    @property
    def metadata_path(self) -> Path:
        return self.project_dir / self.METADATA_FILE

    @property
    def frames_dir(self) -> Path:
        return self.project_dir / self.FRAMES_DIR

    # ── 保存 ──

    def save(self, project: Project, frames: list[np.ndarray] | None = None) -> Path:
        """保存项目：导出帧 PNG + 写入 JSON 元数据"""
        self.init_dir()
        if frames:
            self._save_frame_images(frames, project)
        project.save(str(self.metadata_path))
        return self.metadata_path

    def _save_frame_images(self, frames: list[np.ndarray], project: Project) -> None:
        """导出帧图像到 frames/ 目录"""
        frames_dir = self.frames_dir
        for i, frame in enumerate(frames):
            fname = f"frame_{i:04d}.png"
            path = frames_dir / fname
            cv2.imwrite(str(path), frame)
            # 更新/创建 FrameInfo
            if i < len(project.frames):
                project.frames[i].path = f"{self.FRAMES_DIR}/{fname}"
                project.frames[i].width = frame.shape[1]
                project.frames[i].height = frame.shape[0]
            else:
                project.frames.append(FrameInfo(
                    path=f"{self.FRAMES_DIR}/{fname}",
                    index=i,
                    width=frame.shape[1],
                    height=frame.shape[0],
                ))

    # ── 加载 ──

    def load(self) -> tuple[Project, list[np.ndarray]]:
        """加载项目：返回 (Project, frames)"""
        project = Project.load(str(self.metadata_path))
        frames = self._load_frame_images(project)
        return project, frames

    def _load_frame_images(self, project: Project) -> list[np.ndarray]:
        """从 frames/ 目录加载帧图像"""
        frames = []
        for fi in sorted(project.frames, key=lambda f: f.index):
            path = self.project_dir / fi.path
            if path.exists():
                img = cv2.imread(str(path))
                if img is not None:
                    frames.append(img)
        return frames

    def load_project_only(self) -> Project:
        """仅加载项目元数据（不含帧图像）"""
        return Project.load(str(self.metadata_path))

    # ── 查询 ──

    @staticmethod
    def list_projects(base_dir: str) -> list[Path]:
        """列出 base_dir 下所有项目目录（含 project.json 的）"""
        base = Path(base_dir)
        if not base.exists():
            return []
        return sorted(
            p.parent for p in base.rglob(ProjectStorage.METADATA_FILE)
        )

    def cleanup(self) -> None:
        """删除项目目录"""
        if self.project_dir.exists():
            shutil.rmtree(self.project_dir)
