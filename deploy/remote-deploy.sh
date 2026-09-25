#!/usr/bin/env bash
# Активирует релиз на сервере. Запускается из GitHub Actions по SSH от
# пользователя deploy после загрузки файлов в <APP_ROOT>/releases/<релиз>/.
#
#   bash /opt/nado/releases/<релиз>/remote-deploy.sh <ожидаемая-версия>
#
# Шаги: переключить симлинк current → перезапустить сервис → дождаться
# /healthz с нужной версией. Не дождались — откат на предыдущий релиз и
# exit 1, чтобы job в Actions упал.
set -euo pipefail

EXPECTED_VERSION="${1:-}"
SERVICE="${SERVICE:-nado}"
HEALTH_URL="${HEALTH_URL:-http://127.0.0.1:8080/healthz}"
HEALTH_TIMEOUT="${HEALTH_TIMEOUT:-40}" # секунд
KEEP_RELEASES="${KEEP_RELEASES:-5}"

# Скрипт лежит внутри релиза, поэтому корень приложения — на два уровня выше.
NEW_RELEASE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
RELEASES_DIR="$(dirname "$NEW_RELEASE")"
APP_ROOT="$(dirname "$RELEASES_DIR")"
CURRENT_LINK="$APP_ROOT/current"

log() { printf '[deploy] %s\n' "$*"; }

# Атомарная замена симлинка: mv -T — это rename(2), в отличие от ln -sfn,
# который удаляет и создаёт ссылку заново, оставляя окно без current.
switch_to() {
	ln -sfn "$1" "$APP_ROOT/.current.tmp"
	mv -Tf "$APP_ROOT/.current.tmp" "$CURRENT_LINK"
}

restart_service() {
	sudo -n /usr/bin/systemctl restart "$SERVICE"
}

# Ждём ответа /healthz. Сверяем версию: иначе можно принять ответ старого
# процесса или ответ от чужого приложения на том же порту.
wait_healthy() {
	local want="$1" body
	for ((i = 0; i < HEALTH_TIMEOUT; i++)); do
		if body="$(curl -fsS --max-time 2 "$HEALTH_URL" 2>/dev/null)"; then
			if [[ -z "$want" || "$body" == *"\"version\":\"$want\""* ]]; then
				return 0
			fi
		fi
		sleep 1
	done
	return 1
}

show_logs() {
	sudo -n /usr/bin/journalctl -u "$SERVICE" -n 80 --no-pager || true
}

cleanup_old_releases() {
	local current
	current="$(readlink -e "$CURRENT_LINK" || true)"
	# Имена релизов начинаются с UTC-времени, поэтому сортировка по имени = по дате.
	find "$RELEASES_DIR" -mindepth 1 -maxdepth 1 -type d -printf '%f\n' |
		sort -r |
		tail -n +"$((KEEP_RELEASES + 1))" |
		while read -r name; do
			[[ "$RELEASES_DIR/$name" == "$current" ]] && continue
			log "удаляю старый релиз $name"
			rm -rf -- "${RELEASES_DIR:?}/$name"
		done
}

[[ -x "$NEW_RELEASE/nado" ]] || chmod 755 "$NEW_RELEASE/nado"

PREVIOUS_RELEASE="$(readlink -e "$CURRENT_LINK" || true)"

log "активирую $(basename "$NEW_RELEASE") (версия ${EXPECTED_VERSION:-?})"
switch_to "$NEW_RELEASE"
restart_service

if wait_healthy "$EXPECTED_VERSION"; then
	log "релиз работает: $(curl -fsS --max-time 2 "$HEALTH_URL")"
	cleanup_old_releases
	exit 0
fi

log "ОШИБКА: /healthz не ответил за ${HEALTH_TIMEOUT}s, журнал сервиса:"
show_logs

if [[ -n "$PREVIOUS_RELEASE" && "$PREVIOUS_RELEASE" != "$NEW_RELEASE" ]]; then
	log "откат на $(basename "$PREVIOUS_RELEASE")"
	switch_to "$PREVIOUS_RELEASE"
	restart_service
	if wait_healthy ""; then
		log "откат выполнен, работает предыдущий релиз"
	else
		log "ОШИБКА: предыдущий релиз тоже не поднялся — нужна ручная проверка"
		show_logs
	fi
else
	log "предыдущего релиза нет — откатываться некуда"
fi

rm -rf -- "$NEW_RELEASE"
exit 1
