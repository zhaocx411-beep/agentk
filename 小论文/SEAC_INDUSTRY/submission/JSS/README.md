# JSS submission package

Status: **prepared structure; blocked by human annotations, formal results, and
author-supplied metadata**.

Primary target: Journal of Systems and Software. The final manuscript must be a
single-column editable DOCX with an abstract of at most 250 words, 1–7 keywords,
author–year references, editable equations, and no confidential enterprise
identifiers.

Files in this directory:

- `title_page_template.md`: author and affiliation fields that must be supplied
  by the authors;
- `cover_letter_template.md`: submission letter requiring corresponding-author
  details and final evidence statements;
- `declarations_template.md`: funding, CRediT, competing interests,
  acknowledgements, AI disclosure, and data availability;
- `submission_checklist.md`: evidence-based release gate.

After formal analysis, `scripts/fill_manuscript_results.py` creates the complete
results manuscript, result audit, and JSS highlights. The JSS preparation and
DOCX scripts then convert citations to author–year form, format the editable
single-column manuscript, and refuse any remaining placeholder or sensitive
identifier.
