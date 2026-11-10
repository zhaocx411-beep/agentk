# P01 BGE-M3 validation-only run

This run validates the strict unit/span evaluation path and estimates CPU cost.
It is not reportable as a formal paper result because the 27 P01 QA records had
not received human privacy clearance when the run started. It also predates the
per-boundary detail and implementation-hash additions to the runner.

Observed validation metrics:

- boundary F1: 0.8722 on 4,458 evaluated human labels (325 `NOISE` excluded);
- unit-level MRR: 0.7352; Hit@5: 0.9259;
- exact-span MRR: 0.8062; exact-span Hit@5: 0.9259;
- chunking: 400.7 s; indexing: 1,259.6 s on CPU;
- parents: 2,064; retrieval children: 9,677.

The formal matrix must be rerun from `configs/formal_run_manifest.json` after
`scripts/audit_formal_readiness.py` reports all required projects ready.
