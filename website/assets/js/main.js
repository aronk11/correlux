/*
 * Correlux website — small, dependency-free interaction layer.
 *
 * Two things happen here: the install command can be copied, and the fleet
 * panel's clock ticks. Neither is required for the page to be usable — both
 * degrade to plain, correct, static markup if JavaScript never runs.
 */
(function () {
  "use strict";

  /* Copy-to-clipboard for the install command. */
  document.querySelectorAll("[data-copy-target]").forEach(function (button) {
    var targetId = button.getAttribute("data-copy-target");
    var target = document.getElementById(targetId);
    if (!target) return;

    button.addEventListener("click", function () {
      var text = target.textContent.trim();
      copyText(text).then(function (ok) {
        if (!ok) return;
        button.setAttribute("data-copied", "true");
        button.setAttribute("aria-label", "Copied");
        window.clearTimeout(button._resetTimer);
        button._resetTimer = window.setTimeout(function () {
          button.removeAttribute("data-copied");
          button.setAttribute("aria-label", "Copy install command");
        }, 1800);
      });
    });
  });

  function copyText(text) {
    if (navigator.clipboard && window.isSecureContext) {
      return navigator.clipboard.writeText(text).then(
        function () { return true; },
        function () { return fallbackCopy(text); }
      );
    }
    return Promise.resolve(fallbackCopy(text));
  }

  function fallbackCopy(text) {
    var el = document.createElement("textarea");
    el.value = text;
    el.setAttribute("readonly", "");
    el.style.position = "fixed";
    el.style.opacity = "0";
    document.body.appendChild(el);
    el.select();
    var ok = false;
    try {
      ok = document.execCommand("copy");
    } catch (e) {
      ok = false;
    }
    document.body.removeChild(el);
    return ok;
  }

  /* A plain digital clock in the fleet panel header. Text content changing
     once a second is not the kind of motion prefers-reduced-motion asks us
     to remove, so it keeps running either way — there is nothing to miss if
     it doesn't (the label falls back to "fleet state" with no clock). */
  var clock = document.querySelector("[data-clock]");
  if (clock) {
    var update = function () {
      var d = new Date();
      var pad = function (n) { return String(n).padStart(2, "0"); };
      clock.textContent =
        pad(d.getHours()) + ":" + pad(d.getMinutes()) + ":" + pad(d.getSeconds());
    };
    update();
    window.setInterval(update, 1000);
  }
})();
