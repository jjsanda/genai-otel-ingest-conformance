"""Deterministic local vector store over a tiny travel corpus.

The embedding is a hashed bag-of-words: each token seeds a PRNG that yields a
fixed pseudo-random unit vector, and a text's embedding is the normalized sum
of its token vectors. Pure stdlib, fully deterministic across runs and
platforms — which is what lets the evaluation harness assert exact scores and
the conformance goldens stay stable. It is intentionally simple; the point of
this demo is the telemetry shape of a retrieval step, not retrieval quality.
"""

import hashlib
import math
import random
import re
from dataclasses import dataclass

DIMENSIONS = 64
DATA_SOURCE_ID = "travel-kb"

_TOKEN_RE = re.compile(r"[a-z0-9]+")


@dataclass(frozen=True)
class Document:
    id: str
    title: str
    text: str


CORPUS: list[Document] = [
    Document(
        "paris-overview",
        "Paris overview",
        "Paris is the capital of France, famous for the Eiffel Tower, "
        "the Louvre museum, and cafe culture along the Seine.",
    ),
    Document(
        "paris-food",
        "Paris food",
        "Parisian food highlights include fresh croissants, macarons, "
        "and bistro classics like steak frites and onion soup.",
    ),
    Document(
        "rome-overview",
        "Rome overview",
        "Rome, the capital of Italy, is home to the Colosseum, "
        "the Roman Forum, and the Vatican museums.",
    ),
    Document(
        "rome-history",
        "Rome history",
        "Ancient Rome grew from a small town into an empire; "
        "ruins like the Pantheon still stand in the city center.",
    ),
    Document(
        "tokyo-overview",
        "Tokyo overview",
        "Tokyo is Japan's capital, blending neon districts like Shibuya "
        "with quiet temples, gardens, and exceptional food.",
    ),
    Document(
        "tokyo-transport",
        "Tokyo transport",
        "Tokyo's rail network is punctual and dense; a prepaid Suica card "
        "makes trains and metro travel effortless.",
    ),
    Document(
        "prague-overview",
        "Prague overview",
        "Prague, the capital of Czechia, is known for its old town square, "
        "astronomical clock, and Charles Bridge.",
    ),
    Document(
        "prague-castle",
        "Prague castle",
        "Prague Castle overlooks the Vltava river and is among the largest "
        "castle complexes in the world.",
    ),
    Document(
        "barcelona-overview",
        "Barcelona overview",
        "Barcelona on Spain's coast is famous for Gaudi's Sagrada Familia, "
        "Park Guell, and lively tapas bars.",
    ),
    Document(
        "london-overview",
        "London overview",
        "London offers the British Museum, the Tower of London, "
        "and West End theatre along the Thames.",
    ),
    Document(
        "vienna-overview",
        "Vienna overview",
        "Vienna, Austria's capital, is celebrated for imperial palaces, "
        "coffee houses, and classical music heritage.",
    ),
    Document(
        "budget-tips",
        "Budget tips",
        "For budget travel in Europe, book trains early, favor lunch menus "
        "over dinner, and use city passes for museums.",
    ),
]


def _tokenize(text: str) -> list[str]:
    return _TOKEN_RE.findall(text.lower())


def _token_vector(token: str) -> list[float]:
    # A stable seed per token: sha256 rather than hash() because the latter is
    # salted per process, which would break run-to-run determinism.
    seed = int.from_bytes(hashlib.sha256(token.encode()).digest()[:8], "big")
    rng = random.Random(seed)
    return [rng.gauss(0.0, 1.0) for _ in range(DIMENSIONS)]


def embed(text: str) -> list[float]:
    acc = [0.0] * DIMENSIONS
    for token in _tokenize(text):
        vec = _token_vector(token)
        for i in range(DIMENSIONS):
            acc[i] += vec[i]
    norm = math.sqrt(sum(x * x for x in acc))
    if norm == 0.0:
        return acc
    return [x / norm for x in acc]


def _cosine(a: list[float], b: list[float]) -> float:
    return sum(x * y for x, y in zip(a, b, strict=True))


@dataclass(frozen=True)
class Hit:
    document: Document
    score: float


class VectorStore:
    """In-memory store with precomputed document embeddings."""

    def __init__(self, corpus: list[Document] | None = None) -> None:
        self._docs = corpus if corpus is not None else CORPUS
        self._vectors = [embed(f"{d.title} {d.text}") for d in self._docs]

    def search(self, query: str, top_k: int = 3) -> list[Hit]:
        qv = embed(query)
        scored = [
            Hit(doc, _cosine(qv, dv)) for doc, dv in zip(self._docs, self._vectors, strict=True)
        ]
        scored.sort(key=lambda h: (-h.score, h.document.id))
        return scored[:top_k]

    def by_id(self, doc_id: str) -> Document | None:
        for doc in self._docs:
            if doc.id == doc_id:
                return doc
        return None
