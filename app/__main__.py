"""协议映射 — python -m app 入口

MaaEnd Go Service 调用方式: python -m app frames_dir/ --output result.py
独立使用方式: python -m app.main --preset medium
"""

from app.cli import main
import sys

if __name__ == "__main__":
    main(sys.argv[1:])
