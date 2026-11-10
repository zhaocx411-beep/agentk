# IEEE BigComp Word-template execution contract

## Reference

- Original official baseline: `/Users/zhaochenxu06/Desktop/bupt/小论文/SEAC_INDUSTRY/submission/BigComp2027/ieee_word_template_2026_baseline.doc`
- Original SHA-256: `e3d9531fdd19eb8a5048521e5f6c4eb39a9ab4b87e8c30d74e73f46990fd992a`
- Retained DOCX conversion used for authoring: `/Users/zhaochenxu06/Desktop/bupt/小论文/SEAC_INDUSTRY/submission/BigComp2027/ieee_word_template_2026_baseline.docx`
- Converted SHA-256: `20f236dc616066833056d52ada821f0b081717ba6bf3ff290a1f48cb84105bcc`
- Conversion: official `.doc` opened by bundled LibreOffice 26.8 and saved as Office Open XML; the original remains unchanged.
- Render evidence: `/private/tmp/seac_bigcomp_docx/template_render/`
- Style evidence: `/private/tmp/seac_bigcomp_docx/template_style_evidence.json`
- Rendered pages: 4. Page 1 contains the title/author block and two-column body; pages 2--3 continue two columns; page 4 is a conversion-generated trailing page holding only the final sample reference.
- Converted sections: 7. The extra continuous sections implement the six-author grid and sample components; they are not semantic paper sections.

## Page system

- US Letter, portrait, 8.5 x 11 in.
- Margins: left/right 0.62 in (one converted section reports 0.63), top 0.75 in, bottom 1.00 in.
- First-page title and author region spans the page; the main paper is two equal columns.
- Main-body column spacing: 360 DXA (0.25 in).
- The original template uses continuous section breaks to move between title, author grids, and the two-column body.
- The first-page footer contains the funding/copyright placeholder. It must be absent from the anonymous review draft; camera-ready copyright text is deferred to the 2027 instructions.
- Intentional simplification: the anonymous review draft uses one single-column title/anonymous-author section followed by one continuous two-column body section. Removing the six-author grid and the trailing converted section is necessary for double-blind review and eliminates the blank fourth template page.

## Typography

- Base `Normal`: Times New Roman, 10 pt.
- `paper title`: Times New Roman, 24 pt, centered, 0 pt before, 6 pt after.
- `Author`: Times New Roman, 11 pt, centered, 18 pt before, 2 pt after. In the review draft it contains only `Author information omitted for double-blind review.`
- `Abstract`: Times New Roman, 9 pt, justified, bold at style level, 10 pt after. Use local runs so only the `Abstract—` label is bold/italic and the abstract text is regular.
- `Keywords`: inherits 9 pt from `Abstract`, italic style, 6 pt after. Use a bold/italic `Keywords—` label followed by regular 9 pt terms.
- `Body Text`: inherits Times New Roman 10 pt, justified, 0 pt before, 6 pt after, 0.95 line spacing.
- `Heading 1`: inherits Times New Roman 10 pt, centered small-caps/uppercase treatment from the template, 8 pt before, 4 pt after.
- `Heading 2`: inherits Times New Roman 10 pt, italic, start aligned, 6 pt before, 3 pt after.
- `Heading 3`: inherits Times New Roman 10 pt, italic, justified, compact line spacing.
- `table head`: Times New Roman 8 pt, centered, 12 pt before, 6 pt after, 0.9 line spacing.
- `figure caption`: Times New Roman 8 pt, 4 pt before, 10 pt after.
- `references`: Times New Roman 8 pt, justified, 0 pt before, 2.5 pt after.

## Lists, equations, and tables

- Contribution lists use the template's real numbered-list behavior; no Unicode or manually prefixed bullet paragraphs.
- Equations are editable Word equations or centered mathematical text with a right-aligned number. They must not be raster images.
- Table captions appear above tables using `table head`.
- Table text is 8 pt Times New Roman. Numeric columns are centered; method/label columns are left aligned.
- Tables use explicit fixed DXA widths equal to one column unless intentionally spanning both columns. Header rows repeat if a table crosses a page.
- The working paper has three genuinely tabular components: corpus split, locked method results, and ablations.

## Content flow and slot map

All textual content in the official file is instructional placeholder content and is editable/removable. The retained styles, theme, numbering, relationships, settings, font table, and page geometry are preserve-only authorities.

1. `word/document.xml`, first `paper title` paragraph: replace with the provisional `[Industrial paper]` title.
2. The author-grid paragraphs and continuous author-grid sections: remove; replace with one anonymous `Author` paragraph.
3. First body section: add Abstract and Keywords.
4. Insert one continuous two-column section after Keywords.
5. Replace all sample body paragraphs with the ordered paper sections in `paper_working_draft.md`.
6. Replace the sample table with three SEAC tables using the source table/caption visual pattern and explicit geometry.
7. Remove the sample figure/text box, sample acknowledgment, funding placeholder, sample footer copyright line, and all template instructions.
8. Add 17 numbered references using the `references` style.

## Package preservation

Preserve from the converted reference unless a documented content edit requires change:

- `word/styles.xml`, `word/theme/*`, `word/fontTable.xml`, and numbering definitions;
- document relationships and content types not tied only to removed sample objects;
- language and compatibility settings;
- built-in IEEE paragraph styles used by the new content.

Editable/rebuildable:

- `word/document.xml` body content and section properties;
- headers/footers containing only template placeholder furniture;
- sample table, sample text box, drawing relationships, and sample equation;
- core/app metadata, which must be scrubbed for anonymous review.

## Fidelity gates

- Original `.doc` and retained converted `.docx` hashes remain unchanged.
- Final document is US Letter with the recorded margins, a full-width title block, and a two-column main body.
- The review draft contains no template instructions, funding/copyright placeholders, author identity, affiliations, acknowledgments, organization names, or identifying URLs.
- Title, Abstract, Keywords, body, headings, captions, tables, and references visibly follow the retained IEEE styles.
- No text/table clips or overlaps; all equations and tables remain legible at 100% render.
- Final page count is no more than 8 under the 2026 baseline. The page limit must be rechecked against the official 2027 call.
- Every page PNG is inspected before delivery, and unexplained blank trailing pages fail the gate.
