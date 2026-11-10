# Provenance-Preserving Structure-Aware Chunking for Auditable Enterprise Retrieval-Augmented Generation

## Abstract

Safe and accountable deployment of large language model (LLM) applications increasingly relies on retrieval-augmented generation (RAG) over enterprise documents, yet retrieved evidence can appear relevant while losing the structural context and source lineage required for human oversight. This provenance gap weakens transparency, accountability, and post-incident auditability. We present SEAC, a deterministic structure-aware chunking method designed as a retrieval-side transparency and governance mechanism for enterprise RAG. SEAC preserves stable source identifiers, protects headings and tables, applies an inspectable boundary rule combining semantic, structural, and key-information-density signals, and links precise row-level retrieval children to coherent parent evidence. A frozen industrial study covers 66,981 units from ten enterprise software projects and compares 15 methods using BGE-M3 embeddings. On six locked, retrieval-eligible projects with 125 human-approved questions, SEAC achieves 0.7937 unit MRR and 0.9520 Hit@5, versus 0.7290 MRR for the strongest non-SEAC baseline. The prespecified project-paired comparison with LlamaIndex yields a difference of 0.1003 (exact p = 0.0625), which is not confirmatory at 0.05. Boundary F1 is 0.5673. Removing row-level children reduces MRR to 0.6853, whereas removing fixed parent fusion increases it to 0.8289. The results show that provenance-preserving retrieval can improve evidence localization and provide inspectable lineage for human review and incident reconstruction, at higher indexing cost. SEAC does not itself prevent hallucination, prompt injection, privacy leakage, or authorization failures; instead, it supplies a transparent evidence layer for safer LLM operation and AI governance.

**Keywords -** large language model safety, retrieval-augmented generation, explainable artificial intelligence, AI governance, data provenance, auditability, document chunking, trustworthy artificial intelligence

## I. INTRODUCTION

Retrieval-augmented generation (RAG) grounds a language model in external evidence [1]. In enterprise software engineering, that evidence is distributed across requirements, interface specifications, database dictionaries, detailed designs, technical agreements, issue records, and meeting minutes. The relevant fact is often not an isolated sentence. A requirement carries force through an obligation term and its section; an interface parameter depends on its endpoint; and a database value is meaningful under a column header and constraint. A flat character window can retrieve plausible words while losing exactly the context needed to audit an answer.

This creates a transparency problem before generation begins. If retrieval returns a passage whose source units are unknown, or a table whose correct row cannot be distinguished, a downstream system cannot provide a defensible citation. Conversely, returning an entire document increases irrelevant context and can hide the supporting span. Chunking should therefore balance coherent returned context, fine-grained matching, and stable provenance.

We propose SEAC, a deterministic structure- and key-information-aware adaptive chunker for auditable enterprise RAG. SEAC parses ordered units with stable source identifiers, prevents cuts through protected groups, uses adjacent semantic distance together with explicit document signals, and creates retrieval children whose matches resolve back to a coherent parent. Table rows retain their headers and original row identity. Every returned parent carries its ordered source-unit lineage.

This paper makes three contributions. First, it defines a provenance-preserving chunk representation and a transparent boundary rule for heterogeneous engineering documents. Second, it provides a frozen industrial evaluation with human approval and privacy-release gates, axis-specific withholding rules, and project-clustered inference. Third, it reports both quality and cost, including negative ablation findings. The evaluation measures evidence retrieval and boundary agreement; it does not claim that chunking alone prevents hallucination, prompt injection, or privacy leakage.

Our threat model is deliberately narrow. We consider an authorized enterprise RAG service in which access control has already selected a corpus, but the service must still return evidence that can be inspected and traced. The risks in scope are fragmentation of structured facts, loss of source lineage, and retrieval of a topically similar but wrong table row. Model compromise, malicious documents, poisoned embeddings, prompt injection, and authorization bypass remain out of scope. This separation is important for governance: a traceable chunk is useful evidence for review, but provenance metadata is not an access-control decision and high retrieval rank is not proof of answer faithfulness.

