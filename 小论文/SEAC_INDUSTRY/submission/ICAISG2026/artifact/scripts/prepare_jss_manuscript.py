from __future__ import annotations

import argparse
import json
import re
from pathlib import Path

from lint_manuscript import BANNED_IDENTIFIERS


CITATIONS = {
    1: "(Lewis et al., 2020)",
    2: "(Karpukhin et al., 2020)",
    3: "(Reimers and Gurevych, 2019)",
    4: "(Chen et al., 2024a)",
    5: "(Chen et al., 2024b)",
    6: "(Günther et al., 2024)",
    7: "(Sarthi et al., 2024)",
    8: "(Auer et al., 2024)",
    9: "(Guo et al., 2024)",
    10: "(Ali et al., 2024)",
    11: "(LangChain, 2026)",
    12: "(LlamaIndex, 2026)",
    13: "(Cohen, 1960)",
    14: "(Efron, 1979)",
    15: "(Holm, 1979)",
    16: "(Wang et al., 2024b)",
    17: "(Duarte et al., 2024)",
    18: "(Wang et al., 2025)",
    19: "(Zhao et al., 2025)",
    20: "(Rau et al., 2024)",
    21: "(Liu et al., 2024)",
    22: "(Mucha et al., 2024)",
    23: "(Wang et al., 2024a)",
}

