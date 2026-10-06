"""A single sub-element link given an origin feature or datum with no sub-element (an origin axis as a pattern's Direction) is stored as FreeCAD's whole-object form; any other object is stored as before."""

import importlib.util
import sys
import types
from pathlib import Path

import pytest

ADDON = Path(__file__).resolve().parents[1] / "addon" / "FreeCADMCP"


@pytest.fixture
def mapper(monkeypatch: pytest.MonkeyPatch):
    monkeypatch.syspath_prepend(str(ADDON))
    freecad = types.ModuleType("FreeCAD")
    freecad.Console = types.SimpleNamespace(PrintError=lambda _message: None)
    freecad.Vector = type("Vector", (), {})
    monkeypatch.setitem(sys.modules, "FreeCAD", freecad)
    spec = importlib.util.spec_from_file_location("_property_mapper_whole_test", ADDON / "rpc_server" / "property_mapper.py")
    module = importlib.util.module_from_spec(spec)
    monkeypatch.setitem(sys.modules, spec.name, module)
    spec.loader.exec_module(module)
    return module


AXIS = types.SimpleNamespace(Name="X_Axis", TypeId="App::Line")
SKETCH = types.SimpleNamespace(Name="Sketch", TypeId="Sketcher::SketchObject")
LINK = types.SimpleNamespace(Name="Axis_link", TypeId="App::Link", getLinkedObject=lambda: AXIS)


class Doc:
    def getObject(self, name):
        return {"X_Axis": AXIS, "Sketch": SKETCH, "Axis_link": LINK}.get(name)


class Feature:
    """A feature whose assignments are recorded: Direction and Profile are sub-element links, Originals a list of links."""

    TYPES = {
        "Direction": "App::PropertyLinkSub",
        "Profile": "App::PropertyLinkSub",
        "References": "App::PropertyLinkSubList",
        "Originals": "App::PropertyLinkList",
    }

    def __init__(self):
        self.PropertiesList = list(self.TYPES)
        self.set = {}

    def __setattr__(self, name, value):
        if name in ("PropertiesList", "set"):
            object.__setattr__(self, name, value)
        else:
            self.set[name] = value

    def __getattr__(self, name):
        if name in self.TYPES:
            return None
        raise AttributeError(name)

    def getTypeIdOfProperty(self, name):
        return self.TYPES[name]


@pytest.mark.parametrize("value", ["X_Axis", ["X_Axis", ""], ["X_Axis", [""]], ("X_Axis", "")])
def test_every_whole_object_form_of_an_origin_feature_is_stored_with_an_empty_sub_element(mapper, value) -> None:
    pattern = Feature()
    mapper.set_object_property(Doc(), pattern, {"Direction": value})
    assert pattern.set == {"Direction": (AXIS, [""])}


def test_any_other_object_is_stored_with_no_sub_element_as_before(mapper) -> None:
    pad = Feature()
    mapper.set_object_property(Doc(), pad, {"Profile": "Sketch"})
    assert pad.set == {"Profile": SKETCH}
    for value in (["Sketch", ""], ["Sketch", [""]]):
        again = Feature()
        mapper.set_object_property(Doc(), again, {"Profile": value})
        assert again.set == {"Profile": SKETCH}


def test_a_real_sub_element_is_unchanged(mapper) -> None:
    pattern = Feature()
    mapper.set_object_property(Doc(), pattern, {"Direction": ["Sketch", "Edge1"]})
    assert pattern.set == {"Direction": (SKETCH, ["Edge1"])}


def test_a_missing_object_or_an_app_link_is_refused(mapper) -> None:
    pattern = Feature()
    with pytest.raises(ValueError, match="Referenced object 'Nope' not found"):
        mapper.set_object_property(Doc(), pattern, {"Direction": "Nope"})
    with pytest.raises(ValueError, match="Axis_link is an App::Link: point Direction at X_Axis"):
        mapper.set_object_property(Doc(), pattern, {"Direction": "Axis_link"})
    assert pattern.set == {}


def test_references_still_refuse_an_empty_sub_element_and_lists_take_names(mapper) -> None:
    pattern = Feature()
    with pytest.raises(ValueError, match="Invalid reference entry"):
        mapper.set_object_property(Doc(), pattern, {"References": [["X_Axis", ""]]})
    with pytest.raises(ValueError, match="Invalid reference entry"):
        mapper.set_object_property(Doc(), pattern, {"References": [["X_Axis", [""]]]})
    mapper.set_object_property(Doc(), pattern, {"Originals": ["Sketch"]})
    assert pattern.set == {"Originals": [SKETCH]}


def test_a_bad_sub_element_link_error_names_the_bare_object_form(mapper) -> None:
    pattern = Feature()
    with pytest.raises(ValueError) as error:
        mapper.set_object_property(Doc(), pattern, {"Direction": ["X_Axis", "Edge1", "Edge2"]})
    assert 'or the bare object name for a whole object, such as an origin axis "X_Axis".' in str(error.value)
    assert "Invalid reference entry" in str(error.value)
