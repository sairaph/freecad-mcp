"""The reply of create_object and update_object echoes the Material a FEM material object stores."""

import importlib.util
import sys
import types
from pathlib import Path

import pytest

ADDON = Path(__file__).resolve().parents[1] / "addon" / "FreeCADMCP"


@pytest.fixture
def fem_loads(monkeypatch):
    mapper = types.ModuleType("rpc_server.property_mapper")
    mapper.quantity_text = str
    monkeypatch.setitem(sys.modules, "rpc_server.property_mapper", mapper)
    spec = importlib.util.spec_from_file_location("_fem_loads_under_test", ADDON / "rpc_server" / "fem_loads.py")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def material(name="Mat", type_id="App::MaterialObjectPython", **values):
    return types.SimpleNamespace(Name=name, TypeId=type_id, PropertiesList=["Material"], Material=values)


def test_the_stored_material_is_echoed_in_a_fixed_order(fem_loads) -> None:
    obj = material(Name="Alu", Density="2700 kg/m^3", PoissonRatio="0.33", YoungsModulus="70 GPa")
    assert fem_loads.material_info(obj) == {
        "name": "Mat",
        "text": "Material Alu: YoungsModulus 70 GPa, PoissonRatio 0.33, Density 2700 kg/m^3",
    }


def test_a_material_without_a_name_or_values_is_still_described(fem_loads) -> None:
    assert fem_loads.material_info(material(YoungsModulus="1.5 GPa"))["text"] == "Material Mat: YoungsModulus 1.5 GPa"
    assert fem_loads.material_info(material(Name="Empty"))["text"] == "Material Empty: no stiffness or density stored."


def test_other_objects_have_no_material_echo(fem_loads) -> None:
    box = types.SimpleNamespace(Name="Box", TypeId="Part::Box", PropertiesList=["Length"])
    assert fem_loads.material_info(box) is None
    odd = types.SimpleNamespace(Name="X", TypeId="Part::Feature", PropertiesList=["Material"], Material={"Name": "x"})
    assert fem_loads.material_info(odd) is None
