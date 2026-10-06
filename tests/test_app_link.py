"""An App::Link where FreeCAD needs the object itself is refused before FreeCAD sees it."""

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
    spec = importlib.util.spec_from_file_location("_property_mapper_link_test", ADDON / "rpc_server" / "property_mapper.py")
    module = importlib.util.module_from_spec(spec)
    monkeypatch.setitem(sys.modules, spec.name, module)
    spec.loader.exec_module(module)
    return module


class Doc:
    def __init__(self, *objects):
        self.objects = {o.Name: o for o in objects}

    def getObject(self, name):
        return self.objects.get(name)


def make_link(target):
    return types.SimpleNamespace(Name="Beam_link", TypeId="App::Link", getLinkedObject=lambda: target)


class Constraint:
    """A FEM constraint: setting a link on it is recorded, never reaching FreeCAD."""

    def __init__(self, props, kind="single"):
        self.PropertiesList = list(props)
        self.set = {}

    def __setattr__(self, name, value):
        if name in ("PropertiesList", "set"):
            object.__setattr__(self, name, value)
        else:
            self.set[name] = value

    def __getattr__(self, name):
        if name in self.PropertiesList:
            return None
        raise AttributeError(name)

    def getTypeIdOfProperty(self, name):
        return {"Shape": "App::PropertyLink", "Direction": "App::PropertyLinkSub", "References": "App::PropertyLinkSubList"}[name]


def test_references_on_an_app_link_are_refused_naming_the_linked_object(mapper) -> None:
    beam = types.SimpleNamespace(Name="Beam", TypeId="Part::Box")
    doc = Doc(beam, make_link(beam))
    fixed = Constraint(["References"])
    with pytest.raises(ValueError, match="Beam_link is an App::Link: point References at Beam, the object it links"):
        mapper.set_object_property(doc, fixed, {"References": [{"object_name": "Beam_link", "faces": ["Face1"]}]})
    assert fixed.set == {}
    mapper.set_object_property(doc, fixed, {"References": [["Beam", "Face1"]]})
    assert fixed.set["References"] == [(beam, "Face1")]


def test_a_force_direction_on_an_app_link_is_refused(mapper) -> None:
    beam = types.SimpleNamespace(Name="Beam", TypeId="Part::Box")
    force = Constraint(["Direction"])
    with pytest.raises(ValueError, match="Beam_link is an App::Link: point Direction at Beam"):
        mapper.set_object_property(Doc(beam, make_link(beam)), force, {"Direction": ["Beam_link", "Edge5"]})
    assert force.set == {}


def test_a_gmsh_mesh_shape_on_an_app_link_is_refused_but_a_plain_link_property_is_not(mapper) -> None:
    beam = types.SimpleNamespace(Name="Beam", TypeId="Part::Box")
    doc = Doc(beam, make_link(beam))
    mesh = Constraint(["Shape", "CharacteristicLengthMax"])
    with pytest.raises(ValueError, match="Beam_link is an App::Link: point Shape at Beam"):
        mapper.set_object_property(doc, mesh, {"Shape": "Beam_link"})
    assert mesh.set == {}
    mapper.set_object_property(doc, mesh, {"Shape": "Beam"})
    assert mesh.set == {"Shape": beam}
    other = Constraint(["Shape"])
    mapper.set_object_property(doc, other, {"Shape": "Beam_link"})
    assert other.set["Shape"].TypeId == "App::Link"


def test_a_link_whose_target_cannot_be_read_still_gets_a_message(mapper) -> None:
    broken = types.SimpleNamespace(Name="L", TypeId="App::Link", getLinkedObject=lambda: (_ for _ in ()).throw(RuntimeError("x")))
    with pytest.raises(ValueError, match="L is an App::Link: point References at the object it links"):
        mapper.reject_app_link(broken, "References")


@pytest.mark.parametrize("type_id", ["App::LinkElement", "App::LinkGroup", "App::LinkPython", "App::LinkElementPython", "App::LinkGroupPython"])
def test_every_link_kind_is_refused(mapper, type_id) -> None:
    beam = types.SimpleNamespace(Name="Beam", TypeId="Part::Box")
    link = types.SimpleNamespace(Name="L", TypeId=type_id, getLinkedObject=lambda: beam)
    with pytest.raises(ValueError, match=f"L is an {type_id}: point References at"):
        mapper.reject_app_link(link, "References")
    for other in ("Part::Box", "Part::Cut", "PartDesign::Body", "Fem::AnalysisPython"):
        mapper.reject_app_link(types.SimpleNamespace(Name="X", TypeId=other), "References")


def test_a_group_of_links_is_told_to_name_the_objects_it_groups(mapper) -> None:
    group = types.SimpleNamespace(Name="G", TypeId="App::LinkGroup", getLinkedObject=lambda: None)
    with pytest.raises(ValueError, match="point References at the objects it groups"):
        mapper.reject_app_link(group, "References")


def test_a_netgen_mesh_shape_on_a_link_is_refused_too(mapper) -> None:
    beam = types.SimpleNamespace(Name="Beam", TypeId="Part::Box")
    doc = Doc(beam, make_link(beam))
    netgen = Constraint(["Shape", "MaxSize"])
    with pytest.raises(ValueError, match="Beam_link is an App::Link: point Shape at Beam"):
        mapper.set_object_property(doc, netgen, {"Shape": "Beam_link"})
    assert netgen.set == {}
