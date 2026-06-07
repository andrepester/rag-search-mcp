const form = document.querySelector("#search-form");
const submitButton = form.querySelector("button[type='submit']");
const queryInput = document.querySelector("#query");
const sourceFilterInput = document.querySelector("#source-filter");
const sourceBrowserOpen = document.querySelector("#source-browser-open");
const sourceBrowserClose = document.querySelector("#source-browser-close");
const sourceModal = document.querySelector("#source-modal");
const sourceTree = document.querySelector("#source-tree");
const sourceTreeState = document.querySelector("#source-tree-state");
const selectedSource = document.querySelector("#selected-source");
const sourceClear = document.querySelector("#source-clear");
const sourceApply = document.querySelector("#source-apply");
const resultsState = document.querySelector("#results-state");
const resultsList = document.querySelector("#results-list");
const resultCount = document.querySelector("#result-count");
const resultHidden = document.querySelector("#result-hidden");
const resultHiddenCount = document.querySelector("#result-hidden-count");
const detailState = document.querySelector("#detail-state");
const detailScope = document.querySelector("#detail-scope");
const chunkDetail = document.querySelector("#chunk-detail");
const chunkMetadata = document.querySelector("#chunk-metadata");
const chunkText = document.querySelector("#chunk-text");

let activeChunkID = "";
let indexedSources = [];
let pendingSourceFilter = "";
let sourceRequestID = 0;

form.addEventListener("submit", (event) => {
  event.preventDefault();
  runSearch();
});

document.querySelectorAll("input[name='scope']").forEach((input) => {
  input.addEventListener("change", () => {
    loadSources(input.value);
  });
});

sourceFilterInput.addEventListener("click", () => {
  openSourceBrowser();
});

sourceBrowserOpen.addEventListener("click", () => {
  openSourceBrowser();
});

sourceBrowserClose.addEventListener("click", () => {
  sourceModal.close();
});

sourceModal.addEventListener("click", (event) => {
  if (event.target === sourceModal) {
    sourceModal.close();
  }
});

sourceClear.addEventListener("click", () => {
  pendingSourceFilter = "";
  sourceFilterInput.value = "";
  updateSelectedSource();
  renderSourceTree(indexedSources);
});

sourceApply.addEventListener("click", () => {
  sourceFilterInput.value = pendingSourceFilter;
  updateSelectedSource();
  sourceModal.close();
});

loadSources(currentScope());

async function runSearch() {
  const query = queryInput.value.trim();
  const sourceFilter = sourceFilterInput.value.trim();

  clearDetail("Select a result to open the full chunk.");

  if (!query) {
    setResultsState("Enter a search before searching.", true);
    queryInput.focus();
    return;
  }

  setResultsState("Searching index...", false);
  renderResults([]);
  submitButton.disabled = true;

  try {
    const response = await fetchJSON("/api/search", {
      method: "POST",
      body: JSON.stringify({
        query,
        scope: currentScope(),
        source_filter: sourceFilter,
        max_distance: currentMaxDistance(),
      }),
    });

    const matches = response.matches || [];
    const omittedWeakMatches = Number.parseInt(response.omitted_weak_matches || 0, 10);
    renderResults(matches, omittedWeakMatches);
    if (matches.length === 0) {
      if (omittedWeakMatches > 0) {
        setResultsState("No relevant matches found.", false);
      } else {
        setResultsState("No matches found.", false);
      }
      return;
    }

    resultsState.textContent = "";
    resultsState.hidden = true;
  } catch (error) {
    setResultsState(error.message, true);
  } finally {
    submitButton.disabled = false;
  }
}

function currentMaxDistance() {
  const checked = form.querySelector("input[name='max_distance']:checked");
  return numberOr(checked ? checked.value : "", 0.5);
}

async function openChunk(match) {
  activeChunkID = match.chunk_id || "";
  clearDetail("Loading chunk...");

  try {
    const response = await fetchJSON("/api/chunk", {
      method: "POST",
      body: JSON.stringify({ chunk_id: activeChunkID }),
    });
    if (!response.found || !response.chunk) {
      clearDetail("Chunk not found.");
      return;
    }
    renderChunk(response);
  } catch (error) {
    clearDetail(error.message);
    detailState.classList.add("is-error");
  }
}

