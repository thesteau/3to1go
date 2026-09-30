interface AppDialogOptions {
  title?: string;
  message?: string;
  input?: boolean;
  inputLabel?: string;
  inputType?: string;
  confirmLabel?: string;
  danger?: boolean;
}

const TOAST_DURATION_MS = 8000;
let _appDialogResolve: ((confirmed: boolean) => void) | null = null;

function normalizeTheme(theme: string | undefined): "light" | "dark" {
  return theme === "light" ? "light" : "dark";
}

function applyTheme(theme: string | undefined): void {
  const resolved = normalizeTheme(theme);
  document.documentElement.dataset.theme = resolved;
  const setting = document.getElementById("settings_theme_dark");
  if (setting) {
    const on = resolved === "dark";
    setting.setAttribute("aria-checked", on ? "true" : "false");
    setting.classList.toggle("toggle-on", on);
  }
}

function showToast(message: unknown, kind: StatusKind = "info", { duration = TOAST_DURATION_MS, title = "" } = {}): void {
  const text = formatMessage(message);
  if (!text) return;
  const region = document.getElementById("toast-region");
  if (!region) return;
  if (region.showPopover) {
    if (region.matches(":popover-open")) region.hidePopover();
    region.showPopover();
  }

  const defaultTitle = kind === "error" ? "Something needs attention" : kind === "success" ? "Done" : "Notice";
  const toast = document.createElement("div");
  toast.className = `toast ${kind}`;
  toast.setAttribute("role", "status");
  toast.innerHTML = `<strong class="toast-title">${escapeHtml(title || defaultTitle)}</strong><span>${escapeHtml(text)}</span>`;
  region.appendChild(toast);
  requestAnimationFrame(() => toast.classList.add("visible"));

  window.setTimeout(() => {
    toast.classList.remove("visible");
    window.setTimeout(() => toast.remove(), 180);
  }, duration);
}

function setActionStatus(message: unknown, kind: StatusKind = "info"): void {
  showToast(message, kind);
}

function setStatus(id: string, message: unknown, kind: StatusKind = "info"): void {
  const text = formatMessage(message);
  if (text) showToast(text, kind);
}

function clearStatus(id: string): void {}

function openDialog(id: string): void {
  const dialog = document.getElementById(id) as HTMLDialogElement | null;
  if (!dialog?.showModal || dialog.open) return;
  dialog.showModal();
  const region = document.getElementById("toast-region");
  if (region?.showPopover && region.matches(":popover-open")) {
    region.hidePopover();
    region.showPopover();
  }
}

function closeDialog(id: string): void {
  const dialog = document.getElementById(id) as HTMLDialogElement | null;
  if (dialog?.open) {
    dialog.close();
  }
}

function appDialog(options: AppDialogOptions & { input: true }): Promise<string | null>;
function appDialog(options?: AppDialogOptions): Promise<string | boolean | null>;
function appDialog({ title, message, input = false, inputLabel = "", inputType = "text", confirmLabel = "Continue", danger = false }: AppDialogOptions = {}): Promise<string | boolean | null> {
  const dialog = document.getElementById("app-dialog") as HTMLDialogElement | null;
  if (!dialog?.showModal) {
    return Promise.resolve(input ? null : false);
  }
  if (_appDialogResolve) {
    resolveAppDialog(false);
  }

  document.getElementById("app-dialog-title")!.textContent = title || "Confirm";
  document.getElementById("app-dialog-message")!.textContent = message || "";
  const inputWrap = document.getElementById("app-dialog-input-wrap")!;
  const inputElement = document.getElementById("app-dialog-input") as HTMLInputElement;
  document.getElementById("app-dialog-input-label")!.textContent = inputLabel || "";
  inputWrap.hidden = !input;
  inputElement.type = inputType === "secret" ? "text" : inputType;
  inputElement.classList.toggle("secret-value", inputType === "secret");
  inputElement.autocomplete = "off";
  inputElement.spellcheck = false;
  inputElement.autocapitalize = "none";
  inputElement.value = "";
  const confirmButton = document.getElementById("app-dialog-confirm")!;
  confirmButton.textContent = confirmLabel;
  confirmButton.className = danger ? "danger" : "";
  dialog.oncancel = (event) => {
    event.preventDefault();
    resolveAppDialog(false);
  };
  inputElement.onkeydown = (event) => {
    if (event.key === "Enter") {
      event.preventDefault();
      resolveAppDialog(true);
    }
  };

  dialog.showModal();
  if (input) {
    window.setTimeout(() => inputElement.focus(), 0);
  }

  return new Promise((resolve) => {
    _appDialogResolve = (confirmed) => {
      const value = input ? inputElement.value.trim() : confirmed;
      _appDialogResolve = null;
      closeDialog("app-dialog");
      resolve(confirmed ? value : null);
    };
  });
}

function resolveAppDialog(confirmed: boolean): void {
  if (_appDialogResolve) {
    _appDialogResolve(confirmed);
  }
}

function confirmApp(options: AppDialogOptions): Promise<boolean> {
  return appDialog(options).then(Boolean);
}

