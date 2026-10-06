"""Children FreeCAD starts get no console window of their own."""

import types

import pytest

import test_gui_dispatch  # noqa: F401  puts the addon directory on sys.path
from rpc_server import console_windows as cw


class FakePopen:
    calls: list = []

    def __init__(self, *args, **kwargs):
        FakePopen.calls.append((args, kwargs))


@pytest.fixture(autouse=True)
def fresh():
    cw._installed = False
    FakePopen.calls = []
    yield
    cw._installed = False


def fake_module() -> types.SimpleNamespace:
    return types.SimpleNamespace(Popen=FakePopen)


def test_a_child_without_flags_gets_no_window() -> None:
    module = fake_module()
    assert cw.install(module, "win32", lambda: 0) is True
    module.Popen(["calc"])
    assert FakePopen.calls[0][1]["creationflags"] == cw.CREATE_NO_WINDOW


def test_other_flags_are_kept_and_added_to() -> None:
    module = fake_module()
    cw.install(module, "win32", lambda: 0)
    module.Popen(["a"], creationflags=0x200)
    assert FakePopen.calls[0][1]["creationflags"] == 0x200 | cw.CREATE_NO_WINDOW


@pytest.mark.parametrize("flag", [cw.CREATE_NEW_CONSOLE, cw.DETACHED_PROCESS, cw.CREATE_NO_WINDOW])
def test_explicit_console_flags_are_left_alone(flag: int) -> None:
    module = fake_module()
    cw.install(module, "win32", lambda: 0)
    module.Popen(["a"], creationflags=flag)
    assert FakePopen.calls[0][1]["creationflags"] == flag


def test_positional_creationflags_are_read() -> None:
    module = fake_module()
    cw.install(module, "win32", lambda: 0)
    module.Popen(["a"], -1, None, None, None, None, None, True, False, None, None, False, None, 0x200)
    assert FakePopen.calls[0][0][13] == 0x200 | cw.CREATE_NO_WINDOW
    assert "creationflags" not in FakePopen.calls[0][1]


def test_install_is_idempotent() -> None:
    module = fake_module()
    cw.install(module, "win32", lambda: 0)
    wrapped = module.Popen
    assert cw.install(module, "win32", lambda: 0) is True
    assert module.Popen is wrapped
    module.Popen(["a"])
    assert FakePopen.calls[0][1]["creationflags"] == cw.CREATE_NO_WINDOW


def test_nothing_changes_off_windows_or_with_a_console_of_its_own() -> None:
    module = fake_module()
    assert cw.install(module, "linux", lambda: 0) is False
    assert cw.install(module, "win32", lambda: 1234) is False
    assert module.Popen is FakePopen