async function loadSources(scope) {
  const requestID = sourceRequestID + 1;
  sourceRequestID = requestID;

  try {
    const response = await fetchJSON(`/api/sources?scope=${encodeURIComponent(scope)}`, {
      method: "GET",
    });
    if (requestID !== sourceRequestID) {
      return;
    }
    indexedSources = response.sources || [];
    clearUnavailableSourceFilter();
    renderSourceTree(indexedSources);
  } catch {
    if (requestID !== sourceRequestID) {
      return;
    }
    indexedSources = [];
    renderSourceTree(indexedSources, true);
  }
}

async function openSourceBrowser() {
  pendingSourceFilter = sourceFilterInput.value.trim();
  updateSelectedSource();
  renderSourceTree(indexedSources);
  if (sourceModal.showModal) {
    sourceModal.showModal();
  } else {
    sourceModal.setAttribute("open", "");
  }
  sourceBrowserClose.focus();
  await loadSources(currentScope());
}

function renderSourceTree(sources, hasError) {
  sourceTree.replaceChildren();
  sourceTreeState.classList.remove("is-error");

  if (hasError) {
    sourceTreeState.textContent = "Could not load indexed directories.";
    sourceTreeState.classList.add("is-error");
    sourceTreeState.hidden = false;
    return;
  }
  if (!sources || sources.length === 0) {
    sourceTreeState.textContent = "No indexed directories found.";
    sourceTreeState.hidden = false;
    return;
  }

  const tree = buildSourceTree(sources);
  if (tree.children.size === 0) {
    sourceTreeState.textContent = "No indexed directories found.";
    sourceTreeState.hidden = false;
    return;
  }

  sourceTreeState.hidden = true;
  sourceTree.append(renderTreeList(tree.children, 1));
}

function buildSourceTree(sources) {
  const root = createDirectoryNode("", "");
  sources.forEach((sourcePath) => {
    const parts = sourcePath.split("/").filter(Boolean);
    if (parts.length < 2) {
      return;
    }

    let current = root;
    parts.slice(0, -1).forEach((part, index) => {
      const path = parts.slice(0, index + 1).join("/") + "/";
      if (!current.children.has(part)) {
        current.children.set(part, createDirectoryNode(part, path));
      }
      current = current.children.get(part);
      current.fileCount += 1;
    });
  });
  return root;
}

function createDirectoryNode(name, path) {
  return {
    name,
    path,
    fileCount: 0,
    children: new Map(),
  };
}

function renderTreeList(children, level) {
  const list = document.createElement("ul");
  list.className = "source-tree-list";
  list.setAttribute("role", "group");

  Array.from(children.values())
    .sort(compareTreeNodes)
    .forEach((node) => {
      const item = document.createElement("li");
      item.setAttribute("role", "none");

      const button = document.createElement("button");
      button.type = "button";
      button.className = "source-node-button";
      button.classList.toggle("is-active", node.path === pendingSourceFilter);
      button.setAttribute("role", "treeitem");
      button.setAttribute("aria-level", String(level));
      button.setAttribute("aria-selected", node.path === pendingSourceFilter ? "true" : "false");
      if (node.children.size > 0) {
        button.setAttribute("aria-expanded", "true");
      }
      button.setAttribute("aria-label", `${node.path} ${formatFileCount(node.fileCount)}`);
      button.addEventListener("click", () => {
        pendingSourceFilter = node.path;
        updateSelectedSource();
        renderSourceTree(indexedSources);
      });

      const kind = document.createElement("span");
      kind.className = "source-node-kind";
      kind.textContent = "dir";

      const label = document.createElement("span");
      label.className = "source-node-label";
      label.textContent = `${node.name}/`;

      const count = document.createElement("span");
      count.className = "source-node-count";
      count.textContent = formatFileCount(node.fileCount);

      button.append(kind, label, count);
      item.append(button);

      if (node.children.size > 0) {
        item.append(renderTreeList(node.children, level + 1));
      }
      list.append(item);
    });

  return list;
}

function compareTreeNodes(a, b) {
  return a.name.localeCompare(b.name);
}

function formatFileCount(count) {
  return `${count} ${count === 1 ? "file" : "files"}`;
}

function updateSelectedSource() {
  selectedSource.textContent = pendingSourceFilter || "All directories";
}

function clearUnavailableSourceFilter() {
  const appliedFilter = sourceFilterInput.value.trim();
  if (appliedFilter && !sourceFilterHasMatches(appliedFilter, indexedSources)) {
    sourceFilterInput.value = "";
  }

  if (pendingSourceFilter && !sourceFilterHasMatches(pendingSourceFilter, indexedSources)) {
    pendingSourceFilter = sourceFilterInput.value.trim();
  }
  updateSelectedSource();
}

