# SEAC: Structure- and Key-Information-Aware Adaptive Semantic Chunking for Retrieval-Augmented Software Engineering Documents

Author information withheld from the working draft.

## Abstract

Retrieval-augmented generation (RAG) systems commonly index fixed or recursively
split passages. Enterprise software-engineering documents violate these
strategies' assumptions: headings carry module context, tables encode API and
database contracts, and short expressions such as “required,” “primary key,” or
an error code can determine whether evidence is usable. We present SEAC, a structure- and
key-information-aware adaptive semantic chunking method for heterogeneous
software-engineering documents. SEAC represents table rows and text blocks as
traceable units; combines adjacent-unit semantic distance, section transitions,
and key-information-density changes; protects coupled groups; and adapts its
decision threshold and target parent size. It retrieves fine-grained children
and returns coherent parents.

We define a frozen, leakage-controlled evaluation of ten Chinese enterprise
software projects containing 89 documents, 31,841 parsed blocks, 66,981
retrieval units, and approximately 1.68 million regex tokens. The design uses
development, validation, and seven locked cross-project test sets; independent blinded
boundary annotation; provenance-aware unit evidence; evidence-span metrics;
official LangChain and LlamaIndex baselines; component ablations; and
project-clustered bootstrap intervals. On the locked test set, SEAC
achieves 0.5673 boundary F1 and 0.7937 unit-level
MRR, differing from the strongest non-SEAC baseline by
an absolute MRR gain of 0.0647 while maintaining 0.9520 Hit@5. The ablation
study found the largest observed unit-MRR reductions after removing table-row children. These findings
show an observed retrieval advantage for SEAC over the prespecified LlamaIndex comparator, but not a statistically significant confirmatory advantage.

**Keywords:** retrieval-augmented generation; software-engineering documents;
semantic chunking; structure-aware retrieval; hierarchical retrieval;
requirements engineering; enterprise document intelligence

## 1. Introduction

Requirements specifications, detailed designs, API specifications, database
dictionaries, technical agreements, proposals, research reports, and meeting
minutes record the decisions that make an enterprise software system
implementable and maintainable. These artifacts are heterogeneous but tightly
related. An interface method is meaningful only under its endpoint and module;
a database field is meaningful under its table and constraints; and a meeting
decision may depend on its topic, owner, and deadline. When this information is
used as an external knowledge base, the retrieval unit determines whether a
downstream model receives a complete, attributable answer or a fragment stripped
of its engineering context.

RAG combines parametric generation with retrieved non-parametric evidence [1].
Its effectiveness therefore depends not only on the retriever and generator but
also on the construction of the searchable corpus. Dense retrieval makes the
choice of unit especially consequential: different granularities trade factual
specificity against contextual completeness [4]. Fine-grained proposition
retrieval can improve localization [5], whereas hierarchical methods can recover
broader context for questions that require it [7]. Contextual embedding methods
similarly address information lost when a chunk is encoded in isolation [6].
These findings establish retrieval granularity as a modeling choice rather than
a neutral preprocessing detail.

Generic chunking nevertheless remains ill-suited to many engineering artifacts.
A fixed window may bisect a requirement or table row. Character-recursive
splitting respects separators but not the functional role of a boundary.
Embedding-based semantic splitting can cut between lexically dissimilar but
functionally coupled elements, such as an API parameter table and its constraint
paragraph. Heading-only splitting preserves a document tree but can return an
entire long design section when the query asks for one field. Overlap partially
mitigates boundary loss at the cost of duplicate indexing and ambiguous source
attribution.

This paper studies chunking as a first-class problem for retrieval over
enterprise software-engineering documents. We propose SEAC, which combines
semantic, structural, and key-information signals at adjacent document-unit
boundaries. SEAC makes four design commitments. First, it preserves source
identity from parsed block to retrieval result. Second, rows belonging to the
same table form a protected parent group, while row-level children retain
fine-grained retrievability. Third, threshold and size decisions adapt to the
structural composition and information density of each document. Fourth,
heading-path metadata is applied consistently at retrieval time, and a
parent–child fusion score ranks coherent parents using their most relevant
children.

Evaluating this problem requires more than block-level retrieval labels. A
method can appear correct when it retrieves the right table but the wrong row.
We therefore define a benchmark protocol with human boundary judgments,
unit-level evidence identifiers, exact supporting spans, blinded annotation,
and locked projects. The corpus covers ten independent enterprise software
projects and multiple artifact types rather than a single document template.
The evaluation isolates chunking by holding the dense encoder, retrieval
context, maximum length, and question set constant across methods. This control
is important because broader RAG studies show that preprocessing, retrieval,
reranking, and generation choices interact [16], while standardized
benchmarking remains difficult across fragmented pipelines [20].

This work makes the following contributions:

1. It formalizes boundary detection and provenance-aware evidence retrieval for
   heterogeneous software-engineering documents.
2. It introduces SEAC, an adaptive boundary model coupled with protected
   structures and hierarchical parent–child retrieval.
3. It constructs and documents a ten-project industrial benchmark protocol with
   blinded annotation, row-level table evidence, privacy gates, and a locked
   cross-project test split.
4. It provides a reproducible comparison with six core baselines (including two
   official framework splitters), a non-hierarchical SEAC variant, and seven
   controlled ablations, together with project-clustered confidence intervals
   and paired tests.

The rest of the paper reviews relevant retrieval and software-engineering work,
defines the task, presents SEAC, describes the frozen study, reports the formal
results, and discusses validity, privacy, and reproducibility.

## 2. Background and Related Work

### 2.1 Retrieval granularity in RAG

The original RAG formulation retrieves external passages to condition
generation [1], while dense passage retrieval established efficient semantic
matching over large collections [2]. Sentence-BERT made cosine-comparable
sentence embeddings practical for semantic search [3]. These developments
created a standard pipeline in which documents are segmented, encoded, and
ranked. Segmentation is often configured with a window size and overlap, even
though this choice changes both the evidence available to the retriever and the
number of indexed vectors.

End-to-end RAG studies further show that segmentation cannot be treated in
isolation from the rest of the pipeline, motivating controlled comparisons in
which the encoder and retrieval procedure remain fixed [16]. BERGEN similarly
identifies inconsistent experimental configurations as a barrier to comparing
RAG components and provides a standardized evaluation library [20]. SEAC adopts
the same isolation principle at the chunking layer and records every frozen
input, implementation, and configuration hash.

