# [Industrial paper] SEAC: Provenance-Aware Structure-Preserving Chunking for Enterprise Software-Engineering Retrieval

Author information omitted for double-blind review.

## Abstract

Retrieval-augmented generation systems commonly index fixed or recursively
split passages, but enterprise software-engineering documents combine headings,
requirements, interface descriptions, and large tables whose rows remain
meaningful only with their structural context. We present SEAC, a deterministic
chunking method that preserves source provenance, protects structurally coupled
units, and separates returned parent context from fine-grained retrieval
children. SEAC combines adjacent-unit semantic distance, section transitions,
and changes in engineering-keyword density, then creates row-level table
children that retain headers and heading paths. We evaluate SEAC on a frozen
corpus of 89 documents from ten Chinese enterprise software projects, containing
31,841 parsed blocks and 66,981 provenance-traceable units. Seven projects are
locked for cross-project testing, with predeclared axis-specific exclusions
leaving six projects for each primary inference. Full SEAC obtains unit MRR
0.7937 and Hit@5 0.9520, compared with MRR 0.7290 for the strongest non-SEAC
baseline. Against the prespecified LlamaIndex comparator, the paired MRR
difference is +0.1003 (exact two-sided p=0.0625). Removing table-row children
reduces MRR to 0.6853, whereas parent fusion and heading context do not improve
MRR on this sample. SEAC therefore provides an observed retrieval benefit and
precise evidence provenance, with higher indexing cost, but the limited number
of project clusters does not support a claim of general statistical
superiority.

**Keywords—** retrieval-augmented generation, document chunking, enterprise
documents, information retrieval, provenance, software engineering

## I. Introduction

Requirements specifications, database dictionaries, API descriptions,
detailed designs, technical agreements, and meeting records jointly describe
an enterprise software system. These artifacts are heterogeneous but tightly
coupled. A database field is meaningful under its table and constraints; an API
parameter depends on its endpoint and module; and a decision in a meeting record
may depend on its topic, owner, and deadline. When such documents form the
knowledge source of a retrieval-augmented generation (RAG) system, chunking
determines whether the retriever returns attributable engineering evidence or a
fragment detached from its context.

RAG conditions generation on retrieved external evidence [1]. Dense retrieval
has made semantic matching practical at scale, but recent work shows that the
retrieval unit is a modeling decision rather than neutral preprocessing.
Proposition-level retrieval can improve fact localization [3], contextual
embedding can restore information lost at a boundary [4], and hierarchical
methods can recover broader context [5]. Generic document splitters remain
problematic for enterprise artifacts: fixed windows bisect requirements and
rows; separator-recursive splitting ignores functional coupling; semantic
splitting can separate a table from its constraints; and heading-only splitting
may return an entire long design section for a single field-level question.

This paper studies structure-preserving chunk construction for retrieval over
enterprise software-engineering documents. SEAC treats paragraphs, headings,
and table rows as traceable units. It constructs coherent parent chunks with a
deterministic boundary score and indexes fine-grained children, especially
table rows, to activate those parents. Every returned chunk carries the source
unit and block identifiers required for audit or downstream traceability.

The contributions are:

1. a provenance-aware chunking method that combines semantic, structural, and
   key-information signals while protecting coupled engineering structures;
2. a parent/child retrieval design in which table-row children retain their
   headers and source identity;
3. a leakage-controlled industrial evaluation over ten projects, including
   human verification, row-level evidence, official framework baselines,
   ablations, project-clustered uncertainty, and explicit runtime costs.

The locked results show an observed MRR advantage, but the exact confirmatory
test is not significant at 0.05. Moreover, the ablations support table-row
children but not every component of the full design. We report these limitations
because they determine the defensible scope of the industrial contribution.

## II. Related Work

Retrieval granularity affects both localization and available context. Dense X
Retrieval reports advantages for proposition-sized units on fact-oriented tasks
[3]. Late Chunking embeds a long context before pooling chunk representations
[4], while RAPTOR organizes recursively summarized text into a hierarchy [5].
These methods address localization, out-of-context embeddings, and multi-level
retrieval, respectively. SEAC instead changes deterministic boundaries over
semi-structured engineering units and maintains an explicit mapping back to
source elements.

