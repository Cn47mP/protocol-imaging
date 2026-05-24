from app.project.model import FrameInfo, Project
from app.project.storage import ProjectStorage


def test_project_storage_save_load(tmp_path):
    storage = ProjectStorage(tmp_path)
    project = Project(name="存档测试", frames=[FrameInfo("a.png", 0, 100, 80)])

    saved_path = storage.save(project)
    loaded = storage.load()

    assert saved_path.exists()
    assert loaded.name == "存档测试"
    assert loaded.frames[0].width == 100


def test_project_storage_list_projects(tmp_path):
    storage = ProjectStorage(tmp_path)
    storage.save(Project(name="A"), "a.json")
    storage.save(Project(name="B"), "b.json")

    names = sorted(path.name for path in storage.list_projects())

    assert names == ["a.json", "b.json"]
