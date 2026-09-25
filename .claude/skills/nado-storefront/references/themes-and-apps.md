# Темы витрины и гибридные приложения магазинов

Справка собрана 2026-09-25. Правила сторов меняются: перед подачей сверяйся с первоисточником.
Пометка «⚠️ не проверено» означает, что факт не подтверждён по первоисточнику.

## 1. App Store: правила, которые касаются нас

Источник: https://developer.apple.com/app-store/review/guidelines/ (цитаты сняты 2026-09-25).

- **4.2 Minimum Functionality.** «Your app should include features, content, and UI that elevate it beyond a
  repackaged website… not particularly useful, unique, or "app-like"» — отказ. **4.2.2**: кроме каталогов,
  приложение не должно быть «web clippings, content aggregators, or a collection of links». Каталог товаров
  прямо назван исключением, но обёртка сайта без нативной ценности всё равно попадает под 4.2.
- **4.2.6 Template and App Generation Services** (действует, текст дословно): «Apps created from a commercialized
  template or app generation service will be rejected unless they are submitted directly by the provider of the
  app's content. These services should not submit apps on behalf of their clients and should offer tools that let
  their clients create customized, innovative apps… Another acceptable option for template providers is to create a
  single binary to host all client content in an aggregated or "picker" model».
  Для nado это значит: приложение продавца подаётся **из аккаунта разработчика продавца**, а не из аккаунта nado.
  Второй разрешённый путь — одно приложение nado с выбором магазина.
  Практика рынка (Tapcart и конструкторы для Shopify): продавец сам покупает Apple Developer ($99/год) и Google Play
  ($25), а платформа собирает и загружает сборки (https://www.tapcart.com/pricing). Даже так бывают отказы по
  4.2.6 и 4.3: однотипные бинарники одного шаблона ревьюеры узнают (https://sellmycode.co/blogs/apple-guideline-4-3-white-label-apps/).
  ⚠️ не проверено: как именно Apple определяет, что приложение сделано «по шаблону».
- **4.3 Spam.** (a) не плодить Bundle ID одного и того же приложения (пример Apple — отдельная карта для каждого
  города); (b) не подавать приложения, неотличимые от уже существующих. Для варианта A (см. §5) это главный риск:
  десятки почти одинаковых приложений. Снижают риск свой контент, свой бренд и разный набор функций.
- **5.1.1(v)** — «If your app supports account creation, you must also offer account deletion within the app».
  Регистрация покупателей обязательна (D-17), поэтому в приложении обязательна кнопка «Удалить аккаунт», которая
  реально удаляет данные, а не «напишите в поддержку». Apple также требует не заставлять входить без нужды: каталог,
  карточки и корзина доступны без входа, вход по телефону запрашивается только на оформлении заказа. Так и устроена
  витрина (SKILL.md → «Корзина и оформление»).
- **4.8 Login Services.** Требование срабатывает только при входе через сторонний/социальный сервис (Google, VK ID,
  Яндекс ID и т. п.): тогда нужен ещё один равноценный вход, который собирает только имя и email, позволяет скрыть
  email и не отслеживает для рекламы. На практике это Sign in with Apple. Исключение: «Your app exclusively uses your
  company's own account setup and sign-in systems». **Вход по телефону + OTP или email + код — собственная система,
  Sign in with Apple не нужен.** Доставка кода через WhatsApp или Telegram этого не меняет: учётка всё равно наша,
  а не «Login with Telegram» ⚠️ это толкование, не цитата Apple. Если используем виджет «Войти через Telegram»,
  это уже сторонний вход, и нужен Apple. Добавили Google/Яндекс-вход — добавляй и Apple.
  Для ревью дай тестовый номер с фиксированным кодом.
- **3.1.3(e)** — физические товары и услуги, потребляемые вне приложения, **обязаны** оплачиваться не через IAP
  («Apple Pay or traditional credit card entry»). Наши заказы — физические товары: Apple комиссию не берёт, провайдеры
  продавца (Halyk ePay, Freedom, Kaspi, ЮKassa) допустимы. 3.1.5 касается криптовалют и к нам не относится.
  Нельзя продавать через приложение цифровое (подписку nado, подарочные e-коды без физического товара ⚠️ спорно).
- **Прочее для ревью.** Приватность: App Privacy labels и `PrivacyInfo.xcprivacy` для «required reason API».
  **ATT**: если в приложении работают пиксели и метрики продавца (Meta Pixel, Яндекс Метрика с рекламными целями),
  это трекинг, нужен запрос ATT. Проще отключать сторонние скрипты продавца в контексте приложения.
  Также: `NS*UsageDescription` на каждую запрошенную разрешением функцию и демо-доступ в App Review Notes.

**Что обычно помогает пройти 4.2** (опыт рынка, не формальный список Apple ⚠️):
push-уведомления о статусе заказа и акциях; нативный tab bar или хотя бы нативная навигация «назад» и жесты;
свой офлайн-экран вместо белой страницы; universal links (ссылка из письма или SMS открывает приложение);
нативный share sheet; сканер штрихкода для поиска; избранное и корзина, которые сохраняются между запусками;
бейдж на иконке; haptics; запрос оценки (SKStoreReviewController); вход по Face ID для учётки покупателя (опционально).
Минимум для первой подачи: push, офлайн-экран, universal links, share и нативный tab bar.

## 2. Google Play

- **Spam → Webview and Affiliate Spam**: «We don't allow apps whose primary purpose is to… provide a webview of a
  website without permission». Если сайт принадлежит продавцу и приложение публикует он сам, разрешение очевидно.
  При публикации от nado нужна явная оферта-разрешение продавца.
  **Repetitive Content**: «apps that merely provide the same experience as other apps already on Google Play».
  https://support.google.com/googleplay/android-developer/answer/9899034
- **Minimum Functionality**: запрещены «static without app-specific functionalities» и «very little content».
  https://support.google.com/googleplay/android-developer/answer/9898783
- **Удаление аккаунта** (User Data policy): если в приложении можно создать учётку, нужно удаление и в приложении,
  и по **веб-ссылке**, которая указывается в анкете Data safety. Без неё нельзя выпускать обновления.
  https://support.google.com/googleplay/android-developer/answer/13327111
- **Target API**: с 31.08.2026 новые приложения и обновления должны таргетить **Android 16 (API 36)**. Уже
  опубликованным нужен минимум API 35, иначе их не видят новые пользователи. Продление можно запросить до 01.11.2026.
  https://support.google.com/googleplay/android-developer/answer/11926878
  Следствие: с targetSdk 35+ включён принудительный edge-to-edge, поэтому safe-area в CSS обязательна (§4).
- **Аккаунты разработчика.** Организации нужен **D-U-N-S** (получение до 30 дней), государственные органы
  исключены (https://support.google.com/googleplay/android-developer/answer/13628312). **Личные аккаунты**, созданные
  после 13.11.2023, перед выходом в прод проходят закрытое тестирование: **12 тестировщиков 14 дней подряд**.
  Аккаунты организаций от этого освобождены (https://support.google.com/googleplay/android-developer/answer/14151465).
  Для продавцов-ИП это серьёзное трение, поэтому рекомендуем организационный аккаунт (ТОО/ИП с D-U-N-S ⚠️ выдают
  ли D-U-N-S казахстанским ИП — не проверено).
- **Android developer verification.** Верификация личности обязательна для всех приложений на сертифицированных
  устройствах. Приложения из Google Play регистрируются автоматически (99 %). С 30.09.2026 неверифицированные APK
  блокируются в Бразилии, Индонезии, Сингапуре и Таиланде, глобально — с 2027 года.
  https://developer.android.com/developer-verification ,
  https://android-developers.googleblog.com/2026/03/android-developer-verification.html
  Для нас это важно, только если раздавать APK мимо Play: тогда нужна регистрация package name и ключа подписи.
- **РФ.** Оплатить взносы Apple и Google с российских карт обычно нельзя, для продавцов из РФ нужен иностранный
  юрлицо-владелец аккаунта или альтернатива (RuStore) ⚠️ не проверено по первоисточнику
  (https://media.friflex.com/development/kak-sozdat-akkaunt-razrabotchika). Список стран, где можно регистрировать
  аккаунт: https://support.google.com/googleplay/android-developer/answer/9306917

## 3. Техника гибридного приложения

**Оболочка: Capacitor (текущая мажорная версия — v8).** Варианты:
1. `server.url = "https://<домен магазина>"` — WebView грузит живую витрину. Капризная деталь: в документации
   прямо сказано «intended for use with live-reload servers. **This is not intended for use in production.**».
   То же написано про `server.allowNavigation` (https://capacitorjs.com/docs/config). На практике так делают
   многие, но официальной поддержки нет: мост плагинов на удалённом origin и поведение при обновлениях — на наш риск
   ⚠️ не проверено, что мост инжектится в удалённые страницы во всех версиях.
2. **Встроенная оболочка (рекомендуется):** локальный `index.html` в бандле — сплэш, офлайн-экран, нативный tab bar
   (плагин или свой), а затем `window.location` на витрину. Внешние домены при этом открываются в системном браузере,
   пока их нет в `allowNavigation`. Тот же риск по origin, но есть офлайн-экран при первом запуске.
   `server.errorPath` для своей страницы ошибки ⚠️ проверь наличие в v8.
3. Своя тонкая нативная обёртка (Swift WKWebView + Kotlin WebView, ~500 строк на платформу). Полный контроль,
   без зависимости от «not for production». Разумно, если Capacitor начнёт мешать.
   Hotwire Native заточен под Turbo, с htmx напрямую не совместим.

**Поведение htmx и серверного рендера внутри WebView**
- Витрина — first-party origin для WebView, поэтому сессионные cookie (`SameSite=Lax`, `Secure`, `HttpOnly`)
  работают. ITP в WKWebView бьёт по третьим сторонам (iframe и виджеты других доменов). App-Bound Domains
  (`WKAppBoundDomains`, максимум 10 доменов) нужны только для service worker в WKWebView, а нам SW в приложении не
  нужен (https://webkit.org/blog/10882/app-bound-domains/).
- Возврат из банка: многие шлюзы возвращают покупателя **POST-ом** с чужого домена. Cookie с `SameSite=Lax` на
  кросс-сайтовом POST не отправляются, поэтому страница результата должна работать по `order token` в URL
  (как уже `/order/{number}?t=`), а не по сессии корзины.
- Оплата: открывать платёжную страницу во **внутреннем браузере** (`@capacitor/browser` → SFSafariViewController /
  Custom Tabs). `return_url` должен быть universal link витрины: приложение ловит `appUrlOpen`, вызывает
  `Browser.close()` и ведёт WebView на страницу заказа. Ссылки `kaspi.kz`/Kaspi Pay открывать внешне, чтобы
  сработало приложение Kaspi. Держать 3DS внутри WebView (`allowNavigation`) можно, но хрупко.
- `<input type=file>`: в WKWebView работает из коробки. На Android нужен `onShowFileChooser` (Capacitor его
  реализует). В Info.plist — `NSCameraUsageDescription` и `NSPhotoLibraryUsageDescription`, иначе будет краш или
  отказ ревью.
- `target=_blank`, `tel:`, `mailto:`, `whatsapp:` — перехватывать и открывать внешне.
  `hx-push-url` и история работают, но нативная кнопка «назад» на Android должна вызывать `history.back()`.
- **Детект приложения:** в `capacitor.config` задать `appendUserAgent: "NadoApp/<ver> (<ios|android>; store=<id>)"`.
  Сервер выставляет в контекст `InApp` и платформу. Кеши и CDN обязаны учитывать это: `Vary: User-Agent` или
  отдельный cookie `nado_app=1`, который оболочка ставит первым запросом `/?app=ios`. Надёжнее cookie + UA вместе.

**Universal Links / App Links на каждый домен магазина**
- iOS: entitlement `com.apple.developer.associated-domains` = `applinks:<домен>` зашит **в бинарник**. Смена домена
  магазина требует новой сборки. Файл `/.well-known/apple-app-site-association` (JSON без расширения, HTTPS, без
  редиректов) отдаёт nado по Host: `{"applinks":{"details":[{"appIDs":["<TEAMID>.<bundle>"],"components":[{"/":"/*"}]}]}}`.
  Apple забирает его через свой CDN, так что кеш обновляется не мгновенно ⚠️ сроки не проверены.
- Android: `/.well-known/assetlinks.json` с `package_name` и `sha256_cert_fingerprints` **ключа Play App Signing**
  (не upload key) и intent-filter `autoVerify="true"`.
- Оба файла — Go-хендлер в дереве витрины, данные из таблицы `store_apps` (bundle_id, team_id, package, sha256).

**Push-уведомления.** `@capacitor/push-notifications`: FCM на Android, APNs на iOS. С сервера (Go) проще слать всё
через FCM HTTP v1 (`firebase.google.com/go/v4/messaging`), загрузив в Firebase APNs-ключ `.p8` команды Apple.
Ключ APNs принадлежит **команде владельца аккаунта**: при варианте A продавец создаёт ключ и передаёт его nado.
Firebase: одно приложение Android/iOS на каждый магазин, проекты можно группировать ⚠️ лимит приложений на проект
не проверен. Токены хранить в `device_tokens(store_id, customer_id NULL, platform, token, updated_at)`.

**Иконки и сплэш.** `npx @capacitor/assets generate` из `icon.png` 1024×1024 без прозрачности (iOS) и
`splash.png` 2732×2732 плюс цвета фона. Исходники — из настроек магазина (логотип и основной цвет темы).

**Сборка множества white-label приложений в CI**
- Один репозиторий оболочки. На каждый магазин — `app.json`: appId/bundleId (`kz.nado.s<id>` или домен продавца),
  имя, домен, цвета, версия. Генератор пишет `capacitor.config.ts`, ассеты и `Info.plist`/`AndroidManifest` плейсхолдеры.
- iOS собирается только на macOS-раннере (GitHub Actions macOS, Codemagic, свой Mac mini). fastlane: `match`
  хранит сертификаты и профили по командам в зашифрованном хранилище, `gym` собирает, `deliver`/`pilot` загружает.
  Авторизация — **App Store Connect API key** (.p8, роль App Manager/Admin), который продавец выпускает в своей команде.
- **Запись приложения в App Store Connect через официальный REST API не создать.** Есть web UI или `fastlane produce`
  (веб-сессия, 2FA владельца), поэтому первый шаг для каждого магазина полуручной
  (https://github.com/andrewralon/app-template/issues/2).
- Android: сборка AAB через `gradle bundleRelease`. Upload key — один на приложение, в секретах. Play App Signing
  включён. `fastlane supply` использует сервисный аккаунт с доступом к аккаунту продавца. Первое приложение и его
  первый релиз создаются в Play Console вручную ⚠️ не проверено для 2026.
- Секреты продавцов (ключи ASC, APNs, JSON сервисного аккаунта) хранятся шифрованными, как секреты провайдеров
  (см. nado-go-conventions).

**Кто владеет аккаунтами.** По 4.2.6 для отдельных приложений — продавец. nado получает роль в его команде: в Apple
«Users and Access» — Admin (нужен для сертификатов) или App Manager ⚠️ точные права ролей на сертификаты не
проверены; в Google — приглашение пользователя с правами релиза. Следствия: продавец платит взносы и проходит
верификацию (D-U-N-S, до 30 дней), ушёл с nado — приложение остаётся у него, но без сервера витрины оно бесполезно.
Нужен пункт в оферте о передаче и отзыве доступов.

## 4. Требования к темам nado («app-ready»)

**Вёрстка**
- Mobile-first: базовые стили для 360 px, расширение через `@media (min-width: …)`. Внутри карточек и сеток —
  container queries (`container-type: inline-size`, `@container`), чтобы partial работал в любой колонке.
  Флюидная типографика и отступы: `font-size: clamp(1rem, 0.9rem + 0.5vw, 1.125rem)`.
- `<meta name="viewport" content="width=device-width, initial-scale=1, viewport-fit=cover">`. Фиксированные шапка и
  нижняя навигация получают `padding-top: env(safe-area-inset-top)` и
  `padding-bottom: calc(env(safe-area-inset-bottom) + .5rem)`. Высота экрана — `100dvh`, не `100vh`.
- Никаких hover-only взаимодействий: hover-эффекты только в `@media (hover:hover) and (pointer:fine)`, меню и
  подсказки открываются по tap/click. Зоны касания ≥ 44×44 pt (Apple HIG) и 48 dp (Material).
- На мобильном — **нижняя навигация** (Главная, Каталог, Поиск, Корзина с бейджем, Профиль/Заказы). В приложении
  её заменяет нативный tab bar, HTML-версия скрыта (`{{if not .InApp}}`).
- Поля ввода — `font-size ≥ 16px` (иначе iOS зумит). Атрибуты `inputmode`, `autocomplete="tel"`/`"one-time-code"`,
  `enterkeyhint`.
- **Режим приложения** (`.InApp`): скрыть баннер «скачайте приложение», ссылки на сторы, веб-футер с юридическим
  мусором (ссылки оставить в «Профиль → О магазине»), cookie-баннер сторонних метрик (метрики выключены).
  Добавить `overscroll-behavior: none` и `-webkit-tap-highlight-color: transparent`. Кнопка «Поделиться» вызывает
  нативный Share через мост.
- Светлая и тёмная тема обязательны (D-21): `prefers-color-scheme`, `<meta name="color-scheme" content="light dark">`
  и `theme-color` с `media`. Продавец выбирает только режим по умолчанию, переключатель у покупателя остаётся всегда.
  Все цвета — только через токены. Подробно — скилл **nado-ui-design**.
- Доступность — WCAG 2.2 AA: контраст ≥ 4.5:1. Цвета продавца проверяем при сохранении и предупреждаем.
  Видимый `:focus-visible`, `alt` из названия товара, семантические `<nav>/<main>/<button>`, `prefers-reduced-motion`.

**Производительность** (бюджет на p75 мобильных, Core Web Vitals): LCP ≤ 2.5 с, INP ≤ 200 мс, CLS ≤ 0.1.
Проверять в Lighthouse на «Slow 4G» и отдельно на эмуляции 3G (цель там — LCP ≤ 4 с).
- HTML первой страницы ≤ 50 КБ gzip, критический CSS ≤ 30 КБ, JS ≤ 30 КБ (htmx ≈ 16 КБ min+gz ⚠️) без фреймворков.
- Изображения: `<picture>` с AVIF → WebP → JPEG, `srcset` 320/480/768/1080/1440 и `sizes`, всегда `width`/`height`
  (против CLS). LCP-картинке — `fetchpriority="high"` без lazy, остальным — `loading="lazy" decoding="async"`.
  Ресайз и конвертация — фоновая задача при импорте фото.
- Шрифты: системный стек по умолчанию или один `woff2` с `font-display: swap`. Подмножество обязательно включает
  **кириллицу и казахские буквы** (ә ғ қ ң ө ұ ү һ і), иначе будет fallback посреди слова.

**PWA как базовый уровень** (нужен и для варианта C): `/manifest.webmanifest` по Host (name, icons 192/512 +
maskable, `theme_color`, `display: standalone`, `start_url: /?source=pwa`). Service worker кеширует статику
(cache-first по версионному имени) и офлайн-страницу. HTML не кешируем: цены и остатки должны быть живыми.
Web Push в iOS работает только для установленных на домашний экран веб-приложений (iOS 16.4+) ⚠️ актуальность
ограничений 2026 не проверена.

**Архитектура тем на Go-шаблонах**
```
web/themes/
  base/     theme.json  layouts/ partials/ pages/ static/   ← полный набор, все блоки через {{block}}
  aurora/   theme.json  partials/header.gohtml  static/aurora.css   ← "extends": "base", только отличия
```
- `theme.json`: `id`, `name`, `version`, `extends`, `settings` — схема полей настроек (`color` primary/accent/bg/text,
  `font` из белого списка, `image` logo/favicon/banner[], `select` layout_home, `bool` show_bottom_nav) с default.
  В кабинете форма строится по схеме, значения хранятся в `store_theme_settings` (JSON) и валидируются по той же
  схеме.
- Сборка: при старте для каждой темы клонировать скомпилированный набор `base` (`tmpl.Clone()`) и `ParseFS` поверх
  файлы темы. `{{define "header"}}` из темы переопределяет блок базы: html/template разрешает переопределение до
  первого Execute. Цепочку `extends` разворачивать рекурсивно, наборы кешировать (`embed.FS` в проде, диск в dev).
  Отсутствующий в теме partial берётся из базы.
- Настройки превращаются в `<style nonce>:root{--c-primary:#1a73e8;--radius:12px;--font-body:…}</style>`.
  Значения **только после валидации** (`^#[0-9a-f]{6}$`, числа в диапазоне, шрифт из списка), иначе будет CSS-инъекция.
  CSS тем использует только `var(--…)`.
- Контракт данных страниц (`.Store`, `.Products`, `.InApp`, `.Lang`, `T`, `Money`) общий для всех тем. Тема не
  ходит в БД и не вызывает сервисы, только рендерит. Каждая тема прогоняется одним набором golden-тестов по страницам.
- Для старта: `base` (нейтральная, одна колонка на мобильном, сетка 2/3/4 колонки) и одна «витринная» тема с
  крупными баннерами.

## 5. Как публиковать приложения: варианты

| | A. Аккаунты продавцов, сборки делает nado | B. Одно приложение «nado» с выбором магазина | C. Сначала только PWA |
|---|---|---|---|
| Соответствие 4.2.6 | Формально да (подаёт владелец контента) | Прямо разрешено («picker model») | Ревью нет |
| Риск отказа | Средний–высокий: 4.2, 4.2.6, 4.3 (однотипные бинарники) | Низкий–средний: 4.2 (нужна ценность — поиск по магазинам, единая корзина/заказы) | Нет |
| Бренд продавца в сторе | Да | Нет (магазин внутри nado) | Иконка на экране, без стора |
| Нагрузка на продавца | D-U-N-S, $99/год + $25, 2FA, ключи; ИП с личным Google — 12 тестеров/14 дней | Нулевая | Нулевая |
| Нагрузка на nado | macOS CI, N подписей и N ревью на каждый релиз, поддержка | 1 аккаунт (ТОО nado с D-U-N-S), 1 пайплайн | Минимальная |
| Push | Да (ключи продавца) | Да (по подписке на магазин) | Android — да; iOS — только после установки PWA |
| Продавцы из РФ | Проблемы с оплатой взносов, RuStore как альтернатива ⚠️ | Решает nado один раз | Без ограничений |

**Рекомендация.**
1. Сейчас: **C**. Все темы делаем app-ready по §4 (safe-area, `.InApp`, нижняя навигация, manifest, SW). Это ничего
   не стоит, если заложено сразу.
2. Затем **пилот A** как платный тариф на 2–3 лояльных продавцах с организационными аккаунтами. В первой же версии
   обязательны нативные функции: push о заказах, universal links, офлайн, share и нативный tab bar. В App Review
   Notes описываем уникальный бренд и каталог продавца и указываем, что аккаунт принадлежит ему.
3. **B** держим как запасной путь на случай систематических отказов по 4.2.6/4.3, а также как точку роста в
   маркетплейс nado. Модель данных сразу допускает, что одно приложение обслуживает много `store_id`:
   `device_tokens.store_id` и подписки.
4. Не публиковать приложения продавцов из аккаунта nado: это прямое нарушение 4.2.6 и «webview without permission»
   в Google Play.