Recent work directly questions the passage as the default unit. Dense X
Retrieval shows that proposition-level units can outperform coarser passages
for fact-oriented retrieval [5]. Late Chunking encodes a long context before
pooling chunk representations, retaining surrounding information without
changing the visible chunk boundary [6]. RAPTOR recursively clusters and
summarizes text into a tree, enabling retrieval at multiple abstraction levels
[7]. These approaches address different failure modes: localization,
out-of-context representations, and missing global context. SEAC is
complementary. It changes the boundary construction itself and explicitly
models semi-structured engineering elements, then uses a shallow hierarchy to
separate retrieval granularity from returned-context granularity.

More recent methods learn or generate document boundaries directly.
LumberChunker asks an LLM to locate semantic shifts in long-form narratives
[17]. PIC uses a document summary as a pseudo-instruction for grouping related
sentences [18], while MoC combines multiple LLM-based chunking learners and
proposes intrinsic boundary-quality measures [19]. These studies reinforce that
segmentation is an optimization target, but their learned or generated
boundaries are evaluated mainly on public narrative or open-domain QA data.
SEAC instead targets semi-structured engineering artifacts, uses deterministic
observable signals after parsing, and evaluates boundaries against blinded
human judgments. Moreover, simply enlarging returned context is not a complete
alternative: long-context models can still underuse evidence depending on its
position [21].

### 2.2 Structure-aware technical document processing

Modern document pipelines recover headings, reading order, and tables rather
than flattening every file into plain text. Docling, for example, represents
document structure and table layout in a unified conversion pipeline [8]. Such
parsing is necessary but does not by itself decide which adjacent elements
should share a retrieval unit. A preserved table may still be indexed as one
oversized chunk, while row-only indexing may discard its header and section
context.

Widely used RAG frameworks expose recursive or sentence-aware splitters. The
LangChain `RecursiveCharacterTextSplitter` recursively applies ordered
separators [11], while the LlamaIndex `SentenceSplitter` favors sentence and
paragraph boundaries [12]. We evaluate their official implementations rather
than approximate them locally. Both are strong engineering baselines, but
their boundary decisions are intentionally domain-agnostic.

### 2.3 Retrieval and traceability for software artifacts

Traceability links software artifacts so engineers can reason about quality and
evolution. A recent overview of NLP for requirements traceability emphasizes
the role of textual and learned representations in recovering relations among
artifacts [9]. RAG and language models have also been combined to connect
natural-language requirements with software artifacts [10]. These studies
motivate retrieval that is not merely topically related but attributable to a
specific requirement, interface element, database row, or decision.

The industrial corpus also includes meeting records, technical agreements, and
design documents that precede or surround a requirements specification. A
systematic review of pre-requirements traceability shows that such sources,
including meeting protocols and legacy systems, are important but less studied
than requirement-to-code links [22]. At the post-specification end, a recent
multi-dataset comparison of requirement-to-code recovery methods reports both
benchmark inconsistency and the value of fine-grained, information-rich text
representations [23]. Together, these findings motivate preserving stable
source units before any downstream trace link is inferred.

Most traceability research evaluates artifact-to-artifact links. Our task is
earlier in the pipeline: constructing evidence units from the documents that
will later support question answering, traceability, or impact analysis. If a
chunk loses its source element or fuses multiple rows, even a strong retriever
cannot provide precise provenance.

### 2.4 Research gap

Existing work establishes three useful principles: retrieval granularity
matters, context should accompany small units, and structured parsing should
preserve document elements. What remains underexplored is a boundary mechanism
that integrates these principles for heterogeneous software-engineering
documents and is evaluated with human boundary labels plus exact evidence
provenance. SEAC addresses this gap without invoking an LLM during chunking,
which keeps the method deterministic after parsing and permits direct ablation
of each boundary signal. In contrast to LumberChunker [17], PIC [18], and MoC
[19], the study jointly evaluates human parent-boundary agreement,
provenance-aware retrieval, exact supporting spans, cross-project transfer, and
runtime cost on heterogeneous enterprise software artifacts.

## 3. Task Formulation

### 3.1 Document units and provenance

Let a parsed document be an ordered sequence

\[
D=(u_1,u_2,\ldots,u_n).
\tag{1}
\]

Each unit contains text, a stable unit identifier, its source block identifier,
document type, block type, heading path, and an optional protected-group
identifier. Paragraphs and headings are units. A table with header row \(h\) is
expanded into row units. For data row \(r_j\), the rendered unit contains both
the table header and the row values, so a retrieved row remains interpretable:

\[
t(r_j)=\text{header}(h)\;\Vert\;\text{row}(r_j).
\tag{2}
\]

All chunks store the ordered source unit and block identifiers from which they
were assembled. This mapping is used for evaluation and citation rather than
recovering provenance through string matching.

### 3.2 Boundary detection

A candidate boundary exists between each adjacent pair \((u_i,u_{i+1})\).
Each candidate carries one reference label, `BREAK`, `NO_BREAK`, or `NOISE`,
produced by the annotation procedure of Section 5.3. `NOISE` identifies
navigation, footers, empty templates, or unusable conversion fragments and is
excluded from binary precision, recall, and F1. For the remaining labels, a
predicted boundary is positive when two adjacent units belong to different
SEAC parents. This definition evaluates semantic parent boundaries; row-level
children do not create additional boundary positives.

### 3.3 Evidence retrieval

For a question \(q\), let \(G_q\) be its gold evidence set and \(z_q\) its
minimal supporting text span. \(G_q\) holds every corpus unit that carries
\(z_q\), which is a single unit for most questions and several units when a
project's documents reuse the passage verbatim, as Section 5.4 describes. A
ranked returned parent is provenance-correct when its source-unit list contains
any member of \(G_q\). Unit MRR and Hit@\(k\) therefore penalize retrieval of the
correct table but an unrelated row when no gold row is in the returned parent,
while not penalizing a method for returning one legitimate copy of a passage
instead of another. Span MRR and span Hit@\(k\) measure whether the returned text
contains \(z_q\). Span metrics remain complementary rather than strictly
stronger: they accept any text match, including a location outside \(G_q\) whose
similarity is coincidental, whereas unit metrics require annotated provenance.

## 4. SEAC

### 4.1 Overview

SEAC has four stages: unit construction, boundary scoring, adaptive parent
assembly, and hierarchical child construction. The method uses BGE-M3 dense
embeddings in the formal experiments [4], but its structure and density signals
are encoder-independent. No query or QA label is used during chunking.

### 4.2 Key-information density

