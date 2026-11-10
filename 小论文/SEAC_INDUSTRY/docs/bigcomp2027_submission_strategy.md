# IEEE BigComp 2027 submission strategy for SEAC

Checked on 2026-08-24.

## Status of the official call

The official BigComp site still presents BigComp 2026. A BigComp 2027 call,
submission deadline, EasyChair link, and final template package were not yet
visible on the official site when this strategy was prepared. The reported
January 25--28, 2027 dates and Fukuoka International Congress Center venue are
therefore treated as target information supplied by the author, not yet as a
verified submission rule.

Until the 2027 call appears, this package uses the current official BigComp 2026
rules as a planning baseline:

- original, unpublished work not simultaneously under review;
- PDF in the IEEE conference two-column format;
- double-blind review with desk rejection for missing anonymization;
- regular papers limited to 8 pages and short papers to 4 pages;
- research and industrial tracks, with industrial submissions prefixed
  `[Industrial paper]` in the title;
- submission through the year-specific EasyChair site.

Official baseline sources:

- https://www.bigcomputing.org/
- https://www.bigcomputing.org/call_for_papers
- https://www.bigcomputing.org/paper_submission

All year-specific fields must be rechecked against the 2027 call before upload.

## Recommended track and positioning

Use an **8-page regular paper in the Industrial track** unless the 2027 call
changes the track structure. This is the best fit because SEAC is evaluated on
ten real enterprise software projects, emphasizes a deployable retrieval
pipeline, and reports system cost and privacy constraints. It also aligns with
the listed BigComp topics `Search/retrieval and generation of big data`, `Tools
and systems for big data`, and `Big data in the era of LLMs`.

The conference paper should not be framed as a general software-engineering
journal article. The central BigComp message is:

> Structure-preserving, provenance-aware chunk construction improves observed
> retrieval quality for heterogeneous enterprise technical documents, with
> table-row children carrying most of the measured benefit and with an explicit
> indexing-cost trade-off.

The recommended working title is:

> [Industrial paper] SEAC: Provenance-Aware Structure-Preserving Chunking for
> Enterprise Software-Engineering Retrieval

## Evidence that can be claimed

The frozen GPU study is complete. On the locked retrieval set, full SEAC reached
unit MRR 0.7937 and Hit@5 0.9520; the strongest non-SEAC baseline reached MRR
0.7290. Against the prespecified LlamaIndex comparator, the observed paired MRR
difference was +0.1003, with 95% CI [0.0042, 0.2008] and exact two-sided
`p=0.0625` over six eligible projects. This is an observed gain, not a
statistically significant confirmatory advantage at alpha 0.05.

Boundary F1 was 0.5673 versus 0.5032 for the strongest non-SEAC boundary
baseline; the paired difference did not survive Holm correction (`p=0.4375`).
The table-row-child ablation reduced MRR to 0.6853, while removing parent fusion
or heading context increased MRR on this sample. The paper must report all three
facts. It must not claim that every SEAC component is beneficial.

## Required compression from the long paper

The result-complete long paper is about 8,000 words, contains five tables and
nine displayed equations, and was written in a journal style. It cannot be
submitted unchanged under an 8-page IEEE limit. The BigComp version should aim
for roughly 4,000--4,500 words including compact tables, with references inside
the page limit unless the 2027 call explicitly states otherwise.

Target allocation:

1. Abstract and introduction: 0.8 page.
2. Related work: 0.5 page.
3. SEAC method: 1.5 pages, retaining only the combined score, threshold, and
   parent/child construction essentials.
4. Industrial study: 1.3 pages, condensing annotation provenance and the locked
   protocol.
5. Results: 1.5 pages with at most three tables.
6. Discussion, threats, ethics, and conclusion: 0.9 page.
7. References: approximately 1.5 pages.

Details on post-hoc annotation rule amendments, full per-project kappa values,
all secondary tests, privacy-review mechanics, and complete efficiency results
should move to an anonymous review supplement or the retained long-form paper.
The main paper must still disclose mixed boundary provenance, P08/P10
axis-specific withholding, confidentiality, and the non-significant
confirmatory result.

## Main review risks and mitigations

- **Only six eligible projects per inference axis.** Report exact project-level
  tests, intervals, and effect sizes; avoid threshold-based superiority claims.
- **Negative ablations.** Reframe SEAC's supported contribution around protected
  structure and table-row children. Present fusion and heading context as
  unsupported components rather than hiding them.
- **Private corpus.** Provide an anonymous package containing code, frozen
  configuration, schemas, synthetic examples, aggregate results, and integrity
  hashes. Do not promise raw data access without written owner approval.
- **Index cost.** State the 5.50x vector increase and latency difference; position
  the method for settings where provenance and table-row precision justify the
  cost.
- **Industrial-track identity leakage.** Remove author names, affiliations,
  acknowledgements, repository owner names, organization names, customer names,
  and self-identifying links from the review PDF and supplement.

## Items that remain human-supplied

- final author order, affiliations, email addresses, ORCIDs, and corresponding
  author;
- funding and acknowledgements;
- conflicts of interest;
- data-owner decision on release scope;
- final choice between Research and Industrial track if the 2027 call changes
  the current structure;
- author registration and in-person presentation commitment after acceptance.
