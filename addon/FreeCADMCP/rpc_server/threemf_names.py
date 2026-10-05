"""Put object names into a 3MF file FreeCAD wrote.

FreeCAD's 3MF writer (Mesh.export) writes ``<object id="1" type="model">`` with
no ``name``, so a slicer lists the parts as "object 1" to "object N". The 3MF
core spec allows ``name`` on ``<object>``; this adds it to the model part of
the zip, in the order the objects were written (object ids count from 1 in
export order), and copies every other byte of the file as it was.

Pure Python: no FreeCAD.
"""

import os
import re
import tempfile
import zipfile
from xml.sax.saxutils import quoteattr


MODEL_PART = "3D/3dmodel.model"

# The opening tag of one object as FreeCAD writes it. An object that already
# has a name is left alone.
_OBJECT_TAG = re.compile(r"<object\b([^>]*?)(/?)>")
_ID = re.compile(r'\bid="(\d+)"')
_HAS_NAME = re.compile(r"\bname=")
# Characters XML 1.0 cannot carry, even escaped.
_INVALID_XML = re.compile("[\x00-\x08\x0b\x0c\x0e-\x1f￾￿]")


def _attribute(name: str) -> str:
    """``name`` as a quoted XML attribute value, with tabs and line breaks kept as character references."""
    cleaned = _INVALID_XML.sub("", name)
    return quoteattr(cleaned, {"\t": "&#9;", "\n": "&#10;", "\r": "&#13;"})


def name_objects(path: str, names: list[str]) -> None:
    """Write ``names[i]`` as the name of the object with id ``i + 1`` in the 3MF at ``path``.

    Raises ``ValueError`` when the file does not hold exactly ``len(names)``
    objects, and leaves the file as it was in every failure.
    """
    with zipfile.ZipFile(path) as source:
        model = source.read(MODEL_PART).decode("utf-8")
        count = len(_OBJECT_TAG.findall(model))
        if count != len(names):
            raise ValueError(f"the 3MF holds {count} objects, not {len(names)}")

        def add(match: re.Match) -> str:
            attrs, closing = match.group(1), match.group(2)
            found = _ID.search(attrs)
            if found is None or _HAS_NAME.search(attrs):
                return match.group(0)
            index = int(found.group(1)) - 1
            if not 0 <= index < len(names):
                return match.group(0)
            return f"<object{attrs} name={_attribute(names[index])}{closing}>"

        renamed = _OBJECT_TAG.sub(add, model).encode("utf-8")

        directory = os.path.dirname(path) or "."
        fd, tmp_path = tempfile.mkstemp(suffix=".3mf", dir=directory)
        os.close(fd)
        try:
            with zipfile.ZipFile(tmp_path, "w") as target:
                for item in source.infolist():
                    data = renamed if item.filename == MODEL_PART else source.read(item.filename)
                    target.writestr(item, data, compress_type=item.compress_type)
        except BaseException:
            os.remove(tmp_path)
            raise
    try:
        mask = os.umask(0)
        os.umask(mask)
        os.chmod(tmp_path, 0o666 & ~mask)
    except OSError:
        pass
    os.replace(tmp_path, path)
