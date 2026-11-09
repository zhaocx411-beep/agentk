from __future__ import annotations

import argparse
import json
import sys
from pathlib import Path
from typing import Any

import griffe

PYTHON_ROOT = Path(__file__).resolve().parents[1]
SEARCH_PATHS = [
    PYTHON_ROOT / "client" / "src",
    PYTHON_ROOT / "protocol" / "src",
    PYTHON_ROOT / "worker" / "src",
]


def _compact(**values: Any) -> dict[str, Any]:
    return {
        key: value
        for key, value in values.items()
        if value is not None and value != [] and value != {}
    }


def _text(value: Any) -> str | None:
    return None if value is None else str(value)


def _annotation(value: Any) -> dict[str, Any] | None:
    if value is None:
        return None

    text = str(value)
    references: list[dict[str, Any]] = []
    offset = 0
    if isinstance(value, griffe.Expr):
        for part in value.iterate(flat=True):
            rendered = part if isinstance(part, str) else part.name
            end = offset + len(rendered)
            if isinstance(part, griffe.ExprName):
                references.append(
                    {
                        "start": offset,
                        "end": end,
                        "target": part.canonical_path,
                    }
                )
            offset = end

    return _compact(text=text, references=references)


def _doc(obj: griffe.Object) -> str | None:
    if obj.docstring is None or not obj.docstring.value:
        return None
    return obj.docstring.value


def _source(obj: griffe.Object) -> dict[str, Any] | None:
    filepath = obj.filepath
    if not isinstance(filepath, Path) or not filepath.is_file():
        return None

    try:
        path = filepath.relative_to(PYTHON_ROOT).as_posix()
    except ValueError:
        path = filepath.as_posix()

    return _compact(
        path=path,
        line=obj.lineno,
        endLine=obj.endlineno,
    )


def _decorators(obj: griffe.Class | griffe.Function) -> list[str]:
    return [str(decorator.value) for decorator in obj.decorators]


def _is_public_module(module: griffe.Module) -> bool:
    relative_parts = module.path.split(".")[1:]
    return all(not part.startswith("_") for part in relative_parts)


def _is_documentable_member(
    name: str,
    member: griffe.Object | griffe.Alias,
    *,
    class_member: bool = False,
) -> bool:
    if isinstance(member, griffe.Module) or not member.is_public:
        return False
    if name in {"__init__", "__slots__"}:
        return False
    if not name.startswith("_"):
        return True

    return (
        class_member
        and name.startswith("__")
        and name.endswith("__")
        and member.docstring is not None
        and bool(member.docstring.value)
    )


def _collect_modules(root: griffe.Module) -> list[griffe.Module]:
    modules: list[griffe.Module] = []

    def visit(module: griffe.Module) -> None:
        if _is_public_module(module):
            modules.append(module)
        for member in module.members.values():
            if isinstance(member, griffe.Module):
                visit(member)

    visit(root)
    return modules


def _module_members(
    module: griffe.Module,
) -> list[tuple[str, griffe.Object | griffe.Alias]]:
    return [
        (name, member)
        for name, member in module.members.items()
        if _is_documentable_member(name, member)
    ]


def _parameter(parameter: griffe.Parameter) -> dict[str, Any]:
    return _compact(
        name=parameter.name,
        kind=parameter.kind.value.replace(" ", "_").replace("-", "_"),
        annotation=_annotation(parameter.annotation),
        default=_text(parameter.default),
    )


def _parameters(
    function: griffe.Function,
    *,
    drop_receiver: bool,
) -> list[dict[str, Any]]:
    parameters = list(function.parameters)
    if drop_receiver and parameters and parameters[0].name in {"self", "cls"}:
        parameters = parameters[1:]
    return [_parameter(parameter) for parameter in parameters]


def _attribute_scope(attribute: griffe.Attribute) -> str | None:
    if not isinstance(attribute.parent, griffe.Class):
        return None
    if "instance-attribute" in attribute.labels:
        return "instance"
    if "class-attribute" in attribute.labels:
        return "class"
    return None


def _emit_attribute(attribute: griffe.Attribute) -> dict[str, Any]:
    kind = "type_alias" if "type-alias" in attribute.labels else "attribute"
    return _compact(
        kind=kind,
        doc=_doc(attribute),
        source=_source(attribute),
        scope=_attribute_scope(attribute),
        annotation=_annotation(attribute.annotation),
        value=_text(attribute.value),
    )


