#!/usr/bin/env python3
"""协议映射 · 入口 — 启动参数 + 全局异常边界"""

from __future__ import annotations

import argparse
import logging
import sys
import traceback
from pathlib import Path

from PySide6.QtCore import Qt
from PySide6.QtWidgets import QApplication, QMessageBox

from app.ui.main_window import MainWindow


LOG_PATH = Path("logs/protocol-imaging.log")


def _setup_logging() -> None:
    LOG_PATH.parent.mkdir(parents=True, exist_ok=True)
    logging.basicConfig(
        filename=LOG_PATH,
        level=logging.INFO,
        format="%(asctime)s %(levelname)s %(name)s: %(message)s",
        encoding="utf-8",
    )


def _exception_hook(exc_type, exc_value, exc_tb):
    """全局未捕获异常 — 弹窗提示 + 日志记录."""
    tb_str = "".join(traceback.format_exception(exc_type, exc_value, exc_tb))
    logging.critical("Uncaught exception", exc_info=(exc_type, exc_value, exc_tb))
    print(f"[协议映射 · 异常] {tb_str}", file=sys.stderr)

    msg = QMessageBox()
    msg.setIcon(QMessageBox.Icon.Critical)
    msg.setWindowTitle("协议映射 · 异常")
    msg.setText(f"{exc_type.__name__}: {exc_value}")
    msg.setDetailedText(tb_str)
    msg.setStandardButtons(QMessageBox.StandardButton.Ok)
    msg.exec()


def main(argv: list[str] | None = None) -> None:
    _setup_logging()
    logging.info("Starting protocol-imaging")

    if argv is None:
        argv = sys.argv[1:]

    parser = argparse.ArgumentParser(
        prog="protocol-imaging",
        description="协议映射 — 终末地基地全景快照成像工具",
    )
    parser.add_argument(
        "--project", "-p",
        type=Path,
        metavar="PATH",
        help="启动后直接加载指定项目 (.json 或项目目录)",
    )
    parser.add_argument(
        "--debug", "-d",
        action="store_true",
        help="启用调试模式（全局异常弹窗）",
    )
    args = parser.parse_args(argv)

    app = QApplication(sys.argv)
    app.setApplicationName("协议映射")
    app.setOrganizationName("终末地工业")

    # 全局异常边界
    if args.debug:
        sys.excepthook = _exception_hook

    window = MainWindow()
    window.show()

    # 自动加载项目
    if args.project:
        path = args.project.resolve()
        if path.exists():
            window._load_project_quiet(path)
        else:
            QMessageBox.warning(
                window, "项目不存在",
                f"指定的项目路径不存在：\n{path}",
            )

    sys.exit(app.exec())


if __name__ == "__main__":
    main()
