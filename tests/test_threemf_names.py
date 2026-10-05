"""3MF object names: the model part FreeCAD writes gets each label, nothing else changes."""

import importlib.util
import zipfile
from pathlib import Path
from xml.etree import ElementTree

import pytest

_path = Path(__file__).resolve().parent.parent / "addon" / "FreeCADMCP" / "rpc_server" / "threemf_names.py"
_spec = importlib.util.spec_from_file_location("threemf_names", _path)
threemf_names = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(threemf_names)

NS = {"m": "http://schemas.microsoft.com/3dmanufacturing/core/2015/02"}

# The model part as Mesh.export writes it on FreeCAD 1.1.3 (vertices cut down).
MODEL = """<?xml version="1.0" encoding="UTF-8"?>
<model unit="millimeter" xml:lang="en-US" xmlns="http://schemas.microsoft.com/3dmanufacturing/core/2015/02">
 <metadata name="Application">FreeCAD</metadata>
 <resources>
  <object id="1" type="model">
   <mesh>
    <vertices>
     <vertex x="0" y="0" z="0" />
    </vertices>
   </mesh>
  </object>
  <object id="2" type="model">
   <mesh>
    <vertices>
     <vertex x="1" y="1" z="1" />
    </vertices>
   </mesh>
  </object>
 </resources>
 <build>
  <item objectid="1" transform="1 0 0 0 1 0 0 0 1 0 0 0" />
  <item objectid="2" transform="1 0 0 0 1 0 0 0 1 0 0 0" />
 </build>
</model>
"""


def write_3mf(path: Path, model: str = MODEL) -> None:
    with zipfile.ZipFile(path, "w", zipfile.ZIP_DEFLATED) as z:
        z.writestr("3D/3dmodel.model", model)
        z.writestr("_rels/.rels", "<Relationships/>")
        z.writestr("[Content_Types].xml", "<Types/>")


def object_names(path: Path) -> list[str | None]:
    root = ElementTree.fromstring(zipfile.ZipFile(path).read("3D/3dmodel.model"))
    return [o.get("name") for o in root.findall(".//m:object", NS)]


def test_each_object_gets_its_label_in_export_order(tmp_path: Path) -> None:
    p = tmp_path / "a.3mf"
    write_3mf(p)
    threemf_names.name_objects(str(p), ["Left half", "Right half"])
    assert object_names(p) == ["Left half", "Right half"]


def test_only_the_name_attributes_are_added(tmp_path: Path) -> None:
    p = tmp_path / "a.3mf"
    write_3mf(p)
    threemf_names.name_objects(str(p), ["A", "B"])
    z = zipfile.ZipFile(p)
    assert z.testzip() is None
    assert z.namelist() == ["3D/3dmodel.model", "_rels/.rels", "[Content_Types].xml"]
    assert z.read("_rels/.rels") == b"<Relationships/>"
    model = z.read("3D/3dmodel.model").decode()
    assert model.replace(' name="A"', "").replace(' name="B"', "") == MODEL


def test_xml_special_characters_in_a_label_are_escaped(tmp_path: Path) -> None:
    p = tmp_path / "a.3mf"
    write_3mf(p)
    label = 'Rib & <"x"> \'y\'\tZä\x01'
    threemf_names.name_objects(str(p), [label, "B"])
    assert object_names(p) == ['Rib & <"x"> \'y\'\tZä', "B"]


def test_a_count_mismatch_refuses_and_leaves_the_file_as_it_was(tmp_path: Path) -> None:
    p = tmp_path / "a.3mf"
    write_3mf(p)
    before = p.read_bytes()
    with pytest.raises(ValueError, match="2 objects, not 3"):
        threemf_names.name_objects(str(p), ["A", "B", "C"])
    assert p.read_bytes() == before
    assert [f.name for f in tmp_path.iterdir()] == ["a.3mf"]


def test_a_name_already_there_is_kept(tmp_path: Path) -> None:
    p = tmp_path / "a.3mf"
    write_3mf(p, MODEL.replace('<object id="1" type="model">', '<object id="1" type="model" name="Own">'))
    threemf_names.name_objects(str(p), ["A", "B"])
    assert object_names(p) == ["Own", "B"]
