# [Industrial paper] SEAC: Provenance-Aware Structure-Preserving Chunking for Enterprise Software Retrieval

Author information omitted for double-blind review.

## Abstract

Retrieval-augmented generation systems commonly index fixed or recursively split passages, but enterprise software-engineering documents combine headings, requirements, interface descriptions, and large tables whose rows remain meaningful only with their structural context. We present SEAC, a deterministic chunking method that preserves source provenance, protects structurally coupled units, and separates fine-grained retrieval children from the coherent parent context returned downstream. SEAC combines adjacent-unit semantic distance, structural transitions, and changes in engineering-keyword density, then creates row-level table children that retain headers and heading paths. We evaluate the frozen method on 89 documents from ten Chinese enterprise projects, containing 31,841 parsed blocks and 66,981 traceable units. Seven projects form the locked test set; predeclared axis-specific exclusions leave six independent projects and 125 questions for retrieval inference. Full SEAC obtains unit MRR 0.7937 and Hit@5 0.9520, versus MRR 0.7290 for the strongest non-SEAC baseline. Against the prespecified LlamaIndex comparator, the paired MRR difference is +0.1003 (95% CI [0.0042, 0.2008]), but the exact two-sided project-level test gives p=0.0625. Removing table-row children reduces MRR to 0.6853, while removing parent fusion increases it to 0.8289. The locked evidence therefore supports row-level retrieval under coherent table context more strongly than every component of the original scoring design. SEAC provides observed retrieval gains and exact evidence provenance at 5.50x the index size of the strongest flat baseline, but the small number of project clusters does not establish general statistical superiority.

**Keywords—** retrieval-augmented generation, document chunking, enterprise documents, information retrieval, provenance, software engineering

## I. Introduction

Requirements specifications, database dictionaries, API descriptions, detailed designs, technical agreements, proposals, and meeting records jointly describe an enterprise software system. These artifacts are heterogeneous but tightly coupled. A database field is meaningful under its table and constraints; an API parameter depends on its endpoint and module; and a decision in a meeting record may depend on its topic, owner, and deadline. When these documents form the knowledge source of a retrieval-augmented generation (RAG) system, chunk construction determines whether the retriever returns attributable engineering evidence or a fragment detached from its context.

RAG conditions generation on retrieved external evidence [1], while dense passage retrieval and sentence embeddings make semantic matching practical at scale [2], [3]. Recent work shows that the retrieval unit is a modeling decision rather than neutral preprocessing. Proposition-level retrieval can improve fact localization [5], contextual embedding can restore information lost at boundaries [6], and hierarchical methods can recover broader context [7]. Long-context models also remain sensitive to where evidence occurs [21]. These findings motivate a representation that can match a small unit without discarding the larger engineering structure required to interpret it.

Generic document splitters are poorly aligned with enterprise artifacts. Fixed windows bisect requirements and rows; separator-recursive splitting ignores functional coupling; semantic splitting can detach a table from its constraints; and heading-only splitting may return an entire design section for a field-level question. Structured parsers preserve headings, reading order, and tables [8], but parsed structure alone does not decide which adjacent elements should share a retrievable unit. Requirements-traceability studies likewise emphasize the need to retain links between natural-language evidence and downstream artifacts [9], [10], [22], [23].

This paper studies deterministic, structure-preserving chunk construction for retrieval over enterprise software-engineering documents. SEAC treats paragraphs, headings, and table rows as traceable units. It constructs coherent parent chunks with a boundary score and indexes fine-grained children, especially header-aware table rows, to activate those parents. Every returned chunk carries its source unit and block identifiers. The experimental protocol freezes the corpus split, parameters, baselines, questions, withholding rules, and primary comparison before locked results are accessed.

The contributions are:

1. a provenance-aware chunking method that combines semantic, structural, and key-information signals while protecting coupled engineering structures
2. a parent/child retrieval design in which table-row children retain their headers and source identity while the retriever returns coherent parent context
3. a leakage-controlled industrial study over ten projects, including human verification, exact supporting spans, official framework baselines, component ablations, project-clustered uncertainty, and explicit resource costs
4. a transparent negative-result analysis showing that the locked evidence supports table-row children but not every component of the frozen full system.