## II. RELATED WORK

RAG and dense passage retrieval connect a query to external text before generation [1], [2]. Retrieval granularity is consequential: smaller units improve localization but can lose context, while larger units retain context but dilute the matching signal [4]. Recent work explores long-context embeddings, recursive hierarchies, learned chunk mixtures, and systematic RAG design [5]-[8]. Document segmentation itself materially affects retrieval quality [6], yet most evaluations use public prose or synthetic transformations rather than confidential software artifacts with tables and repeated specifications.

Document conversion systems can recover headings, reading order, and tables [9], but extraction does not determine which elements should share a retrieval unit. Software traceability research links requirements to downstream artifacts [10], [11]; it motivates stable source identity and evidence equivalence when the same approved passage occurs in several documents. Long-context studies further show that merely placing evidence in a large context does not guarantee effective use [12]. SEAC focuses on the missing interface between structured document conversion and provenance-aware enterprise retrieval.

## III. SEAC

[[FIGURE_PIPELINE]]

### A. Provenance-Carrying Units

Let a parsed document be an ordered sequence of units (D = (u_1, ..., u_n)). Each unit stores text, a stable unit identifier, its source block, document type, block type, heading path, and an optional protected-group identifier. Paragraphs and headings are units. For a table with header (h), each row (r_j) is rendered as


\[
t(r_j) = header(h) \Vert row(r_j),
\]


so a row remains interpretable outside the table. A parent chunk stores the ordered unit and block identifiers from which it was assembled. Retrieval correctness can then be resolved by identifier rather than by approximate string matching.

### B. Transparent Boundary Scoring

For adjacent units, semantic distance is (d_i = 1 - cos(e_i,e_{i+1})), where (e_i) is a normalized dense embedding. A structural score (s_i) combines a new-heading indicator, heading-path change, and protected-group transition. A frozen engineering lexicon counts obligation, constraint, interface, field, table, process, and limit terms. Its normalized density is 


\[
rho(u) = \frac{\sum_{v \in V} count(v,u)}{max(1,tokens(u))},
\]


and the adjacent density-change score is (k_i = min(1,10|rho(u_i)-rho(u_{i+1})|)). Continuity penalty (p_i) discourages cuts within a heading path and forbids practical separation inside a protected group. The frozen score is


\[
b_i = 0.45d_i + 0.30s_i + 0.25k_i - p_i.
\]


For document (D), the threshold is (theta_D = Q_{0.8}(b) + 0.08r_t - 0.05r_h), where (r_t) and (r_h) are the fractions of table rows and headings. Parent construction is a deterministic left-to-right pass with a 512-token hard maximum, a 320-token ordinary target, a 224-token target in dense regions, and a 48-token minimum. Candidate cuts are suppressed when they split a protected group or detach a heading from its first content unit.

### C. Hierarchical Retrieval

Each parent is the context returned downstream. Non-heading, non-noise units also produce retrieval children with a 112-token target and 176-token maximum. Table rows normally become individual children and retain row provenance. Retrieval text is prefixed with document type and heading path. For parent score (S_p) and child scores (S_c), full SEAC ranks


\[
S(p,q) = 0.35S_p(q) + 0.65 max_{c \in children(p)} S_c(q).
\]


A precise row can therefore activate its parent while the returned text preserves nearby constraints. Excluding encoder cost, construction is linear in the number of units; index size grows with parents plus children.

### D. Audit Interface and Trust Boundary

SEAC returns a parent identifier, score, ordered source-unit identifiers, block identifiers, document type, heading path, and the triggering child. A consuming RAG service can display the supporting row and nearby context while an auditor resolves the same identifiers against a versioned corpus. No citation is reconstructed from generated text. This makes document conversion, chunk construction, retrieval, and answer generation separable audit stages.

