// Запуск виджета Halyk ePay без inline-скрипта (CSP script-src 'self' + домен ePay).
// Конфиг лежит в data-config элемента #halyk-pay; библиотека payment-api.js
// подключается отдельным <script src> и определяет window.halyk.
(function () {
  var el = document.getElementById('halyk-pay');
  if (!el || !window.halyk || typeof window.halyk.pay !== 'function') return;
  var cfg;
  try {
    cfg = JSON.parse(el.getAttribute('data-config'));
  } catch (e) {
    return;
  }
  window.halyk.pay(cfg);
})();