The final point is central to the paper's scope. Full SEAC is the prespecified primary method and cannot be replaced after observing the test-set ablations. The results therefore support a mechanism and a reproducible industrial finding, not a post-hoc claim that the most favorable ablation is a validated new method.

## II. Related Work

### A. Retrieval granularity and context

Dense retrieval traditionally indexes passages [2], but Dense X Retrieval evaluates propositions and other granularities, finding that smaller units can improve fact-oriented retrieval [5]. Late Chunking embeds a long context before pooling chunk representations [6], while RAPTOR organizes recursively summarized text into a retrieval hierarchy [7]. These methods address localization, out-of-context embeddings, and multi-level retrieval, respectively. SEAC instead changes deterministic boundaries over semi-structured engineering units and maintains an explicit mapping from each parent and child back to source elements.

Recent methods also learn or generate boundaries. LumberChunker asks a language model to identify semantic shifts [17]; PIC groups sentences using a document summary as a pseudo-instruction [18]; and MoC combines multiple chunking learners [19]. Their results reinforce that segmentation should be evaluated directly. Their principal settings, however, are narrative or open-domain corpora. SEAC targets confidential technical artifacts and invokes no language model during chunk construction, making every boundary decision reproducible after parsing.

### B. Structured technical documents

Docling and related parsers preserve document structure, including headings, reading order, and tables [8]. The remaining challenge is to decide which adjacent elements form coherent retrieval context. A table illustrates the tension: treating the full table as one vector dilutes field-level distinctions, whereas indexing a row without its header removes the schema. SEAC represents the header-aware row as a child while preserving the containing table region in its parent.

Widely used frameworks provide domain-agnostic recursive and sentence-aware splitters [11], [12]. Following controlled RAG evaluations [16], [20], all methods in our study share the same parsed units, encoder, queries, contextual prefixes, and maximum length. This isolates chunk construction rather than confounding it with a different embedding model or generator.

### C. Provenance and software traceability

Software-engineering retrieval differs from generic passage search because evidence is often consumed in audits, requirements reviews, and change-impact analysis. Prior work on requirements traceability connects natural-language requirements with code and other artifacts [9], [10], [22], [23]. SEAC does not attempt full artifact-link recovery. Instead, it provides a lower-level guarantee: retrieved text retains stable document, block, and unit identifiers, and evaluation credits only annotated source units or verified equivalent copies. This distinguishes finding text that merely resembles an answer from returning attributable evidence.

## III. Task Formulation and SEAC

### A. Units, evidence, and provenance

Let a parsed document be an ordered sequence

\[
D=(u_1,u_2,\ldots,u_n).
\]

Each unit stores text, a stable unit identifier, its source block identifier, document type, block type, heading path, and an optional protected-group identifier. Paragraphs and headings are units. A table with header row h is expanded into row units; data row r_j is rendered as

\[
t(r_j)=\operatorname{header}(h)\;\Vert\;\operatorname{row}(r_j),
\]

so the row remains interpretable outside the original layout. Parent and child chunks retain the ordered source unit and block identifiers from which they were built. Evaluation checks provenance directly instead of recovering it through string similarity.

For a question q, let G(q) contain every gold source unit that legitimately carries its verified supporting span z(q). The set can contain multiple units when templates repeat the same passage across documents. A returned parent is provenance-correct when its source-unit list contains any member of G(q). Unit MRR and Hit@k therefore penalize retrieval of the correct table but the wrong row. Span MRR and span Hit@k complement the provenance metrics by checking whether returned text contains z(q).

### B. Boundary score and parent construction

For adjacent units (u_i,u_{i+1}), SEAC computes semantic distance d_i, structural transition s_i, key-information-density change k_i, and continuity penalty p_i. With normalized BGE-M3 embeddings [4], d_i is one minus cosine similarity. The structural signal increases at headings, heading-path changes, and protected-group transitions. The density signal uses a frozen Chinese lexicon of obligations, limits, database constraints, request methods, fields, statuses, interfaces, processes, and other engineering terms. Density is the number of lexicon matches divided by the unit's regex-token count; the change signal is the absolute adjacent density difference scaled by ten and capped at one. The lexicon is intentionally small: it indicates transitions between technically dense and narrative regions rather than claiming full entity recognition. The combined score is

