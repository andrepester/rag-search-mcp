# Installation Golden Fixture

Installation starts with make install. The bootstrap flow prepares .env, starts
docker compose, pulls the embedding model, rebuilds the index, and verifies the
indexed data.

After changing mounted documentation or source code, run make reindex so the
fresh Chroma collection reflects the current fixture state.
