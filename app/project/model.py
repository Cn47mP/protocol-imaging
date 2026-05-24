"""
协议映射 · 项目模型
保存截图帧、锚点、标注信息的项目文件
"""

import json
from dataclasses import asdict, dataclass, field


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
    frames: list[FrameInfo] = field(default_factory=list)
    anchors: list[AnchorPoint] = field(default_factory=list)
    annotations: list[Annotation] = field(default_factory=list)
    output_path: str = ""

    def to_dict(self) -> dict:
        return asdict(self)

    @classmethod
    def from_dict(cls, data: dict) -> "Project":
        payload = dict(data)
        payload["frames"] = [
            frame if isinstance(frame, FrameInfo) else FrameInfo(**frame)
            for frame in payload.get("frames", [])
        ]
        payload["anchors"] = [
            anchor if isinstance(anchor, AnchorPoint) else AnchorPoint(**anchor)
            for anchor in payload.get("anchors", [])
        ]
        payload["annotations"] = [
            annotation if isinstance(annotation, Annotation) else Annotation(**annotation)
            for annotation in payload.get("annotations", [])
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