The implementation uses a frozen Chinese lexicon of engineering expressions.
Examples include *bìxū* (“must”), *bùdé* (“must not”), and
*zuìdà/zuìxiǎo* (“maximum/minimum”) for obligations and limits;
*zhǔjiàn/wàijiàn* (“primary/foreign key”) for database constraints; and terms
for request method and path, parameter, return, field, status, interface, table
name, process, and constraint. The exact UTF-8 literals and configuration hash
are included in the reproducibility package. For unit \(u\), density is

\[
\rho(u)=\frac{\sum_{v\in V}\operatorname{count}(v,u)}{
\max(1,\operatorname{tokens}(u))}.
\tag{3}
\]

The lexicon is deliberately small and fixed before test evaluation. It is not a
claim of complete domain entity recognition; instead, it provides a transparent
signal for abrupt changes between technically dense and narrative regions.

### 4.3 Boundary score

For adjacent units, the semantic distance is

\[
d_i=1-\cos(e_i,e_{i+1}),
\tag{4}
\]

where \(e_i\) is the normalized dense embedding. The structural score is

\[
s_i=\min\{1,
\mathbb{1}[u_{i+1}\text{ is a heading}]
+0.7\mathbb{1}[path_i\ne path_{i+1}]
+0.5\mathbb{1}[group_i\ne group_{i+1}\land(group_i\lor group_{i+1})]
\}.
\tag{5}
\]

The key-density-change score is

\[
k_i=\min\{1,10|\rho(u_i)-\rho(u_{i+1})|\}.
\tag{6}
\]

Continuity penalties discourage cuts within a section or protected table:

\[
p_i=0.35\mathbb{1}[path_i=path_{i+1}\ne\emptyset]
+1.0\mathbb{1}[group_i=group_{i+1}\ne\emptyset].
\tag{7}
\]

The combined boundary score is

\[
b_i=0.45d_i+0.30s_i+0.25k_i-p_i.
\tag{8}
\]

The weights are fixed in the protocol and removed one component at a time in
the ablation study.

### 4.4 Adaptive threshold and parent size

Let \(Q_{0.8}(b)\) be the 80th percentile of boundary scores within a document,
\(r_t\) its fraction of table-row units, and \(r_h\) its fraction of headings.
SEAC uses

\[
\theta_D=Q_{0.8}(b)+0.08r_t-0.05r_h.
\tag{9}
\]

Table-heavy documents receive a slightly higher threshold because row
transitions are frequent and often protected, while heading-rich documents
receive a lower threshold to respect explicit organization.

Parent construction is a left-to-right deterministic pass. The hard maximum is
512 regex tokens. The soft target is 320 tokens in ordinary regions and 224
tokens when mean density in the current parent is at least 0.03. A scored or
size-based cut is suppressed when it would split a protected group, detach a
heading from its first content unit, or leave the current parent below 48
tokens. A new heading creates a boundary, subject to the current parent being
nonempty. Units longer than the hard maximum are split at sentence, punctuation,
line, or list boundaries before token fallback.

### 4.5 Hierarchical children and contextual retrieval text

Each assembled parent retains a coherent segment and is the object returned to
the downstream RAG system. Non-heading, non-noise units also produce retrieval
children with a target of 112 and maximum of 176 tokens. Table rows normally
become individual children; a long row is logically split while retaining the
same row provenance. Each retrieval text is prefixed with document type,
heading path, and the leaf topic. The same contextual prefix is applied to every
comparison method in the fair-context condition.

For parent \(p\), dense parent score \(S_p\), and child scores \(S_c\), SEAC
ranks parents by

\[
S(p,q)=0.35S_p(q)+0.65\max_{c\in children(p)}S_c(q).
\tag{10}
\]

This design allows a precise row or sentence to activate its parent while the
returned context preserves nearby engineering information. The no-fusion
ablation sets the parent weight to zero; the no-table-children ablation removes
only row children and leaves other children intact.

### 4.6 Complexity

For \(n\) units in a document, SEAC requires \(n\) unit embeddings, \(n-1\)
adjacent similarities, and a linear assembly pass. Excluding encoder cost,
boundary construction is \(O(n)\) in time and storage. Index size grows with the
number of parents plus children. We therefore report parent count, child count,
chunking time, indexing time, and query latency rather than treating retrieval
quality as cost-free.

## 5. Industrial Study Design

### 5.1 Research questions

The frozen study addresses five questions:

- **RQ1:** Does SEAC identify human semantic parent boundaries more accurately
  than generic and structure-only chunkers?
- **RQ2:** Does SEAC improve provenance-aware unit retrieval and supporting-span
  retrieval under a shared dense encoder?
- **RQ3:** Do the effects generalize across projects, document types, and
  question types that were not used for parameter selection?
- **RQ4:** Which semantic, structural, density, threshold, context, table-child,
  and fusion components account for the observed effect?
- **RQ5:** What index-size and runtime costs accompany SEAC's hierarchical
  representation?

### 5.2 Corpus

The corpus contains Chinese-language artifacts from ten enterprise software
projects. Project identities are replaced with P01–P10 in the study outputs.
The parser extracts paragraphs, headings, and tables from DOCX and XLSX files;
legacy Word sources are converted non-destructively and tracked by SHA-256.
Six PDF or PPTX sources that were not supported by the frozen parser are
disclosed as substantive skips rather than silently omitted. Table 1 summarizes
the frozen corpus and split.

**Table 1. Frozen industrial corpus and project split.**

| Project | Split | Documents | Blocks | Units | Approx. tokens | Tables | Table rows |
|---|---|---:|---:|---:|---:|---:|---:|
| P01 | Development | 7 | 4,790 | 10,574 | 194,119 | 839 | 6,623 |
| P02 | Development | 8 | 4,849 | 10,797 | 224,422 | 986 | 6,934 |
| P03 | Validation | 7 | 3,075 | 5,193 | 103,562 | 412 | 2,530 |
| P04 | Locked test | 17 | 4,180 | 8,532 | 301,754 | 474 | 4,826 |
| P05 | Locked test | 17 | 3,886 | 8,166 | 228,920 | 464 | 4,744 |
| P06 | Locked test | 12 | 5,623 | 10,122 | 255,278 | 758 | 5,257 |
| P07 | Locked test | 8 | 1,564 | 2,496 | 59,925 | 172 | 1,104 |
| P08 | Locked test | 5 | 1,206 | 4,369 | 123,307 | 198 | 3,361 |
| P09 | Locked test | 4 | 1,606 | 5,324 | 133,710 | 329 | 4,047 |
| P10 | Locked test | 4 | 1,062 | 1,408 | 52,903 | 62 | 408 |
| **Total** | — | **89** | **31,841** | **66,981** | **1,677,900** | **4,694** | **39,834** |

