# SEAC anonymous review artifact for ICAISG 2026

This review-only package accompanies the anonymous paper **“Provenance-Preserving Structure-Aware Chunking for Auditable Enterprise Retrieval-Augmented Generation.”** It contains the SEAC implementation, frozen configurations, formal analysis code, schemas, selected tests, invented smoke-test data, and privacy-safe aggregate locked-test results.

The confidential enterprise corpus, document-derived annotations, real questions, evidence text, source names, credentials, and machine paths are intentionally excluded. Formal end-to-end reproduction therefore requires controlled-access inputs whose hashes match the frozen manifest. The included synthetic data exercises the public pipeline without revealing enterprise content.

## Package contents

- `seac_chunking/`: chunking, provenance, retrieval, and evaluation implementation.
- `scripts/`: experiment, formal-suite, audit, and analysis utilities.
- `configs/`: frozen formal and synthetic configurations.
- `schemas/`: public corpus, boundary, and QA schemas.
- `examples/synthetic/`: invented smoke-test corpus and labels.
- `results/aggregate_locked_results.json`: privacy-safe aggregate results reported in the paper.
- `tests/`: selected implementation, gate, and inference tests.

## Verification

Use Python 3.12 and install `requirements-paper.txt`. From this directory, run:

```bash
python3 -m unittest discover -s tests -p 'test_*.py'
```

The deterministic CPU smoke-test command is in `examples/synthetic/README.md`. Its output is exploratory and must not be substituted for the locked results.

## Interpretation limits

The aggregate file reports observed retrieval, boundary, and cost results. The prespecified SEAC-versus-LlamaIndex comparison has exact two-sided `p = 0.0625`; it is not a statistically significant confirmatory result at 0.05. The artifact does not establish end-to-end answer faithfulness, prompt-injection resistance, access control, or privacy protection.

## Release status

This package is for confidential scholarly review. It grants no public data or software license. Public archival release, a license, and a persistent identifier require author and data-owner approval.
