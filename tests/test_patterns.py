"""Patterns join their Body after their Originals are set, and a new solid feature that is not the Body's Tip says so."""

import pytest

from test_object_validation import FakeObject, load_object_factory
from test_partdesign import Body, BodyDocument, Doc, Obj, pd  # noqa: F401  (pd is a fixture)


class PatternObject(FakeObject):
    """A pattern that records, in ``events``, when its properties are set."""

    def __init__(self, events):
        super().__init__(Name="Pat", TypeId="PartDesign::LinearPattern")
        object.__setattr__(self, "events", events)
        object.__setattr__(self, "PropertiesList", ["Originals", "Occurrences"])
        object.__setattr__(self, "Originals", [])
        object.__setattr__(self, "Occurrences", 1)

    def __setattr__(self, name, value):
        if name in ("Originals", "Occurrences"):
            self.events.append(f"set {name}")
        object.__setattr__(self, name, value)

    def getTypeIdOfProperty(self, name):
        return "App::PropertyLinkList" if name == "Originals" else "App::PropertyIntegerConstraint"


def test_a_pattern_is_given_its_originals_before_it_joins_its_body_so_it_becomes_the_tip() -> None:
    events: list[str] = []
    pattern = PatternObject(events)
    doc = BodyDocument(pattern)
    hole = FakeObject(Name="Hole", TypeId="PartDesign::SubtractiveCylinder")
    doc.Objects.append(hole)
    doc.body.Group.append(hole)

    def add_to_body(feature):
        events.append("add")
        doc.Objects.append(feature)
        doc.body.Group.append(feature)
        doc.body.Tip = feature

    doc.body.addObject = add_to_body
    with load_object_factory(doc) as factory:
        result = factory.create_object_gui(
            "Doc",
            factory.Object(name="Pat", type="PartDesign::LinearPattern", properties={"Occurrences": 4, "Originals": ["Hole"]}),
        )
    assert events == ["set Originals", "add", "set Occurrences"]
    assert doc.made_with is None and doc.add_object_calls == 1
    assert pattern.Originals == [hole]
    assert result["success"] is True
    assert result["body"] == {"name": "Body", "tip": "Pat"}
    assert "notes" not in result


def test_a_solid_feature_that_is_not_the_tip_says_so_plainly() -> None:
    pad = FakeObject(Name="Pad", TypeId="PartDesign::Pad")
    pad.isDerivedFrom = lambda type_id: type_id == "PartDesign::Feature"
    doc = BodyDocument(pad)
    doc.body.Tip = FakeObject(Name="Hole")

    def new_object(type_id, name):
        doc.Objects.append(pad)
        doc.body.Group.append(pad)
        return pad

    doc.body.newObject = new_object
    with load_object_factory(doc) as factory:
        result = factory.create_object_gui("Doc", factory.Object(name="Pad", type="PartDesign::Pad", properties={}))
    assert result["body"] == {"name": "Body", "tip": "Hole"}
    assert result["notes"] == ["Tip stays Hole: the Body's shape leaves out Pad; set the Body's Tip to Pad with update_object."]


def test_no_tip_note_for_the_tip_itself_a_sketch_or_a_datum(pd) -> None:
    body = Body("Body")
    feature = Obj("Pad", "PartDesign::Pad")
    feature.isDerivedFrom = lambda type_id: type_id == "PartDesign::Feature"
    body.Tip = feature
    assert pd.tip_note(body, feature) is None
    body.Tip = None
    assert pd.tip_note(body, feature) == "Tip stays unset: the Body's shape leaves out Pad; set the Body's Tip to Pad with update_object."
    sketch = Obj("Sketch", "Sketcher::SketchObject")
    sketch.isDerivedFrom = lambda type_id: False
    assert pd.tip_note(body, sketch) is None
    assert pd.tip_note(body, Obj("Plain")) is None


def test_a_transformation_with_no_originals_gets_no_note_but_one_with_originals_does(pd) -> None:
    body = Body("Body")
    body.Tip = Obj("Pad")
    pattern = Obj("Pat", "PartDesign::LinearPattern")
    pattern.isDerivedFrom = lambda type_id: type_id in ("PartDesign::Feature", "PartDesign::Transformed")
    pattern.Originals = []
    assert pd.tip_note(body, pattern) is None
    pattern.Originals = [Obj("Hole")]
    assert pd.tip_note(body, pattern) is not None


def test_the_originals_of_a_pattern_name_its_body(pd) -> None:
    first, second = Body("Body"), Body("Body2", 1)
    hole = Obj("Hole", "PartDesign::SubtractiveCylinder")
    second.Group.append(hole)
    doc = Doc(first, second, hole)
    plan = pd.plan_create(doc, "PartDesign::LinearPattern", {"Originals": ["Hole"], "Occurrences": 3}, None)
    assert plan.body is second
    with pytest.raises(ValueError, match=r"body_name 'Body' differs from the Body that Originals points into \('Body2'\)"):
        pd.plan_create(doc, "PartDesign::LinearPattern", {"Originals": ["Hole"]}, "Body")