“Tokens” in this table are produced by the frozen regex tokenizer and are not
BGE WordPiece counts. The corpus includes requirements, API specifications,
database designs, detailed designs, technical agreements, meeting minutes,
proposals, and research reports. P01–P02 are development projects, P03 is used
for validation, and P04–P10 form a collectively locked test set. No test result
may change parameters, query wording, or baseline selection.

### 5.3 Boundary annotation

P01 contains a fully double-annotated and adjudicated set of 4,783 adjacent
boundaries: 1,065 `BREAK`, 3,393 `NO_BREAK`, and 325 `NOISE`. For each P02–P10,
300 boundaries are selected deterministically with seed 20260804 using
document- and structure-stratified sampling. The strata are heading transition,
section transition, same-section continuity, within-table, table transition,
and noise candidate.

P01 was labelled by two human annotators working from independently ordered
files, with disagreements resolved by a third judgment. For P02–P10 the same
2,700 blinded candidates were first labelled by a large language model acting as
a single annotator under the written label rules, with no access to SEAC
predictions, baseline outputs, retrieval scores, or experimental results. Every
pre-label records the model identifier, the decisive semantic relation, and a
review flag for cases the model could not resolve confidently.

Human verification then draws two disjoint groups per project from those
pre-labels: a deterministic stratum-proportional random sample of 100
candidates, and every remaining candidate the model flagged for review. The
verification workbook omits the pre-label, so the human decision is independent.
Human-model raw agreement and Cohen's kappa [13] are computed on the random
sample only, which keeps the estimate free of the flagged hard cases; the
flagged group is reported separately. Human labels override pre-labels wherever
the two disagree. A project is promoted to the formal gold standard only when
its verification rows are complete and its kappa reaches
0.70.

The first verification round exposed a gap in the noise rule rather than a
quality problem in either annotator. Three families of adjacent units carry no
retrievable evidence but were not enumerated in the written rule: bare structural
labels such as "output" or "processing logic", conversion fragments consisting
only of punctuation, and sign-off blocks closing a meeting record. The human
verifier and the model each applied the noise-priority clause to a different
subset of them, and within every family the disagreement was entirely
one-directional, which is characteristic of a definitional gap. Two of the three
families are confirmed by a label-independent corpus statistic: in one design
specification the token "output" occurs 289 times and "processing logic" 288
times, and in another document the fragment `}` occurs 267 times, so these units
are form scaffolding rather than evidence by construction. The rule was therefore
amended to settle these three families deterministically, and rows it settles are
excluded from the agreement statistic, since a label neither party judged carries
no information about their agreement.

We state plainly that the amendment was specified after inspecting the first
round of disagreements, which makes it a post-hoc specification, and we therefore
report agreement both before and after it: P02 kappa 0.87 after the amendment and 0.61 before it, with 94.0% raw agreement, P03 kappa 0.76 after the amendment and 0.56 before it, with 88.8% raw agreement, P04 kappa 0.89 after the amendment and 0.81 before it, with 96.3% raw agreement, P05 kappa 0.96 after the amendment and 0.66 before it, with 98.8% raw agreement, P06 kappa 0.91 after the amendment and 0.42 before it, with 97.2% raw agreement, P07 kappa 0.98 after the amendment and 0.75 before it, with 98.8% raw agreement, P08 kappa 0.65 after the amendment and 0.68 before it, with 84.5% raw agreement, P09 kappa 0.72 after the amendment and 0.75 before it, with 87.6% raw agreement, P10 kappa 0.76 after the amendment and 0.77 before it, with 89.9% raw agreement.
The amendment does not act uniformly in the direction that favours us. It raises
kappa where the three families are common and lowers it where they are rare,
raising it for P02, P03, P04, P05, P06, P07 and lowering it for P08, P09, P10, which argues against reading it as a
device tuned to clear the threshold. No further category was added once
P08 still fell below the threshold: its residual
disagreement concerns cover-page furniture, for which no label-independent
justification exists, so its boundary gold standard is withheld and boundary
metrics omit it. Its retrieval and question-answering metrics are unaffected.

Each promoted boundary therefore carries per-row provenance: rows settled by the
noise rule are `RULE_4_1`, independently verified rows are `HUMAN_VERIFIED`, and
the remainder are `LLM_PRELABEL`. Results in this paper rest on
5566 of 7183 (77.5%) human-verified boundaries,
387 of 7183 (5.4%) rule-determined boundaries, and
1230 of 7183 (17.1%) model-labelled boundaries, and the boundary
metrics must be read as agreement with this mixed reference rather than with an
exhaustively human-annotated one. `NOISE` remains part of the annotation audit
but is excluded from binary boundary metrics. The label rules, the applied
interpretation record, the pre-label files with their notes, the blind
verification workbook, the agreement summary before and after the amendment, the
rule-conformance audit, and corpus hashes are retained as audit artifacts.

### 5.4 Question and evidence annotation

Candidate questions are generated from observable document patterns only to
reduce authoring cost, then treated as unapproved drafts. A mechanical check
first resolves each declared evidence unit against the corpus and verifies span
containment, parent consistency, and near-duplicate question groups. A language
model then performs an advisory content pre-review that records a verdict, a
difficulty estimate, and suggested privacy categories for each draft; it holds
no approval authority. Human reviewers must confirm naturalness, a unique
answer, the parent block, the minimal evidence unit, the supporting span,
question type, and privacy status. Table questions must resolve to a unique row.
A question is excluded rather than approved to meet a quota when its evidence is
ambiguous.

The pre-review found that most drafts cannot be approved as written:
71 of 262 drafts were approved as written, 124 required a rewrite, and 67 were rejected. The dominant cause is a property of enterprise
document sets rather than of any chunking method. A project's technical
agreement, proposal and requirements specification reuse whole passages verbatim,
so the same sentence legitimately exists in several documents and no single unit
can be defended as *the* evidence.

We therefore evaluate against an evidence equivalence set rather than one
location. For each question, every corpus unit containing the supporting span is
resolved automatically, and retrieval is credited for returning any member of
that set. A span that recurs beyond a ceiling of twelve occurrences is not
treated as an equivalence set: such a span, for instance a request method shared
by every interface in one specification, is not distinctive evidence, and the
question is rewritten to cite a locating span or is excluded. The ceiling is not
arbitrary. Across the drafts, span occurrence counts cluster at one to eight and
then jump to eighteen and above, with no draft in between, so the ceiling falls
inside an empty region of the observed distribution.

