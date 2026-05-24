"""
协议映射 · 项目模型
保存截图帧、锚点、标注信息的项目文件
"""

from dataclasses import dataclass, field, asdict
from typing import List, Optional, Tuple
from pathlib import Path
import json


@dataclass
class AnchorPoint:
    """锚点坐标"""
    src_x: float
    src_y: float
    dst_x: float
    dst_y: float


@dataclass
class FrameInfo:
    """帧信息"""
    path: str       # 帧文件路径
    index: int      # 帧序号
    width: int
    height: int


@dataclass
class Annotation:
    """标注"""
    text: str
    x: int
    y: int


@dataclass
class Project:
    """项目文件"""
    name: str = "协议映射项目"
    version: str = "1.0"
    frames: List[FrameInfo] = field(default_factory=list)
    anchors: List[AnchorPoint] = field(default_factory=list)
    annotations: List[Annotation] = field(default_factory=list)
    output_path: str = ""

    def to_dict(self) -> dict:
        return asdict(self)

    @classmethod
    def from_dict(cls, data: dict) -> "Project":
        return cls(**data)

    def save(self, path: str):
        with open(path, "w", encoding="utf-8") as f:
            json.dump(self.to_dict(), f, ensure_ascii=False, indent=2)

    @classmethod
    def load(cls, path: str) -> "Project":
        with open(path, "r", encoding="utf-8") as f:
            data = json.load(f)
        return cls.from_dict(data)
