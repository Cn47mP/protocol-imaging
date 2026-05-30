from app.project.model import FrameInfo, MapLabel, PipeSegment, Project


def test_project_roundtrip_dict():
    project = Project(
        name="测试项目",
        frames=[FrameInfo(path="frames/0001.png", index=1, width=1920, height=1080)],
        labels=[MapLabel(x=10, y=20, text="设备A", color="#ff0000")],
        pipes=[PipeSegment(points=[[0, 0], [100, 100]], color="#4488ff", label="主管")],
        output_path="output/result.png",
    )

    restored = Project.from_dict(project.to_dict())

    assert restored.name == "测试项目"
    assert restored.frames[0].path == "frames/0001.png"
    assert restored.labels[0].text == "设备A"
    assert restored.labels[0].color == "#ff0000"
    assert restored.pipes[0].points == [[0, 0], [100, 100]]
    assert restored.pipes[0].label == "主管"
    assert restored.output_path == "output/result.png"