Question stems that named one document or one numbered clause were generalized
to ask about the fact itself, incomplete answers were completed from the cited
span, and per-project shortfalls were closed with supplementary drafts. Every
rewritten or supplementary draft is verified mechanically before it may enter the
candidate set: its span must occur in a real unit, stay within the occurrence
ceiling, and support the answer, where support permits paraphrase of a table row
but requires each substantive part of the answer to be traceable to the span.
Drafts whose evidence sits in a table with duplicated or placeholder column
labels are flagged, because character-level grounding cannot detect an answer
that assigns a column a role the header never states. The resulting candidate set
is 225 questions across 9 projects, 43 of them carrying an evidence equivalence set, with a per-project minimum of 18. Approval and privacy status are set only by
human reviewers.

Final human approval retained 195 of 215 candidates across 9 projects. The rejections are
concentrated in one generator defect rather than spread across the set:
20 candidates were rejected, of which 16 name a document section rather than asking the fact itself. Such a question is answerable, but its scope is
set by a document section instead of by the question, so it does not state an
information need that stands on its own.

Reviewing the retained questions against that same requirement exposed an
inconsistency in our own first pass. A question whose subject *was* a section
number had been rejected, while a question that merely carried a leading section
number in front of an otherwise self-contained subject had been approved; deleting
the number leaves the latter unchanged in meaning, evidence and answer. That part
of the requirement needs no judgement, so we fixed it as a mechanical rule —
a question may not begin with a section number — and applied it after the human
round. The rule recognizes a section number as a leading group of one or two
digits followed by at least one further dotted component, which excludes both a
four-digit year and a decimal quantity, and on this benchmark it selects
10 approved questions in P03, P04, P05, leaving 195 in the formal question files. It never rewrites a reviewer's decision: the
workbook keeps what the reviewer wrote and the exclusion is recorded separately,
so the override is auditable. This is a post-hoc specification, exactly as in
Section 5.3, and we report the counts on both sides of it.

Eight projects met the fifteen-question minimum. P10 retained 11 of the 15 required questions.
Its questions are individually valid and are retained on disk, but reporting a
per-project retrieval score from a set that small would misrepresent its
precision, so the project is withheld from retrieval metrics while its boundary
metrics are unaffected. Withholding is declared per axis in the run manifest with
a stated reason, and the readiness audit rejects the declaration on a project that
in fact meets the minimum, so the mechanism cannot quietly drop an inconvenient
result.

The automated experiment gate accepts only `KEEP`, `APPROVED`, or `VERIFIED`
records with an existing unit, a contained evidence span, and a privacy status
of `NO_RISK`, `ANONYMIZED`, or `CLEARED`. Questions with internal URLs,
credentials, personal identifiers, customer identity, security architecture,
or proprietary business information are anonymized or rejected according to
the release protocol.

P01's questions predate the candidate-set path and carry a separate privacy
release round: 27 P01 questions received an individual privacy verdict: 23 carried no identifiable information, 1 was cleared for controlled use under authorization, and 3 were rejected for stating a customer or affiliate identity in the answer and the evidence span, leaving 24 questions. The automatic pre-screen matches
the question, answer, evidence span, file name, and section path as one string, so
a flag identifies a category but not the field that triggered it. Only the first
three of those fields are published with the benchmark, and the distinction
decides the verdict: a name occurring solely in a source file name is not
released, whereas a name stated in an answer is. Each row was therefore resolved
per field before a verdict was given. Rejection was preferred over rewriting the
evidence span, because editing a span to remove a name would break the
span-containment check that the same gate enforces.

### 5.5 Comparison methods

All methods use a hard maximum of 512 tokens. Overlap-capable fixed and
recursive baselines use 64-token overlap. The comparison includes:

1. fixed-size windows;
2. a local recursive character splitter;
3. structure-only splitting at headings, protected groups, and maximum length;
4. semantic splitting at the document-level 80th percentile of adjacent
   BGE-M3 distance;
5. official LangChain `RecursiveCharacterTextSplitter` 1.1.2 [11];
6. official LlamaIndex `SentenceSplitter` 0.14.23 [12];
7. non-hierarchical SEAC parents;
8. full hierarchical SEAC.

Seven ablations remove semantic distance, density change, continuity penalties,
adaptive thresholds, heading context, table-row children, and parent–child
fusion one at a time. All retrieval comparisons use BGE-M3 dense vectors [4]
and the same heading-path context unless the context itself is the ablated
component.

The no-density variant also disables density-driven selection of the 224-token
parent target. The no-continuity variant removes both numeric penalty terms but
retains hard protected-group integrity so that the ablation does not create
structurally invalid table parents.

### 5.6 Metrics and statistical analysis

Boundary evaluation reports precision, recall, F1, and the confusion matrix.
Retrieval reports unit MRR and Hit@1/3/5, span MRR and span Hit@1/3/5, plus
per-project and per-question-type results. Efficiency measures include parent
and child count, token distribution, chunking time, index time, and mean,
median, and 95th-percentile query latency.

We use 10,000 hierarchical bootstrap samples, resampling projects and then
questions or boundary candidates within sampled projects, to form 95% intervals
[14]. The single confirmatory hypothesis compares SEAC with the official
LlamaIndex `SentenceSplitter` on unit MRR. This comparator and metric were frozen
before locked-test retrieval results were accessed. Its two-sided paired test
exchanges every matched observation within a project together and is not
multiplicity-adjusted because it is the sole confirmatory test. After the
predeclared axis-specific withholding, each evaluation axis contains six
eligible locked projects, so all 64 project-level assignments are enumerated.
The remaining 55 SEAC-versus-alternative retrieval tests form one
secondary family controlled by Holm's sequential correction; the 14 boundary-F1
comparisons form a separate Holm family [15]. Ablations are interpreted as
exploratory effect estimates rather than causal component tests. The random seed
is 20260804. Development and validation analyses are labeled separately from the
locked-test estimate.

### 5.7 Reproducibility controls

Each run records corpus, QA, and boundary SHA-256 values; Python and package
versions; the full command; platform; method parameters; and hashes of the
chunking, unit, semantic, and evaluation implementation. The runner rejects
provisional labels or unreviewed QA unless an explicit exploratory flag is used,
and any such run is marked non-reportable. Embedding caches are cleared between
methods so method order does not reuse representations. The P04–P10 metrics are
unavailable until every locked project passes the collective release gate.

## 6. Results

This section reports only the locked-test results produced after the formal
readiness audit and frozen GPU matrix completed.

