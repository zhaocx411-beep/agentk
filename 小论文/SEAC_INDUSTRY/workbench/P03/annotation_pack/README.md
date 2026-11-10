# P03 SEAC blind annotation pack

This pack is a paper benchmark input, not gold data yet.

Read `docs/annotation_and_privacy_protocol.md` before editing this pack. It is
the normative decision, adjudication, QA, and privacy-review guide.

## Boundary labels

- `BREAK`: the right unit should start a new semantically self-contained chunk.
- `NO_BREAK`: the two units should remain together when length constraints permit.
- `NOISE`: at least one side is navigation, a footer, an empty template, or otherwise not meaningful content.

Annotator A and B must work independently. Do not consult method outputs, model
predictions, or the other annotator. Fill `human_label`, `review_status`, and
`review_notes`; use `NEEDS_DISCUSSION` only when the evidence is genuinely
ambiguous. The 300 boundaries were selected deterministically with
document- and structure-stratified sampling.

## QA review

The 36 QA records are rule-generated drafts. A reviewer must verify that
the question is natural, the answer is unique, the evidence span is sufficient,
and confidential details are removed. Table questions require a precise
`gold_evidence_unit_id`. Only records later marked `APPROVED` or `KEEP` may enter
formal retrieval evaluation. Set `privacy_review_status` to `NO_RISK`,
`ANONYMIZED`, or `CLEARED` for approved records; otherwise reject the record.