function sourceFilterHasMatches(filter, sources) {
  const normalizedFilter = filter.toLowerCase();
  return sources.some((sourcePath) => sourcePath.toLowerCase().startsWith(normalizedFilter));
}

async function fetchJSON(url, options = {}) {
  const headers = {
    Accept: "application/json",
    ...(options.body ? { "Content-Type": "application/json" } : {}),
    ...(options.headers || {}),
  };
  const response = await fetch(url, {
    ...options,
    headers,
  });
  const payload = await response.json().catch(() => ({}));
  if (!response.ok) {
    throw new Error(payload.error || `Request failed with HTTP ${response.status}`);
  }
  return payload;
}

function renderResults(matches, hiddenMatches = 0) {
  resultsList.replaceChildren();
  resultCount.textContent = `${matches.length} shown`;
  updateHiddenResultCount(hiddenMatches);

  matches.forEach((match) => {
    const item = document.createElement("li");
    item.className = "result-item";

    const button = document.createElement("button");
    button.type = "button";
    button.className = "result-button";
    button.addEventListener("click", () => {
      resultsList.querySelectorAll(".result-button").forEach((node) => node.classList.remove("is-active"));
      button.classList.add("is-active");
      openChunk(match);
    });

    const title = document.createElement("div");
    title.className = "result-title";
    title.textContent = match.source_path || "Unknown source";

    const meta = document.createElement("div");
    meta.className = "result-meta";
    addPill(meta, match.scope || "scope");
    addPill(meta, match.chunk_id || "chunk");
    if (Number.isInteger(match.chunk_index)) {
      addPill(meta, `#${match.chunk_index}`);
    }
    if (typeof match.distance === "number") {
      addPill(meta, `distance ${match.distance.toFixed(4)}`);
    }

    const text = document.createElement("div");
    text.className = "result-text";
    text.textContent = match.text || "";

    button.append(title, meta, text);
    item.append(button);
    resultsList.append(item);
  });
}

function updateHiddenResultCount(hiddenMatches) {
  const hiddenCount = Number.parseInt(hiddenMatches || 0, 10);
  if (!hiddenCount) {
    resultHidden.hidden = true;
    resultHiddenCount.textContent = "0 hidden";
    return;
  }

  resultHiddenCount.textContent = `${hiddenCount} hidden`;
  resultHidden.hidden = false;
}

function renderChunk(response) {
  const chunk = response.chunk;
  chunkMetadata.replaceChildren();
  addMetadata("Source", chunk.source_path || "");
  addMetadata("Scope", chunk.scope || "");
  addMetadata("Chunk ID", response.chunk_id || chunk.chunk_id || "");
  if (Number.isInteger(chunk.chunk_index)) {
    addMetadata("Index", String(chunk.chunk_index));
  }
  if (response.metadata) {
    addMetadata("Metadata", JSON.stringify(response.metadata, null, 2));
  }

  chunkText.textContent = chunk.text || "";
  detailScope.textContent = chunk.scope || "";
  detailState.textContent = "";
  detailState.hidden = true;
  detailState.classList.remove("is-error");
  chunkDetail.hidden = false;
}

function addMetadata(label, value) {
  const term = document.createElement("dt");
  term.textContent = label;
  const description = document.createElement("dd");
  description.textContent = value;
  chunkMetadata.append(term, description);
}

function addPill(parent, value) {
  const pill = document.createElement("span");
  pill.className = "pill";
  pill.textContent = value;
  parent.append(pill);
}

function setResultsState(message, isError) {
  resultsState.textContent = message;
  resultsState.hidden = false;
  resultsState.classList.toggle("is-error", Boolean(isError));
}

function clearDetail(message) {
  detailState.textContent = message;
  detailState.hidden = false;
  detailState.classList.remove("is-error");
  detailScope.textContent = "";
  chunkDetail.hidden = true;
  chunkMetadata.replaceChildren();
  chunkText.textContent = "";
}

function numberOr(value, fallback) {
  const parsed = Number.parseFloat(value);
  return Number.isFinite(parsed) ? parsed : fallback;
}

function currentScope() {
  const checked = form.querySelector("input[name='scope']:checked");
  return checked ? checked.value : "all";
}
