# ICAISG 2026 template execution contract

## Reference

- Retained source: `/Users/zhaochenxu06/Desktop/bupt/小论文/SEAC_INDUSTRY/submission/ICAISG2026/template/ICAISG2026_official_template.docx`
- SHA-256: `6bebf58e54ada3509266a3f3037621d04bd5db474499aa79167a1eeffcb76306`
- Source render: `submission/ICAISG2026/qa/template_render/`
- Source page count: 4; source section count: 7.
- The source uses ISO/IEC 29500 Strict namespaces. It renders correctly in LibreOffice but must be converted to Transitional OOXML in a task-local working copy before `python-docx` can edit it. The retained source must remain unchanged.

## Page system

- US Letter portrait, 612 x 792 pt.
- Main paper margins: top 54 pt (0.75 in), bottom 72 pt (1.0 in), left/right approximately 45 pt (0.625 in).
- Header/footer distances: 36 pt.
- Title block: one column.
- Main paper body: two equal columns separated by 18 pt (0.25 in), created by a continuous section break.
- No visible page numbers. No running header.
- The template contains additional 4-column author sections and a final one-column author-background form. These are removable slots; they are not part of the anonymous review manuscript.

## Typography and paragraph roles

- Typeface: Times New Roman throughout unless the template requires Symbol for mathematical glyphs.
- Paper title: 24 pt, centered, regular, no subtitle.
- Author/affiliation styles: centered, 11/10 pt. These slots are removed for the double-blind initial submission.
- Body: 10 pt, justified, approximately 12 pt line pitch, first-line indent approximately 14.4 pt.
- Abstract and keywords: 9 pt, justified; labels are bold italic.
- Heading 1: 10 pt, centered, small caps, uppercase Roman numbering, 8 pt before and 4 pt after.
- Heading 2: 10 pt italic, left aligned, alphabetic numbering, 6 pt before and 3 pt after.
- Heading 5/component heading: 10 pt, centered, small caps, unnumbered.
- Figure/table labels and captions: 8 pt. Table captions appear above; figure captions below.
- References: 8 pt, hanging indent, bracketed numeric order of appearance.

## Lists and tables

- Avoid lists in the final compressed paper unless essential.
- Tables use explicit fixed widths equal to one column or the full two-column width; no autofit.
- Table headings are 8 pt bold and centered; body cells are 8 pt with compact but non-clipping padding.
- Figures/tables are placed after first mention and should remain at column top/bottom where feasible.

## Components and content flow

1. One-column title only for anonymous review; no author, affiliation, funding, acknowledgment, or author-background form.
2. Continuous transition to two columns.
3. Abstract and keywords.
4. Numbered sections: Introduction; Related Work; Method; Industrial Evaluation; Results; Discussion and Limitations; Conclusion.
5. Unnumbered References.

## Slot map

- Entire title text is rewritten.
- Author, affiliation, funding, acknowledgment, and author-background slots are removed for double-blind review.
- All IEEE instructional body text, example tables/figures/equations, and example references are removed.
- The final footer and package-level styles/theme/numbering are preserved where compatible with the required two-section anonymous layout.
- New paper text, tables, one architecture figure, and references are inserted using the template's visual roles.

## Package preservation

- Preserve the retained template byte-for-byte.
- The working copy may be converted from Strict to Transitional OOXML because this is required for deterministic editing.
- Preserve theme, font table, core style definitions, and footer relationship in the working copy unless a generated component requires an explicit addition.
- Remove obsolete custom XML and source author metadata from the final submission artifact.

## Fidelity gates

- Final page size, margins, two-column body, Times New Roman hierarchy, heading pattern, caption placement, and reference treatment must visually match the source.
- Final initial-review manuscript must be 5 or 6 pages, contain no template guidance text, and contain no author-identifying content or document metadata.
- Render every final page to PNG and inspect at 100% zoom.
- Verify the retained source SHA-256 remains unchanged.
