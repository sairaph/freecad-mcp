"""create_object of Draft::ShapeString, Draft::OrthoArray and Draft::PolarArray through their factories."""

import os
import sys
import types

import pytest

from test_object_validation import FakeDocument, FakeObject, load_object_factory


class Vector:
    def __init__(self, x=0.0, y=0.0, z=0.0):
        self.x, self.y, self.z = x, y, z

    def __eq__(self, other):
        return (self.x, self.y, self.z) == (other.x, other.y, other.z)

    def __repr__(self):
        return f"Vector({self.x}, {self.y}, {self.z})"


class DraftStub:
    """The Draft factories: each records its call and returns a plain object."""

    def __init__(self, type_id="Part::FeaturePython"):
        self.calls = []
        self.type_id = type_id

    def make_shapestring(self, text, font, size):
        self.calls.append(("shapestring", text, font, size))
        made = FakeObject(Name="ShapeString", TypeId=self.type_id)
        made.FontFile = font
        return made

    def make_ortho_array(self, base, v_x, v_y, v_z, n_x, n_y, n_z, use_link=True):
        self.calls.append(("ortho", base.Name, v_x, v_y, v_z, n_x, n_y, n_z, use_link))
        return FakeObject(Name="OrthoArray", TypeId=self.type_id)

    def make_polar_array(self, base, number, angle, center, use_link=True):
        self.calls.append(("polar", base.Name, number, angle, center, use_link))
        return FakeObject(Name="PolarArray", TypeId=self.type_id)


@pytest.fixture
def draft(monkeypatch):
    stub = DraftStub()
    stub.monkeypatch = monkeypatch
    monkeypatch.setitem(sys.modules, "Draft", stub)
    return stub


def make(draft, doc, obj_type, properties, fonts=(), preference=""):
    """create_object_gui for ``obj_type`` where only the paths in ``fonts`` exist as files."""
    with load_object_factory(doc) as factory:
        freecad = sys.modules["FreeCAD"]
        freecad.Vector = Vector
        freecad.ParamGet = lambda _path: types.SimpleNamespace(GetString=lambda _key, default="": preference)
        draft.monkeypatch.setattr(os.path, "isfile", lambda path: path in fonts)
        return factory.create_object_gui("Doc", factory.Object(name="Made", type=obj_type, properties=properties))


def document(*others):
    doc = FakeDocument(FakeObject(Name="Made"))
    objects = {o.Name: o for o in others}
    doc.getObject = lambda name: objects.get(name)
    return doc


def test_a_shapestring_uses_the_font_it_is_given_and_the_reply_names_it(draft) -> None:
    result = make(draft, document(), "Draft::ShapeString", {"String": "PSU 12V", "Size": 8, "FontFile": "/fonts/a.ttf"}, fonts=("/fonts/a.ttf",))
    assert result["success"] is True
    assert draft.calls == [("shapestring", "PSU 12V", "/fonts/a.ttf", 8)]
    assert result["notes"] == ["Draft::ShapeString is a flat profile: extrude it with Part::Extrusion to make a solid.", "Font: /fonts/a.ttf"]
    assert "warning" not in result


def test_a_shapestring_without_a_font_takes_the_draft_preference_then_the_first_default_that_exists(draft) -> None:
    pick = lambda **kw: make(draft, document(), "Draft::ShapeString", {"String": "A"}, **kw)  # noqa: E731
    assert pick(fonts=("/pref.ttf", "C:/Windows/Fonts/arial.ttf"), preference="/pref.ttf")["notes"][-1] == "Font: /pref.ttf"
    # A preference that names no file is ignored; bold sans comes before regular.
    both = ("C:/Windows/Fonts/arialbd.ttf", "C:/Windows/Fonts/arial.ttf")
    assert pick(fonts=both, preference="/gone.ttf")["notes"][-1] == "Font: C:/Windows/Fonts/arialbd.ttf"
    assert pick(fonts=("/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf",))["notes"][-1] == "Font: /usr/share/fonts/truetype/dejavu/DejaVuSans.ttf"
    assert [call[3] for call in draft.calls] == [10, 10, 10]


