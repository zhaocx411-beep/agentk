# SEAC submission strategy

Checked on 2026-08-04.

## Primary target: Journal of Systems and Software

The Journal of Systems and Software (JSS) is the primary target because its
scope explicitly includes AI and data analytics applied in software
engineering, methods and tools for requirements/design/maintenance, empirical
software-engineering methods, and industrial evidence. SEAC is positioned as a
software-engineering document retrieval method evaluated on ten industrial
projects, rather than as a general-purpose NLP or finance system.

Official sources:

- Aims and scope: https://www.sciencedirect.com/journal/journal-of-systems-and-software
- Guide for authors: https://www.sciencedirect.com/journal/journal-of-systems-and-software/publish/guide-for-authors
- Elsevier AI-writing policy: https://www.elsevier.com/en-gb/about/policies-and-standards/the-use-of-generative-ai-and-ai-assisted-technologies-in-writing-for-elsevier

The current JSS requirements that affect this repository are:

- editable Word submissions use a single-column layout;
- the abstract must not exceed 250 words (the working SEAC abstract is 221
  whitespace-delimited words before result replacement);
- 1–7 English keywords are required;
- 3–5 highlights are required, each no more than 85 characters including spaces;
- equations must remain editable rather than being supplied as images;
- author–year citations and an alphabetized reference list are required;
- full papers below 36 single-column pages are encouraged;
- a data-availability statement, funding statement, competing-interest
  declaration, CRediT contributions, author details, and short biographies are
  required at the appropriate submission stage;
- generative-AI use during manuscript preparation must be disclosed before the
  references; AI-generated submission artwork is not permitted.

## Backup target: Information and Software Technology

Information and Software Technology (IST) is the backup because it publishes
research and experience intended to improve software-development practices and
explicitly covers requirements, architecture, design, and empirical software
engineering. Its research-paper limit is 15,000 words, but it requires a
five-part structured abstract, so switching to IST requires a targeted abstract
rewrite rather than only a style change.

- Aims and scope: https://www.sciencedirect.com/journal/information-and-software-technology
- Guide for authors: https://www.sciencedirect.com/journal/information-and-software-technology/publish/guide-for-authors

## Positioning and non-negotiable evidence

The submission should claim only what the locked P04–P10 analysis supports.
The title, abstract, and conclusions may claim a confirmatory retrieval
advantage only if the SEAC-minus-LlamaIndex unit-MRR effect is positive and its
prespecified exact project-cluster test is significant. Comparisons with the
data-selected strongest baseline are descriptive; the remaining 55 retrieval
tests use the frozen secondary Holm family, and the 14 boundary comparisons use
a separate Holm family. Boundary agreement, QA/privacy review, all ten formal run records,
and the result-injection audit must exist before a submission DOCX is produced.

Raw enterprise documents, raw review workbooks, document-derived QA, and
customer identities are not submission supplements. The public artifact should
contain only non-data-dependent code, frozen configuration, schemas, synthetic
examples, aggregate results cleared for release, and test/reproduction
instructions. Any broader release requires written data-owner authorization.

## Submission package

The prepared JSS package lives in `submission/JSS/`. It intentionally keeps
author identity, funding, CRediT roles, conflicts, acknowledgements, repository
DOI, and biographies as explicit human-supplied fields. These cannot be inferred
or fabricated by the pipeline.
