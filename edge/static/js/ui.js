const TOAST_DURATION_MS = 8000;
let _appDialogResolve = null;

function normalizeTheme(theme) {
  return theme === "light" ? "light" : "dark";
}

function applyTheme(theme) {
  const resolved = normalizeTheme(theme);
  document.documentElement.dataset.theme = resolved;
  const setting = document.getElementById("settings_theme_dark");
  if (setting) {
    const on = resolved === "dark";
    setting.setAttribute("aria-checked", on ? "true" : "false");
    setting.classList.toggle("toggle-on", on);
  }
}

function showToast(message, kind = "info", { duration = TOAST_DURATION_MS, title = "" } = {}) {
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

function setActionStatus(message, kind = "info") {
  showToast(message, kind);
}

function setStatus(id, message, kind = "info") {
  const text = formatMessage(message);
  if (text) showToast(text, kind);
}

function clearStatus(id) {}

function initializeFieldHelp(helpEntries) {
  Object.entries(helpEntries).forEach(([id, helpText]) => {
    const label = document.querySelector(`label[for="${id}"]`);
    if (!label || label.querySelector(".field-help")) {
      return;
    }
    label.insertAdjacentHTML("beforeend", ` <span class="field-help hover-hint" tabindex="0" aria-label="${escapeHtml(helpText)}" title="${escapeHtml(helpText)}">?</span>`);
  });
}

function openDialog(id) {
  const dialog = document.getElementById(id);
  if (!dialog?.showModal) return;
  if (dialog.open) return;
  dialog.showModal();
  const region = document.getElementById("toast-region");
  if (region?.showPopover && region.matches(":popover-open")) {
    region.hidePopover();
    region.showPopover();
  }
}

function closeDialog(id) {
  const dialog = document.getElementById(id);
  if (dialog?.open) {
    dialog.close();
  }
}

function appDialog({ title, message, input = false, inputLabel = "", inputType = "text", confirmLabel = "Continue", danger = false } = {}) {
  const dialog = document.getElementById("app-dialog");
  if (!dialog?.showModal) {
    return Promise.resolve(input ? null : false);
  }
  if (_appDialogResolve) {
    resolveAppDialog(false);
  }

  document.getElementById("app-dialog-title").textContent = title || "Confirm";
  document.getElementById("app-dialog-message").textContent = message || "";
  const inputWrap = document.getElementById("app-dialog-input-wrap");
  const inputElement = document.getElementById("app-dialog-input");
  document.getElementById("app-dialog-input-label").textContent = inputLabel || "";
  inputWrap.hidden = !input;
  inputElement.type = inputType;
  inputElement.value = "";
  const confirmButton = document.getElementById("app-dialog-confirm");
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

function resolveAppDialog(confirmed) {
  if (_appDialogResolve) {
    _appDialogResolve(confirmed);
  }
}

function confirmApp(options) {
  return appDialog(options).then(Boolean);
}

// Editing controls remain locked until their own data has loaded successfully.
const readyPanels = new Set();
function setPanelReady(name, ready) {
  if (ready) readyPanels.add(name); else readyPanels.delete(name);
  document.querySelectorAll(`[data-requires="${name}"]`).forEach(control => {
    control.disabled = !ready;
    control.title = ready ? "" : "Waiting for this panel to load successfully";
  });
}
function requirePanelReady(name) {
  if (readyPanels.has(name)) return true;
  setActionStatus("This panel is still loading or could not load. Retry before making changes.", "error");
  return false;
}

const editorLoads = new Map();
function loadEditorPanel(name, task) {
  if (editorLoads.has(name)) return editorLoads.get(name);
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
// Content that replaces a loading placeholder fades in instead of snapping into place.
// Routine refreshes of already-loaded content are left alone so polling never flickers.
const LOADING_PLACEHOLDER = ".section-loading, .loading-placeholder";
function fadeInLoadedContent(mutations) {
  const faded = new Set();
  for (const { target, removedNodes } of mutations) {
    if (faded.has(target) || !target.animate) continue;
    const replacedPlaceholder = Array.from(removedNodes).some((node) => node.nodeType === 1 && node.matches(LOADING_PLACEHOLDER));
    if (!replacedPlaceholder || target.querySelector(LOADING_PLACEHOLDER)) continue;
    faded.add(target);
    target.animate([{ opacity: 0 }, { opacity: 1 }], { duration: 260, easing: "ease-out" });
  }
}
if (globalThis.MutationObserver && globalThis.document?.body) {
  new MutationObserver(fadeInLoadedContent).observe(document.body, { childList: true, subtree: true });
}