def test_a_shapestring_with_no_font_or_a_missing_one_is_refused_and_creates_nothing(draft) -> None:
    result = make(draft, document(), "Draft::ShapeString", {"String": "A"})
    assert result == (
        "Draft::ShapeString needs FontFile, the path of a .ttf or .otf font on the computer running FreeCAD; "
        "no default font was found. Nothing was created."
    )
    result = make(draft, document(), "Draft::ShapeString", {"String": "A", "FontFile": "/no/such.ttf"}, fonts=("C:/Windows/Fonts/arial.ttf",))
    assert "FontFile '/no/such.ttf' does not exist" in result and result.endswith("Nothing was created.")
    assert draft.calls == []


@pytest.mark.parametrize(
    "properties, message",
    [
        ({}, "requires a 'String' property"),
        ({"String": ""}, "String must be the non-empty text"),
        ({"String": "A", "Size": 0}, "Size must be a number above 0"),
        ({"String": "A", "Size": "8 mm"}, "Size must be a number above 0"),
    ],
)
def test_bad_shapestring_values_are_refused(draft, properties, message) -> None:
    result = make(draft, document(), "Draft::ShapeString", properties, fonts=("C:/Windows/Fonts/arial.ttf",))
    assert message in result and "Nothing was created." in result


def test_an_ortho_array_takes_numbers_or_vectors_for_its_intervals(draft) -> None:
    pin = FakeObject(Name="Pin")
    result = make(draft, document(pin), "Draft::OrthoArray", {"Base": "Pin", "NumberX": 10, "IntervalX": 5, "NumberY": 2, "IntervalY": [0, 7, 1]})
    assert result["success"] is True
    assert draft.calls == [("ortho", "Pin", Vector(5, 0, 0), Vector(0, 7, 1), Vector(0, 0, 10), 10, 2, 1, False)]
    # An array is not a flat profile.
    assert "notes" not in result


def test_an_ortho_array_has_one_copy_per_axis_and_a_step_of_10_by_default(draft) -> None:
    make(draft, document(FakeObject(Name="Pin")), "Draft::OrthoArray", {"Base": "Pin", "IntervalZ": {"x": 1, "z": 4}})
    assert draft.calls == [("ortho", "Pin", Vector(10, 0, 0), Vector(0, 10, 0), Vector(1, 0, 4), 1, 1, 1, False)]


@pytest.mark.parametrize(
    "properties, message",
    [
        ({}, "requires a 'Base' property"),
        ({"Base": "Gone"}, "Referenced object 'Gone' not found"),
        ({"Base": "Pin", "NumberX": 0}, "NumberX must be a whole number of 1 or more"),
        ({"Base": "Pin", "NumberY": 2.5}, "NumberY must be a whole number of 1 or more"),
        ({"Base": "Pin", "IntervalX": "5 mm"}, "IntervalX must be"),
        ({"Base": "Pin", "IntervalZ": [1, 2, 3, 4]}, "IntervalZ must be"),
    ],
)
def test_bad_ortho_array_values_are_refused(draft, properties, message) -> None:
    result = make(draft, document(FakeObject(Name="Pin")), "Draft::OrthoArray", properties)
    assert message in result and "Nothing was created." in result
    assert draft.calls == []


def test_a_polar_array_defaults_to_a_full_turn_about_the_origin(draft) -> None:
    pin = FakeObject(Name="Pin")
    result = make(draft, document(pin), "Draft::PolarArray", {"Base": "Pin", "NumberPolar": 6})
    assert result["success"] is True and "notes" not in result
    assert draft.calls == [("polar", "Pin", 6, 360, Vector(0, 0, 0), False)]
    make(draft, document(pin), "Draft::PolarArray", {"Base": "Pin", "NumberPolar": 3, "Angle": 90, "Center": {"x": 5, "y": 5}})
    assert draft.calls[-1] == ("polar", "Pin", 3, 90, Vector(5, 5, 0), False)


def test_a_polar_array_needs_its_count(draft) -> None:
    result = make(draft, document(FakeObject(Name="Pin")), "Draft::PolarArray", {"Base": "Pin"})
    assert "requires a 'NumberPolar' property" in result
