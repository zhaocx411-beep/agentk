# SEAC formal statistical analysis

- Primary method: `proposed_v6`
- Analysis scope: `locked_test`
- Projects: P04, P05, P06, P07, P08, P09, P10
- Hierarchical bootstrap samples: 1
- Paired randomization unit: `project_id`
- Confirmatory comparison: `proposed_v6` vs `llamaindex_sentence` on `mrr`
- Retrieval Holm family size: 55
- Boundary Holm family size: 14
- Seed: 20260804

## Retrieval (micro average)

| Method | N | MRR (95% CI) | Hit@1 | Hit@3 | Hit@5 | Span MRR | Span Hit@5 |
|---|---:|---:|---:|---:|---:|---:|---:|
| fixed | 125 | 0.5158 [0.4435, 0.4435] | 0.3680 | 0.6240 | 0.7200 | 0.1350 | 0.1840 |
| langchain_recursive | 125 | 0.6460 [0.6798, 0.6798] | 0.4960 | 0.7440 | 0.8480 | 0.7329 | 0.8720 |
| llamaindex_sentence | 125 | 0.6934 [0.6357, 0.6357] | 0.5520 | 0.8400 | 0.8880 | 0.7586 | 0.8960 |
| proposed | 125 | 0.6796 [0.6361, 0.6361] | 0.5280 | 0.8160 | 0.8800 | 0.7770 | 0.8960 |
| proposed_v6 | 125 | 0.7937 [0.7476, 0.7476] | 0.6800 | 0.9040 | 0.9520 | 0.8561 | 0.9520 |
| recursive | 125 | 0.6086 [0.5915, 0.5915] | 0.4480 | 0.7200 | 0.8080 | 0.7072 | 0.8480 |
| seac_no_adaptive_threshold | 125 | 0.7959 [0.7932, 0.7932] | 0.6800 | 0.9120 | 0.9520 | 0.8583 | 0.9520 |
| seac_no_continuity | 125 | 0.7979 [0.8892, 0.8892] | 0.6880 | 0.9040 | 0.9520 | 0.8603 | 0.9520 |
| seac_no_density | 125 | 0.7959 [0.7861, 0.7861] | 0.6800 | 0.9120 | 0.9520 | 0.8583 | 0.9520 |
| seac_no_fusion | 125 | 0.8289 [0.8641, 0.8641] | 0.7280 | 0.9200 | 0.9520 | 0.8569 | 0.9520 |
| seac_no_heading_context | 125 | 0.8129 [0.8251, 0.8251] | 0.7280 | 0.8880 | 0.9280 | 0.8209 | 0.9280 |
| seac_no_semantic | 125 | 0.7937 [0.8325, 0.8325] | 0.6800 | 0.9040 | 0.9520 | 0.8561 | 0.9520 |
| seac_no_table_children | 125 | 0.6853 [0.6610, 0.6610] | 0.5440 | 0.8160 | 0.8640 | 0.7827 | 0.8800 |
| semantic | 125 | 0.6999 [0.6841, 0.6841] | 0.5680 | 0.8240 | 0.8640 | 0.8121 | 0.8960 |
| structure | 125 | 0.7290 [0.6556, 0.6556] | 0.6080 | 0.8320 | 0.8880 | 0.8211 | 0.9040 |

## Boundary detection

| Method | N | Precision | Recall | F1 (95% CI) |
|---|---:|---:|---:|---:|
| fixed | 1368 | 0.0000 | 0.0000 | 0.0000 [0.0000, 0.0000] |
| langchain_recursive | 1368 | 0.0074 | 0.0043 | 0.0054 [0.0099, 0.0099] |
| llamaindex_sentence | 1368 | 0.4615 | 0.0511 | 0.0920 [0.1344, 0.1344] |
| proposed | 1368 | 0.4321 | 0.8255 | 0.5673 [0.6073, 0.6073] |
| proposed_v6 | 1368 | 0.4321 | 0.8255 | 0.5673 [0.5282, 0.5282] |
| recursive | 1368 | 0.0402 | 0.0340 | 0.0369 [0.0350, 0.0350] |
| seac_no_adaptive_threshold | 1368 | 0.4365 | 0.8340 | 0.5731 [0.5470, 0.5470] |
| seac_no_continuity | 1368 | 0.4362 | 0.8298 | 0.5718 [0.4790, 0.4790] |
| seac_no_density | 1368 | 0.4305 | 0.8170 | 0.5639 [0.5226, 0.5226] |
| seac_no_fusion | 1368 | 0.4321 | 0.8255 | 0.5673 [0.5270, 0.5270] |
| seac_no_heading_context | 1368 | 0.4321 | 0.8255 | 0.5673 [0.6168, 0.6168] |
| seac_no_semantic | 1368 | 0.4350 | 0.8255 | 0.5698 [0.5531, 0.5531] |
| seac_no_table_children | 1368 | 0.4321 | 0.8255 | 0.5673 [0.5445, 0.5445] |
| semantic | 1368 | 0.2920 | 0.5617 | 0.3843 [0.3715, 0.3715] |
| structure | 1368 | 0.3611 | 0.8298 | 0.5032 [0.5476, 0.5476] |

## Efficiency

| Method | Parents | Retrieval vectors | Chunking s | Index s | Query ms mean/p50/p95 |
|---|---:|---:|---:|---:|---:|
| fixed | 4295 | 4295 | 0.567 | 133.534 | 19.562/16.680/30.661 |
| langchain_recursive | 4502 | 4502 | 1.683 | 116.182 | 14.697/16.515/17.517 |
| llamaindex_sentence | 4390 | 4390 | 4.129 | 119.572 | 14.666/16.532/17.498 |
| proposed | 8746 | 8746 | 110.537 | 125.694 | 15.268/16.989/18.552 |
| proposed_v6 | 8746 | 48912 | 112.106 | 351.115 | 20.925/20.876/27.284 |
| recursive | 4268 | 4268 | 2.045 | 108.983 | 14.589/16.423/17.396 |
| seac_no_adaptive_threshold | 9001 | 49167 | 118.971 | 371.275 | 26.718/21.408/41.373 |
| seac_no_continuity | 8801 | 48967 | 118.479 | 367.491 | 24.749/21.364/41.052 |
| seac_no_density | 8590 | 48756 | 117.090 | 368.472 | 23.785/21.302/39.467 |
| seac_no_fusion | 8746 | 48912 | 115.847 | 349.055 | 20.984/20.937/27.033 |
| seac_no_heading_context | 8746 | 48912 | 119.796 | 123.116 | 21.032/20.830/27.364 |
| seac_no_semantic | 8873 | 49039 | 115.790 | 359.822 | 21.183/20.775/27.378 |
| seac_no_table_children | 8746 | 25107 | 118.756 | 184.696 | 17.895/17.971/23.036 |
| semantic | 10324 | 10324 | 110.170 | 147.684 | 15.375/17.052/18.605 |
| structure | 8891 | 8891 | 0.247 | 139.456 | 15.184/16.873/18.405 |

The prespecified confirmatory p-value, secondary Holm-adjusted p-values, project-level tables, question-type tables, and boundary strata are stored in `formal_analysis.json`.
