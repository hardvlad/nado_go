// Переключатель светлой/тёмной темы (D-21, скилл nado-ui-design).
//
// Сервер уже отрендерил <html data-theme> по cookie, поэтому страница не мигает.
// Скрипт только меняет режим по кнопке: system → light → dark → system.
(function () {
    "use strict";

    var MODES = ["system", "light", "dark"];
    var root = document.documentElement;

    function currentMode() {
        var m = document.cookie.match(/(?:^|;\s*)nado_theme=(system|light|dark)/);
        return m ? m[1] : "system";
    }

    function saveMode(mode) {
        var secure = location.protocol === "https:" ? "; Secure" : "";
        document.cookie = "nado_theme=" + mode + "; Path=/; Max-Age=31536000; SameSite=Lax" + secure;
    }

    // Цвет строки состояния браузера. Оба цвета сервер кладёт в data-атрибуты
    // мета-тегов: в режиме «как в системе» каждый тег показывает цвет своей
    // media-темы, при явном выборе оба показывают цвет выбранной.
    function updateThemeColor(mode) {
        document.querySelectorAll('meta[name="theme-color"]').forEach(function (m) {
            var own = (m.getAttribute("media") || "").indexOf("dark") >= 0 ? "dark" : "light";
            var use = mode === "system" ? own : mode;
            m.setAttribute("content", m.getAttribute("data-" + use));
        });
    }

    function apply(mode) {
        if (mode === "system") {
            root.removeAttribute("data-theme");
        } else {
            root.setAttribute("data-theme", mode);
        }
        document.querySelectorAll("[data-theme-toggle]").forEach(function (btn) {
            btn.setAttribute("data-mode", mode);
            var label = btn.getAttribute("data-label-" + mode);
            if (label) {
                btn.setAttribute("aria-label", label);
                btn.setAttribute("title", label);
            }
        });
        updateThemeColor(mode);
        document.dispatchEvent(new CustomEvent("nado:theme-changed", { detail: { mode: mode } }));
    }

    document.addEventListener("click", function (e) {
        var btn = e.target.closest("[data-theme-toggle]");
        if (!btn) {
            return;
        }
        var next = MODES[(MODES.indexOf(currentMode()) + 1) % MODES.length];
        saveMode(next);
        apply(next);
    });
})();