def _emit_function(function: griffe.Function) -> dict[str, Any]:
    labels = function.labels
    kind = "property" if "property" in labels else "function"
    drop_receiver = (
        isinstance(function.parent, griffe.Class) and "staticmethod" not in labels
    )
    data = _compact(
        kind=kind,
        doc=_doc(function),
        source=_source(function),
        decorators=_decorators(function),
        returns=_annotation(function.returns),
    )
    if "async" in labels:
        data["async"] = True
    data["parameters"] = _parameters(function, drop_receiver=drop_receiver)
    return data


def _emit_class(
    cls: griffe.Class,
    public_definitions: set[str],
    resolving: set[str],
) -> dict[str, Any]:
    data = _compact(
        kind="class",
        doc=_doc(cls),
        source=_source(cls),
        decorators=_decorators(cls),
        bases=[_annotation(base) for base in cls.bases],
    )

    constructor = cls.members.get("__init__")
    if isinstance(constructor, griffe.Function):
        data["parameters"] = _parameters(constructor, drop_receiver=True)

    members = {
        name: _emit_member(member, public_definitions, resolving)
        for name, member in cls.members.items()
        if _is_documentable_member(name, member, class_member=True)
    }
    if members:
        data["members"] = members
    return data


def _emit_alias(
    alias: griffe.Alias,
    public_definitions: set[str],
    resolving: set[str],
) -> dict[str, Any]:
    try:
        target = alias.final_target
    except (griffe.AliasResolutionError, griffe.CyclicAliasError):
        return {"kind": "alias", "target": str(alias.target_path)}

    if target.path in public_definitions:
        return {"kind": "alias", "target": target.path}
    if target.path in resolving:
        return {"kind": "alias", "target": target.path}

    return {
        **_emit_member(target, public_definitions, resolving | {target.path}),
        "definedAt": target.path,
    }


def _emit_member(
    member: griffe.Object | griffe.Alias,
    public_definitions: set[str],
    resolving: set[str],
) -> dict[str, Any]:
    if isinstance(member, griffe.Alias):
        return _emit_alias(member, public_definitions, resolving)
    if isinstance(member, griffe.Class):
        return _emit_class(member, public_definitions, resolving)
    if isinstance(member, griffe.Function):
        return _emit_function(member)
    if isinstance(member, griffe.Attribute):
        return _emit_attribute(member)
    raise TypeError(f"unsupported Griffe object: {member.path} ({member.kind})")


def _emit_module(
    module: griffe.Module,
    members: list[tuple[str, griffe.Object | griffe.Alias]],
    public_definitions: set[str],
) -> dict[str, Any]:
    data = _compact(
        doc=_doc(module),
        source=_source(module),
    )
    data["members"] = {
        name: _emit_member(member, public_definitions, {member.path})
        for name, member in members
    }
    return data


def generate() -> dict[str, Any]:
    root = griffe.load(
        "agentk",
        search_paths=SEARCH_PATHS,
        allow_inspection=False,
        resolve_aliases=True,
        resolve_external=False,
    )
    modules = _collect_modules(root)
    module_members = {module.path: _module_members(module) for module in modules}
    public_definitions = {
        member.path
        for members in module_members.values()
        for _, member in members
        if not isinstance(member, griffe.Alias)
    }

    return {
        "package": root.name,
        "modules": {
            module.path: _emit_module(
                module,
                module_members[module.path],
                public_definitions,
            )
            for module in sorted(modules, key=lambda item: item.path)
            if module_members[module.path] or _doc(module) is not None
        },
    }


def main() -> None:
    parser = argparse.ArgumentParser(
        description="Generate the public AgentK Python API model as JSON."
    )
    parser.add_argument(
        "--output",
        required=True,
        help="Output JSON path, or '-' for stdout.",
    )
    args = parser.parse_args()

    rendered = json.dumps(generate(), ensure_ascii=False, indent=2) + "\n"
    if args.output == "-":
        sys.stdout.write(rendered)
        return
    Path(args.output).write_text(rendered, encoding="utf-8")


if __name__ == "__main__":
    main()
