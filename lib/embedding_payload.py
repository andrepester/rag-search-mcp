from __future__ import annotations


def _parse_embedding_vector(vector: object) -> list[float]:
    if not isinstance(vector, list) or not vector:
        raise ValueError("Ollama response contains an invalid embedding vector.")

    values: list[float] = []
    for value in vector:
        if not isinstance(value, (int, float)):
            raise ValueError("Embedding vector contains non-numeric values.")
        values.append(float(value))
    return values


def parse_batch_embedding_payload(
    payload: object, *, expected_count: int | None = None
) -> list[list[float]]:
    if not isinstance(payload, dict):
        raise ValueError("Ollama returned an invalid embeddings payload.")

    embeddings = payload.get("embeddings")
    if not isinstance(embeddings, list):
        raise ValueError("Ollama response is missing 'embeddings'.")

    parsed = [_parse_embedding_vector(embedding) for embedding in embeddings]
    if expected_count is not None and len(parsed) != expected_count:
        raise ValueError(
            "Ollama returned an unexpected embedding count "
            f"(expected {expected_count}, got {len(parsed)})."
        )
    return parsed


def parse_query_embedding_payload(payload: object) -> list[float]:
    parsed = parse_batch_embedding_payload(payload, expected_count=None)
    if not parsed:
        raise ValueError("Ollama response is missing 'embeddings'.")
    return parsed[0]
