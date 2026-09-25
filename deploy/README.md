# Деплой на сервер Ubuntu + Plesk

Push в `master` → GitHub Actions: `go vet` и тесты → сборка статического
бинарника под Linux → загрузка по SSH в новый каталог релиза → переключение
симлинка `current` → перезапуск systemd-сервиса → проверка `/healthz`.
Если новая версия не поднялась, скрипт возвращает предыдущий релиз, а job падает.

```
Интернет ─► nginx (Plesk, 80/443, SSL) ─► 127.0.0.1:8080 ─► nado (systemd, пользователь nado)

/opt/nado/
  releases/20260924120000-923ca35/{nado, remote-deploy.sh}
  releases/...                      последние 5 релизов
  current -> releases/<активный>
/etc/nado/nado.env                  конфигурация и секреты (root:nado, 0640)
/etc/systemd/system/nado.service
/etc/sudoers.d/nado-deploy          deploy может только перезапускать nado
```

| Файл | Назначение |
|---|---|
| `.github/workflows/deploy.yml` | CI/CD-конвейер |
| `deploy/setup-server.sh` | одноразовая подготовка сервера (root) |
| `deploy/remote-deploy.sh` | активация релиза с откатом (запускает CI) |
| `deploy/systemd/nado.service` | unit systemd |
| `deploy/nado.env.example` | шаблон `/etc/nado/nado.env` |
| `deploy/plesk/nginx-directives.conf` | обратный прокси для Plesk |

## 1. Ключ для GitHub Actions (на рабочей машине)

```bash
ssh-keygen -t ed25519 -N "" -C "github-actions-nado" -f nado_deploy
# nado_deploy     — приватный, пойдёт в секрет DEPLOY_SSH_KEY
# nado_deploy.pub — публичный, передаётся setup-server.sh
```

## 2. Подготовка сервера

```bash
scp -r deploy root@SERVER:/root/nado-deploy
ssh root@SERVER
bash /root/nado-deploy/setup-server.sh "$(cat nado_deploy.pub)"   # или вставить строку ключа
nano /etc/nado/nado.env                                            # доступ к БД и т.д.
```

Схему БД накатить один раз: `sqlcmd ... -i migrations/0001_init.sql`
(миграции конвейер не выполняет).

## 3. Домен в Plesk

1. **Websites & Domains → Add Domain** (или Add Subdomain), тип — Website hosting.
2. **Hosting Settings**: PHP support — выключить; включить
   «Permanent SEO-safe 301 redirect from HTTP to HTTPS».
3. **SSL/TLS Certificates → Let's Encrypt** — выпустить сертификат.
4. **Apache & nginx Settings**:
   * снять **Proxy mode** (Apache не участвует);
   * снять **Smart static files processing** и **Serve static files directly by nginx** —
     иначе `/static/*.css` nginx будет искать в `httpdocs` и отдавать 404;
   * в **Additional nginx directives** вставить `deploy/plesk/nginx-directives.conf`;
   * OK.

## 4. Секреты GitHub

Settings → Secrets and variables → Actions → New repository secret:

| Секрет | Значение |
|---|---|
| `DEPLOY_HOST` | IP или имя сервера |
| `DEPLOY_PORT` | порт SSH, если не 22 |
| `DEPLOY_USER` | `deploy` |
| `DEPLOY_SSH_KEY` | содержимое `nado_deploy` целиком, с BEGIN/END |
| `DEPLOY_KNOWN_HOSTS` | вывод `ssh-keyscan -p 22 SERVER` (отпечатки сверить с выводом setup-server.sh) |

Затем push в `master` или Actions → deploy → Run workflow.

## Эксплуатация

```bash
systemctl status nado                  # состояние
journalctl -u nado -f                  # логи (JSON в prod)
curl -s 127.0.0.1:8080/healthz         # версия и аптайм
ls /opt/nado/releases                  # доступные релизы
```

Ручной откат на другой релиз:

```bash
sudo -u deploy ln -sfn /opt/nado/releases/<релиз> /opt/nado/current
sudo systemctl restart nado
```

Изменили `/etc/nado/nado.env` — `sudo systemctl restart nado`.

Перезапуск занимает несколько секунд (пауза дренирования + старт), в это
время nginx отвечает 502. Для одного инстанса это ожидаемо.
