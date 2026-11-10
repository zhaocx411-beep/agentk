# SEAC anonymous-review artifact scaffold

This package contains release-safe code, frozen configurations, annotation
schemas, tests, and invented smoke-test data for the SEAC paper. It deliberately
excludes raw enterprise documents, document-derived annotation workbooks, real
questions and gold evidence, project source names, and experiment outputs.

This build is a scaffold, not the final submission artifact: human boundary,
QA, and privacy review is still incomplete, and no formal locked-test result is
included. `BUILD_STATUS.json` records those gates explicitly.

Run the unit tests:

```bash
python3 -m unittest discover -s tests
```

Run the synthetic CPU smoke test using the command in
`examples/synthetic/README.md`. Formal reproduction requires controlled-access
inputs whose hashes must match the run manifest; those inputs are not present.
