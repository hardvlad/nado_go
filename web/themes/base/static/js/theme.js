// Переключение светлой/тёмной темы витрины без перезагрузки. Выбор хранится в
// cookie nado_theme и применяется сервером при рендере (без мигания).
(function () {
  "use strict";
  var order = ["system", "light", "dark"];

  function readCookie(name) {
    return document.cookie.split("; ").reduce(function (acc, part) {
      var kv = part.split("=");
      return kv[0] === name ? decodeURIComponent(kv[1] || "") : acc;
    }, "");
  }

  function apply(mode) {
    var root = document.documentElement;
    if (mode === "light" || mode === "dark") {
      root.setAttribute("data-theme", mode);
    } else {
      root.removeAttribute("data-theme");
    }
  }

  function save(mode) {
    var year = 60 * 60 * 24 * 365;
    document.cookie = "nado_theme=" + mode + "; path=/; max-age=" + year + "; samesite=lax";
  }

  document.addEventListener("click", function (e) {
    var btn = e.target.closest("[data-theme-toggle]");
    if (!btn) return;
    e.preventDefault();
    var current = readCookie("nado_theme") || "system";
    var next = order[(order.indexOf(current) + 1) % order.length];
    apply(next);
    save(next);
    btn.setAttribute("data-theme-mode", next);
  });
})();
