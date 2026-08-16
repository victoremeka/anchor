import hashlib

from anchor_client.client import EMBEDDING_DIM


def simple_embed(text: str) -> list:
    """Deterministic stand in for a real embedding model.

    A production deployment calls AWS Bedrock here, per the product spec.
    This keeps the demo runnable without cloud credentials while still
    returning a vector of the right dimension.
    """
    out = []
    seed = text.encode()
    while len(out) < EMBEDDING_DIM:
        seed = hashlib.sha256(seed).digest()
        out.extend((b - 128) / 128.0 for b in seed)
    return out[:EMBEDDING_DIM]