### 6.1 RQ1: Boundary detection

Table 2 reports locked-test boundary performance.

**Table 2. Boundary detection on the locked P04–P10 projects.**

| Method | Precision | Recall | F1 | 95% CI |
|---|---:|---:|---:|---:|
| Fixed | 0.0000 | 0.0000 | 0.0000 | [0.0000, 0.0000] |
| Recursive | 0.0402 | 0.0340 | 0.0369 | [0.0051, 0.0783] |
| Structure-only | 0.3611 | 0.8298 | 0.5032 | [0.4108, 0.5859] |
| Semantic | 0.2920 | 0.5617 | 0.3843 | [0.2779, 0.4800] |
| LangChain recursive | 0.0074 | 0.0043 | 0.0054 | [0.0000, 0.0265] |
| LlamaIndex sentence | 0.4615 | 0.0511 | 0.0920 | [0.0367, 0.1570] |
| SEAC parents | 0.4321 | 0.8255 | 0.5673 | [0.4704, 0.6486] |
| **SEAC** | 0.4321 | 0.8255 | 0.5673 | [0.4708, 0.6488] |

Across the 6 boundary-eligible locked projects, SEAC obtained boundary F1 0.5673 (95% CI [0.4708, 0.6488]); the strongest non-SEAC boundary baseline was Structure-only at 0.5032. The absolute SEAC-minus-baseline difference was +0.0640 (paired 95% CI [0.0313, 0.0956], Holm-adjusted p=0.4375), and SEAC had the higher project-level F1 on 6/6 projects.

### 6.2 RQ2 and RQ3: Evidence retrieval and generalization

Table 3 reports locked-test provenance and supporting-span retrieval.

**Table 3. Evidence retrieval on the locked P04–P10 projects.**

| Method | Unit MRR | Hit@1 | Hit@3 | Hit@5 | Span MRR | Span Hit@5 |
|---|---:|---:|---:|---:|---:|---:|
| Fixed | 0.5158 | 0.3680 | 0.6240 | 0.7200 | 0.1350 | 0.1840 |
| Recursive | 0.6086 | 0.4480 | 0.7200 | 0.8080 | 0.7072 | 0.8480 |
| Structure-only | 0.7290 | 0.6080 | 0.8320 | 0.8880 | 0.8211 | 0.9040 |
| Semantic | 0.6999 | 0.5680 | 0.8240 | 0.8640 | 0.8121 | 0.8960 |
| LangChain recursive | 0.6460 | 0.4960 | 0.7440 | 0.8480 | 0.7329 | 0.8720 |
| LlamaIndex sentence | 0.6934 | 0.5520 | 0.8400 | 0.8880 | 0.7586 | 0.8960 |
| SEAC parents | 0.6796 | 0.5280 | 0.8160 | 0.8800 | 0.7770 | 0.8960 |
| **SEAC** | 0.7937 | 0.6800 | 0.9040 | 0.9520 | 0.8561 | 0.9520 |

SEAC achieved unit MRR 0.7937 and span MRR 0.8561. The strongest non-SEAC retrieval baseline was Structure-only with unit MRR 0.7290, an observed SEAC-minus-baseline difference of +0.0647. The prespecified confirmatory comparison against LlamaIndex sentence gave a paired mean difference of +0.1003 (95% CI [0.0042, 0.2008], exact two-sided p=0.0625); SEAC had higher project-level MRR on 5/6 retrieval-eligible locked projects. Its Hit@5 was 0.9520.

### 6.3 RQ4: Ablation study

Table 4 reports the frozen single-component ablations.

**Table 4. SEAC ablations on the locked P04–P10 projects.**

| Variant | Boundary F1 | Unit MRR | Hit@5 | Change in MRR |
|---|---:|---:|---:|---:|
| Full SEAC | 0.5673 | 0.7937 | 0.9520 | — |
| No semantic distance | 0.5698 | 0.7937 | 0.9520 | +0.0000 |
| No density change | 0.5639 | 0.7959 | 0.9520 | +0.0021 |
| No continuity penalty | 0.5718 | 0.7979 | 0.9520 | +0.0042 |
| Fixed threshold | 0.5731 | 0.7959 | 0.9520 | +0.0022 |
| No heading context | 0.5673 | 0.8129 | 0.9280 | +0.0192 |
| No table-row children | 0.5673 | 0.6853 | 0.8640 | −0.1084 |
| No parent fusion | 0.5673 | 0.8289 | 0.9520 | +0.0352 |

The largest observed unit-MRR change relative to full SEAC occurred for No table-row children (−0.1084). Its paired SEAC-minus-ablation difference was +0.1084 (95% CI [0.0351, 0.2045], secondary-family Holm-adjusted p=1.0000). The complete table is reported because a component is not treated as causally necessary solely from the largest observed drop.

### 6.4 RQ5: Efficiency

Table 5 compares the observed locked-test resource costs.

**Table 5. Aggregate locked-test efficiency of SEAC and the strongest retrieval baseline.**

| Method | Parents | Retrieval vectors | Chunking time | Index time | Query latency mean / p50 / p95 |
|---|---:|---:|---:|---:|---:|
| Best non-SEAC baseline | 8,891 | 8,891 | 0.25 s | 139.46 s | 15.18 / 16.87 / 18.40 ms |
| SEAC | 8,746 | 48,912 | 112.11 s | 351.12 s | 20.93 / 20.88 / 27.28 ms |

Relative to the strongest non-SEAC retrieval baseline (Structure-only), SEAC used 5.50× as many retrieval vectors. Its pooled mean query latency differed by +5.7410 ms, while total locked-set chunking and indexing times were 112.11 s and 351.12 s, respectively.

## 7. Discussion

### 7.1 Why structure and hierarchy may help

SEAC separates two decisions that generic splitters conflate: where coherent
context should begin and end, and which fine-grained representation should
activate that context during retrieval. Protected table parents prevent
arbitrary row separation at the semantic-boundary stage, while row children
preserve localization. Heading paths restore the module or interface context
that short children cannot express alone. The formal ablation results determine
whether these mechanisms improve retrieval in practice rather than merely
appearing plausible.

### 7.2 Interpreting unit and span metrics

Provenance and textual support answer different questions. A low unit rank with
a higher span rank means the retrieved text supports the answer but comes from a
different annotated source location, often because templates repeat across
documents. The reverse suggests the correct parent contains the gold unit but
the minimal supporting span is diluted or altered. Reporting only block hits
would hide both cases. For enterprise use, provenance-aware evaluation is
important because audit and change-impact workflows often require the original
artifact location, not only a semantically equivalent statement.

