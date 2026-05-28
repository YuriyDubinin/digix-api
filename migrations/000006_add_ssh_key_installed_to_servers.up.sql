-- Флаг: установлен ли наш SSH-ключ приложения в authorized_keys этого сервера.
-- Заполняется отдельным методом install-key (бутстрап по паролю → добавление
-- публичного ключа на удалённый сервер). По умолчанию FALSE — ключа ещё нет.
ALTER TABLE servers
    ADD COLUMN ssh_key_installed BOOLEAN NOT NULL DEFAULT FALSE;
