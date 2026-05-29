-- Гео-факты сервера + кэш публичного IP, который видит сам удалённый хост.
-- remote_public_ip заполняется при /api/servers/remote/connect: на сервере
-- выполняется best-effort `curl https://api.ipify.org` (с fallback на dig).
-- country_code / country проставляются по этому IP через встроенную базу
-- DB-IP Lite в пакете internal/geo — без внешних сетевых вызовов из API.
ALTER TABLE servers
    ADD COLUMN remote_public_ip VARCHAR(45),     -- влезает и IPv6 (39 симв.), и IPv4
    ADD COLUMN country_code     CHAR(2),         -- ISO 3166-1 alpha-2, например 'RU'
    ADD COLUMN country          VARCHAR(64);     -- английское имя из mmdb, например 'Russia'