Recent methods learn or generate boundaries. LumberChunker asks a language model
to identify semantic shifts [10], PIC groups sentences using a document summary
as a pseudo-instruction [11], and MoC combines multiple chunking learners [12].
Their results reinforce that segmentation should be evaluated directly, but
their primary settings are public narrative or open-domain corpora. SEAC targets
enterprise technical artifacts and does not invoke an LLM during chunking.

Structured parsers such as Docling preserve headings, reading order, and tables
[6], yet parsed structure alone does not decide which adjacent elements should
share a retrievable unit. Widely used frameworks provide domain-agnostic
recursive and sentence-aware splitters [7], [8]. We use their official
implementations as baselines. Following controlled RAG evaluations [9], [13],
all methods share the same parsed units, dense encoder, queries, and maximum
length so the experiment isolates chunk construction.

## III. SEAC

### A. Units and provenance

Let a parsed document be an ordered sequence

\[
D=(u_1,u_2,\ldots,u_n).
\]

Each unit stores its text, unit identifier, source block identifier, document
type, block type, heading path, and optional protected-group identifier.
Paragraphs and headings are units. A table is expanded into row units; row
\(r_j\) is rendered as

\[
t(r_j)=\operatorname{header}(h)\;\Vert\;\operatorname{row}(r_j),
\]

so the row remains interpretable outside the original layout. Parent and child
chunks retain the ordered unit and block identifiers from which they were built.
Evaluation therefore checks provenance directly rather than attempting to
recover it through string similarity.

### B. Boundary scoring and parent construction

For adjacent units \((u_i,u_{i+1})\), SEAC computes semantic distance \(d_i\),
structural transition \(s_i\), key-information-density change \(k_i\), and a
continuity penalty \(p_i\). With normalized BGE-M3 embeddings [2],
\(d_i=1-\cos(e_i,e_{i+1})\). The structural signal increases at headings,
heading-path changes, and protected-group transitions. Key-information density
counts a frozen lexicon of obligations, limits, database constraints, request
methods, parameters, fields, statuses, and interface terms per token. The score
is

\[
b_i=0.45d_i+0.30s_i+0.25k_i-p_i.
\]

The document-specific threshold is

\[
\theta_D=Q_{0.8}(b)+0.08r_t-0.05r_h,
\]

where \(r_t\) and \(r_h\) are the fractions of table rows and headings. A
left-to-right pass creates parents with a 512-token hard maximum, ordinary and
dense-region soft targets of 320 and 224 tokens, and a 48-token minimum. A cut
is suppressed if it would split a protected table group or detach a heading
from its first content unit. These decisions are deterministic after parsing;
queries and QA labels never enter chunk construction.

### C. Retrieval children

Every parent is retrievable. Long parents additionally produce fine-grained
children. Paragraph and sentence children carry a heading-path prefix, while a
table child contains the table header and one row. The full method ranks a
parent using both its own similarity and its best child similarity. A child hit
returns its parent as coherent context but preserves the child's source-unit
identity for evidence attribution.

This design separates two choices that flat splitters conflate: which small unit
should match the query, and which coherent context should be returned. The
formal ablations remove semantic distance, density, continuity, adaptive
thresholding, heading context, table children, and parent fusion separately.

## IV. Industrial Evaluation

### A. Corpus and split

The frozen corpus contains 89 DOCX/XLSX documents from ten Chinese enterprise
software projects. Project names are replaced by P01--P10. The documents include
requirements, APIs, database and detailed designs, technical agreements,
proposals, research reports, and meeting records. Legacy conversions are
hash-tracked. Six unsupported PDF/PPTX sources are disclosed but excluded from
all methods.

**TABLE I. CORPUS AND PROJECT SPLIT**

| Split | Projects | Documents | Blocks | Units | Regex tokens |
|---|---:|---:|---:|---:|---:|
| Development | 2 | 15 | 9,639 | 21,371 | 418,541 |
| Validation | 1 | 7 | 3,075 | 5,193 | 103,562 |
| Locked test | 7 | 67 | 19,127 | 40,417 | 1,155,797 |
| **Total** | **10** | **89** | **31,841** | **66,981** | **1,677,900** |

P01--P02 are used for development, P03 for validation, and P04--P10 form a
collectively locked test. No parameter, query wording, or baseline selection is
changed after locked release.

### B. Boundary and question references

