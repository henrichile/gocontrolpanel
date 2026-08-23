-- Los planes no tenían límite de dominios: un cliente podía añadir dominios
-- adicionales sin tope alguno, sin importar lo que dijera el plan.
ALTER TABLE plans ADD COLUMN max_domains INT NOT NULL DEFAULT 5;
