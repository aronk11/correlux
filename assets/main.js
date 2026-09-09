const motion = window.matchMedia("(prefers-reduced-motion: reduce)");
const motionButton = document.querySelector("[data-motion]");
if (motionButton) {
  motionButton.hidden = motion.matches;
  motionButton.addEventListener("click", () => {
    const paused = document.documentElement.classList.toggle("motion-paused");
    motionButton.setAttribute("aria-pressed", String(paused));
    motionButton.textContent = paused ? "Resume motion ▷" : "Pause motion Ⅱ";
  });
  motion.addEventListener("change", () => {
    motionButton.hidden = motion.matches;
  });
}
if (!motion.matches && "IntersectionObserver" in window) {
  document.documentElement.classList.add("motion-ready");
  const observer = new IntersectionObserver(
    (entries) => {
      for (const entry of entries)
        if (entry.isIntersecting) {
          entry.target.classList.add("is-visible");
          observer.unobserve(entry.target);
        }
    },
    { threshold: 0.08 },
  );
  document.querySelectorAll(".reveal").forEach((el) => observer.observe(el));
}

function selectTab(button, focus = false) {
  const group = button.closest('[role="tablist"]');
  group.querySelectorAll('[role="tab"]').forEach((tab) => {
    const selected = tab === button;
    tab.setAttribute("aria-selected", String(selected));
    tab.tabIndex = selected ? 0 : -1;
    const panel = document.getElementById(tab.getAttribute("aria-controls"));
    panel.hidden = !selected;
    panel.classList.toggle("panel-enter", selected);
  });
  if (focus) button.focus();
}
document.querySelectorAll('[role="tablist"]').forEach((group) => {
  group.addEventListener("click", (event) => {
    const tab = event.target.closest('[role="tab"]');
    if (tab) selectTab(tab);
  });
  group.addEventListener("keydown", (event) => {
    const tabs = [...group.querySelectorAll('[role="tab"]')];
    const index = tabs.indexOf(document.activeElement);
    if (index < 0) return;
    let next;
    if (event.key === "ArrowRight") next = (index + 1) % tabs.length;
    if (event.key === "ArrowLeft")
      next = (index - 1 + tabs.length) % tabs.length;
    if (event.key === "Home") next = 0;
    if (event.key === "End") next = tabs.length - 1;
    if (next !== undefined) {
      event.preventDefault();
      selectTab(tabs[next], true);
    }
  });
});
document
  .querySelectorAll("[data-open-why]")
  .forEach((button) =>
    button.addEventListener("click", () =>
      selectTab(document.getElementById("tab-why"), true),
    ),
  );

let toastTimer;
function notify(message) {
  const toast = document.querySelector(".toast");
  if (!toast) return;
  clearTimeout(toastTimer);
  toast.textContent = message;
  toast.classList.add("visible");
  toastTimer = setTimeout(() => toast.classList.remove("visible"), 3500);
}
document.querySelectorAll(".prose pre").forEach((pre) => {
  const text = pre.querySelector("code")?.textContent;
  if (!text) return;
  const button = document.createElement("button");
  button.className = "copy-button";
  button.setAttribute("aria-label", "Copy code");
  button.dataset.copy = text.trimEnd();
  button.textContent = "⧉";
  pre.prepend(button);
});
document.addEventListener("click", async (event) => {
  const button = event.target.closest("[data-copy]");
  if (!button) return;
  try {
    await navigator.clipboard.writeText(button.dataset.copy);
    notify("Copied. Your terminal is waiting.");
  } catch {
    notify("Copy unavailable here. Select the command and copy it manually.");
  }
});

const dialog = document.querySelector(".search-dialog");
if (dialog) {
  const input = dialog.querySelector("input");
  const results = dialog.querySelector(".search-results");
  const status = dialog.querySelector(".search-count");
  let index;
  let pending;
  let opener;
  async function getIndex() {
    if (index) return index;
    if (!pending)
      pending = fetch(new URL("search.json", document.baseURI))
        .then((response) => {
          if (!response.ok) throw new Error("Search index unavailable");
          return response.json();
        })
        .then((data) => {
          index = data;
          return data;
        })
        .finally(() => {
          pending = undefined;
        });
    return pending;
  }
  async function search() {
    const query = input.value.trim().toLowerCase();
    results.replaceChildren();
    if (!query) {
      status.textContent =
        "Search the guides, architecture and design decisions.";
      return;
    }
    status.textContent = "Searching…";
    try {
      const entries = await getIndex();
      if (query !== input.value.trim().toLowerCase()) return;
      const words = query.split(/\s+/);
      const matches = entries
        .filter((entry) =>
          words.every((word) =>
            `${entry.title} ${entry.text}`.toLowerCase().includes(word),
          ),
        )
        .map((entry) => ({
          ...entry,
          score: words.reduce(
            (sum, word) =>
              sum + (entry.title.toLowerCase().includes(word) ? 10 : 0),
            0,
          ),
        }))
        .sort((a, b) => b.score - a.score)
        .slice(0, 20);
      results.replaceChildren();
      status.textContent = matches.length
        ? `${matches.length} results. Tab to a result and press Enter.`
        : "No results. Try “fleet”, “logs” or “configuration”.";
      for (const entry of matches) {
        const link = document.createElement("a");
        link.href = entry.href;
        const title = document.createElement("strong");
        title.textContent = entry.title;
        const snippet = document.createElement("p");
        const position = entry.text.toLowerCase().indexOf(words[0]);
        const start = Math.max(0, position - 45);
        snippet.textContent =
          (start ? "…" : "") + entry.text.slice(start, start + 160) + "…";
        link.append(title, snippet);
        results.append(link);
      }
    } catch {
      status.textContent =
        "Search could not load. Use the documentation navigation, or try again.";
    }
  }
  function openSearch() {
    opener = document.activeElement;
    dialog.showModal();
    input.focus();
    search();
  }
  document
    .querySelectorAll("[data-search]")
    .forEach((button) => button.addEventListener("click", openSearch));
  dialog
    .querySelector(".search-close")
    .addEventListener("click", () => dialog.close());
  dialog.addEventListener("close", () => opener?.focus());
  dialog.addEventListener("click", (event) => {
    if (event.target === dialog) {
      const rect = dialog.getBoundingClientRect();
      if (
        event.clientX < rect.left ||
        event.clientX > rect.right ||
        event.clientY < rect.top ||
        event.clientY > rect.bottom
      )
        dialog.close();
    }
  });
  input.addEventListener("input", search);
  document.addEventListener("keydown", (event) => {
    if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === "k") {
      event.preventDefault();
      if (!dialog.open) openSearch();
    }
  });
}
