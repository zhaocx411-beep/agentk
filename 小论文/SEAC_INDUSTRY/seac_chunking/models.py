from __future__ import annotations

from dataclasses import asdict, dataclass, field


@dataclass
class Unit:
    unit_id: str
    source_block_id: str
    document: str
    document_type: str
    block_type: str
    heading_path: str
    text: str
    token_count: int
    protected_group: str = ""
    row_index: int | None = None
    metadata: dict = field(default_factory=dict)

    def to_dict(self):
        return asdict(self)


@dataclass
class Chunk:
    chunk_id: str
    method: str
    document: str
    text: str
    token_count: int
    source_unit_ids: list[str] = field(default_factory=list)
    source_block_ids: list[str] = field(default_factory=list)
    heading_paths: list[str] = field(default_factory=list)
    metadata: dict = field(default_factory=dict)

    def to_dict(self):
        return asdict(self)
