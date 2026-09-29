// Автообновление количества в корзине без кнопки «Обновить» (CSP script-src 'self').
// При изменении количества форма отправляется сама → сервер пересчитывает корзину
// и возвращает обновлённую страницу.
(function () {
  var forms = document.querySelectorAll('form[data-cart-line]');
  for (var i = 0; i < forms.length; i++) {
    (function (form) {
      var qty = form.querySelector('[data-cart-qty]');
      if (!qty) return;
      var last = qty.value;
      qty.addEventListener('change', function () {
        if (qty.value === '' || qty.value === last) return;
        last = qty.value;
        if (typeof form.requestSubmit === 'function') {
          form.requestSubmit();
        } else {
          form.submit();
        }
      });
    })(forms[i]);
  }
})();
