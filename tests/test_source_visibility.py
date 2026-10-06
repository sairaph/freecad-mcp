"""The source of a fillet, chamfer, extrusion, revolution, thickness, loft or sweep is hidden, as FreeCAD's own command does."""

import importlib.util
import sys
import types
from pathlib import Path

import pytest

MODULE = Path(__file__).resolve().parents[1] / "addon" / "FreeCADMCP" / "rpc_server" / "source_visibility.py"


@pytest.fixture
def hide_sources(monkeypatch: pytest.MonkeyPatch):
    spec = importlib.util.spec_from_file_location("_source_visibility_test", MODULE)
    module = importlib.util.module_from_spec(spec)
    monkeypatch.setitem(sys.modules, spec.name, module)
    spec.loader.exec_module(module)
    return module.hide_sources


def shape_object(name: str, visible: bool = True) -> types.SimpleNamespace:
    return types.SimpleNamespace(Name=name, ViewObject=types.SimpleNamespace(Visibility=visible))


def feature(type_id: str, **links) -> types.SimpleNamespace:
    return types.SimpleNamespace(TypeId=type_id, **links)


@pytest.mark.parametrize(
    "type_id, prop",
    [
        ("Part::Fillet", "Base"),
        ("Part::Chamfer", "Base"),
        ("Part::Extrusion", "Base"),
        ("Part::Revolution", "Source"),
    ],
)
def test_the_source_of_a_hiding_type_is_hidden_and_named(hide_sources, type_id: str, prop: str) -> None:
    source = shape_object("Outer")
    assert hide_sources(feature(type_id, **{prop: source}), None) == ["Outer"]
    assert source.ViewObject.Visibility is False


def test_thickness_hides_the_object_of_its_face_link(hide_sources) -> None:
    source = shape_object("Solid")
    assert hide_sources(feature("Part::Thickness", Faces=(source, ["Face1"])), None) == ["Solid"]
    assert source.ViewObject.Visibility is False


@pytest.mark.parametrize("type_id", ["Part::Mirroring", "Part::Offset", "Part::RuledSurface", "Part::Cut"])
def test_types_whose_command_leaves_the_source_shown_are_left_alone(hide_sources, type_id: str) -> None:
    source = shape_object("Box")
    assert hide_sources(feature(type_id, Base=source, Source=source), None) == []
    assert source.ViewObject.Visibility is True


def test_a_source_that_was_hidden_already_is_not_named(hide_sources) -> None:
    assert hide_sources(feature("Part::Fillet", Base=shape_object("Outer", visible=False)), None) == []


def test_an_update_hides_the_source_only_when_it_changed_the_source_link(hide_sources) -> None:
    source = shape_object("Outer")
    fillet = feature("Part::Fillet", Base=source)
    assert hide_sources(fillet, {"Radius": 3}) == []
    assert source.ViewObject.Visibility is True
    assert hide_sources(fillet, {"Base": "Outer"}) == ["Outer"]


def test_no_view_or_no_source_hides_nothing(hide_sources) -> None:
    assert hide_sources(feature("Part::Fillet", Base=types.SimpleNamespace(Name="X", ViewObject=None)), None) == []
    assert hide_sources(feature("Part::Fillet", Base=None), None) == []


def test_a_loft_hides_every_section(hide_sources) -> None:
    sections = [shape_object("Circle"), shape_object("Ellipse")]
    assert hide_sources(feature("Part::Loft", Sections=sections), None) == ["Circle", "Ellipse"]
    assert all(section.ViewObject.Visibility is False for section in sections)


def test_a_sweep_hides_its_profiles_and_its_spine(hide_sources) -> None:
    profile, path = shape_object("Profile"), shape_object("Path")
    assert hide_sources(feature("Part::Sweep", Sections=[profile], Spine=(path, ["Edge1"])), None) == ["Profile", "Path"]
    assert profile.ViewObject.Visibility is False and path.ViewObject.Visibility is False


def test_a_sweep_spine_given_as_a_bare_object_is_hidden(hide_sources) -> None:
    path = shape_object("Path")
    assert hide_sources(feature("Part::Sweep", Sections=[], Spine=path), None) == ["Path"]


def test_an_update_of_the_spine_leaves_the_sections_alone(hide_sources) -> None:
    profile, path = shape_object("Profile"), shape_object("Path")
    sweep = feature("Part::Sweep", Sections=[profile], Spine=(path, ["Edge1"]))
    assert hide_sources(sweep, {"Spine": ["Path", ["Edge1"]]}) == ["Path"]
    assert profile.ViewObject.Visibility is True