// Editing controls remain locked until their own data has loaded successfully.
const readyPanels = new Set<string>();
function setPanelReady(name: string, ready: boolean): void {
  if (ready) readyPanels.add(name); else readyPanels.delete(name);
  document.querySelectorAll<HTMLButtonElement>(`[data-requires="${name}"]`).forEach(control => {
    control.disabled = !ready;
    control.title = ready ? "" : "Waiting for this panel to load successfully";
  });
}
function requirePanelReady(name: string): boolean {
  if (readyPanels.has(name)) return true;
  setActionStatus("This panel is still loading or could not load. Retry before making changes.", "error");
  return false;
}

const editorLoads = new Map<string, Promise<unknown>>();
function loadEditorPanel<T>(name: string, task: () => Promise<T>): Promise<T> {
  if (editorLoads.has(name)) return editorLoads.get(name) as Promise<T>;
  setPanelReady(name, false);
  const status = document.getElementById(`${name}-load-status`);
  if (status) status.innerHTML = '<div class="section-loading" role="status"><span class="section-spinner" aria-hidden="true"></span>Loading...</div>';
  const pending = (async () => {
    try {
      const result = await task();
      setPanelReady(name, true);
      if (status) status.replaceChildren();
      return result;
    } catch (error) {
      setPanelReady(name, false);
      if (status) {
        status.replaceChildren();
        status.textContent = 'Could not load this panel. Your saved settings have not been changed. ';
        const retry = document.createElement('button');
        retry.type = 'button';
        retry.textContent = 'Retry';
        retry.onclick = () => loadEditorPanel(name, task).catch(() => {});
        status.appendChild(retry);
      }
      throw error;
    } finally {
      editorLoads.delete(name);
    }
  })();
  editorLoads.set(name, pending);
  return pending;
}

// Last-resort guard: an unexpected failure (e.g. network loss mid-save) is reported
// instead of silently leaving an action looking stuck. Dialogs stay closable.
globalThis.addEventListener?.("unhandledrejection", (event) => {
  const message = event.reason?.name === "TimeoutError"
    ? "The server took too long to respond. Please retry."
    : event.reason instanceof TypeError
      ? "Could not reach the server. Check the connection and retry."
      : (event.reason?.message || "Something went wrong. Please retry.");
  setActionStatus(message, "error");
  event.preventDefault();
});
// New content fades in instead of snapping into place. Keyed items (directories, jobs, edges,
// instances) that were already on screen are left alone when re-rendered, so polling never flickers.
const LOADING_PLACEHOLDER = ".section-loading, .loading-placeholder";
const FADE_KEY_ATTRS = ["data-path", "data-edge-id", "data-instance-id"];
const FADE_KEYED = FADE_KEY_ATTRS.map((attr) => `[${attr}]`).join(", ");

function prefersReducedMotion(): boolean {
  return Boolean(globalThis.matchMedia?.("(prefers-reduced-motion: reduce)").matches);
}

function fadeKey(element: Element): string {
  // Only keyed elements reach here, so one of the key attributes is always present.
  const attr = FADE_KEY_ATTRS.find((name) => element.hasAttribute(name))!;
  return `${element.classList[0] || element.tagName}|${attr}=${element.getAttribute(attr)}`;
}

function keyedElements(node: Element): Element[] {
  const nested = Array.from(node.querySelectorAll(FADE_KEYED));
  return node.matches(FADE_KEYED) ? [node, ...nested] : nested;
}

function fadeInNewContent(mutations: MutationRecord[]): void {
  if (prefersReducedMotion()) return;
  const shownKeys = new Set<string>();
  const placeholderTargets = new Set<Element>();
  for (const { target, removedNodes } of mutations) {
    for (const node of removedNodes) {
      if (node.nodeType !== 1) continue;
      const element = node as Element;
      keyedElements(element).forEach((keyed) => shownKeys.add(fadeKey(keyed)));
      if (element.matches(LOADING_PLACEHOLDER)) placeholderTargets.add(target as Element);
    }
  }
  const faded: Element[] = [];
  const fade = (element: Element) => {
    if (!element.animate || !element.isConnected || faded.some((done) => done.contains(element))) return;
    faded.push(element);
    element.animate([{ opacity: 0 }, { opacity: 1 }], { duration: 260, easing: "ease-out" });
  };
  // A section replacing its loading placeholder fades as a whole.
  placeholderTargets.forEach((target) => {
    if (!target.querySelector(LOADING_PLACEHOLDER)) fade(target);
  });
  for (const { addedNodes, removedNodes } of mutations) {
    const replacedElements = Array.from(removedNodes).some((node) => node.nodeType === 1);
    for (const node of addedNodes) {
      if (node.nodeType !== 1) continue;
      const element = node as Element;
      if (element.matches(LOADING_PLACEHOLDER) || element.closest(".toast-region")) continue;
      const keyed = keyedElements(element);
      if (keyed.length) {
        keyed.filter((item) => !shownKeys.has(fadeKey(item))).forEach(fade);
      } else if (!replacedElements) {
        // Unkeyed markup swapped for other markup is a refresh; only pure additions are new.
        fade(element);
      }
    }
  }
}
if (globalThis.MutationObserver && globalThis.document?.body) {
  new MutationObserver(fadeInNewContent).observe(document.body, { childList: true, subtree: true });
}
