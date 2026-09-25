// Мелкие улучшения интерфейса. Без скрипта всё продолжает работать:
// меню и выпадающие списки — это <details>, формы — обычные POST.
(function () {
    "use strict";

    function closeAll(except) {
        document.querySelectorAll("details[data-dropdown][open]").forEach(function (d) {
            if (d !== except) {
                d.removeAttribute("open");
            }
        });
    }

    // Открытие одного выпадающего меню закрывает остальные.
    document.addEventListener("toggle", function (e) {
        var d = e.target;
        if (d.matches && d.matches("details[data-dropdown]") && d.open) {
            closeAll(d);
        }
    }, true);

    // Клик мимо меню или по ссылке внутри него — закрыть.
    document.addEventListener("click", function (e) {
        var inside = e.target.closest("details[data-dropdown]");
        if (!inside) {
            closeAll(null);
            return;
        }
        if (e.target.closest("a")) {
            inside.removeAttribute("open");
        }
    });

    document.addEventListener("keydown", function (e) {
        if (e.key === "Escape") {
            closeAll(null);
        }
    });

    // Регистрация: сводка справа показывает тариф, выбранный в форме.
    document.addEventListener("change", function (e) {
        if (!e.target.matches('input[type="radio"][name="plan"]')) {
            return;
        }
        document.querySelectorAll("[data-plan-summary]").forEach(function (card) {
            card.hidden = card.getAttribute("data-plan-summary") !== e.target.value;
        });
    });

    // Защита от двойной отправки формы: кнопка блокируется после первого нажатия.
    document.addEventListener("submit", function (e) {
        var btn = e.target.querySelector("button[type=submit]");
        if (btn) {
            window.setTimeout(function () { btn.disabled = true; }, 0);
        }
    });
})();
