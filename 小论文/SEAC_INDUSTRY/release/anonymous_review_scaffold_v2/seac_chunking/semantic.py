from __future__ import annotations

import math
from collections import Counter, defaultdict

from .text import char_ngrams, cosine_sparse


class CharTfidfEncoder:
    """Dependency-free character n-gram TF-IDF encoder for the first experiment pass."""

    def __init__(self, ngram: int = 2):
        self.ngram = ngram
        self.idf = {}

    def fit(self, texts: list[str]):
        document_frequency = defaultdict(int)
        for text in texts:
            for term in char_ngrams(text, self.ngram):
                document_frequency[term] += 1
        total = max(1, len(texts))
        self.idf = {
            term: math.log((1 + total) / (1 + frequency)) + 1
            for term, frequency in document_frequency.items()
        }
        return self

    def clear_cache(self):
        self.idf = {}

    def encode(self, text: str) -> Counter:
        terms = char_ngrams(text, self.ngram)
        return Counter({term: frequency * self.idf.get(term, 1.0) for term, frequency in terms.items()})

    def similarity(self, left: str, right: str) -> float:
        return cosine_sparse(self.encode(left), self.encode(right))

    def similarity_vectors(self, left, right) -> float:
        return cosine_sparse(left, right)

    def encode_many(self, texts: list[str]):
        return [self.encode(text) for text in texts]


class SentenceTransformerEncoder:
    """Dense encoder backed by sentence-transformers, used for formal experiments."""

    def __init__(self, model_name: str = "BAAI/bge-m3", batch_size: int = 8, max_length: int = 512, device: str = "cpu"):
        try:
            import numpy as np
            from sentence_transformers import SentenceTransformer
        except ImportError as exc:
            raise RuntimeError(
                "sentence-transformers and numpy are required for the BGE-M3 encoder."
            ) from exc
        self.np = np
        self.model_name = model_name
        self.batch_size = batch_size
        self.model = SentenceTransformer(model_name, device=device)
        self.model.max_seq_length = max_length
        self.cache = {}

    def fit(self, texts: list[str]):
        return self

    def clear_cache(self):
        self.cache.clear()

    def encode_many(self, texts: list[str]):
        if not texts:
            return []
        missing = list(dict.fromkeys(text for text in texts if text not in self.cache))
        if missing:
            vectors = self.model.encode(
                missing,
                batch_size=self.batch_size,
                normalize_embeddings=True,
                show_progress_bar=False,
                convert_to_numpy=True,
            )
            self.cache.update(zip(missing, vectors))
        return self.np.asarray([self.cache[text] for text in texts])

    def encode(self, text: str):
        return self.encode_many([text])[0]

    def similarity_vectors(self, left, right) -> float:
        return float(self.np.dot(left, right))

    def similarity(self, left: str, right: str) -> float:
        vectors = self.encode_many([left, right])
        return self.similarity_vectors(vectors[0], vectors[1])


def build_encoder(name: str, **kwargs):
    if name == "tfidf":
        return CharTfidfEncoder()
    if name == "bge-m3":
        return SentenceTransformerEncoder(model_name="BAAI/bge-m3", **kwargs)
    raise ValueError(f"Unknown encoder {name!r}. Available: tfidf, bge-m3")
