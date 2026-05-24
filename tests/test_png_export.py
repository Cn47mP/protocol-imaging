import cv2
import numpy as np

from app.export.png_export import export_png, export_with_annotations


def test_export_png_creates_file(tmp_path):
    image = np.zeros((8, 10, 3), dtype=np.uint8)
    output = tmp_path / "nested" / "result.png"

    result = export_png(image, str(output))

    assert result == str(output)
    assert output.exists()
    loaded = cv2.imread(str(output))
    assert loaded.shape == (8, 10, 3)


def test_export_with_annotations_creates_file(tmp_path):
    image = np.zeros((20, 20, 3), dtype=np.uint8)
    output = tmp_path / "annotated.png"

    result = export_with_annotations(image, str(output), grid=True, grid_size=10, labels=[("A", (2, 10))])

    assert result == str(output)
    assert output.exists()
