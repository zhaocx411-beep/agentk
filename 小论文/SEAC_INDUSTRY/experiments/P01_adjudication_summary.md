# P01 Boundary Annotation Adjudication Summary

- Total candidates: 4783
- Second annotator matched candidates: 4783
- First/second disagreements: 131
- Final label counts: {'NOISE': 325, 'BREAK': 1065, 'NO_BREAK': 3393}
- Changed from first annotator: 17
- Changed from second annotator: 114

## Agreement

- first_vs_second: agreement=0.9726, kappa=0.9366, agree=4652/4783
- first_vs_final: agreement=0.9964, kappa=0.9920, agree=4766/4783
- second_vs_final: agreement=0.9762, kappa=0.9449, agree=4669/4783

## Arbitration Rules

- 概述后进入可独立检索的子功能/画面/接口/数据实体：BREAK。
- 数据库设计工作簿开头目录/表清单：NOISE；实际表定义从正文表开始评价。
- 业务流程里的签字/会签/安全告知书字段：NO_BREAK，不视作文档签章页噪声。
- 封面、目录、目录项相邻：NOISE。
- 标题与其直接正文、会议纪要标题与会议基本信息表：NO_BREAK。

## Disagreement Decisions

- BREAK: 104
- NOISE: 20
- NO_BREAK: 7

## Final Gold Evaluation (TF-IDF)

| Method | Boundary F1 | Precision | Recall | MRR | Hit@1 | Hit@3 | Hit@5 |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| fixed | 0.0000 | 0.0000 | 0.0000 | 0.6421 | 0.4722 | 0.7778 | 0.9167 |
| recursive | 0.0241 | 0.0829 | 0.0141 | 0.6439 | 0.5278 | 0.6389 | 0.8333 |
| structure | 0.7283 | 0.5730 | 0.9991 | 0.5636 | 0.4167 | 0.6111 | 0.7500 |
| semantic | 0.3841 | 0.2772 | 0.6254 | 0.3834 | 0.2778 | 0.3889 | 0.4444 |
| proposed | 0.9027 | 0.8272 | 0.9934 | 0.5331 | 0.3889 | 0.5278 | 0.8056 |
| proposed_v6 | 0.9027 | 0.8272 | 0.9934 | 0.6720 | 0.5278 | 0.7778 | 0.8611 |