\[
b_i=0.45d_i+0.30s_i+0.25k_i-p_i.
\]

The document-specific threshold is

\[
\theta_D=Q_{0.8}(b)+0.08r_t-0.05r_h,
\]

where r_t and r_h are the fractions of table rows and headings. The table-row adjustment prevents a document with many structurally similar rows from being oversegmented, whereas the heading adjustment makes explicit organization easier to respect. A left-to-right pass assembles parents with a 512-token hard maximum, ordinary and dense-region soft targets of 320 and 224 tokens, and a 48-token minimum. The dense target activates when mean parent density reaches 0.03. A cut is suppressed if it would split a protected table group, detach a heading from its first content unit, or leave an undersized current parent. A new heading otherwise begins a parent when the current one is nonempty. Units longer than the hard maximum are split at sentence, punctuation, line, or list boundaries before token fallback. The pass has no random decision, and queries and QA labels never enter construction.

Protected groups encode functional coupling before semantic scoring. The header and rows of one source table share a group, so a high local semantic distance cannot arbitrarily cut the table region. This protection applies to returned parents, not the retrieval index: the child layer can still expose an individual header-aware row for matching. Stable identifiers are propagated through both operations. Consequently, the system can state which row triggered retrieval and which larger parent supplied interpretation context, even when the displayed parent contains several units.

[[FIGURE:SEAC_PIPELINE|SEAC construction and retrieval pipeline. Fine-grained children activate coherent parents while stable identifiers preserve evidence provenance.]]

### C. Hierarchical children and retrieval

Every parent is retrievable. Non-heading, non-noise units additionally produce retrieval children with a target of 112 and maximum of 176 tokens. Paragraph and sentence children carry a heading-path prefix. Table rows normally become individual children containing the header and one row; long rows may be logically split while retaining their row provenance. The same contextual prefix is applied to all comparison methods except in the heading-context ablation.

For parent p, dense parent score Sₚ, and child scores S꜀, full SEAC ranks the parent by

\[
S(p,q)=0.35S_p(q)+0.65\max_{c\in children(p)}S_c(q).
\]

A child hit returns its parent as coherent context while retaining the child's exact source-unit identity. This design separates two choices that flat splitters conflate: which small unit should match the query, and which coherent context should be returned downstream.

### D. Complexity and reproducibility surface

For n units, SEAC requires n unit embeddings, n-1 adjacent similarities, and a linear assembly pass. Excluding encoder cost, parent construction is O(n) in time and storage. Index size grows with parents plus children. All thresholds, weights, token targets, prefix rules, and protected-group rules are frozen in configuration. Each formal run records corpus, QA, boundary, and implementation hashes; package versions; the command line; random seed; and hardware metadata.

## IV. Industrial Study Design

### A. Research questions and corpus

The frozen study addresses five questions: RQ1 asks whether SEAC better matches human semantic parent boundaries; RQ2 asks whether it improves provenance and supporting-span retrieval; RQ3 examines cross-project and question-type consistency; RQ4 isolates the contribution of each component; and RQ5 measures index and runtime cost.

The corpus contains 89 DOCX and XLSX documents from ten Chinese enterprise software projects. Project identities are replaced by P01--P10. The documents include requirements, API specifications, database and detailed designs, technical agreements, proposals, research reports, and meeting records. Legacy conversions are hash-tracked. Six unsupported PDF or PPTX sources are disclosed but excluded from every method.

**TABLE I. FROZEN CORPUS AND PROJECT SPLIT**

| Split | Proj. | Docs | Blocks | Units | Regex tokens |
|---|---:|---:|---:|---:|---:|
| Development | 2 | 15 | 9,639 | 21,371 | 418,541 |
| Validation | 1 | 7 | 3,075 | 5,193 | 103,562 |
| Locked test | 7 | 67 | 19,127 | 40,417 | 1,155,797 |
| **Total** | **10** | **89** | **31,841** | **66,981** | **1,677,900** |

P01--P02 are used for development, P03 for validation, and P04--P10 form a collectively locked test. No parameter, query wording, withholding rule, or baseline selection changes after locked release.

**TABLE II. LOCKED PROJECT COVERAGE AND AXIS ELIGIBILITY**

