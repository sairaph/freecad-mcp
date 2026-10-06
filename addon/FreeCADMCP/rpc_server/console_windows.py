"""Children FreeCAD starts do not open console windows.

FreeCAD's FEM tools start Gmsh and CalculiX with STARTUPINFO SW_HIDE, which
Windows Terminal (the default console host) ignores: every run flashes a
window. A GUI process with no console of its own can start a child with
CREATE_NO_WINDOW instead; the child's output still flows through pipes.
"""

import subprocess
import sys

CREATE_NO_WINDOW = 0x08000000
CREATE_NEW_CONSOLE = 0x00000010
DETACHED_PROCESS = 0x00000008
_EXPLICIT = CREATE_NO_WINDOW | CREATE_NEW_CONSOLE | DETACHED_PROCESS
# Popen's positional order: args, bufsize, executable, stdin, stdout, stderr,
# preexec_fn, close_fds, shell, cwd, env, universal_newlines, startupinfo, creationflags.
_FLAGS_POSITION = 13

_installed = False


def _own_console_window() -> int:
    import ctypes

    return int(ctypes.windll.kernel32.GetConsoleWindow() or 0)


def install(module=subprocess, platform: str = sys.platform, console_window=_own_console_window) -> bool:
    """Wrap ``module.Popen`` once (idempotent) so a child started without
    explicit console flags gets CREATE_NO_WINDOW. Returns True when the wrap is
    in place; nothing happens off Windows or when FreeCAD has a console of its
    own (started from a terminal, where children share it)."""
    global _installed
    if _installed:
        return True
    if platform != "win32":
        return False
    try:
        if console_window():
            return False
    except Exception:
        return False
    original = module.Popen

    class HiddenConsolePopen(original):
        def __init__(self, *args, **kwargs):
            if len(args) > _FLAGS_POSITION:
                flags = args[_FLAGS_POSITION] or 0
                if not flags & _EXPLICIT:
                    args = (*args[:_FLAGS_POSITION], flags | CREATE_NO_WINDOW, *args[_FLAGS_POSITION + 1 :])
            else:
                flags = kwargs.get("creationflags", 0) or 0
                if not flags & _EXPLICIT:
                    kwargs["creationflags"] = flags | CREATE_NO_WINDOW
            super().__init__(*args, **kwargs)

    module.Popen = HiddenConsolePopen
    _installed = True
    return True
