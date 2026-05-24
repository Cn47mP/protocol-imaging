from app.project.model import AnchorPoint, Annotation, FrameInfo, Project


def test_project_roundtrip_dict():
    project = Project(
        name="测试项目",
        frames=[FrameInfo(path="frames/0001.png", index=1, width=1920, height=1080)],
        anchors=[AnchorPoint(src_x=1, src_y=2, dst_x=3, dst_y=4)],
        annotations=[Annotation(text="设备A", x=10, y=20)],
        output_path="output/result.png",
    )

    restored = Project.from_dict(project.to_dict())

    assert restored.name == "测试项目"
    assert restored.frames[0].path == "frames/0001.png"
    assert restored.anchors[0].dst_x == 3
    assert restored.annotations[0].text == "设备A"
    assert restored.output_path == "output/result.png"