| Project | Documents | Units | Boundary axis | Retrieval questions |
|---|---:|---:|---|---:|
| P04 | 17 | 8,532 | Eligible | 22 |
| P05 | 17 | 8,166 | Eligible | 17 |
| P06 | 12 | 10,122 | Eligible | 22 |
| P07 | 8 | 2,496 | Eligible | 23 |
| P08 | 5 | 4,369 | Withheld (kappa=0.650) | 22 |
| P09 | 4 | 5,324 | Eligible | 19 |
| P10 | 4 | 1,408 | Eligible | Withheld (11<15) |

Table II makes the two evaluation axes explicit. A project can contribute to retrieval while failing the boundary-agreement gate, or contribute to boundary evaluation while failing the minimum-question gate. Keeping those decisions separate avoids deleting an entire project because it is weak on an unrelated annotation axis.

### B. Boundary reference

P01 contains 4,783 boundaries independently labelled by two humans and adjudicated. For P02--P10, 300 candidates per project are selected by document and structural stratum. A large language model first produces blinded prelabels without access to SEAC predictions, baselines, retrieval scores, or formal results. Human verification then covers a hidden random sample of 100 candidates per project and every flagged case; human decisions override prelabels. A project is eligible only if Cohen's kappa [13] reaches 0.70.

The first verification round exposed three unenumerated noise families: bare structural labels, punctuation-only conversion fragments, and meeting-record sign-off blocks. The rule was amended after inspecting these disagreements, so agreement is retained both before and after the amendment and rule-resolved rows are excluded from kappa. No further family was added when P08 remained below threshold at kappa 0.650; P08 is therefore withheld from boundary evaluation. The promoted reference contains 5,566 human-verified boundaries (77.5%), 387 deterministic rule-resolved boundaries (5.4%), and 1,230 retained prelabels (17.1%). Boundary F1 must consequently be read as agreement with a mixed reference, not an exhaustively human-labelled gold standard.

### C. Question and evidence reference

Question drafts are generated from observable document patterns and then mechanically grounded and human-reviewed for naturalness, unique answer, minimal evidence, supporting span, and privacy. Repeated legitimate evidence is represented as an equivalence set. Human approval retains 195 of 215 candidates across nine projects after a recorded mechanical rule excludes ten approved questions that begin with a section number. The rule is applied after review and never rewrites a human decision.

P10 retains 11 approved questions, below the predeclared 15-question minimum, and is withheld from retrieval evaluation while remaining eligible for boundary evaluation. The locked retrieval set therefore contains 125 questions from P04--P09. The boundary set contains 1,368 non-noise candidates from P04--P07, P09, and P10. Withholding is axis-specific and fixed before formal results are accessed.

### D. Comparators, metrics, and inference

All methods use a 512-token maximum and BGE-M3 dense vectors [4]. Comparators are fixed windows, local recursive splitting, structure-only splitting, semantic splitting, official LangChain RecursiveCharacterTextSplitter 1.1.2 [11], official LlamaIndex SentenceSplitter 0.14.23 [12], non-hierarchical SEAC parents, and full SEAC. Seven ablations remove semantic distance, density change, continuity penalties, adaptive thresholds, heading context, table-row children, and parent fusion one at a time.

Retrieval reports unit MRR and Hit@1/3/5 plus supporting-span MRR and Hit@1/3/5. Boundary detection reports precision, recall, and F1 after excluding NOISE. Efficiency includes vector count, construction time, index time, and query latency. We form 95% intervals with 10,000 hierarchical bootstrap samples, resampling projects and then observations [14].

The sole confirmatory comparison is full SEAC versus the official LlamaIndex SentenceSplitter on unit MRR. This external framework baseline and metric were frozen before locked results were available; the stronger structure-only result is reported as an observed comparison rather than substituted post hoc. An exact two-sided paired test exchanges all matched observations at the project level. Six eligible projects yield all 64 assignments. Secondary retrieval and boundary families use Holm correction [15], and ablations remain exploratory.

### E. Reproducibility and leakage controls

The experiment runner records SHA-256 values for the corpus, QA reference, boundary reference, configurations, and the chunking, unit-construction, semantic, and evaluation implementations. It also records the Python and package versions, platform and hardware metadata, random seed, full command, and method parameters. Official-framework versions are pinned. These records make a numerical result attributable to one immutable set of inputs and code rather than only to a method label.

