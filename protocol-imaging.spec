# -*- mode: python ; coding: utf-8 -*-
# PyInstaller spec — protocol-imaging

_EXCLUDES = [
    # PySide6 模块
    'PySide6.QtQml', 'PySide6.QtQuick', 'PySide6.QtQuickWidgets',
    'PySide6.Qt3DCore', 'PySide6.Qt3DRender', 'PySide6.Qt3DInput',
    'PySide6.Qt3DLogic', 'PySide6.Qt3DExtras', 'PySide6.Qt3DAnimation',
    'PySide6.QtBluetooth', 'PySide6.QtMultimedia', 'PySide6.QtMultimediaWidgets',
    'PySide6.QtNfc', 'PySide6.QtPositioning', 'PySide6.QtQuick3D',
    'PySide6.QtRemoteObjects', 'PySide6.QtSensors', 'PySide6.QtSerialBus',
    'PySide6.QtSerialPort', 'PySide6.QtSvg', 'PySide6.QtSvgWidgets',
    'PySide6.QtTest', 'PySide6.QtTextToSpeech', 'PySide6.QtWebChannel',
    'PySide6.QtWebEngine', 'PySide6.QtWebEngineCore', 'PySide6.QtWebEngineWidgets',
    'PySide6.QtWebSockets', 'PySide6.QtPdf', 'PySide6.QtPdfWidgets',
    'PySide6.QtHelp', 'PySide6.QtDesigner', 'PySide6.QtSql',
    'PySide6.QtStateMachine', 'PySide6.QtSpatialAudio', 'PySide6.QtHttpServer',
    'PySide6.QtOpcUa', 'PySide6.QtCharts', 'PySide6.QtDataVisualization',
    'PySide6.QtUiTools', 'PySide6.QtConcurrent',
    # 未使用的大型依赖
    'matplotlib', 'scipy', 'pandas', 'PIL.ImageTk', 'PIL.ImageQt',
    'tkinter', '_tkinter',
]

# 需要排除的大型 DLL（PySide6 hook 会强制收集，手动剔除）
_EXCLUDE_DLLS = [
    '*Qt6WebEngineCore*',     # 195 MB
    '*opengl32sw*',           # 20 MB
    '*Qt6Quick*',             # 6 MB
    '*Qt6Qml*',               # 5 MB
    '*Qt6Designer*',          # 5 MB
    '*Qt6Pdf*',               # 4 MB
    '*Qt6ShaderTools*',
    '*Qt63D*',
    '*Qt6Shader*',
    '*Qt6Labs*',
    '*Qt6Help*',
    '*avcodec*',
    '*avformat*',
    '*avutil*',
    '*swresample*',
    '*swscale*',
    '*d3dcompiler*',
    '*libcrypto*',
    '*libssl*',
    '*opencv_videoio_ffmpeg*',
    '*_avif*',
]

a = Analysis(
    ['app\\main.py'],
    pathex=[],
    binaries=[],
    datas=[],
    hiddenimports=['mss', 'mss.tools'],
    hookspath=[],
    hooksconfig={},
    runtime_hooks=[],
    excludes=_EXCLUDES,
    noarchive=False,
    optimize=0,
)

# 从收集的二进制文件中剔除不需要的 DLL
import fnmatch
_filtered = []
for dest, src, typ in a.binaries:
    skip = False
    for pat in _EXCLUDE_DLLS:
        if fnmatch.fnmatch(dest, pat) or fnmatch.fnmatch(src, pat):
            skip = True
            break
    if not skip:
        _filtered.append((dest, src, typ))
a.binaries = _filtered

pyz = PYZ(a.pure)

exe = EXE(
    pyz,
    a.scripts,
    a.binaries,
    a.datas,
    [],
    name='protocol-imaging',
    debug=False,
    bootloader_ignore_signals=False,
    strip=False,
    upx=True,
    upx_exclude=[],
    runtime_tmpdir=None,
    console=False,           # 无控制台窗口
    disable_windowed_traceback=False,
    argv_emulation=False,
    target_arch=None,
    codesign_identity=None,
    entitlements_file=None,
)
