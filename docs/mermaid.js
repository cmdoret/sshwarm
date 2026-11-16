// Draws the ```mermaid blocks as diagrams, as GitHub does: mdBook leaves them as code.
import("https://cdn.jsdelivr.net/npm/mermaid@12.2.0/dist/mermaid.esm.min.mjs").then(({ default: mermaid }) => {
  const dark = ["coal", "navy", "ayu"].some((theme) => document.documentElement.classList.contains(theme));
  mermaid.initialize({ startOnLoad: false, theme: dark ? "dark" : "default" });

  for (const code of document.querySelectorAll("code.language-mermaid")) {
    const diagram = document.createElement("pre");
    diagram.className = "mermaid";
    diagram.textContent = code.textContent;
    code.parentElement.replaceWith(diagram);
  }

  mermaid.run({ querySelector: "pre.mermaid" });
});
