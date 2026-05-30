import numpy as np

from app.project.model import FrameInfo, Project
from app.project.storage import ProjectStorage


def test_project_storage_save_load(tmp_path):
    storage = ProjectStorage(tmp_path)
    project = Project(name="存档测试", frames=[FrameInfo("a.png", 0, 100, 80)])

    saved_path = storage.save(project)
    loaded_project, loaded_frames = storage.load()

    assert saved_path.exists()
    assert loaded_project.name == "存档测试"
    assert loaded_project.frames[0].width == 100


def test_project_storage_save_with_frames(tmp_path):
    storage = ProjectStorage(tmp_path)
    project = Project(name="帧测试")
    frame = np.zeros((80, 100, 3), dtype=np.uint8)

    storage.save(project, frames=[frame])
    loaded_project, loaded_frames = storage.load()

    assert len(loaded_frames) == 1
    assert loaded_frames[0].shape == (80, 100, 3)


def test_project_storage_list_projects(tmp_path):
    # 创建两个项目目录
    for name in ["A", "B"]:
        storage = ProjectStorage(tmp_path / name)
        storage.save(Project(name=name))

    project_dirs = ProjectStorage.list_projects(str(tmp_path))
    names = sorted(p.name for p in project_dirs)

    assert "A" in names
    assert "B" in names
