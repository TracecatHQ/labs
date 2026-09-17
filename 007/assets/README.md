# Source provenance

The Cases are derived from `test.jsonl` in
[`Multilingual-Multimodal-NLP/SEVENLLM-Dataset`](https://huggingface.co/datasets/Multilingual-Multimodal-NLP/SEVENLLM-Dataset)
at immutable revision `1de23ce55cadc984d3f3a7b52c4035a68c6cd5b0`.
The dataset card declares Apache-2.0 and describes a 1,300-record test split:
50 English MCQ, 50 Chinese MCQ, 600 English QA, and 600 Chinese QA records.

Only records 1201–1250 are imported. `scripts/import_cases.py` requires an
exact revision checkout and rejects count, language, option, answer, and
leakage violations. The generated Cases retain `category`, `input`,
`instruction.question`, and ordered `instruction.choice` values. Source
`thought` and `output` values are not copied into candidate-visible content;
only the normalized output letter is placed in the hidden Oracle.