JSS_REFERENCES = [
    "Ali, S.J., Naganathan, V., Bork, D., 2024. Establishing traceability between natural language requirements and software artifacts by combining RAG and LLMs. In: Conceptual Modeling, pp. 295–314. https://doi.org/10.1007/978-3-031-75872-0_16.",
    "Auer, C., Lysak, M., Nassar, A., Dolfi, M., Livathinos, N., Vagenas, P., Berrospi Ramis, C., Omenetti, M., Lindlbauer, F., Dinkla, K., Mishra, L., Kim, Y., Gupta, S., Teixeira de Lima, R., Weber, V., Morin, L., Meijer, I., Kuropiatnyk, V., Staar, P.W.J., 2024. Docling technical report. arXiv:2408.09869v5. https://doi.org/10.48550/arXiv.2408.09869.",
    "Chen, J., Xiao, S., Zhang, P., Luo, K., Lian, D., Liu, Z., 2024a. M3-Embedding: Multi-linguality, multi-functionality, multi-granularity text embeddings through self-knowledge distillation. In: Findings of the Association for Computational Linguistics: ACL 2024, pp. 2318–2335. https://doi.org/10.18653/v1/2024.findings-acl.137.",
    "Chen, T., Wang, H., Chen, S., Yu, W., Ma, K., Zhao, X., Zhang, H., Yu, D., 2024b. Dense X retrieval: What retrieval granularity should we use? In: Proceedings of the 2024 Conference on Empirical Methods in Natural Language Processing, pp. 15159–15177. https://doi.org/10.18653/v1/2024.emnlp-main.845.",
    "Cohen, J., 1960. A coefficient of agreement for nominal scales. Educational and Psychological Measurement 20 (1), 37–46. https://doi.org/10.1177/001316446002000104.",
    "Duarte, A.V., Marques, J.D.S., Graça, M., Freire, M., Li, L., Oliveira, A.L., 2024. LumberChunker: Long-form narrative document segmentation. In: Findings of the Association for Computational Linguistics: EMNLP 2024, pp. 6473–6486. https://doi.org/10.18653/v1/2024.findings-emnlp.377.",
    "Efron, B., 1979. Bootstrap methods: Another look at the jackknife. The Annals of Statistics 7 (1), 1–26. https://doi.org/10.1214/aos/1176344552.",
    "Günther, M., Mohr, I., Williams, D.J., Wang, B., Xiao, H., 2024. Late chunking: Contextual chunk embeddings using long-context embedding models. arXiv:2409.04701.",
    "Guo, J.L.C., Steghöfer, J.-P., Vogelsang, A., Cleland-Huang, J., 2024. Natural language processing for requirements traceability. arXiv:2405.10845.",
    "Holm, S., 1979. A simple sequentially rejective multiple test procedure. Scandinavian Journal of Statistics 6 (2), 65–70. https://doi.org/10.2307/4615733.",
    "Karpukhin, V., Oğuz, B., Min, S., Lewis, P., Wu, L., Edunov, S., Chen, D., Yih, W.-t., 2020. Dense passage retrieval for open-domain question answering. In: Proceedings of EMNLP, pp. 6769–6781. https://doi.org/10.18653/v1/2020.emnlp-main.550.",
    "LangChain, 2026. langchain-text-splitters, version 1.1.2 [Computer software]. Python Package Index. https://pypi.org/project/langchain-text-splitters/1.1.2/.",
    "Lewis, P., Perez, E., Piktus, A., Petroni, F., Karpukhin, V., Goyal, N., Küttler, H., Lewis, M., Yih, W.-t., Rocktäschel, T., Riedel, S., Kiela, D., 2020. Retrieval-augmented generation for knowledge-intensive NLP tasks. Advances in Neural Information Processing Systems 33.",
    "Liu, N.F., Lin, K., Hewitt, J., Paranjape, A., Bevilacqua, M., Petroni, F., Liang, P., 2024. Lost in the middle: How language models use long contexts. Transactions of the Association for Computational Linguistics 12, 157–173. https://doi.org/10.1162/tacl_a_00638.",
    "LlamaIndex, 2026. SentenceSplitter, llama-index-core version 0.14.23 [Computer software]. Python Package Index. https://pypi.org/project/llama-index-core/0.14.23/.",
    "Mucha, J., Kaufmann, A., Riehle, D., 2024. A systematic literature review of pre-requirements specification traceability. Requirements Engineering 29, 119–141. https://doi.org/10.1007/s00766-023-00412-z.",
    "Rau, D., Déjean, H., Chirkova, N., Formal, T., Wang, S., Clinchant, S., Nikoulina, V., 2024. BERGEN: A benchmarking library for retrieval-augmented generation. In: Findings of the Association for Computational Linguistics: EMNLP 2024, pp. 7640–7663. https://doi.org/10.18653/v1/2024.findings-emnlp.449.",
    "Reimers, N., Gurevych, I., 2019. Sentence-BERT: Sentence embeddings using Siamese BERT-networks. In: Proceedings of EMNLP-IJCNLP, pp. 3982–3992. https://doi.org/10.18653/v1/D19-1410.",
    "Sarthi, P., Abdullah, S., Tuli, A., Khanna, S., Goldie, A., Manning, C.D., 2024. RAPTOR: Recursive abstractive processing for tree-organized retrieval. In: International Conference on Learning Representations.",
    "Wang, B., Zou, Z., Wan, H., Li, Y., Deng, Y., Li, X., 2024a. An empirical study on the state-of-the-art methods for requirement-to-code traceability link recovery. Journal of King Saud University—Computer and Information Sciences 36, 102118. https://doi.org/10.1016/j.jksuci.2024.102118.",
    "Wang, X., Wang, Z., Gao, X., Zhang, F., Wu, Y., Xu, Z., Shi, T., Wang, Z., Li, S., Qian, Q., Yin, R., Lv, C., Zheng, X., Huang, X., 2024b. Searching for best practices in retrieval-augmented generation. In: Proceedings of the 2024 Conference on Empirical Methods in Natural Language Processing, pp. 17716–17736. https://doi.org/10.18653/v1/2024.emnlp-main.981.",
    "Wang, Z., Gao, C., Xiao, C., Huang, Y., Si, S., Luo, K., Bai, Y., Li, W., Duan, T., Lv, C., Lu, G., Chen, G., Qi, F., Sun, M., 2025. Document segmentation matters for retrieval-augmented generation. In: Findings of the Association for Computational Linguistics: ACL 2025, pp. 8063–8075. https://doi.org/10.18653/v1/2025.findings-acl.422.",
    "Zhao, J., Ji, Z., Fan, Z., Wang, H., Niu, S., Tang, B., Xiong, F., Li, Z., 2025. MoC: Mixtures of text chunking learners for retrieval-augmented generation system. In: Proceedings of the 63rd Annual Meeting of the Association for Computational Linguistics, pp. 5172–5189. https://doi.org/10.18653/v1/2025.acl-long.258.",
]


def abstract_word_count(text: str) -> int:
    abstract = text.split("## Abstract", 1)[1].split("**Keywords:**", 1)[0]
    return len(re.findall(r"\b[\w–-]+\b", abstract))


