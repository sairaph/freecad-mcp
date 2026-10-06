"""A property the object does not have: the error names the nearest ones and, for the booleans, the type that has the name."""

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
    spec = importlib.util.spec_from_file_location("_property_mapper_unknown_test", ADDON / "rpc_server" / "property_mapper.py")
    module = importlib.util.module_from_spec(spec)
    monkeypatch.setitem(sys.modules, spec.name, module)
    spec.loader.exec_module(module)
    return module


class Obj:
    """An object with some properties; setting an unknown one raises as FreeCAD does."""

    def __init__(self, type_id, names, hidden=()):
        self.TypeId, self.Name = type_id, "X"
        self.PropertiesList = list(names)
        self._hidden = set(hidden)
        for n in names:
            object.__setattr__(self, n, 1)

    def getPropertyStatus(self, name):
        return ["Hidden"] if name in self._hidden else []

    def __setattr__(self, name, value):
        if name.startswith("_") or name in ("TypeId", "Name", "PropertiesList") or name in self.PropertiesList:
            object.__setattr__(self, name, value)
        else:
            raise AttributeError(f"'PrimitivePy' object has no attribute '{name}'")


BOX = ["Length", "Width", "Height", "Label", "Placement", "Shape"]


def message(mapper, obj, props):
    with pytest.raises(ValueError) as error:
        mapper.set_object_property(None, obj, props)
    return str(error.value)


def test_a_misspelt_property_names_the_nearest_ones(mapper) -> None:
    text = message(mapper, Obj("Part::Box", BOX), {"Lenght": 10})
    assert "Part::Box 'X' has no property 'Lenght'. Nearest properties: Length, Height." in text
    assert "Nearest properties: Height." in message(mapper, Obj("Part::Box", BOX), {"Hieght": 3})


def test_at_most_three_and_never_a_hidden_property(mapper) -> None:
    names = ["Radius", "Radius1", "Radius2", "Radius3", "Radiuz"]
    text = message(mapper, Obj("Part::Cone", names, hidden={"Radiuz"}), {"Radius0": 1})
    near = text.split("Nearest properties: ")[1].rstrip(".").split(", ")
    assert len(near) == 3 and "Radiuz" not in near


def test_a_name_far_from_everything_points_at_get_object(mapper) -> None:
    text = message(mapper, Obj("Part::Box", BOX), {"Zzzzzzzz": 1})
    assert "has no property 'Zzzzzzzz'. get_object lists its properties." in text


@pytest.mark.parametrize("type_id", ["Part::Common", "Part::Cut", "Part::Fuse"])
def test_shapes_on_a_two_input_boolean_names_the_types_that_take_it(mapper, type_id) -> None:
    text = message(mapper, Obj(type_id, ["Base", "Tool", "Shape", "Label"]), {"Shapes": ["A", "A"]})
    assert f"{type_id} takes Base and Tool; Part::MultiCommon and Part::MultiFuse take Shapes." in text
    assert "Nearest" not in text


@pytest.mark.parametrize("type_id", ["Part::MultiFuse", "Part::MultiCommon"])
def test_base_and_tool_on_a_multi_boolean_name_the_reverse(mapper, type_id) -> None:
    text = message(mapper, Obj(type_id, ["Shapes", "Shape", "Label"]), {"Base": "A", "Tool": "B"})
    assert text.count(f"{type_id} takes Shapes, a list of objects; Part::Common, Part::Cut and Part::Fuse take Base and Tool.") == 2


def test_a_known_property_with_a_bad_value_keeps_its_own_error(mapper) -> None:
    class Strict(Obj):
        def __setattr__(self, name, value):
            if name == "Length":
                raise AttributeError("custom failure")
            super().__setattr__(name, value)

    text = message(mapper, Strict("Part::Box", BOX), {"Length": 4})
    assert "custom failure" in text and "no property" not in text


def test_a_swapped_pair_of_letters_is_one_edit(mapper) -> None:
    assert mapper._edit_distance("lenght", "length") == 1
    assert mapper._edit_distance("kitten", "sitting") == 3
    assert mapper._edit_distance("", "abc") == 3