The implementation records the corpus hash and method configuration with each run. If a document changes, its derived identifiers and run manifest can be invalidated before retrieval. This supports change control and incident reconstruction, but it does not decide whether the user may view a source. Runtime authorization must be enforced before indexing or filtering, and confidential text must not be exposed merely because its identifier is traceable. In this study, privacy release is an evaluation-data gate rather than an algorithmic privacy guarantee.

## IV. INDUSTRIAL EVALUATION

### A. Corpus and Frozen Protocol

The corpus contains Chinese-language DOCX and XLSX artifacts from ten anonymized enterprise projects. It includes requirements, API specifications, database designs, detailed designs, technical agreements, meeting minutes, proposals, and research reports. Six unsupported PDF or PPTX files are disclosed as substantive skips. Table I summarizes the fixed split. P01-P02 are development projects, P03 is validation, and P04-P10 are collectively locked. No locked result may change parameters, question wording, baseline selection, or exclusion rules.

**TABLE I. AGGREGATED INDUSTRIAL CORPUS**

| Split | Projects | Documents | Units | Regex tokens | Table rows |
|---|---:|---:|---:|---:|---:|
| Dev. | 2 | 15 | 21,371 | 418541 | 13,557 |
| Validation | 1 | 7 | 5,193 | 103562 | 2,530 |
| Locked test | 7 | 67 | 40,417 | 1155797 | 23,747 |
| Total | 10 | 89 | 66,981 | 1677900 | 39,834 |

All methods use BGE-M3 [3], a 512-token maximum, and unit-level evidence. We compare fixed windows, a local recursive splitter, structure-only and semantic splitters, official LangChain [16] and LlamaIndex [17] splitters, non-hierarchical SEAC, full SEAC, and seven single-component ablations. The formal matrix contains 15 methods.

### B. Human and Privacy Gates

Boundary references are labelled BREAK, NO_BREAK, or NOISE. P01 has 4,783 independently double-labelled and adjudicated boundaries. For P02-P10, 300 stratified candidates per project were pre-labelled by Claude Opus 5 Thinking through Cursor under a frozen rule set and without method outputs. A human then labelled a blinded random sample of 100 per project plus every case the model flagged. Human labels override pre-labels. A project requires Cohen's kappa of at least 0.70 [13]. P08 reached 0.650 and was withheld only from boundary evaluation; retrieval remains eligible. Across promoted references, 77.5% of 7,183 candidates are human verified, 5.4% rule determined, and 17.1% retain the model pre-label.

Question drafts undergo mechanical evidence checks, advisory model pre-review, and human approval. Evidence is a set of stable unit identifiers because approved spans can legitimately repeat across documents. Questions with ambiguous rows or non-distinctive spans are rejected. Privacy status is assigned only by an authorized human; records are admitted only when marked no-risk, anonymized, or explicitly cleared. P10 retained 11 approved questions, below the frozen minimum of 15, and is withheld only from retrieval evaluation. Consequently, the locked analyses contain 125 questions from six retrieval-eligible projects and 1,368 candidates from six boundary-eligible projects.

### C. Metrics and Inference

Unit MRR and Hit at (k) require a returned parent's provenance to contain an approved evidence unit. Span metrics additionally check the minimal supporting text. Boundary metrics compare adjacent parent decisions after excluding NOISE. We form 95% intervals with 10,000 hierarchical bootstrap samples, resampling projects and then items [14]. The single confirmatory test compares SEAC with LlamaIndex SentenceSplitter on unit MRR. It exchanges all matched observations within a project together and enumerates all 64 assignments across six projects. Secondary retrieval and boundary families use Holm correction [15].

### D. Fairness and Reproducibility Controls

