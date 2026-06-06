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
    setting.checked = resolved === "dark";
  }
}

function showToast(message, kind = "info", { duration = TOAST_DURATION_MS, title = "" } = {}) {
  const text = formatMessage(message);
  if (!text) return;
  const region = document.getElementById("toast-region");
  if (!region) return;

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
  const element = document.getElementById(id);
  if (!element) return;
  const text = formatMessage(message);
  element.textContent = text;
  if (text) {
    element.dataset.kind = kind;
  } else {
    delete element.dataset.kind;
  }
}

function clearStatus(id) {
  setStatus(id, "", "info");
}

function openDialog(id) {
  const dialog = document.getElementById(id);
  if (!dialog?.showModal || dialog.open) return;
  dialog.showModal();
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