def author_block(metadata: dict) -> str:
    authors = metadata.get("authors", [])
    affiliations = metadata.get("affiliations", [])
    if not authors or not affiliations:
        raise ValueError("Author metadata requires non-empty authors and affiliations")
    author_lines = []
    for author in authors:
        name = str(author.get("name", "")).strip()
        ids = [str(value).strip() for value in author.get("affiliation_ids", []) if str(value).strip()]
        if not name or not ids:
            raise ValueError("Every author requires a name and at least one affiliation_id")
        orcid = str(author.get("orcid", "")).strip()
        suffix = f" ({', '.join(ids)})"
        if orcid:
            suffix += f"; ORCID: {orcid}"
        author_lines.append(f"- {name}{suffix}")
    affiliation_lines = []
    affiliation_ids = set()
    for affiliation in affiliations:
        identifier = str(affiliation.get("id", "")).strip()
        name = str(affiliation.get("name", "")).strip()
        if not identifier or not name or identifier in affiliation_ids:
            raise ValueError("Each affiliation requires a unique id and an official name")
        affiliation_ids.add(identifier)
        affiliation_lines.append(f"- {identifier}: {name}")
    used_ids = {
        str(identifier).strip()
        for author in authors
        for identifier in author.get("affiliation_ids", [])
    }
    if not used_ids.issubset(affiliation_ids):
        raise ValueError(f"Unknown affiliation IDs: {sorted(used_ids - affiliation_ids)}")
    corresponding = metadata.get("corresponding_author", {})
    if not str(corresponding.get("name", "")).strip() or not str(corresponding.get("email", "")).strip():
        raise ValueError("Corresponding author name and email are required")
    return "\n".join(
        [
            "## Authors",
            "",
            *author_lines,
            "",
            "## Affiliations",
            "",
            *affiliation_lines,
            "",
            f"**Corresponding author:** {corresponding['name']} ({corresponding['email']})",
        ]
    )


def prepare(source: str, metadata: dict | None = None, anonymous_draft: bool = False) -> str:
    if "{{FORMAL_" in source:
        raise ValueError("Formal result placeholders remain")
    if "## References" not in source:
        raise ValueError("Reference section is missing")
    body, _ = source.split("## References", 1)
    for number in sorted(CITATIONS, reverse=True):
        body = body.replace(f"[{number}]", CITATIONS[number])
    remaining_numeric = re.findall(r"\[(\d+)\]", body)
    if remaining_numeric:
        raise ValueError(f"Unconverted numeric citations remain: {remaining_numeric}")
    author_marker = "Author information withheld from the working draft."
    if anonymous_draft:
        body = body.replace(author_marker, "Anonymous layout draft — not for submission.")
    else:
        if metadata is None:
            raise ValueError("JSS uses single-anonymized review; verified author metadata is required")
        body = body.replace(author_marker, author_block(metadata))
    output = body.rstrip() + "\n\n## References\n\n" + "\n\n".join(JSS_REFERENCES) + "\n"
    if abstract_word_count(output) > 250:
        raise ValueError(f"JSS abstract exceeds 250 words: {abstract_word_count(output)}")
    keyword_line = re.search(r"^\*\*Keywords:\*\*\s*(.+)$", output, flags=re.MULTILINE)
    if not keyword_line:
        raise ValueError("Keywords line is missing")
    keyword_count = len([item for item in keyword_line.group(1).split(";") if item.strip()])
    if not 1 <= keyword_count <= 7:
        raise ValueError(f"JSS requires 1–7 keywords, got {keyword_count}")
    for identifier in BANNED_IDENTIFIERS:
        if identifier in output:
            raise ValueError(f"Sensitive project identifier appears in JSS manuscript: {identifier}")
    if len(re.findall(r"^\*\*Table \d+\.", output, flags=re.MULTILINE)) != 5:
        raise ValueError("Expected five numbered table captions")
    equation_numbers = [int(value) for value in re.findall(r"\\tag\{(\d+)\}", output)]
    if equation_numbers != list(range(1, 11)):
        raise ValueError(f"Expected equation tags 1–10, got {equation_numbers}")
    return output


def main() -> None:
    parser = argparse.ArgumentParser(description="Convert the completed SEAC manuscript to JSS author-year style.")
    parser.add_argument("--source", default="manuscript/seac_industry_completed.md")
    parser.add_argument("--metadata", type=Path)
    parser.add_argument("--output", default="submission/JSS/seac_jss_manuscript.md")
    parser.add_argument("--anonymous-draft", action="store_true")
    parser.add_argument("--force", action="store_true")
    args = parser.parse_args()
    root = Path(__file__).resolve().parents[1]
    source_path = args.source if Path(args.source).is_absolute() else root / args.source
    output_path = Path(args.output) if Path(args.output).is_absolute() else root / args.output
    metadata = None
    if args.metadata:
        metadata_path = args.metadata if args.metadata.is_absolute() else root / args.metadata
        metadata = json.loads(metadata_path.read_text(encoding="utf-8"))
    if output_path.exists() and not args.force:
        raise FileExistsError(f"Output exists: {output_path}")
    output = prepare(source_path.read_text(encoding="utf-8"), metadata, args.anonymous_draft)
    output_path.parent.mkdir(parents=True, exist_ok=True)
    output_path.write_text(output, encoding="utf-8")
    print(json.dumps({"output": str(output_path), "abstract_words": abstract_word_count(output)}, indent=2))


if __name__ == "__main__":
    main()