Every method receives the same parsed units, approved questions, evidence sets, BGE-M3 encoder, 512-token hard maximum, and unit-level scoring. Fixed and recursive overlap methods use a frozen 64-token overlap. Heading-path context is applied consistently in the fair-context condition, except when context itself is the ablated variable. The no-density variant also disables density-driven parent-size selection, while the no-continuity variant retains protected-group integrity so it cannot gain by creating structurally invalid parents.

The formal runner rejects provisional labels, unresolved evidence, and questions without an allowed human privacy verdict. It records the Python and package versions, command line, method parameters, corpus and annotation hashes, and hashes of the chunking and evaluation implementations. Embedding caches are cleared between methods. All ten projects must pass a readiness audit before locked outputs are released. These controls do not remove sampling uncertainty, but they prevent test access, accidental data substitution, and method-specific preprocessing from silently changing the comparison.

## V. RESULTS

### A. Retrieval and Boundary Quality

Table II reports locked-test results. SEAC reaches 0.7937 unit MRR and 0.9520 Hit@5. The strongest non-SEAC retrieval baseline is structure-only at 0.7290 MRR, an observed absolute difference of 0.0647. SEAC also yields 0.8561 span MRR. Against the prespecified LlamaIndex comparator, the project-paired MRR difference is 0.1003 with bootstrap interval [0.0042, 0.2008] and exact two-sided p = 0.0625. SEAC is higher on five of six retrieval projects, but the confirmatory null is not rejected at 0.05.

**TABLE II. LOCKED-TEST RESULTS**

| Method | Unit MRR | Hit@5 | Span MRR | Boundary F1 |
|---|---:|---:|---:|---:|
| Fixed window | 0.5158 | 0.7200 | 0.1350 | 0.0000 |
| Recursive | 0.6086 | 0.8080 | 0.7072 | 0.0369 |
| Structure-only | 0.7290 | 0.8880 | 0.8211 | 0.5032 |
| Semantic | 0.6999 | 0.8640 | 0.8121 | 0.3843 |
| LangChain | 0.6460 | 0.8480 | 0.7329 | 0.0054 |
| LlamaIndex | 0.6934 | 0.8880 | 0.7586 | 0.0920 |
| SEAC parents | 0.6796 | 0.8800 | 0.7770 | 0.5673 |
| Full SEAC | 0.7937 | 0.9520 | 0.8561 | 0.5673 |

SEAC boundary precision is 0.4321, recall is 0.8255, and F1 is 0.5673 with interval [0.4708, 0.6488]. Structure-only is the strongest non-SEAC boundary baseline at 0.5032. The paired F1 difference is 0.0640 with interval [0.0313, 0.0956], but the Holm-adjusted p value is 0.4375. The high recall and moderate precision indicate that SEAC favors coherent separation at the cost of additional boundaries.

### B. Ablations and Cost

Table III prevents a one-sided component story. Removing table-row children produces the largest observed degradation, reducing MRR by 0.1084 and Hit@5 from 0.9520 to 0.8640. However, the Holm-adjusted secondary p value for this comparison is 1.0000. Removing semantic distance changes MRR by less than four decimal places, and removing parent fusion increases MRR to 0.8289. Heading context also increases MRR when removed, although Hit@5 falls to 0.9280. Thus, row-level evidence is useful in this benchmark, while the fixed parent-fusion and heading-prefix choices are not supported as universally beneficial.

**TABLE III. SINGLE-COMPONENT ABLATIONS**

| Variant | Unit MRR | Hit@5 | Change from full |
|---|---:|---:|---:|
| Full SEAC | 0.7937 | 0.9520 | 0.0000 |
| No semantic distance | 0.7937 | 0.9520 | 0.0000 |
| No density change | 0.7959 | 0.9520 | +0.0021 |
| No continuity penalty | 0.7979 | 0.9520 | +0.0042 |
| Fixed threshold | 0.7959 | 0.9520 | +0.0022 |
| No heading context | 0.8129 | 0.9280 | +0.0192 |
| No table-row children | 0.6853 | 0.8640 | -0.1084 |
| No parent fusion | 0.8289 | 0.9520 | +0.0352 |