Boundary candidates are sampled by document and structural transition type.
P01 is double-annotated by humans and adjudicated. P02--P10 use blinded LLM
prelabels followed by a hidden human random sample of 100 candidates per project
and mandatory review of flagged cases. Human decisions override prelabels. A
project is eligible only if Cohen's kappa [15] reaches 0.70. The promoted
reference contains 5,566 human-verified (77.5%), 387 deterministic
rule-resolved (5.4%), and 1,230 retained prelabelled (17.1%) boundaries. P08
reaches kappa 0.650 and is predeclared as withheld from boundary evaluation; its
retrieval questions remain eligible.

Question drafts are generated from observable document patterns, then
mechanically grounded and human-reviewed for naturalness, unique answer,
minimal evidence, supporting span, and privacy. Repeated legitimate evidence is
represented as an equivalence set. Human approval retains 195 of 215 reviewed
candidates across nine projects after a recorded mechanical exclusion. P10 has
11 approved questions, below the 15-question minimum, and is predeclared as
withheld from retrieval evaluation; its boundary labels remain eligible.

The locked retrieval evaluation therefore contains 125 questions from six
projects (P04--P09), and the binary boundary evaluation contains 1,368
non-noise candidates from six projects (P04--P07, P09, P10). Withholding is
axis-specific and was fixed before formal results were accessed.

### C. Comparators, metrics, and inference

All methods use a 512-token maximum and BGE-M3 dense vectors. The core
comparators are fixed windows, local recursive splitting, structure-only
splitting, semantic splitting, LangChain RecursiveCharacterTextSplitter 1.1.2
[7], LlamaIndex SentenceSplitter 0.14.23 [8], SEAC parents without children, and
full SEAC. Seven single-component ablations complete the 15-method matrix.

Retrieval reports provenance-aware unit MRR and Hit@1/3/5 plus supporting-span
MRR and Hit@1/3/5. Boundary detection reports precision, recall, and F1 after
excluding `NOISE`. Efficiency includes vector count, construction/index time,
and query latency.

We form 95% intervals with 10,000 hierarchical bootstrap samples, resampling
projects and then observations [16]. The sole confirmatory comparison is full
SEAC versus LlamaIndex SentenceSplitter on unit MRR. An exact two-sided paired
test exchanges all observations at the project level; six eligible projects
yield all 64 assignments. Secondary retrieval and boundary families use Holm
correction [17]. Ablations remain exploratory.

## V. Results

### A. Boundary and retrieval quality

Table II reports the locked aggregate results. SEAC obtains boundary F1 0.5673
(95% CI [0.4708, 0.6488]). Structure-only is the strongest non-SEAC boundary
baseline at 0.5032. SEAC is higher on all six eligible projects, but the paired
difference +0.0640 does not survive Holm correction (adjusted p=0.4375).

**TABLE II. LOCKED BOUNDARY AND RETRIEVAL RESULTS**

| Method | Boundary F1 | Unit MRR | Hit@1 | Hit@5 | Span MRR |
|---|---:|---:|---:|---:|---:|
| Fixed | 0.0000 | 0.5158 | 0.3680 | 0.7200 | 0.1350 |
| Recursive | 0.0369 | 0.6086 | 0.4480 | 0.8080 | 0.7072 |
| Structure-only | 0.5032 | 0.7290 | 0.6080 | 0.8880 | 0.8211 |
| Semantic | 0.3843 | 0.6999 | 0.5680 | 0.8640 | 0.8121 |
| LangChain recursive | 0.0054 | 0.6460 | 0.4960 | 0.8480 | 0.7329 |
| LlamaIndex sentence | 0.0920 | 0.6934 | 0.5520 | 0.8880 | 0.7586 |
| SEAC parents | 0.5673 | 0.6796 | 0.5280 | 0.8800 | 0.7770 |
| **Full SEAC** | **0.5673** | **0.7937** | **0.6800** | **0.9520** | **0.8561** |

For retrieval, full SEAC reaches unit MRR 0.7937 and span MRR 0.8561. The
strongest non-SEAC baseline is structure-only at unit MRR 0.7290, an observed
difference of +0.0647. The prespecified comparison against LlamaIndex yields a
paired mean difference of +0.1003 (95% CI [0.0042, 0.2008]) and SEAC is higher
on five of six eligible projects. The exact two-sided p-value is 0.0625, so the
confirmatory comparison is not significant at 0.05. The apparent difference
between the bootstrap interval and exact test reflects inference with only six
clusters and the coarse 1/64 randomization grid; effect sizes and project-level
consistency are therefore more informative than a binary threshold.

