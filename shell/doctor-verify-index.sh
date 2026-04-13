#!/bin/sh
set -eu

compose_project_dir=${COMPOSE_PROJECT_DIR:-.}
compose_file=${COMPOSE_FILE:-docker/docker-compose.yml}

docker compose --project-directory "$compose_project_dir" -f "$compose_file" exec -T rag-mcp sh -lc 'set -eu; tenant="${RAG_CHROMA_TENANT:-default_tenant}"; database="${RAG_CHROMA_DATABASE:-default_database}"; collection="${RAG_COLLECTION_NAME:-rag}"; base="http://chroma:8000/api/v2/tenants/$tenant/databases/$database"; col_payload="$(printf "{\"name\":\"%s\",\"get_or_create\":true,\"metadata\":{\"hnsw:space\":\"cosine\"}}" "$collection")"; col="$(printf "%s" "$col_payload" | wget -qO- --header "Content-Type: application/json" --post-file=- "$base/collections")"; cid="$(printf "%s" "$col" | sed -n "s/.*\"id\":\"\([^\"]*\)\".*/\1/p")"; test -n "$cid"; get="$(printf "%s" "{\"limit\":1,\"offset\":0,\"include\":[\"metadatas\"]}" | wget -qO- --header "Content-Type: application/json" --post-file=- "$base/collections/$cid/get")"; printf "%s" "$get" | grep -Eq "\"ids\":\[[^]]*\"[^\"]+\"" && echo "doctor: indexed data present in Chroma"'