SEAC creates 8,746 parents and 48,912 retrieval vectors, 5.50 times the vector count of structure-only. Aggregate locked-set chunking and indexing take 112.11 s and 351.12 s. Mean query latency is 20.93 ms versus 15.18 ms for structure-only, a 5.74 ms increase. This is an explicit auditability-quality-cost tradeoff rather than a free improvement.

### C. Interpreting Project-Level Uncertainty

SEAC's bootstrap interval for unit MRR is [0.7060, 0.8772]. The interval reflects both project and question variation, whereas the exact paired test has only six independent project signs and therefore a coarse p-value grid. The observed advantage on five of six projects and the positive paired interval are encouraging, but p = 0.0625 remains above the prespecified threshold. We therefore retain the confirmatory result as inconclusive rather than relabeling it as significant or replacing the comparator after seeing the data.

The boundary result has the same cluster limitation. SEAC is higher than structure-only on all six boundary-eligible projects, yet its comparison belongs to a 14-test secondary family and has Holm-adjusted p = 0.4375. This illustrates why pooled candidate counts cannot substitute for independent deployments: thousands of boundaries within one project do not create thousands of project replications.

## VI. DISCUSSION AND LIMITATIONS

The results support a bounded claim: preserving tables and explicit source lineage can improve observed evidence retrieval in heterogeneous enterprise documents. Unit-level and span-level metrics answer different audit questions. A span match shows that relevant text appeared; a unit match shows that it came from an approved source location. This distinction matters when requirements are copied across proposals and specifications or when several table rows share the same vocabulary.

For operational governance, the returned lineage can support human review, incident logging, and citation displays. A deployment should store the corpus version with each answer, expose the triggering evidence without hiding the parent context, and make authorization failures distinguishable from retrieval misses. Method parameters should be changed under an explicit validation protocol rather than tuned against production complaints one case at a time. These controls are process recommendations enabled by SEAC's deterministic outputs; they were not evaluated as user-facing governance outcomes.

SEAC is also a transparent component, not an end-to-end safety guarantee. The study does not measure generated-answer faithfulness, adversarial prompts, access-control enforcement, or privacy leakage. Confidential inputs remain protected by external authorization and release gates. The raw corpus cannot be public; an anonymous artifact can include code, frozen configurations, schemas, synthetic examples, hashes, and aggregate analysis. Controlled access to additional artifacts depends on data-owner approval.

Validity is limited by ten projects from one industrial setting, six eligible project clusters per evaluation axis, Chinese documents, one embedding model, and unsupported image-heavy files. The mixed boundary reference includes model pre-labels, although its source is recorded per row and low-agreement P08 is excluded. Post-hoc clarification of three noise categories is disclosed, and agreement is retained before and after the clarification. Finally, higher MRR without confirmatory significance and the negative fusion ablation both argue for evaluation on additional independent projects rather than a general superiority claim.

The component design should also be simplified before broad deployment. Table-row children have a clear observed role, but fixed parent fusion adds vectors without improving this locked MRR, and the heading prefix may over-weight generic section names. A cost-aware successor should keep row provenance while learning or validating when parent scores and heading context are useful. Such a successor requires a new frozen test set; changing the current system in response to these results would convert the locked evaluation into development evidence.

## VII. CONCLUSION

SEAC combines provenance-carrying units, protected structure, transparent boundary signals, and row-level children for auditable enterprise RAG. In the locked study it obtains 0.7937 unit MRR and 0.9520 Hit@5, with an observed but non-significant confirmatory advantage over LlamaIndex. Table-row children are the clearest useful component, while parent fusion and heading context require redesign. Future work should evaluate end-to-end answer faithfulness, adversarial robustness, multilingual public corpora, and learned cost-aware fusion under independently locked projects.

## ACKNOWLEDGMENT