### B. Ablations

Table III shows that the evidence does not support every component of full
SEAC. Removing table-row children decreases MRR by 0.1084 and Hit@5 from 0.9520
to 0.8640. In contrast, removing heading context or parent fusion increases MRR
on the locked sample. Semantic distance, density, continuity, and adaptive
thresholding have changes between 0.0000 and +0.0042 when removed. Because the
ablation family is exploratory and its Holm-adjusted tests are not significant,
the result supports table-row children as the most promising component, not a
causal necessity claim.

**TABLE III. LOCKED SINGLE-COMPONENT ABLATIONS**

| Variant | Boundary F1 | Unit MRR | Hit@5 | MRR change vs. full |
|---|---:|---:|---:|---:|
| Full SEAC | 0.5673 | 0.7937 | 0.9520 | -- |
| No semantic distance | 0.5698 | 0.7937 | 0.9520 | +0.0000 |
| No density change | 0.5639 | 0.7959 | 0.9520 | +0.0021 |
| No continuity penalty | 0.5718 | 0.7979 | 0.9520 | +0.0042 |
| Fixed threshold | 0.5731 | 0.7959 | 0.9520 | +0.0022 |
| No heading context | 0.5673 | 0.8129 | 0.9280 | +0.0192 |
| No table-row children | 0.5673 | 0.6853 | 0.8640 | -0.1084 |
| No parent fusion | 0.5673 | 0.8289 | 0.9520 | +0.0352 |

### C. Resource cost

Full SEAC creates 8,746 parents and 48,912 retrieval vectors, while the
structure-only baseline creates 8,891 parents/vectors. SEAC therefore uses
5.50x as many vectors. Aggregate locked-set construction and indexing take
112.11 s and 351.12 s for SEAC, versus 0.25 s and 139.46 s for structure-only.
Mean query latency is 20.93 ms versus 15.18 ms, with SEAC p95 27.28 ms. The
method is appropriate when evidence granularity and provenance justify a larger
index, rather than as a universally cheaper replacement.

## VI. Discussion

### A. What the evidence supports

The clearest supported design choice is row-level retrieval under a coherent
table parent. A whole-table vector dilutes field-level distinctions, whereas a
row alone can lose its schema. A child containing the header and one row
preserves both localization and interpretability. The difference between SEAC
parents (MRR 0.6796) and full SEAC (0.7937), together with the no-table-child
ablation (0.6853), is consistent with this mechanism.

The results do not validate the complete scoring story. Removing semantic
distance has no observed MRR effect, and removing fusion or heading context
improves MRR. One plausible explanation is that parent fusion overweights broad
context and that repeated heading prefixes reduce embedding discrimination in
template-heavy corpora. These explanations are hypotheses, not post-hoc reasons
to redefine the locked method. A future version should compare a simplified
table-child method on new projects rather than promoting the best locked-set
ablation as a new primary result.

### B. Industrial implications

Stable provenance is useful beyond QA. Returned unit identifiers can support
requirements traceability, change-impact review, and audit logs. The method is
deterministic after parsing, making its decisions reproducible and easier to
inspect than generated propositions. Deployment teams can restrict children to
tables or other high-value structures when index cost is binding. Because the
corpus contains confidential artifacts, all retrieved text remains subject to
the same access control and privacy policy as its source document.

## VII. Validity, Ethics, and Reproducibility

Boundary F1 measures agreement with a mixed reference rather than end-to-end
answer quality. Although 77.5% of promoted candidates are human verified,
retained LLM prelabels can share structural bias with SEAC. P08 is withheld
rather than forcing its kappa above threshold. QA drafts originate from rules
and may overrepresent local factual questions despite human review.

Only six independent projects support each inference axis. Exact tests are
therefore discrete and have limited power. Project sizes and question counts are
unequal; hierarchical bootstrap intervals address clustering but cannot create
additional independent projects. The corpus is Chinese, dominated by industrial
management systems, and excludes image-heavy PDF/PPTX sources. Generalization
to English artifacts, source code, issue trackers, or safety-critical systems is
not established.