### 7.3 Practical deployment

SEAC is most appropriate when the corpus contains reliable heading or table
structure and when evidence provenance matters. The method increases the number
of indexed vectors because parents and children coexist. Teams with tight index
budgets can disable non-table children, reduce child generation, or use the
non-hierarchical parent variant, but should validate the trade-off on their own
queries. The deterministic boundary pass and explicit lexicon also make SEAC
easier to audit than LLM-generated propositions, at the cost of less flexible
semantic normalization.

## 8. Threats to Validity

**Construct validity.** Boundary F1 measures agreement with annotated parent
cuts, not end-to-end generation quality. Retrieval metrics isolate chunking and
avoid generator variability, but they do not establish answer faithfulness for
a particular LLM. Rule-generated QA drafts may overrepresent local factual
questions even after human review. We mitigate these risks by reporting
question-type results, provenance and span metrics, and explicit exclusions.

The P02–P10 boundary reference is a mixed standard: a verified human sample plus
model pre-labels for the remaining candidates, as Section 5.3 states. Two
consequences follow. First, the reported boundary agreement for those projects is
agreement with that mixed reference, and its trustworthiness is bounded by the
measured human-model kappa rather than assumed. Second, a systematic labelling
bias shared by the model and by SEAC's own structural signals would inflate
boundary scores even though SEAC invokes no language model when chunking. We
therefore treat P01, whose 4,783 boundaries are entirely human double-annotated
and adjudicated, as the reference point for boundary claims, report the
human-verified and model-labelled shares for every other project, and publish
the applied label-rule record together with a mechanical rule-conformance audit
so that the pre-labels can be re-examined or replaced.

**Internal validity.** Parsing errors can change unit boundaries before SEAC is
applied. Legacy conversions are hash-tracked and visually sampled, but not every
page receives cell-by-cell verification. All methods share extracted units and
retrieval context, while official framework implementations are version pinned.
The test split remains inaccessible until annotation and configuration gates
pass. Code and input hashes are stored for every formal run.

**External validity.** The corpus contains ten Chinese enterprise projects,
primarily industrial management systems. Results may not generalize to English
documentation, source code, issue trackers, safety-certified requirements, or
image-heavy manuals. Six unsupported PDF/PPTX sources are excluded and reported.
Replication on public multilingual technical-document datasets is future work.

**Conclusion validity.** Project sizes and QA counts are unequal, so we report
both micro and project-macro results. Hierarchical bootstrap intervals account
for project clustering. Paired tests exchange all matched observations within a
project together, enumerate every assignment for the six projects eligible on
each evaluation axis, and apply Holm correction to the prespecified families.
Six independent projects support each primary locked-test inference (ten
projects are present in the full corpus), which limits precision and makes small
p-values intrinsically coarse. Effect sizes, intervals, and project-level consistency therefore carry
more weight than thresholded significance.

## 9. Ethics, Privacy, and Data Availability

The source documents are enterprise artifacts and may contain personal,
security, customer, or proprietary information. The study protocol requires a
separate privacy decision for every retained QA record. No credentials are
eligible for release. Project names are replaced with IDs in analysis outputs,
and the paper reports aggregate corpus statistics by default.

Code, configuration schemas, annotation instructions, conversion manifests,
and experiment scripts can be released independently of raw documents. Release
of anonymized QA or document-derived examples requires written authorization
from the data owner and compliance with institutional policy. If authorization
is unavailable, the reproducibility package will include synthetic schema-valid
examples and all non-data-dependent code, while raw corpora and gold labels
remain access-controlled.

### 9.1 Data availability statement

The raw enterprise documents and document-derived QA records cannot be made
public because they contain confidential and potentially identifying business
information. The authors will provide an anonymous review package containing
the implementation, frozen configurations, annotation schemas, synthetic
schema-valid examples, test suite, run manifests, and aggregate analysis code.
Subject to repository and data-owner approval, the archival package and its
persistent identifier will be added to the accepted manuscript. Qualified
researchers may request controlled access to additional artifacts, but access
is not guaranteed and requires authorization from the data owner.

## 10. Conclusion

This paper presented SEAC, a deterministic structure- and
key-information-aware adaptive semantic chunker for retrieval over heterogeneous
software-engineering documents. SEAC combines adjacent-unit semantic distance,
document structure, key-information-density change, protected groups, adaptive
thresholds, contextual metadata, and parent–child fusion. A frozen ten-project
industrial protocol evaluates both human boundary agreement and
provenance-aware evidence retrieval while preventing test leakage and
unreviewed data from entering formal results.

The locked evaluation shows an observed confirmatory unit-MRR difference of 0.1003 for SEAC, with exact two-sided p=0.0625. These results support
further evaluation of structure-aware hierarchy on additional independent projects before claiming a general retrieval advantage. Future work should test multilingual and
public technical corpora, extend parsing to image-heavy PDFs and presentations,
and evaluate how chunk quality propagates to generated answers and
artifact-to-artifact traceability.

## Declaration of generative AI and AI-assisted technologies in the manuscript preparation process

During the preparation of this work, the authors used OpenAI Codex to assist
with code drafting, test generation, literature organization, manuscript
structuring, and language editing. Beyond manuscript preparation, a large
language model was also used as a labelling instrument inside the benchmark
construction pipeline, as described in Sections 5.3 and 5.4: it produced the
boundary pre-labels for P02–P10 and an advisory content pre-review of the
question drafts. Those outputs are recorded with per-row provenance, are
subject to the human verification and promotion gates described there, and are
overridden by human labels wherever the two disagree. No generative AI system
issued a privacy or data-release decision; every privacy status was set by an
authorized human reviewer. No AI-generated image is included in the manuscript.
The authors reviewed and verified the code, sources, analyses, and text;
retained responsibility for all research and publication decisions; and take
full responsibility for the content of the article.

## References

[1] P. Lewis, E. Perez, A. Piktus, F. Petroni, V. Karpukhin, N. Goyal,
H. Küttler, M. Lewis, W.-t. Yih, T. Rocktäschel, S. Riedel, and D. Kiela,
“Retrieval-Augmented Generation for Knowledge-Intensive NLP Tasks,” *Advances
in Neural Information Processing Systems*, vol. 33, 2020.
https://proceedings.nips.cc/paper/2020/hash/6b493230205f780e1bc26945df7481e5-Abstract.html