The runner rejects provisional boundary labels or unreviewed QA unless an explicit exploratory flag is supplied, in which case the output is marked non-reportable. Embedding caches are cleared between methods so execution order cannot reuse method-specific representations. Locked P04--P10 metrics remain unavailable until all projects pass the collective release gate. After release, the same frozen unit inventory, contextual prefix, encoder, maximum length, and question set are used across methods. The review package is designed to contain the non-data-dependent implementation, frozen configurations, annotation schemas, synthetic schema-valid examples, tests, aggregate analysis, and run manifests. These controls cannot substitute for public raw data, but they expose the decision surface and make accidental test-set tuning detectable.

## V. Results

### A. Aggregate boundary and retrieval quality

**TABLE III. LOCKED BOUNDARY AND RETRIEVAL RESULTS**

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

SEAC obtains boundary F1 0.5673 (95% CI [0.4708, 0.6488]). Structure-only is the strongest non-SEAC boundary baseline at 0.5032. SEAC is higher on all six eligible projects, but the paired difference +0.0640 does not survive Holm correction (adjusted p=0.4375).

For retrieval, full SEAC reaches unit MRR 0.7937 (95% CI [0.7060, 0.8772]), span MRR 0.8561, and Hit@5 0.9520. The strongest non-SEAC baseline is structure-only at unit MRR 0.7290, an observed difference of +0.0647. The prespecified comparison with LlamaIndex yields a paired mean difference of +0.1003 (95% CI [0.0042, 0.2008]). The exact two-sided p-value is 0.0625, so the confirmatory test is not significant at 0.05. The difference between the bootstrap interval and exact test reflects inference with only six clusters and the coarse 1/64 randomization grid.

### B. Cross-project and question-type evidence

[[FIGURE:PROJECT_MRR|Locked unit MRR by project for full SEAC and the prespecified LlamaIndex comparator. SEAC is higher on five of six eligible projects.]]

SEAC exceeds LlamaIndex on P04, P05, P07, P08, and P09 and is lower on P06. The gains are therefore not produced by one project alone, but their magnitude varies substantially. P04 and P05 show the largest positive differences; P06 shows a negative difference of approximately 0.040. This heterogeneity motivates project-level inference rather than treating 125 questions as independent replicates.

**TABLE IV. RETRIEVAL BY QUESTION TYPE FOR FULL SEAC**

| Question type | n | Unit MRR | Hit@5 | Span MRR |
|---|---:|---:|---:|---:|
| API | 17 | 0.8211 | 0.9412 | 0.8211 |
| API parameter | 10 | 0.7833 | 1.0000 | 0.7833 |
| Constraint | 16 | 0.9167 | 0.9375 | 0.9167 |
| Database | 31 | 0.7323 | 1.0000 | 0.9839 |
| Feature | 23 | 0.7867 | 0.9130 | 0.7867 |
| Meeting fact | 9 | 0.8370 | 1.0000 | 0.8370 |
| Numeric constraint | 9 | 1.0000 | 1.0000 | 1.0000 |
| Process | 10 | 0.5429 | 0.8000 | 0.5429 |

Numeric constraints are easiest on this sample, while process questions remain difficult. Database questions have span MRR 0.9839 but unit MRR 0.7323. This gap is consistent with repeated schema text: the system often retrieves text that supports the answer but not the annotated source row. Reporting only textual hits would conceal this provenance error.

Two limitations are visible without constructing a post-hoc error taxonomy. First, P06 reverses the overall direction: full SEAC obtains 0.6805 MRR there versus 0.7206 for LlamaIndex. This project is also the largest locked project by unit count, so the reversal cannot be dismissed as a tiny corpus artifact. It shows that structural enrichment is not uniformly beneficial and supports reporting project-clustered effects. Second, process questions obtain only 0.5429 MRR and 0.8000 Hit@5. Such questions often require a sequence distributed across adjacent units; a child may match one step while the annotated evidence unit lies elsewhere in the parent or another section. This is a mechanism-consistent interpretation, not a manually validated error count.

