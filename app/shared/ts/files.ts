interface BrowserFileRowOptions {
  name: string;
  kind: string;
  size?: number;
  depth: number;
  details?: string;
  folder?: { expanded: boolean; toggle: (button: HTMLButtonElement) => unknown };
  preview?: () => unknown;
  selection?: { checked: boolean; disabled: boolean; label: string; change: (checked: boolean) => void };
}

function fileBrowserButton(
  parent: HTMLElement,
  label: string,
  action: (button: HTMLButtonElement) => unknown,
): HTMLButtonElement {
  const button = document.createElement("button");
  button.type = "button";
  button.className = "secondary";
  button.textContent = label;
  button.onclick = () => action(button);
  parent.appendChild(button);
  return button;
}

function setFileBrowserFolderLabel(button: HTMLButtonElement, name: string, expanded: boolean): void {
  button.textContent = `${expanded ? "▾" : "▸"} ${name}`;
  button.setAttribute("aria-expanded", String(expanded));
}

function collapseFileBrowserFolders(expanded: Set<string>, path: string): void {
  for (const folder of expanded) {
    if (folder === path || folder.startsWith(`${path}/`)) expanded.delete(folder);
  }
}

// The two apps supply their own data and actions, while names, indentation, sizes,
// selection and expandable-folder controls use one safe DOM renderer.
function browserFileRow(options: BrowserFileRowOptions): {
  row: HTMLTableRowElement;
  name: HTMLTableCellElement;
  size: HTMLTableCellElement;
  details: HTMLTableCellElement | null;
  actions: HTMLTableCellElement;
} {
  const row = document.createElement("tr");
  const cell = (text = "") => {
    const element = document.createElement("td");
    element.textContent = text;
    row.appendChild(element);
    return element;
  };
  if (options.selection) {
    const selection = cell();
    if (options.kind === "file") {
      const checkbox = document.createElement("input");
      checkbox.type = "checkbox";
      checkbox.checked = options.selection.checked;
      checkbox.disabled = options.selection.disabled;
      checkbox.setAttribute("aria-label", options.selection.label);
      checkbox.onchange = () => options.selection!.change(checkbox.checked);
      selection.appendChild(checkbox);
    }
  }
  const name = cell();
  if (options.depth > 0) name.style.paddingLeft = `${options.depth * 1.25 + 0.75}rem`;
  if (options.folder) {
    const toggle = fileBrowserButton(name, "", options.folder.toggle);
    setFileBrowserFolderLabel(toggle, options.name, options.folder.expanded);
  } else if (options.preview) {
    fileBrowserButton(name, options.name, options.preview);
  } else {
    name.textContent = options.name;
  }
  cell(options.kind);
  const size = cell(options.kind === "file" ? (options.size ? formatBytes(options.size) : "0 B") : "—");
  if (options.size !== undefined) size.title = `${options.size.toLocaleString()} bytes`;
  const details = options.details === undefined ? null : cell(options.details);
  return { row, name, size, details, actions: cell() };
}
