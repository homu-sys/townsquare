(function () {
  document.addEventListener("click", function (event) {
    var button = event.target.closest("[data-emoji]");
    if (!button) {
      return;
    }
    var form = button.closest("form");
    var textarea = form ? form.querySelector("textarea") : null;
    if (!textarea) {
      return;
    }
    var emoji = button.getAttribute("data-emoji");
    var start = textarea.selectionStart;
    var end = textarea.selectionEnd;
    textarea.value = textarea.value.slice(0, start) + emoji + textarea.value.slice(end);
    textarea.selectionStart = textarea.selectionEnd = start + emoji.length;
    textarea.focus();
    var picker = button.closest("details");
    if (picker) {
      picker.open = false;
    }
    event.preventDefault();
  });
})();
