# SEAC formal statistical analysis

- Primary method: `proposed_v6`
- Analysis scope: `locked_test`
- Projects: P04, P05, P06, P07, P08, P09, P10
- Hierarchical bootstrap samples: 10000
- Paired randomization unit: `project_id`
- Confirmatory comparison: `proposed_v6` vs `llamaindex_sentence` on `mrr`
- Retrieval Holm family size: 55
- Boundary Holm family size: 14
- Seed: 20260804

## Retrieval (micro average)

| Method | N | MRR (95% CI) | Hit@1 | Hit@3 | Hit@5 | Span MRR | Span Hit@5 |
|---|---:|---:|---:|---:|---:|---:|---:|
| fixed | 125 | 0.5158 [0.4137, 0.6211] | 0.3680 | 0.6240 | 0.7200 | 0.1350 | 0.1840 |
| langchain_recursive | 125 | 0.6460 [0.5380, 0.7424] | 0.4960 | 0.7440 | 0.8480 | 0.7329 | 0.8720 |
| llamaindex_sentence | 125 | 0.6934 [0.5855, 0.7936] | 0.5520 | 0.8400 | 0.8880 | 0.7586 | 0.8960 |
| proposed | 125 | 0.6796 [0.5713, 0.7865] | 0.5280 | 0.8160 | 0.8800 | 0.7770 | 0.8960 |
| proposed_v6 | 125 | 0.7937 [0.7060, 0.8772] | 0.6800 | 0.9040 | 0.9520 | 0.8561 | 0.9520 |
| recursive | 125 | 0.6086 [0.5076, 0.7031] | 0.4480 | 0.7200 | 0.8080 | 0.7072 | 0.8480 |
| seac_no_adaptive_threshold | 125 | 0.7959 [0.7067, 0.8793] | 0.6800 | 0.9120 | 0.9520 | 0.8583 | 0.9520 |
| seac_no_continuity | 125 | 0.7979 [0.7069, 0.8853] | 0.6880 | 0.9040 | 0.9520 | 0.8603 | 0.9520 |
| seac_no_density | 125 | 0.7959 [0.7090, 0.8805] | 0.6800 | 0.9120 | 0.9520 | 0.8583 | 0.9520 |
| seac_no_fusion | 125 | 0.8289 [0.7535, 0.8990] | 0.7280 | 0.9200 | 0.9520 | 0.8569 | 0.9520 |
| seac_no_heading_context | 125 | 0.8129 [0.7468, 0.8769] | 0.7280 | 0.8880 | 0.9280 | 0.8209 | 0.9280 |
| seac_no_semantic | 125 | 0.7937 [0.7067, 0.8770] | 0.6800 | 0.9040 | 0.9520 | 0.8561 | 0.9520 |
| seac_no_table_children | 125 | 0.6853 [0.5837, 0.7833] | 0.5440 | 0.8160 | 0.8640 | 0.7827 | 0.8800 |
| semantic | 125 | 0.6999 [0.6043, 0.7980] | 0.5680 | 0.8240 | 0.8640 | 0.8121 | 0.8960 |
| structure | 125 | 0.7290 [0.6145, 0.8343] | 0.6080 | 0.8320 | 0.8880 | 0.8211 | 0.9040 |

## Boundary detection

| Method | N | Precision | Recall | F1 (95% CI) |
|---|---:|---:|---:|---:|
| fixed | 1368 | 0.0000 | 0.0000 | 0.0000 [0.0000, 0.0000] |
| langchain_recursive | 1368 | 0.0074 | 0.0043 | 0.0054 [0.0000, 0.0265] |
| llamaindex_sentence | 1368 | 0.4615 | 0.0511 | 0.0920 [0.0367, 0.1570] |
| proposed | 1368 | 0.4321 | 0.8255 | 0.5673 [0.4704, 0.6486] |
| proposed_v6 | 1368 | 0.4321 | 0.8255 | 0.5673 [0.4708, 0.6488] |
| recursive | 1368 | 0.0402 | 0.0340 | 0.0369 [0.0051, 0.0783] |
| seac_no_adaptive_threshold | 1368 | 0.4365 | 0.8340 | 0.5731 [0.4772, 0.6554] |
| seac_no_continuity | 1368 | 0.4362 | 0.8298 | 0.5718 [0.4759, 0.6539] |
| seac_no_density | 1368 | 0.4305 | 0.8170 | 0.5639 [0.4740, 0.6439] |
| seac_no_fusion | 1368 | 0.4321 | 0.8255 | 0.5673 [0.4714, 0.6491] |
| seac_no_heading_context | 1368 | 0.4321 | 0.8255 | 0.5673 [0.4700, 0.6507] |
| seac_no_semantic | 1368 | 0.4350 | 0.8255 | 0.5698 [0.4688, 0.6558] |
| seac_no_table_children | 1368 | 0.4321 | 0.8255 | 0.5673 [0.4688, 0.6519] |
| semantic | 1368 | 0.2920 | 0.5617 | 0.3843 [0.2779, 0.4800] |
| structure | 1368 | 0.3611 | 0.8298 | 0.5032 [0.4108, 0.5859] |

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
