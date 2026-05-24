"""
协议映射 · 项目持久化
"""

from pathlib import Path

from app.project.model import Project


class ProjectStorage:
    """项目文件管理器"""

    def __init__(self, base_dir: str):
        self.base_dir = Path(base_dir)
        self.base_dir.mkdir(parents=True, exist_ok=True)

    def save(self, project: Project, filename: str = "project.json") -> Path:
        """保存项目文件"""
        proj_path = self.base_dir / filename
        project.save(str(proj_path))
        return proj_path

    def load(self, filename: str = "project.json") -> Project:
        """加载项目文件"""
        proj_path = self.base_dir / filename
        return Project.load(str(proj_path))

    def list_projects(self) -> list[Path]:
        """列出所有项目文件"""
        return list(self.base_dir.glob("*.json"))