The database result illustrates a different failure mode. Span Hit@5 is perfect for the category, yet unit MRR trails the span metric by 0.2516. Repeated column names, constraints, and template phrases can make textually equivalent rows interchangeable to an embedding model even when only one row is the accountable source. The gap validates the decision to retain source-unit identifiers and to score both span support and provenance. In an audit workflow, a plausible value from the wrong schema row is not an acceptable success.

### C. Component ablations

**TABLE V. LOCKED SINGLE-COMPONENT ABLATIONS**

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

The evidence does not support every component of full SEAC. Removing table-row children produces the largest adverse change, reducing unit MRR by 0.1084 and Hit@5 from 0.9520 to 0.8640. In contrast, removing parent fusion or heading context improves MRR on the locked sample. Removing semantic distance has no observed MRR effect, and the remaining boundary-score ablations change MRR by at most 0.0042.

The ablation family is exploratory, and the paired SEAC-minus-no-table-children result has Holm-adjusted p=1.0000. We therefore interpret the drop as evidence that header-aware row children are the most promising component, not proof of causal necessity. The no-fusion result is likewise not promoted to a new primary method: doing so would select a method after inspecting locked data. A fresh protocol on independent projects is required to test a simplified child-only design.

### D. Resource cost

**TABLE VI. LOCKED-SET RESOURCE COST**

| Method | Vectors | Chunking (s) | Indexing (s) | Query mean / p95 (ms) |
|---|---:|---:|---:|---:|
| Structure-only | 8,891 | 0.25 | 139.46 | 15.18 / 18.40 |
| Full SEAC | 48,912 | 112.11 | 351.12 | 20.93 / 27.28 |

Full SEAC uses 5.50x as many retrieval vectors as structure-only. Mean query latency increases by 5.74 ms, while construction and indexing are also substantially more expensive. The method is therefore appropriate when evidence granularity and traceability justify a larger index, not as a universally cheaper replacement.

## VI. Discussion

### A. Supported mechanism

The clearest supported design choice is row-level retrieval under coherent table context. A whole-table vector can dilute field-level distinctions, while a row alone can lose its schema. A child containing the header and one row preserves localization and interpretability. The difference between SEAC parents (MRR 0.6796) and full SEAC (0.7937), together with the no-table-child ablation (0.6853), is consistent with this mechanism.

The complete scoring story is less secure. Parent fusion may overweight broad context, and repeated heading prefixes may reduce embedding discrimination in template-heavy corpora. Density and semantic-boundary signals can still affect parent coherence even when they do not improve the locked retrieval metric, but the present data do not justify claiming that they are necessary. The appropriate next experiment is a prespecified comparison of full SEAC, a child-only simplification, and the strongest structure-only baseline on new projects.

### B. Industrial implications

Stable provenance is useful beyond question answering. Returned unit identifiers can support requirements traceability, change-impact review, evidence citations, and audit logs. The deterministic boundary pass is easier to inspect than generated propositions. Deployment teams can restrict children to tables or other high-value structures when index cost is binding. Because the corpus contains confidential artifacts, every retrieved fragment remains subject to the access-control and privacy policy of its source document.

Unit and span metrics answer different operational questions. A high span score with a lower unit score indicates that the system found supporting text but not the preferred accountable location. The opposite indicates that a correct parent contains the annotated source unit but dilutes or alters the minimal supporting span. Enterprise review workflows often require both textual support and the original artifact location.

### C. Next evaluation protocol

The next study should treat the present ablations as hypotheses, not as a menu from which to choose a new winner. Before any new test project is opened, the protocol should freeze three primary candidates: full SEAC, the child-only no-fusion simplification suggested by the current results, and the structure-only baseline. Table children should remain header-aware in both SEAC candidates. Project inclusion, the minimum number of approved questions, annotation-agreement thresholds, the encoder, and the primary metric should again be declared in advance. A larger number of independent projects is more valuable than merely adding questions to the same six clusters because project-level inference is the present statistical bottleneck.

That replication should add two evidence layers. First, a public multilingual technical-document benchmark, even if smaller and less industrially realistic, would permit exact third-party reproduction and reveal whether the Chinese engineering lexicon or document templates drive the result. Second, an access-controlled end-to-end study should pass each method's returned parent to the same generator and score answer correctness, citation validity, unsupported claims, and reviewer time. Retrieval quality and generation quality need separate reporting: a generator may ignore correct evidence, while an apparently correct answer may cite the wrong source unit.