Funding and identity-bearing acknowledgments are withheld for double-blind review. Anthropic Claude Opus 5 Thinking was used as a recorded boundary pre-labeling instrument as described in Section IV-B. OpenAI Codex assisted code drafting, test generation, manuscript structuring, and language editing. Human reviewers overrode experimental pre-labels, verified the analyses and text, and retain responsibility for all research and publication decisions.

## REFERENCES

[1] P. Lewis, E. Perez, A. Piktus, F. Petroni, V. Karpukhin, N. Goyal, H. Kuttler, M. Lewis, W. Yih, T. Rocktaschel, S. Riedel, and D. Kiela, "Retrieval-augmented generation for knowledge-intensive NLP tasks," in *Advances in Neural Information Processing Systems*, vol. 33, 2020.

[2] V. Karpukhin, B. Oguz, S. Min, P. Lewis, L. Wu, S. Edunov, D. Chen, and W. Yih, "Dense passage retrieval for open-domain question answering," in *Proc. EMNLP*, 2020, pp. 6769-6781.

[3] J. Chen, S. Xiao, P. Zhang, K. Luo, D. Lian, and Z. Liu, "M3-Embedding: Multi-linguality, multi-functionality, multi-granularity text embeddings through self-knowledge distillation," in *Findings of ACL*, 2024, pp. 2318-2335.

[4] T. Chen, H. Wang, S. Chen, W. Yu, K. Ma, X. Zhao, H. Zhang, and D. Yu, "Dense X retrieval: What retrieval granularity should we use?" in *Proc. EMNLP*, 2024, pp. 15159-15177.

[5] X. Wang et al., "Searching for best practices in retrieval-augmented generation," in *Proc. EMNLP*, 2024, pp. 17716-17736.

[6] Z. Wang et al., "Document segmentation matters for retrieval-augmented generation," in *Findings of ACL*, 2025, pp. 8063-8075.

[7] J. Zhao et al., "MoC: Mixtures of text chunking learners for retrieval-augmented generation system," in *Proc. ACL*, 2025, pp. 5172-5189.

[8] P. Sarthi, S. Abdullah, A. Tuli, S. Khanna, A. Goldie, and C. D. Manning, "RAPTOR: Recursive abstractive processing for tree-organized retrieval," in *Proc. ICLR*, 2024.

[9] C. Auer et al., "Docling technical report," arXiv:2408.09869, 2024.

[10] J. L. C. Guo, J. Steghofer, A. Vogelsang, and J. Cleland-Huang, "Natural language processing for requirements traceability," arXiv:2405.10845, 2024.

[11] S. J. Ali, V. Naganathan, and D. Bork, "Establishing traceability between natural language requirements and software artifacts by combining RAG and LLMs," in *Conceptual Modeling*, 2024, pp. 295-314.

[12] N. F. Liu, K. Lin, J. Hewitt, A. Paranjape, M. Bevilacqua, F. Petroni, and P. Liang, "Lost in the middle: How language models use long contexts," *Trans. Assoc. Comput. Linguistics*, vol. 12, pp. 157-173, 2024.

[13] J. Cohen, "A coefficient of agreement for nominal scales," *Educ. Psychol. Meas.*, vol. 20, no. 1, pp. 37-46, 1960.

[14] B. Efron, "Bootstrap methods: Another look at the jackknife," *Ann. Stat.*, vol. 7, no. 1, pp. 1-26, 1979.

[15] S. Holm, "A simple sequentially rejective multiple test procedure," *Scand. J. Stat.*, vol. 6, no. 2, pp. 65-70, 1979.

[16] LangChain, "RecursiveCharacterTextSplitter, langchain-text-splitters 1.1.2," 2026. [Online]. Available: https://pypi.org/project/langchain-text-splitters/1.1.2/

[17] LlamaIndex, "SentenceSplitter, llama-index-core 0.14.23," 2026. [Online]. Available: https://pypi.org/project/llama-index-core/0.14.23/
