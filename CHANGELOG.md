# Changelog

## v0.3.0

- Флаг «Мимо туннеля» у правил и URL источников: трафик по ним sing-box не перехватывает.
- IP и CIDR исключений отсекаются в nftables через `route_exclude_address_set`, домены — правилом `bypass` в pre-match.
- Миграция добавляет в конфиг sing-box rule-set `configurer-bypass`; изменения вступят в силу после перезапуска sing-box.
- Имя группы `bypass` зарезервировано.