Operational evaluation should also report index-update cost, access-control filtering, and stale-evidence behavior. Parent/child indices introduce more vectors and an extra provenance mapping that must remain correct after a source document changes. A deployment is successful only if a child hit can still be traced to the current authorized parent and original artifact version. These tests would convert the current mechanism-level industrial result into evidence about maintainability and safe downstream use.

## VII. Validity, Ethics, and Data Availability

Construct validity is limited in two ways. Boundary F1 measures agreement with annotated parent cuts rather than downstream answer quality, and retrieval metrics isolate chunk construction without establishing the faithfulness of a particular generator. Although 77.5% of promoted boundary candidates are human verified, the retained LLM prelabels can share structural bias with SEAC. The human-model agreement gate and P08 withholding reduce but do not eliminate this risk. Rule-generated QA drafts may also overrepresent local factual questions despite human review. We mitigate these concerns by reporting question types, exact supporting spans, provenance-aware unit metrics, and all axis-specific exclusions.

Internal validity depends on the extracted unit inventory. Parsing or legacy-conversion errors can move boundaries before any splitter is applied; all methods share those units, so a common error may remain invisible in a relative comparison. Hash tracking, version-pinned framework implementations, visual sampling of conversions, collective release gates, and recorded configurations reduce accidental drift, but not every cell of every proprietary source was independently checked. The mixed boundary reference is another potential common cause: a structural labelling bias shared by the prelabelling model and SEAC could inflate apparent agreement even though SEAC invokes no language model during chunking.

Conclusion validity is constrained by only six independent projects on each inference axis. Project sizes and question counts are unequal; hierarchical bootstrap intervals address clustering but cannot create additional independent projects. The project-level exact test is necessarily coarse: with six matched projects its assignment space contains 64 outcomes. Results are therefore reported as observed effect sizes, intervals, and project-level consistency rather than proof of general superiority. The exploratory ablation family also should not be used to select a winning configuration on the same locked set.

External validity is limited because the corpus is Chinese and dominated by industrial management systems. Generalization to English artifacts, source code, issue trackers, safety-critical systems, or image-heavy PDF and presentation sources is not established. The six disclosed unsupported PDF/PPTX sources may differ systematically from the parsed material. The experiment compares chunking methods under one dense encoder and one frozen contextual-prefix design; another encoder, sparse-dense hybrid, reranker, or generator could change the ordering. Public multilingual replications and an end-to-end answer study are needed before deployment claims are broadened.

Raw documents and derived QA cannot be publicly released because they contain proprietary and potentially identifying information. A review artifact can include implementation code, frozen configurations, annotation schemas, synthetic schema-valid examples, aggregate results, tests, manifests, and hashes. Broader release requires written data-owner approval. Controlled access may be requested but is not guaranteed. These constraints limit independent reproduction of the exact corpus while still allowing inspection of the method and analysis pipeline.

No AI system made privacy, release, or authorship decisions. A large language model acted only as a blinded boundary-prelabelling instrument and an advisory QA pre-reviewer; authorized humans issued all retained QA and privacy decisions. The study reports these roles because the model is part of the benchmark construction process rather than merely a writing aid.

## VIII. Conclusion

SEAC constructs provenance-aware parent and child chunks for heterogeneous enterprise software-engineering documents. On a frozen ten-project industrial study, full SEAC achieves unit MRR 0.7937 and Hit@5 0.9520, with an observed advantage over non-SEAC baselines and the prespecified LlamaIndex comparator. The exact confirmatory p-value is 0.0625, so the study does not establish general statistical superiority. Cross-project results show improvement on five of six eligible projects, and ablations identify header-aware table-row children as the strongest supported component while challenging the value of heading context and parent fusion. Replication on additional independent and public multilingual projects is required before simplifying or generalizing the method.

## Acknowledgment

OpenAI Codex was used to assist with manuscript restructuring, language drafting, figure-code generation, and consistency checks. The authors verified the cited sources, analysis outputs, numerical values, figures, and prose and remain responsible for all research and publication decisions.

## References

[1] P. Lewis et al., “Retrieval-Augmented Generation for Knowledge-Intensive NLP Tasks,” in *Advances in Neural Information Processing Systems*, vol. 33, 2020.