[2] V. Karpukhin, B. Oğuz, S. Min, P. Lewis, L. Wu, S. Edunov, D. Chen, and
W.-t. Yih, “Dense Passage Retrieval for Open-Domain Question Answering,” in
*Proceedings of EMNLP*, 2020, pp. 6769–6781. doi:10.18653/v1/2020.emnlp-main.550.

[3] N. Reimers and I. Gurevych, “Sentence-BERT: Sentence Embeddings using
Siamese BERT-Networks,” in *Proceedings of EMNLP-IJCNLP*, 2019, pp. 3982–3992.
doi:10.18653/v1/D19-1410.

[4] J. Chen, S. Xiao, P. Zhang, K. Luo, D. Lian, and Z. Liu, “M3-Embedding:
Multi-Linguality, Multi-Functionality, Multi-Granularity Text Embeddings Through
Self-Knowledge Distillation,” in *Findings of ACL*, 2024, pp. 2318–2335.
doi:10.18653/v1/2024.findings-acl.137.

[5] T. Chen, H. Wang, S. Chen, W. Yu, K. Ma, X. Zhao, H. Zhang, and D. Yu,
“Dense X Retrieval: What Retrieval Granularity Should We Use?” in *Proceedings
of EMNLP*, 2024, pp. 15159–15177. doi:10.18653/v1/2024.emnlp-main.845.

[6] M. Günther, I. Mohr, D. J. Williams, B. Wang, and H. Xiao, “Late Chunking:
Contextual Chunk Embeddings Using Long-Context Embedding Models,”
arXiv:2409.04701, 2024.

[7] P. Sarthi, S. Abdullah, A. Tuli, S. Khanna, A. Goldie, and C. D. Manning,
“RAPTOR: Recursive Abstractive Processing for Tree-Organized Retrieval,” in
*International Conference on Learning Representations*, 2024.

[8] C. Auer, M. Lysak, A. Nassar, M. Dolfi, N. Livathinos, P. Vagenas,
C. Berrospi Ramis, M. Omenetti, F. Lindlbauer, K. Dinkla, L. Mishra, Y. Kim,
S. Gupta, R. Teixeira de Lima, V. Weber, L. Morin, I. Meijer, V. Kuropiatnyk,
and P. W. J. Staar, “Docling Technical Report,” arXiv:2408.09869v5, 2024.

[9] J. L. C. Guo, J.-P. Steghöfer, A. Vogelsang, and J. Cleland-Huang,
“Natural Language Processing for Requirements Traceability,” arXiv:2405.10845,
2024.

[10] S. J. Ali, V. Naganathan, and D. Bork, “Establishing Traceability Between
Natural Language Requirements and Software Artifacts by Combining RAG and
LLMs,” in *Conceptual Modeling*, 2024, pp. 295–314.
doi:10.1007/978-3-031-75872-0_16.

[11] LangChain, “langchain-text-splitters 1.1.2,” Python Package Index, 2026.
https://pypi.org/project/langchain-text-splitters/1.1.2/

[12] LlamaIndex, “SentenceSplitter, llama-index-core 0.14.23,” 2026.
https://pypi.org/project/llama-index-core/0.14.23/

[13] J. Cohen, “A Coefficient of Agreement for Nominal Scales,” *Educational
and Psychological Measurement*, vol. 20, no. 1, pp. 37–46, 1960.
doi:10.1177/001316446002000104.

[14] B. Efron, “Bootstrap Methods: Another Look at the Jackknife,” *The Annals
of Statistics*, vol. 7, no. 1, pp. 1–26, 1979.
doi:10.1214/aos/1176344552.

[15] S. Holm, “A Simple Sequentially Rejective Multiple Test Procedure,”
*Scandinavian Journal of Statistics*, vol. 6, no. 2, pp. 65–70, 1979.
doi:10.2307/4615733.

[16] X. Wang, Z. Wang, X. Gao, F. Zhang, Y. Wu, Z. Xu, T. Shi, Z. Wang,
S. Li, Q. Qian, R. Yin, C. Lv, X. Zheng, and X. Huang, “Searching for Best
Practices in Retrieval-Augmented Generation,” in *Proceedings of EMNLP*, 2024,
pp. 17716–17736. doi:10.18653/v1/2024.emnlp-main.981.

[17] A. V. Duarte, J. D. S. Marques, M. Graça, M. Freire, L. Li, and
A. L. Oliveira, “LumberChunker: Long-Form Narrative Document Segmentation,” in
*Findings of EMNLP*, 2024, pp. 6473–6486.
doi:10.18653/v1/2024.findings-emnlp.377.

[18] Z. Wang, C. Gao, C. Xiao, Y. Huang, S. Si, K. Luo, Y. Bai, W. Li,
T. Duan, C. Lv, G. Lu, G. Chen, F. Qi, and M. Sun, “Document Segmentation
Matters for Retrieval-Augmented Generation,” in *Findings of ACL*, 2025,
pp. 8063–8075. doi:10.18653/v1/2025.findings-acl.422.

[19] J. Zhao, Z. Ji, Z. Fan, H. Wang, S. Niu, B. Tang, F. Xiong, and Z. Li,
“MoC: Mixtures of Text Chunking Learners for Retrieval-Augmented Generation
System,” in *Proceedings of ACL*, 2025, pp. 5172–5189.
doi:10.18653/v1/2025.acl-long.258.

[20] D. Rau, H. Déjean, N. Chirkova, T. Formal, S. Wang, S. Clinchant, and
V. Nikoulina, “BERGEN: A Benchmarking Library for Retrieval-Augmented
Generation,” in *Findings of EMNLP*, 2024, pp. 7640–7663.
doi:10.18653/v1/2024.findings-emnlp.449.

[21] N. F. Liu, K. Lin, J. Hewitt, A. Paranjape, M. Bevilacqua, F. Petroni,
and P. Liang, “Lost in the Middle: How Language Models Use Long Contexts,”
*Transactions of the Association for Computational Linguistics*, vol. 12,
pp. 157–173, 2024. doi:10.1162/tacl_a_00638.

[22] J. Mucha, A. Kaufmann, and D. Riehle, “A Systematic Literature Review of
Pre-Requirements Specification Traceability,” *Requirements Engineering*,
vol. 29, pp. 119–141, 2024. doi:10.1007/s00766-023-00412-z.

[23] B. Wang, Z. Zou, H. Wan, Y. Li, Y. Deng, and X. Li, “An Empirical Study
on the State-of-the-Art Methods for Requirement-to-Code Traceability Link
Recovery,” *Journal of King Saud University—Computer and Information Sciences*,
vol. 36, art. 102118, 2024. doi:10.1016/j.jksuci.2024.102118.
