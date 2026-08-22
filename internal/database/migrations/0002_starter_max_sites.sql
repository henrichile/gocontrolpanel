-- Cada cuenta debe alojar un solo sitio salvo que su plan lo permita
-- explícitamente. El plan "Starter" se sembró con max_sites=3; se baja a 1.
-- Solo se toca si sigue en el valor original, para no pisar un ajuste
-- manual que haya hecho el administrador.
UPDATE plans
SET max_sites = 1
WHERE name = 'Starter' AND is_default = TRUE AND max_sites = 3;
