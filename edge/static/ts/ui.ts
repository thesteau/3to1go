function openDialog(id: string): void {
  const dialog = document.getElementById(id) as HTMLDialogElement | null;
  if (!dialog?.showModal) return;
  if (dialog.open) return;
  dialog.showModal();
  const region = document.getElementById("toast-region");
  if (region?.showPopover && region.matches(":popover-open")) {
    region.hidePopover();
    region.showPopover();
  }
}

function appDialog(options: AppDialogOptions & { input: true }): Promise<string | null>;
function appDialog(options?: AppDialogOptions): Promise<string | boolean | null>;
function appDialog({
  title,
  message,
  input = false,
  inputLabel = "",
  inputType = "text",
  confirmLabel = "Continue",
  danger = false,
}: AppDialogOptions = {}): Promise<string | boolean | null> {
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
  inputElement.type = inputType;
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

function initializeFieldHelp(helpEntries: Record<string, string>): void {
  Object.entries(helpEntries).forEach(([id, helpText]) => {
    const label = document.querySelector(`label[for="${id}"]`);
    if (!label || label.querySelector(".field-help")) {
      return;
    }
    label.insertAdjacentHTML(
      "beforeend",
      ` <span class="field-help hover-hint" tabindex="0" aria-label="${escapeHtml(helpText)}" title="${escapeHtml(helpText)}">?</span>`,
    );
  });
}
