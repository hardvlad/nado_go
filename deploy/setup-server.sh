#!/usr/bin/env bash
# Одноразовая подготовка сервера Ubuntu (с Plesk или без) к приёму деплоев.
# Повторный запуск безопасен: существующие пользователи, ключи и
# /etc/nado/nado.env не перезаписываются.
#
#   sudo bash deploy/setup-server.sh "ssh-ed25519 AAAA... github-actions-nado"
#
# Что делает:
#   * пользователь nado   — от него работает приложение, без shell и пароля;
#   * пользователь deploy — под ним заходит GitHub Actions (только по ключу);
#   * /opt/nado/releases  — каталог релизов, /opt/nado/current — активный;
#   * /etc/nado/nado.env  — секреты и конфигурация (заполнить вручную);
#   * systemd-сервис nado и sudo-правило для deploy: только его перезапуск.
set -euo pipefail

APP_USER=nado
DEPLOY_USER=deploy
SERVICE=nado
APP_ROOT=/opt/nado
CONF_DIR=/etc/nado

DEPLOY_PUBKEY="${1:-}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

log() { printf '\n==> %s\n' "$*"; }
die() { printf 'ошибка: %s\n' "$*" >&2; exit 1; }

[[ $EUID -eq 0 ]] || die "запускать от root (sudo)"
[[ -n "$DEPLOY_PUBKEY" ]] || die "передайте публичный SSH-ключ деплоя первым аргументом"
[[ "$DEPLOY_PUBKEY" == ssh-* || "$DEPLOY_PUBKEY" == ecdsa-* ]] || die "это не похоже на публичный SSH-ключ"
[[ -f "$SCRIPT_DIR/systemd/$SERVICE.service" ]] || die "не найден $SCRIPT_DIR/systemd/$SERVICE.service"
command -v systemctl >/dev/null || die "нужен systemd"

log "пакеты"
command -v curl >/dev/null || { apt-get update -q && apt-get install -y -q curl; }

log "пользователь приложения: $APP_USER"
if ! id -u "$APP_USER" >/dev/null 2>&1; then
	useradd --system --user-group --no-create-home \
		--home-dir /nonexistent --shell /usr/sbin/nologin "$APP_USER"
fi

log "пользователь деплоя: $DEPLOY_USER"
if ! id -u "$DEPLOY_USER" >/dev/null 2>&1; then
	useradd --create-home --shell /bin/bash "$DEPLOY_USER"
fi
# Пароля нет — вход только по ключу.
passwd -l "$DEPLOY_USER" >/dev/null

DEPLOY_HOME="$(getent passwd "$DEPLOY_USER" | cut -d: -f6)"
install -d -m 700 -o "$DEPLOY_USER" -g "$DEPLOY_USER" "$DEPLOY_HOME/.ssh"
AUTH_KEYS="$DEPLOY_HOME/.ssh/authorized_keys"
touch "$AUTH_KEYS"
# restrict: без проброса портов, агента и pty — ключ годится только для
# выполнения команд и scp/sftp.
if ! grep -qF "$DEPLOY_PUBKEY" "$AUTH_KEYS"; then
	echo "restrict $DEPLOY_PUBKEY" >>"$AUTH_KEYS"
fi
chown "$DEPLOY_USER:$DEPLOY_USER" "$AUTH_KEYS"
chmod 600 "$AUTH_KEYS"

log "каталоги $APP_ROOT"
install -d -m 755 -o "$DEPLOY_USER" -g "$DEPLOY_USER" "$APP_ROOT" "$APP_ROOT/releases"

log "конфигурация $CONF_DIR/$SERVICE.env"
install -d -m 750 -o root -g "$APP_USER" "$CONF_DIR"
if [[ ! -f "$CONF_DIR/$SERVICE.env" ]]; then
	install -m 640 -o root -g "$APP_USER" "$SCRIPT_DIR/nado.env.example" "$CONF_DIR/$SERVICE.env"
	ENV_CREATED=1
fi

log "systemd-сервис $SERVICE"
install -m 644 "$SCRIPT_DIR/systemd/$SERVICE.service" "/etc/systemd/system/$SERVICE.service"
systemctl daemon-reload
systemctl enable "$SERVICE" >/dev/null

log "sudo-правило для $DEPLOY_USER"
SUDOERS_TMP="$(mktemp)"
# Точные команды без масок: иначе через аргументы можно выйти за пределы
# разрешённого (например, journalctl с пейджером запускает shell).
cat >"$SUDOERS_TMP" <<EOF
$DEPLOY_USER ALL=(root) NOPASSWD: /usr/bin/systemctl restart $SERVICE
$DEPLOY_USER ALL=(root) NOPASSWD: /usr/bin/journalctl -u $SERVICE -n 80 --no-pager
EOF
visudo -cf "$SUDOERS_TMP" >/dev/null || die "sudoers не прошёл проверку"
install -m 440 -o root -g root "$SUDOERS_TMP" "/etc/sudoers.d/$SERVICE-deploy"
rm -f "$SUDOERS_TMP"

log "готово"
if [[ "${ENV_CREATED:-0}" == 1 ]]; then
	echo "  1. Заполните $CONF_DIR/$SERVICE.env (доступ к БД, HTTP_ALLOWED_ORIGINS)."
else
	echo "  1. $CONF_DIR/$SERVICE.env уже существовал — оставлен без изменений."
fi
cat <<EOF
  2. Настройте домен в Plesk по deploy/README.md (nginx → 127.0.0.1:8080).
  3. Добавьте секреты в GitHub и сделайте push в master.

Ключ хоста для секрета DEPLOY_KNOWN_HOSTS сверяйте с отпечатками:
$(for f in /etc/ssh/ssh_host_*_key.pub; do ssh-keygen -lf "$f"; done)
EOF