Raw documents and derived QA cannot be publicly released because they contain
proprietary and potentially identifying information. The anonymous review
artifact can include implementation code, frozen configurations, annotation
schemas, synthetic examples, aggregate results, tests, manifests, and hashes.
Any broader release requires written data-owner approval. Every formal run
records input and implementation hashes, package versions, command line, random
seed, and hardware metadata.

## VIII. Conclusion

SEAC constructs provenance-aware parent and child chunks for heterogeneous
enterprise software-engineering documents. On a frozen ten-project industrial
study, it achieves unit MRR 0.7937 and Hit@5 0.9520, with an observed advantage
over non-SEAC baselines and the prespecified LlamaIndex comparator. The exact
confirmatory p-value is 0.0625, so the study does not establish general
statistical superiority. Ablations identify table-row children as the strongest
supported component while challenging the value of heading context and parent
fusion. Replication on additional independent and public multilingual projects
is required before simplifying or generalizing the method.

## References

[1] P. Lewis et al., “Retrieval-Augmented Generation for Knowledge-Intensive
NLP Tasks,” in *Advances in Neural Information Processing Systems*, vol. 33,
2020.

[2] J. Chen, S. Xiao, P. Zhang, K. Luo, D. Lian, and Z. Liu, “M3-Embedding:
Multi-Linguality, Multi-Functionality, Multi-Granularity Text Embeddings Through
Self-Knowledge Distillation,” in *Findings of ACL*, 2024, pp. 2318--2335.

[3] T. Chen et al., “Dense X Retrieval: What Retrieval Granularity Should We
Use?” in *Proceedings of EMNLP*, 2024, pp. 15159--15177.

[4] M. Guenther, I. Mohr, D. J. Williams, B. Wang, and H. Xiao, “Late
Chunking: Contextual Chunk Embeddings Using Long-Context Embedding Models,”
arXiv:2409.04701, 2024.

[5] P. Sarthi, S. Abdullah, A. Tuli, S. Khanna, A. Goldie, and C. D. Manning,
“RAPTOR: Recursive Abstractive Processing for Tree-Organized Retrieval,” in
*International Conference on Learning Representations*, 2024.

[6] C. Auer et al., “Docling Technical Report,” arXiv:2408.09869, 2024.

[7] LangChain, “langchain-text-splitters 1.1.2,” Python Package Index, 2026.

[8] LlamaIndex, “SentenceSplitter, llama-index-core 0.14.23,” 2026.

[9] X. Wang et al., “Searching for Best Practices in Retrieval-Augmented
Generation,” in *Proceedings of EMNLP*, 2024, pp. 17716--17736.

[10] A. V. Duarte, J. D. S. Marques, M. Graca, M. Freire, L. Li, and A. L.
Oliveira, “LumberChunker: Long-Form Narrative Document Segmentation,” in
*Findings of EMNLP*, 2024, pp. 6473--6486.

[11] Z. Wang et al., “Document Segmentation Matters for Retrieval-Augmented
Generation,” in *Findings of ACL*, 2025, pp. 8063--8075.

[12] J. Zhao et al., “MoC: Mixtures of Text Chunking Learners for
Retrieval-Augmented Generation System,” in *Proceedings of ACL*, 2025,
pp. 5172--5189.

[13] D. Rau et al., “BERGEN: A Benchmarking Library for Retrieval-Augmented
Generation,” in *Findings of EMNLP*, 2024, pp. 7640--7663.

[14] N. F. Liu et al., “Lost in the Middle: How Language Models Use Long
Contexts,” *Transactions of the Association for Computational Linguistics*,
vol. 12, pp. 157--173, 2024.

[15] J. Cohen, “A Coefficient of Agreement for Nominal Scales,” *Educational
and Psychological Measurement*, vol. 20, no. 1, pp. 37--46, 1960.

[16] B. Efron, “Bootstrap Methods: Another Look at the Jackknife,” *The Annals
of Statistics*, vol. 7, no. 1, pp. 1--26, 1979.

[17] S. Holm, “A Simple Sequentially Rejective Multiple Test Procedure,”
*Scandinavian Journal of Statistics*, vol. 6, no. 2, pp. 65--70, 1979.
