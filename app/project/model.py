"""
协议映射 · 项目模型
保存截图帧、单应性矩阵、标注信息的项目文件
"""

from __future__ import annotations

import json
from dataclasses import asdict, dataclass, field
from pathlib import Path


@dataclass
class FrameInfo:
    """帧信息"""
    path: str       # 帧文件相对路径（相对于项目目录）
    index: int      # 帧序号
    width: int
    height: int
    homography: list[list[float]] | None = None  # 3x3 单应性矩阵


@dataclass
class MapLabel:
    """地图标注标签"""
    x: float
    y: float
    text: str
    color: str = "#44ff44"  # hex 颜色


@dataclass
class PipeSegment:
    """暗管线段"""
    points: list[list[float]]  # [[x, y], ...] 像素坐标
    color: str = "#4488ff"     # hex 颜色
    label: str = ""             # 管线类型名称
    thickness: int = 2


@dataclass
class Project:
    """项目文件"""
    name: str = "协议映射项目"
    version: str = "1.0"
    frames: list[FrameInfo] = field(default_factory=list)
    labels: list[MapLabel] = field(default_factory=list)
    pipes: list[PipeSegment] = field(default_factory=list)
    output_path: str = ""
    canvas_width: int = 0
    canvas_height: int = 0

    def to_dict(self) -> dict:
        return asdict(self)

    @classmethod
    def from_dict(cls, data: dict) -> "Project":
        payload = dict(data)
        payload["frames"] = [
            frame if isinstance(frame, FrameInfo) else FrameInfo(**frame)
            for frame in payload.get("frames", [])
        ]
        payload["labels"] = [
            lbl if isinstance(lbl, MapLabel) else MapLabel(**lbl)
            for lbl in payload.get("labels", [])
        ]
        payload["pipes"] = [
            p if isinstance(p, PipeSegment) else PipeSegment(**p)
            for p in payload.get("pipes", [])
        ]
        return cls(**payload)

    def save(self, path: str):
        with open(path, "w", encoding="utf-8") as f:
            json.dump(self.to_dict(), f, ensure_ascii=False, indent=2)

    @classmethod
    def load(cls, path: str) -> "Project":
        with open(path, encoding="utf-8") as f:
            data = json.load(f)
        return cls.from_dict(data)
