# wire_bot — Telegram-бот для управления WireGuard

Управляет WireGuard-сервером, развёрнутым скриптом
[Nyr/wireguard-install](https://github.com/Nyr/wireguard-install).

## Требования

Сервер с уже работающим WireGuard, установленным скриптом
[Nyr/wireguard-install](https://github.com/Nyr/wireguard-install):

```bash
wget https://git.io/wireguard -O wireguard-install.sh && bash wireguard-install.sh
```

wgbot работает с тем же `/etc/wireguard/wg0.conf` и полностью совместим с
форматом скрипта: клиенты, добавленные скриптом, видны боту, и наоборот.
Оба инструмента можно использовать interchangeably.

## Возможности

- **`/add`** — добавление клиента в диалоге, как в скрипте: имя → выбор DNS
  (те же 8 вариантов, включая системные и свои) → конфиг-файл + QR-код.
  Применяется без перезапуска WireGuard (`wg addconf`).
- **`/list`** — список клиентов: статус (активен/заблокирован), IP.
  Меню клиента: блокировка/разблокировка, удаление (с подтверждением),
  статистика.
- **Блокировка** — peer убирается из живого интерфейса (`wg set ... remove`),
  блок в wg0.conf комментируется, адрес остаётся зарезервированным.
- **`/stats`** — графики RX/TX и активности клиента за 24ч / 7д / 30д
  (данные из VictoriaMetrics, `increase(...)`).
- **Суточный саммари** — раз в день (по умолчанию 09:00) в заданный чат
  отправляется график трафика по всем клиентам + текстовая сводка
  (RX/TX/всего, кто был активен). `/summary` — то же самое по запросу.
- Whitelist админов по Telegram user ID; бот игнорирует всех остальных.

## Установка (одна команда, на VPN-сервере с уже установленным WireGuard)

```bash
bash wgbot-install.sh
```

Установщик (в стиле референсного скрипта):
1. Проверяет окружение (root, ОС, наличие `wg` и `/etc/wireguard/wg0.conf`).
2. Спрашивает: токен бота (у [@BotFather](https://t.me/BotFather)),
   ID админов (у [@userinfobot](https://t.me/userinfobot)), чат и время
   суточного саммари, порт и retention VictoriaMetrics.
3. Скачивает и устанавливает **VictoriaMetrics** single-node
   (только 127.0.0.1, systemd-юнит).
4. Собирает **wgbot** (Go; при необходимости ставит golang из репозитория ОС).
5. Пишет `/etc/wgbot/wgbot.env` (chmod 600) и systemd-юниты, запускает всё.

Повторный запуск — меню: перенастройка, перезапуск, пересборка, удаление
(WireGuard при удалении не затрагивается).

## Архитектура

```
Telegram ──> wgbot (Go, /usr/local/bin/wgbot)
              ├── /etc/wireguard/wg0.conf  (формат Nyr: # BEGIN_PEER/END_PEER)
              ├── /etc/wireguard/clients/  (клиентские .conf, 0600)
              ├── wg show wg0 dump ──(раз в 30с)──> VictoriaMetrics (127.0.0.1:8428)
              └── графики: go-chart, QR: go-qrcode
```

Метрики: `wg_client_transfer_bytes_total{client,dir}`,
`wg_client_last_handshake_seconds{client}` (Prometheus-формы).

## Структура

- `wgbot-install.sh` — установщик в одну команду
- `wgbot/` — исходники Go-бота (`go build ./...`, `go test ./...`)
- `wgbot/deploy/` — systemd-юниты и пример env-файла
- Референс: [Nyr/wireguard-install](https://github.com/Nyr/wireguard-install) — обязательный prerequisite