[2] V. Karpukhin et al., “Dense Passage Retrieval for Open-Domain Question Answering,” in *Proceedings of EMNLP*, 2020, pp. 6769--6781.

[3] N. Reimers and I. Gurevych, “Sentence-BERT: Sentence Embeddings using Siamese BERT-Networks,” in *Proceedings of EMNLP-IJCNLP*, 2019, pp. 3982--3992.

[4] J. Chen, S. Xiao, P. Zhang, K. Luo, D. Lian, and Z. Liu, “M3-Embedding: Multi-Linguality, Multi-Functionality, Multi-Granularity Text Embeddings Through Self-Knowledge Distillation,” in *Findings of ACL*, 2024, pp. 2318--2335.

[5] T. Chen et al., “Dense X Retrieval: What Retrieval Granularity Should We Use?” in *Proceedings of EMNLP*, 2024, pp. 15159--15177.

[6] M. Guenther, I. Mohr, D. J. Williams, B. Wang, and H. Xiao, “Late Chunking: Contextual Chunk Embeddings Using Long-Context Embedding Models,” arXiv:2409.04701, 2024.

[7] P. Sarthi et al., “RAPTOR: Recursive Abstractive Processing for Tree-Organized Retrieval,” in *International Conference on Learning Representations*, 2024.

[8] C. Auer et al., “Docling Technical Report,” arXiv:2408.09869, 2024.

[9] J. L. C. Guo, J.-P. Steghoefer, A. Vogelsang, and J. Cleland-Huang, “Natural Language Processing for Requirements Traceability,” arXiv:2405.10845, 2024.

[10] S. J. Ali, V. Naganathan, and D. Bork, “Establishing Traceability Between Natural Language Requirements and Software Artifacts by Combining RAG and LLMs,” in *Conceptual Modeling*, 2024, pp. 295--314.

[11] LangChain, “langchain-text-splitters 1.1.2,” Python Package Index, 2026.

[12] LlamaIndex, “SentenceSplitter, llama-index-core 0.14.23,” Python Package Index, 2026.

[13] J. Cohen, “A Coefficient of Agreement for Nominal Scales,” *Educational and Psychological Measurement*, vol. 20, no. 1, pp. 37--46, 1960.

[14] B. Efron, “Bootstrap Methods: Another Look at the Jackknife,” *The Annals of Statistics*, vol. 7, no. 1, pp. 1--26, 1979.

[15] S. Holm, “A Simple Sequentially Rejective Multiple Test Procedure,” *Scandinavian Journal of Statistics*, vol. 6, no. 2, pp. 65--70, 1979.

[16] X. Wang et al., “Searching for Best Practices in Retrieval-Augmented Generation,” in *Proceedings of EMNLP*, 2024, pp. 17716--17736.

[17] A. V. Duarte et al., “LumberChunker: Long-Form Narrative Document Segmentation,” in *Findings of EMNLP*, 2024, pp. 6473--6486.

[18] Z. Wang et al., “Document Segmentation Matters for Retrieval-Augmented Generation,” in *Findings of ACL*, 2025, pp. 8063--8075.

[19] J. Zhao et al., “MoC: Mixtures of Text Chunking Learners for Retrieval-Augmented Generation System,” in *Proceedings of ACL*, 2025, pp. 5172--5189.

[20] D. Rau et al., “BERGEN: A Benchmarking Library for Retrieval-Augmented Generation,” in *Findings of EMNLP*, 2024, pp. 7640--7663.

[21] N. F. Liu et al., “Lost in the Middle: How Language Models Use Long Contexts,” *Transactions of the Association for Computational Linguistics*, vol. 12, pp. 157--173, 2024.

[22] J. Mucha, A. Kaufmann, and D. Riehle, “A Systematic Literature Review of Pre-Requirements Specification Traceability,” *Requirements Engineering*, vol. 29, pp. 119--141, 2024.

[23] B. Wang, Z. Zou, H. Wan, Y. Li, Y. Deng, and X. Li, “An Empirical Study on the State-of-the-Art Methods for Requirement-to-Code Traceability Link Recovery,” *Journal of King Saud University—Computer and Information Sciences*, vol. 36, art. 102118, 2024.
