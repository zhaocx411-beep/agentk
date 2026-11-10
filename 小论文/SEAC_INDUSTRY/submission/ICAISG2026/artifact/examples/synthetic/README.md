# Synthetic SEAC smoke-test data

These files describe an invented inventory service. They contain no enterprise
source text, people, organizations, network locations, credentials, or formal
paper results. Their only purpose is to exercise the public corpus, boundary,
QA, privacy-gate, and retrieval schemas.

Run the deterministic CPU smoke test from the repository root:

```bash
python3 scripts/run_chunking_experiment.py \
  --corpus examples/synthetic/corpus.json \
  --qa examples/synthetic/qa.json \
  --breakpoints examples/synthetic/breakpoints.json \
  --methods fixed structure proposed_v6 \
  --encoder tfidf \
  --evidence-mode unit \
  --max-tokens 64 \
  --overlap 8 \
  --proposed-soft-target 48 \
  --proposed-dense-target 32 \
  --proposed-min-chunk 4 \
  --v6-child-target 16 \
  --v6-child-max 24 \
  --output-dir /tmp/seac-synthetic-smoke
```

The output is exploratory and must never be inserted into the manuscript.
