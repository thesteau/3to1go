# -*- mode: python ; coding: utf-8 -*-

from pathlib import Path


spec_dir = Path(globals().get("SPECPATH", Path.cwd() / "edge")).resolve()

a = Analysis(
    [str(spec_dir / "app" / "main.py")],
    pathex=[str(spec_dir)],
    binaries=[],
    datas=[
        (str(spec_dir / "app" / "api" / "static"), "app/api/static"),
        (str(spec_dir / "app" / "api" / "templates"), "app/api/templates"),
    ],
    hiddenimports=[],
    hookspath=[],
    hooksconfig={},
    runtime_hooks=[],
    excludes=[],
    noarchive=False,
)
pyz = PYZ(a.pure)

exe = EXE(
    pyz,
    a.scripts,
    [],
    exclude_binaries=True,
    name="relaycentralizer-edge",
    debug=False,
    bootloader_ignore_signals=False,
    strip=False,
    upx=False,
    console=True,
)

coll = COLLECT(
    exe,
    a.binaries,
    a.zipfiles,
    a.datas,
    strip=False,
    upx=False,
    upx_exclude=[],
    name="relaycentralizer-edge",
)
