# EasyChair metadata for ICAISG 2026

## Title

Provenance-Preserving Structure-Aware Chunking for Auditable Enterprise Retrieval-Augmented Generation

## Abstract

Safe and accountable deployment of large language model (LLM) applications increasingly relies on retrieval-augmented generation (RAG) over enterprise documents, yet retrieved evidence can appear relevant while losing the structural context and source lineage required for human oversight. This provenance gap weakens transparency, accountability, and post-incident auditability. We present SEAC, a deterministic structure-aware chunking method designed as a retrieval-side transparency and governance mechanism for enterprise RAG. SEAC preserves stable source identifiers, protects headings and tables, applies an inspectable boundary rule combining semantic, structural, and key-information-density signals, and links precise row-level retrieval children to coherent parent evidence. A frozen industrial study covers 66,981 units from ten enterprise software projects and compares 15 methods using BGE-M3 embeddings. On six locked, retrieval-eligible projects with 125 human-approved questions, SEAC achieves 0.7937 unit MRR and 0.9520 Hit@5, versus 0.7290 MRR for the strongest non-SEAC baseline. The prespecified project-paired comparison with LlamaIndex yields a difference of 0.1003 (exact p = 0.0625), which is not confirmatory at 0.05. Boundary F1 is 0.5673. Removing row-level children reduces MRR to 0.6853, whereas removing fixed parent fusion increases it to 0.8289. The results show that provenance-preserving retrieval can improve evidence localization and provide inspectable lineage for human review and incident reconstruction, at higher indexing cost. SEAC does not itself prevent hallucination, prompt injection, privacy leakage, or authorization failures; instead, it supplies a transparent evidence layer for safer LLM operation and AI governance.

## Keywords

1. large language model safety
2. retrieval-augmented generation
3. explainable artificial intelligence
4. AI governance
5. data provenance
6. auditability
7. document chunking
8. trustworthy artificial intelligence

## Topic selection

- Primary: Track 11 — Explainable and Transparent Artificial Intelligence
- Optional secondary, only if EasyChair requires more than one topic: Track 3 — Security of Large Language Models

## Author information — complete before submission

Do not copy these fields into the anonymous review PDF.

### Author 1

- Full name in English: `[TO BE FILLED]`
- Email: `[TO BE FILLED]`
- Country/region: `[TO BE FILLED]`
- Affiliation in English: `[TO BE FILLED]`
- ORCID, if available: `[TO BE FILLED]`
- Corresponding author: `[YES/NO]`

### Additional authors

Duplicate the Author 1 block for every coauthor, preserving the agreed order.

## Conflicts and declarations

- Presenter: `[TO BE FILLED]`
- Conflicts of interest required by EasyChair: `[TO BE FILLED]`
- All authors approved this version: `[YES/NO]`
- Original and unpublished: `[YES/NO]`
- Not under review elsewhere: `[YES/NO]`
- Enterprise-data use and disclosure approved by the responsible human/data owner: `[YES/NO]`

## Upload

- File: `ICAISG2026_SEAC_blind_submission.pdf`
- Paper type: Full paper / publication
- Pages: 5
- Language: English
- Do not upload the DOCX unless the system asks for it.
- Upload the anonymous artifact only if the system provides a supplement field or the chairs approve it.
