ALTER TABLE usdt_wallets ADD COLUMN invoice_code_offset integer NOT NULL DEFAULT 0
    CHECK (invoice_code_offset IN (0, 10000));

-- Keep historical invoices valid, including fractional quotes from legacy rate mode.
ALTER TABLE usdt_invoices DROP CONSTRAINT usdt_invoices_check3,
    ADD CONSTRAINT usdt_invoice_max_adjustment CHECK (amount_units - quote_units <= 20000);

CREATE FUNCTION enforce_usdt_invoice_window() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    offset_units integer;
BEGIN
    SELECT invoice_code_offset INTO STRICT offset_units FROM usdt_wallets
        WHERE address = NEW.address FOR SHARE;
    IF NEW.amount_units - NEW.quote_units NOT BETWEEN offset_units + 1 AND offset_units + 10000
       OR (offset_units <> 0 AND NEW.quote_units % 1000000 <> 0) THEN
        RAISE EXCEPTION 'USDT invoice outside the active wallet window' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

-- An old application binary must also fail closed on a recovered wallet.
CREATE TRIGGER usdt_invoice_window BEFORE INSERT OR UPDATE OF address, amount_units, quote_units
    ON usdt_invoices FOR EACH ROW EXECUTE FUNCTION enforce_usdt_invoice_window();
